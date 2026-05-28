from __future__ import annotations

import csv
from datetime import UTC, datetime
from pathlib import Path

from .models import MarketSnapshot


ALIASES = {
    "name": ("market_hash_name", "hash_name", "item_name", "name", "skin", "skin_name"),
    "time": ("captured_at", "timestamp", "time", "date", "created_at", "snapshot_at"),
    "price": ("price", "lowest_price", "min_price", "median_price", "mean_price", "avg_price"),
    "quantity": ("quantity", "listings", "listing_count", "volume", "sales_volume"),
    "currency": ("currency",),
    "min_price": ("min_price", "lowest_price"),
    "max_price": ("max_price", "highest_price"),
    "mean_price": ("mean_price", "avg_price", "average_price"),
    "median_price": ("median_price",),
}


def load_historical_csv(
    path: Path,
    *,
    source: str = "historical.csv",
    currency: str = "USD",
    name_column: str | None = None,
    time_column: str | None = None,
    price_column: str | None = None,
    quantity_column: str | None = None,
) -> list[MarketSnapshot]:
    with path.open(newline="", encoding="utf-8-sig") as f:
        reader = csv.DictReader(f)
        if not reader.fieldnames:
            raise ValueError(f"{path} has no header row")

        columns = {
            "name": name_column or _infer_column(reader.fieldnames, ALIASES["name"]),
            "time": time_column or _infer_column(reader.fieldnames, ALIASES["time"]),
            "price": price_column or _infer_column(reader.fieldnames, ALIASES["price"]),
            "quantity": quantity_column or _infer_column(reader.fieldnames, ALIASES["quantity"], required=False),
            "currency": _infer_column(reader.fieldnames, ALIASES["currency"], required=False),
            "min_price": _infer_column(reader.fieldnames, ALIASES["min_price"], required=False),
            "max_price": _infer_column(reader.fieldnames, ALIASES["max_price"], required=False),
            "mean_price": _infer_column(reader.fieldnames, ALIASES["mean_price"], required=False),
            "median_price": _infer_column(reader.fieldnames, ALIASES["median_price"], required=False),
        }
        missing = [key for key in ("name", "time", "price") if not columns[key]]
        if missing:
            raise ValueError(f"{path} is missing required columns: {', '.join(missing)}")
        unknown = [column for column in columns.values() if column and column not in reader.fieldnames]
        if unknown:
            raise ValueError(f"{path} does not contain columns: {', '.join(unknown)}")

        snapshots: list[MarketSnapshot] = []
        for row_number, row in enumerate(reader, start=2):
            name = _clean(row.get(columns["name"] or ""))
            if not name:
                continue
            price = _float(row.get(columns["price"] or ""))
            if price is None or price <= 0:
                raise ValueError(f"{path}:{row_number} has invalid price")
            raw_currency = _clean(row.get(columns["currency"] or "")) if columns["currency"] else ""
            snapshots.append(
                MarketSnapshot(
                    source=source,
                    market_hash_name=name,
                    price=price,
                    currency=raw_currency or currency,
                    quantity=_int(row.get(columns["quantity"] or "")) if columns["quantity"] else None,
                    min_price=_float(row.get(columns["min_price"] or "")) if columns["min_price"] else None,
                    max_price=_float(row.get(columns["max_price"] or "")) if columns["max_price"] else None,
                    mean_price=_float(row.get(columns["mean_price"] or "")) if columns["mean_price"] else None,
                    median_price=_float(row.get(columns["median_price"] or "")) if columns["median_price"] else None,
                    captured_at=_parse_time(_clean(row.get(columns["time"] or ""))),
                    raw={key: value for key, value in row.items() if value not in ("", None)},
                )
            )
    return snapshots


def _infer_column(fieldnames: list[str], aliases: tuple[str, ...], *, required: bool = True) -> str | None:
    by_lower = {name.lower(): name for name in fieldnames}
    for alias in aliases:
        if alias.lower() in by_lower:
            return by_lower[alias.lower()]
    if required:
        return None
    return None


def _parse_time(value: str) -> str:
    if not value:
        raise ValueError("timestamp is empty")
    if value.replace(".", "", 1).isdigit():
        stamp = float(value)
        if stamp > 10_000_000_000:
            stamp /= 1000.0
        return _iso(datetime.fromtimestamp(stamp, UTC))
    normalized = value.replace("Z", "+00:00")
    try:
        parsed = datetime.fromisoformat(normalized)
    except ValueError:
        parsed = datetime.strptime(value, "%Y-%m-%d").replace(tzinfo=UTC)
    if parsed.tzinfo is None:
        parsed = parsed.replace(tzinfo=UTC)
    return _iso(parsed.astimezone(UTC))


def _iso(value: datetime) -> str:
    return value.replace(microsecond=0).isoformat().replace("+00:00", "Z")


def _clean(value: object) -> str:
    return str(value or "").strip()


def _float(value: object) -> float | None:
    text = _clean(value).replace(",", "")
    if not text:
        return None
    return float(text)


def _int(value: object) -> int | None:
    parsed = _float(value)
    return int(parsed) if parsed is not None else None
