from __future__ import annotations

import csv
from collections import defaultdict
from dataclasses import dataclass
from pathlib import Path

from .features import FeatureRow, NUMERIC_FEATURES
from .models import MarketSnapshot, parse_time


@dataclass(frozen=True)
class LabeledRow:
    feature_row: FeatureRow
    future_price: float
    gross_return: float
    net_return: float
    label: int


def attach_labels(
    feature_rows: list[FeatureRow],
    snapshots: list[MarketSnapshot],
    *,
    horizon_days: int = 30,
    target_return: float = 0.08,
    fee_rate: float = 0.12,
) -> list[LabeledRow]:
    grouped: dict[str, list[MarketSnapshot]] = defaultdict(list)
    for snapshot in snapshots:
        if snapshot.price is not None and snapshot.price > 0:
            grouped[snapshot.market_hash_name].append(snapshot)
    for name in grouped:
        grouped[name].sort(key=lambda s: s.captured_at)

    labeled: list[LabeledRow] = []
    for row in feature_rows:
        future = _future_snapshot(grouped.get(row.market_hash_name, []), row.as_of, horizon_days)
        if future is None or future.price is None:
            continue
        gross_return = (future.price - row.price) / row.price
        net_return = (future.price * (1.0 - fee_rate) / row.price) - 1.0
        labeled.append(
            LabeledRow(
                feature_row=row,
                future_price=future.price,
                gross_return=gross_return,
                net_return=net_return,
                label=1 if net_return >= target_return else 0,
            )
        )
    return labeled


def write_labels_csv(rows: list[LabeledRow], path: Path) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    fieldnames = [
        "market_hash_name",
        "as_of",
        "category",
        "wear",
        "source",
        "future_price",
        "gross_return",
        "net_return",
        "label",
        *NUMERIC_FEATURES,
    ]
    with path.open("w", newline="", encoding="utf-8") as f:
        writer = csv.DictWriter(f, fieldnames=fieldnames)
        writer.writeheader()
        for row in rows:
            record = {
                "market_hash_name": row.feature_row.market_hash_name,
                "as_of": row.feature_row.as_of,
                "category": row.feature_row.category,
                "wear": row.feature_row.wear,
                "source": row.feature_row.source,
                "future_price": row.future_price,
                "gross_return": row.gross_return,
                "net_return": row.net_return,
                "label": row.label,
            }
            record.update({name: row.feature_row.features.get(name, 0.0) for name in NUMERIC_FEATURES})
            writer.writerow(record)


def read_labels_csv(path: Path) -> list[LabeledRow]:
    rows: list[LabeledRow] = []
    with path.open(newline="", encoding="utf-8") as f:
        reader = csv.DictReader(f)
        for record in reader:
            features = {name: float(record.get(name) or 0.0) for name in NUMERIC_FEATURES}
            feature_row = FeatureRow(
                market_hash_name=record["market_hash_name"],
                as_of=record["as_of"],
                category=record.get("category", ""),
                wear=record.get("wear", ""),
                source=record.get("source", ""),
                price=features["price"],
                features=features,
            )
            rows.append(
                LabeledRow(
                    feature_row=feature_row,
                    future_price=float(record["future_price"]),
                    gross_return=float(record["gross_return"]),
                    net_return=float(record["net_return"]),
                    label=int(record["label"]),
                )
            )
    return rows


def _future_snapshot(snapshots: list[MarketSnapshot], as_of: str, horizon_days: int) -> MarketSnapshot | None:
    current = parse_time(as_of)
    candidates = [s for s in snapshots if (parse_time(s.captured_at) - current).days >= horizon_days]
    return candidates[0] if candidates else None
