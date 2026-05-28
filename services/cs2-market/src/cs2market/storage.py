from __future__ import annotations

import json
import sqlite3
from collections.abc import Iterable
from pathlib import Path
from urllib.parse import urlparse

from .models import MarketItem, MarketSnapshot, Signal, isoformat
from .normalize import infer_item


class Store:
    def init_schema(self) -> None:
        raise NotImplementedError

    def start_run(self, source: str) -> int:
        raise NotImplementedError

    def finish_run(self, run_id: int, status: str, warnings: list[str] | None = None) -> None:
        raise NotImplementedError

    def put_snapshots(self, source: str, snapshots: Iterable[MarketSnapshot]) -> int:
        raise NotImplementedError

    def snapshots_for_scoring(self) -> list[MarketSnapshot]:
        raise NotImplementedError

    def replace_signals(self, signals: Iterable[Signal]) -> list[Signal]:
        raise NotImplementedError

    def list_signals(self, limit: int = 20, min_confidence: float = 0.0) -> list[Signal]:
        raise NotImplementedError

    def get_signal(self, signal_id: int) -> Signal | None:
        raise NotImplementedError

    def close(self) -> None:
        pass


class SQLiteStore(Store):
    def __init__(self, path: Path):
        path.parent.mkdir(parents=True, exist_ok=True)
        self.conn = sqlite3.connect(path)
        self.conn.row_factory = sqlite3.Row

    def init_schema(self) -> None:
        self.conn.executescript(
            """
            CREATE TABLE IF NOT EXISTS ingestion_runs (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                source TEXT NOT NULL,
                status TEXT NOT NULL,
                started_at TEXT NOT NULL,
                finished_at TEXT,
                warnings_json TEXT NOT NULL DEFAULT '[]'
            );
            CREATE TABLE IF NOT EXISTS market_items (
                market_hash_name TEXT PRIMARY KEY,
                category TEXT NOT NULL,
                wear TEXT NOT NULL,
                collection_name TEXT NOT NULL,
                source_refs_json TEXT NOT NULL
            );
            CREATE TABLE IF NOT EXISTS market_snapshots (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                source TEXT NOT NULL,
                market_hash_name TEXT NOT NULL REFERENCES market_items(market_hash_name),
                price REAL,
                currency TEXT NOT NULL,
                quantity INTEGER,
                min_price REAL,
                max_price REAL,
                mean_price REAL,
                median_price REAL,
                captured_at TEXT NOT NULL,
                raw_json TEXT NOT NULL
            );
            CREATE INDEX IF NOT EXISTS idx_market_snapshots_item_time
                ON market_snapshots(market_hash_name, captured_at);
            CREATE TABLE IF NOT EXISTS signals (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                market_hash_name TEXT NOT NULL,
                action TEXT NOT NULL,
                score REAL NOT NULL,
                confidence REAL NOT NULL,
                horizon TEXT NOT NULL,
                price REAL,
                currency TEXT NOT NULL,
                reasons_json TEXT NOT NULL,
                risks_json TEXT NOT NULL,
                generated_at TEXT NOT NULL
            );
            """
        )
        self.conn.commit()

    def start_run(self, source: str) -> int:
        cur = self.conn.execute(
            "INSERT INTO ingestion_runs (source, status, started_at) VALUES (?, ?, ?)",
            (source, "running", isoformat()),
        )
        self.conn.commit()
        return int(cur.lastrowid)

    def finish_run(self, run_id: int, status: str, warnings: list[str] | None = None) -> None:
        self.conn.execute(
            "UPDATE ingestion_runs SET status = ?, finished_at = ?, warnings_json = ? WHERE id = ?",
            (status, isoformat(), json.dumps(warnings or []), run_id),
        )
        self.conn.commit()

    def put_snapshots(self, source: str, snapshots: Iterable[MarketSnapshot]) -> int:
        count = 0
        with self.conn:
            for snapshot in snapshots:
                item = infer_item(snapshot.market_hash_name, snapshot.raw)
                self._upsert_item(item)
                self.conn.execute(
                    """
                    INSERT INTO market_snapshots (
                        source, market_hash_name, price, currency, quantity,
                        min_price, max_price, mean_price, median_price, captured_at, raw_json
                    ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                    """,
                    (
                        source,
                        snapshot.market_hash_name,
                        snapshot.price,
                        snapshot.currency,
                        snapshot.quantity,
                        snapshot.min_price,
                        snapshot.max_price,
                        snapshot.mean_price,
                        snapshot.median_price,
                        snapshot.captured_at,
                        json.dumps(snapshot.raw, sort_keys=True),
                    ),
                )
                count += 1
        return count

    def _upsert_item(self, item: MarketItem) -> None:
        self.conn.execute(
            """
            INSERT INTO market_items (
                market_hash_name, category, wear, collection_name, source_refs_json
            ) VALUES (?, ?, ?, ?, ?)
            ON CONFLICT(market_hash_name) DO UPDATE SET
                category = excluded.category,
                wear = excluded.wear,
                collection_name = excluded.collection_name,
                source_refs_json = excluded.source_refs_json
            """,
            (
                item.market_hash_name,
                item.category,
                item.wear,
                item.collection,
                json.dumps(item.source_refs, sort_keys=True),
            ),
        )

    def snapshots_for_scoring(self) -> list[MarketSnapshot]:
        rows = self.conn.execute(
            """
            SELECT source, market_hash_name, price, currency, quantity, min_price,
                   max_price, mean_price, median_price, captured_at, raw_json
            FROM market_snapshots
            ORDER BY market_hash_name, captured_at
            """
        ).fetchall()
        return [_snapshot_from_row(row) for row in rows]

    def replace_signals(self, signals: Iterable[Signal]) -> list[Signal]:
        saved: list[Signal] = []
        with self.conn:
            self.conn.execute("DELETE FROM signals")
            for signal in signals:
                cur = self.conn.execute(
                    """
                    INSERT INTO signals (
                        market_hash_name, action, score, confidence, horizon, price,
                        currency, reasons_json, risks_json, generated_at
                    ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                    """,
                    (
                        signal.market_hash_name,
                        signal.action,
                        signal.score,
                        signal.confidence,
                        signal.horizon,
                        signal.price,
                        signal.currency,
                        json.dumps(signal.reasons),
                        json.dumps(signal.risks),
                        signal.generated_at,
                    ),
                )
                saved.append(Signal(**{**signal.to_dict(), "id": int(cur.lastrowid)}))
        return saved

    def list_signals(self, limit: int = 20, min_confidence: float = 0.0) -> list[Signal]:
        rows = self.conn.execute(
            """
            SELECT * FROM signals
            WHERE confidence >= ?
            ORDER BY score DESC, confidence DESC, generated_at DESC
            LIMIT ?
            """,
            (min_confidence, limit),
        ).fetchall()
        return [_signal_from_row(row) for row in rows]

    def get_signal(self, signal_id: int) -> Signal | None:
        row = self.conn.execute("SELECT * FROM signals WHERE id = ?", (signal_id,)).fetchone()
        return _signal_from_row(row) if row else None

    def close(self) -> None:
        self.conn.close()


class PostgresStore(Store):
    def __init__(self, database_url: str):
        try:
            import psycopg
        except ImportError as err:
            raise RuntimeError("Postgres storage requires psycopg; run `python -m pip install -e services/cs2-market`") from err
        self.psycopg = psycopg
        self.conn = psycopg.connect(database_url)

    def init_schema(self) -> None:
        with self.conn.cursor() as cur:
            cur.execute(
                """
                CREATE TABLE IF NOT EXISTS cs2market_ingestion_runs (
                    id BIGSERIAL PRIMARY KEY,
                    source TEXT NOT NULL,
                    status TEXT NOT NULL,
                    started_at TIMESTAMPTZ NOT NULL,
                    finished_at TIMESTAMPTZ,
                    warnings_json JSONB NOT NULL DEFAULT '[]'::jsonb
                );
                CREATE TABLE IF NOT EXISTS cs2market_market_items (
                    market_hash_name TEXT PRIMARY KEY,
                    category TEXT NOT NULL,
                    wear TEXT NOT NULL,
                    collection_name TEXT NOT NULL,
                    source_refs_json JSONB NOT NULL
                );
                CREATE TABLE IF NOT EXISTS cs2market_market_snapshots (
                    id BIGSERIAL PRIMARY KEY,
                    source TEXT NOT NULL,
                    market_hash_name TEXT NOT NULL REFERENCES cs2market_market_items(market_hash_name),
                    price DOUBLE PRECISION,
                    currency TEXT NOT NULL,
                    quantity INTEGER,
                    min_price DOUBLE PRECISION,
                    max_price DOUBLE PRECISION,
                    mean_price DOUBLE PRECISION,
                    median_price DOUBLE PRECISION,
                    captured_at TIMESTAMPTZ NOT NULL,
                    raw_json JSONB NOT NULL
                );
                CREATE INDEX IF NOT EXISTS idx_cs2market_snapshots_item_time
                    ON cs2market_market_snapshots(market_hash_name, captured_at);
                CREATE TABLE IF NOT EXISTS cs2market_signals (
                    id BIGSERIAL PRIMARY KEY,
                    market_hash_name TEXT NOT NULL,
                    action TEXT NOT NULL,
                    score DOUBLE PRECISION NOT NULL,
                    confidence DOUBLE PRECISION NOT NULL,
                    horizon TEXT NOT NULL,
                    price DOUBLE PRECISION,
                    currency TEXT NOT NULL,
                    reasons_json JSONB NOT NULL,
                    risks_json JSONB NOT NULL,
                    generated_at TIMESTAMPTZ NOT NULL
                );
                """
            )
        self.conn.commit()

    def start_run(self, source: str) -> int:
        with self.conn.cursor() as cur:
            cur.execute(
                "INSERT INTO cs2market_ingestion_runs (source, status, started_at) VALUES (%s, %s, %s) RETURNING id",
                (source, "running", isoformat()),
            )
            run_id = cur.fetchone()[0]
        self.conn.commit()
        return int(run_id)

    def finish_run(self, run_id: int, status: str, warnings: list[str] | None = None) -> None:
        with self.conn.cursor() as cur:
            cur.execute(
                """
                UPDATE cs2market_ingestion_runs
                SET status = %s, finished_at = %s, warnings_json = %s::jsonb
                WHERE id = %s
                """,
                (status, isoformat(), json.dumps(warnings or []), run_id),
            )
        self.conn.commit()

    def put_snapshots(self, source: str, snapshots: Iterable[MarketSnapshot]) -> int:
        count = 0
        with self.conn.cursor() as cur:
            for snapshot in snapshots:
                item = infer_item(snapshot.market_hash_name, snapshot.raw)
                cur.execute(
                    """
                    INSERT INTO cs2market_market_items (
                        market_hash_name, category, wear, collection_name, source_refs_json
                    ) VALUES (%s, %s, %s, %s, %s::jsonb)
                    ON CONFLICT(market_hash_name) DO UPDATE SET
                        category = excluded.category,
                        wear = excluded.wear,
                        collection_name = excluded.collection_name,
                        source_refs_json = excluded.source_refs_json
                    """,
                    (
                        item.market_hash_name,
                        item.category,
                        item.wear,
                        item.collection,
                        json.dumps(item.source_refs, sort_keys=True),
                    ),
                )
                cur.execute(
                    """
                    INSERT INTO cs2market_market_snapshots (
                        source, market_hash_name, price, currency, quantity,
                        min_price, max_price, mean_price, median_price, captured_at, raw_json
                    ) VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s::jsonb)
                    """,
                    (
                        source,
                        snapshot.market_hash_name,
                        snapshot.price,
                        snapshot.currency,
                        snapshot.quantity,
                        snapshot.min_price,
                        snapshot.max_price,
                        snapshot.mean_price,
                        snapshot.median_price,
                        snapshot.captured_at,
                        json.dumps(snapshot.raw, sort_keys=True),
                    ),
                )
                count += 1
        self.conn.commit()
        return count

    def snapshots_for_scoring(self) -> list[MarketSnapshot]:
        with self.conn.cursor() as cur:
            cur.execute(
                """
                SELECT source, market_hash_name, price, currency, quantity, min_price,
                       max_price, mean_price, median_price, captured_at, raw_json
                FROM cs2market_market_snapshots
                ORDER BY market_hash_name, captured_at
                """
            )
            rows = cur.fetchall()
        return [_snapshot_from_seq(row) for row in rows]

    def replace_signals(self, signals: Iterable[Signal]) -> list[Signal]:
        saved: list[Signal] = []
        with self.conn.cursor() as cur:
            cur.execute("DELETE FROM cs2market_signals")
            for signal in signals:
                cur.execute(
                    """
                    INSERT INTO cs2market_signals (
                        market_hash_name, action, score, confidence, horizon, price,
                        currency, reasons_json, risks_json, generated_at
                    ) VALUES (%s, %s, %s, %s, %s, %s, %s, %s::jsonb, %s::jsonb, %s)
                    RETURNING id
                    """,
                    (
                        signal.market_hash_name,
                        signal.action,
                        signal.score,
                        signal.confidence,
                        signal.horizon,
                        signal.price,
                        signal.currency,
                        json.dumps(signal.reasons),
                        json.dumps(signal.risks),
                        signal.generated_at,
                    ),
                )
                saved.append(Signal(**{**signal.to_dict(), "id": int(cur.fetchone()[0])}))
        self.conn.commit()
        return saved

    def list_signals(self, limit: int = 20, min_confidence: float = 0.0) -> list[Signal]:
        with self.conn.cursor() as cur:
            cur.execute(
                """
                SELECT id, market_hash_name, action, score, confidence, horizon, price,
                       currency, reasons_json, risks_json, generated_at
                FROM cs2market_signals
                WHERE confidence >= %s
                ORDER BY score DESC, confidence DESC, generated_at DESC
                LIMIT %s
                """,
                (min_confidence, limit),
            )
            rows = cur.fetchall()
        return [_signal_from_seq(row) for row in rows]

    def get_signal(self, signal_id: int) -> Signal | None:
        with self.conn.cursor() as cur:
            cur.execute(
                """
                SELECT id, market_hash_name, action, score, confidence, horizon, price,
                       currency, reasons_json, risks_json, generated_at
                FROM cs2market_signals WHERE id = %s
                """,
                (signal_id,),
            )
            row = cur.fetchone()
        return _signal_from_seq(row) if row else None

    def close(self) -> None:
        self.conn.close()


def open_store(database_url: str) -> Store:
    if _looks_like_windows_path(database_url):
        return SQLiteStore(Path(database_url))
    parsed = urlparse(database_url)
    if parsed.scheme in ("", "sqlite"):
        if parsed.scheme == "sqlite":
            path = Path(parsed.path)
            if len(parsed.path) >= 4 and parsed.path[0] == "/" and parsed.path[2] == ":":
                path = Path(parsed.path[1:])
            if parsed.netloc:
                path = Path(f"//{parsed.netloc}{parsed.path}")
        else:
            path = Path(database_url)
        return SQLiteStore(path)
    if parsed.scheme in ("postgres", "postgresql"):
        return PostgresStore(database_url)
    raise ValueError(f"unsupported database url scheme: {parsed.scheme}")


def _looks_like_windows_path(value: str) -> bool:
    return len(value) >= 3 and value[1] == ":" and value[2] in ("\\", "/")


def _snapshot_from_row(row: sqlite3.Row) -> MarketSnapshot:
    return MarketSnapshot(
        source=row["source"],
        market_hash_name=row["market_hash_name"],
        price=row["price"],
        currency=row["currency"],
        quantity=row["quantity"],
        min_price=row["min_price"],
        max_price=row["max_price"],
        mean_price=row["mean_price"],
        median_price=row["median_price"],
        captured_at=row["captured_at"],
        raw=json.loads(row["raw_json"]),
    )


def _snapshot_from_seq(row: tuple) -> MarketSnapshot:
    raw = row[10]
    if isinstance(raw, str):
        raw = json.loads(raw)
    return MarketSnapshot(
        source=row[0],
        market_hash_name=row[1],
        price=row[2],
        currency=row[3],
        quantity=row[4],
        min_price=row[5],
        max_price=row[6],
        mean_price=row[7],
        median_price=row[8],
        captured_at=str(row[9]).replace("+00:00", "Z"),
        raw=raw,
    )


def _signal_from_row(row: sqlite3.Row) -> Signal:
    return Signal(
        id=row["id"],
        market_hash_name=row["market_hash_name"],
        action=row["action"],
        score=row["score"],
        confidence=row["confidence"],
        horizon=row["horizon"],
        price=row["price"],
        currency=row["currency"],
        reasons=json.loads(row["reasons_json"]),
        risks=json.loads(row["risks_json"]),
        generated_at=row["generated_at"],
    )


def _signal_from_seq(row: tuple) -> Signal:
    reasons = row[8]
    risks = row[9]
    return Signal(
        id=row[0],
        market_hash_name=row[1],
        action=row[2],
        score=row[3],
        confidence=row[4],
        horizon=row[5],
        price=row[6],
        currency=row[7],
        reasons=json.loads(reasons) if isinstance(reasons, str) else reasons,
        risks=json.loads(risks) if isinstance(risks, str) else risks,
        generated_at=str(row[10]).replace("+00:00", "Z"),
    )
