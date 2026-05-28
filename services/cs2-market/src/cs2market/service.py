from __future__ import annotations

import json
from pathlib import Path

from .config import Settings
from .firecrawl import FirecrawlCLI
from .historical import load_historical_csv
from .models import Signal
from .normalize import normalize_skinport_items, normalize_take_skin_history, normalize_take_skin_listing
from .scoring import ScoringConfig, score_snapshots
from .scripts import export_short_scripts
from .sources import SkinportClient, SteamImageClient, TakeSkinClient
from .storage import Store, open_store


class MarketService:
    def __init__(self, settings: Settings, store: Store | None = None):
        self.settings = settings
        self.store = store or open_store(settings.database_url)

    def init_db(self) -> None:
        self.store.init_schema()

    def ingest_skinport(self, *, currency: str = "USD", sample_file: Path | None = None) -> int:
        self.store.init_schema()
        run_id = self.store.start_run("skinport")
        try:
            if sample_file:
                items = json.loads(sample_file.read_text(encoding="utf-8"))
            else:
                items = SkinportClient(self.settings.skinport_base_url).items(currency=currency)
            snapshots = normalize_skinport_items(items)
            count = self.store.put_snapshots("skinport", snapshots)
            self.store.finish_run(run_id, "succeeded", [f"stored {count} snapshots"])
            return count
        except Exception as err:
            self.store.finish_run(run_id, "failed", [str(err)])
            raise

    def ingest_take_skin(
        self,
        *,
        pages: int = 1,
        limit: int = 100,
        history_days: int = 30,
        history_limit: int = 10,
    ) -> int:
        self.store.init_schema()
        run_id = self.store.start_run("take.skin")
        client = TakeSkinClient()
        warnings: list[str] = []
        try:
            snapshots = []
            names: list[str] = []
            for page in range(pages):
                payload = client.skins(page=page, limit=limit)
                rows = payload.get("data", [])
                if not isinstance(rows, list):
                    warnings.append(f"page {page} returned no skin list")
                    continue
                snapshots.extend(normalize_take_skin_listing(rows))
                names.extend(str(row.get("marketHashName", "")).strip() for row in rows if row.get("marketHashName"))
            for name in names[:history_limit]:
                try:
                    snapshots.extend(normalize_take_skin_history(client.price_history(name, days=history_days)))
                except Exception as err:
                    warnings.append(f"history {name}: {err}")
            count = self.store.put_snapshots("take.skin", snapshots)
            self.store.finish_run(run_id, "succeeded", [f"stored {count} snapshots", *warnings])
            return count
        except Exception as err:
            self.store.finish_run(run_id, "failed", [str(err), *warnings])
            raise

    def scrape_context(self, urls: list[str]) -> list[Path]:
        raw_dir = self.settings.data_dir / "raw" / "firecrawl"
        firecrawl = FirecrawlCLI(self.settings.firecrawl_command)
        written: list[Path] = []
        for index, url in enumerate(urls, start=1):
            out = raw_dir / f"context-{index:03d}.json"
            result = firecrawl.scrape(url, output=out)
            if not out.exists():
                out.write_text(json.dumps(result, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
            written.append(out)
        return written

    def ingest_historical_csv(
        self,
        path: Path,
        *,
        source: str = "historical.csv",
        currency: str = "USD",
        name_column: str | None = None,
        time_column: str | None = None,
        price_column: str | None = None,
        quantity_column: str | None = None,
    ) -> int:
        self.store.init_schema()
        run_id = self.store.start_run(source)
        try:
            snapshots = load_historical_csv(
                path,
                source=source,
                currency=currency,
                name_column=name_column,
                time_column=time_column,
                price_column=price_column,
                quantity_column=quantity_column,
            )
            count = self.store.put_snapshots(source, snapshots)
            self.store.finish_run(run_id, "succeeded", [f"stored {count} historical snapshots"])
            return count
        except Exception as err:
            self.store.finish_run(run_id, "failed", [str(err)])
            raise

    def refresh_signals(
        self,
        *,
        limit: int = 50,
        write: Path | None = None,
        categories: tuple[str, ...] = (),
    ) -> list[Signal]:
        self.store.init_schema()
        signals = score_snapshots(self.store.snapshots_for_scoring(), ScoringConfig(categories=categories))
        saved = self.store.replace_signals(signals[:limit])
        if write:
            write.parent.mkdir(parents=True, exist_ok=True)
            write.write_text(json.dumps([s.to_dict() for s in saved], indent=2) + "\n", encoding="utf-8")
        return saved

    def list_signals(self, *, limit: int = 20, min_confidence: float = 0.0) -> list[Signal]:
        self.store.init_schema()
        return self.store.list_signals(limit=limit, min_confidence=min_confidence)

    def download_signal_images(self, *, signals: list[Signal] | None = None, out_dir: Path | None = None) -> dict[str, Path]:
        signals = signals if signals is not None else self.list_signals(limit=20)
        image_dir = out_dir or self.settings.data_dir / "assets" / "skins"
        client = SteamImageClient()
        paths: dict[str, Path] = {}
        for signal in signals:
            try:
                paths[signal.market_hash_name] = client.download_item_image(signal.market_hash_name, image_dir)
            except Exception:
                continue
        return paths

    def export_shorts(
        self,
        *,
        signals: list[Signal] | None = None,
        out_dir: Path | None = None,
        include_images: bool = False,
    ) -> list[Path]:
        signals = signals if signals is not None else self.list_signals(limit=20)
        image_paths = self.download_signal_images(signals=signals) if include_images else {}
        return export_short_scripts(signals, out_dir or self.settings.output_dir, image_paths)

    def close(self) -> None:
        self.store.close()
