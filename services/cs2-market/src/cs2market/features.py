from __future__ import annotations

import csv
import math
from collections import defaultdict
from dataclasses import dataclass
from pathlib import Path

from .models import MarketSnapshot, parse_time
from .normalize import infer_item


NUMERIC_FEATURES = [
    "price",
    "log_price",
    "quantity",
    "liquidity_score",
    "spread_ratio",
    "momentum_1d",
    "momentum_7d",
    "momentum_14d",
    "momentum_30d",
    "volatility_14d",
    "history_points",
    "history_span_days",
    "category_skin",
    "category_case",
    "category_sticker",
    "category_capsule",
    "category_agent",
    "category_knife",
    "category_gloves",
]


@dataclass(frozen=True)
class FeatureRow:
    market_hash_name: str
    as_of: str
    category: str
    wear: str
    source: str
    price: float
    features: dict[str, float]


def build_feature_rows(
    snapshots: list[MarketSnapshot],
    *,
    categories: tuple[str, ...] = (),
    min_price: float = 0.0,
    max_price: float = 1_000_000.0,
) -> list[FeatureRow]:
    grouped: dict[str, list[MarketSnapshot]] = defaultdict(list)
    for snapshot in snapshots:
        if snapshot.price is not None and snapshot.price > 0:
            grouped[snapshot.market_hash_name].append(snapshot)

    rows: list[FeatureRow] = []
    for name, item_snapshots in grouped.items():
        ordered = sorted(item_snapshots, key=lambda s: s.captured_at)
        for index, snapshot in enumerate(ordered):
            if snapshot.price is None or not (min_price <= snapshot.price <= max_price):
                continue
            item = infer_item(name, snapshot.raw)
            if categories and item.category not in categories:
                continue
            history = ordered[: index + 1]
            features = _features_for(snapshot, history, item.category)
            rows.append(
                FeatureRow(
                    market_hash_name=name,
                    as_of=snapshot.captured_at,
                    category=item.category,
                    wear=item.wear,
                    source=snapshot.source,
                    price=snapshot.price,
                    features=features,
                )
            )
    return sorted(rows, key=lambda r: (r.as_of, r.market_hash_name))


def write_features_csv(rows: list[FeatureRow], path: Path) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", newline="", encoding="utf-8") as f:
        writer = csv.DictWriter(f, fieldnames=["market_hash_name", "as_of", "category", "wear", "source", *NUMERIC_FEATURES])
        writer.writeheader()
        for row in rows:
            record = {
                "market_hash_name": row.market_hash_name,
                "as_of": row.as_of,
                "category": row.category,
                "wear": row.wear,
                "source": row.source,
            }
            record.update({name: row.features.get(name, 0.0) for name in NUMERIC_FEATURES})
            writer.writerow(record)


def read_features_csv(path: Path) -> list[FeatureRow]:
    rows: list[FeatureRow] = []
    with path.open(newline="", encoding="utf-8") as f:
        reader = csv.DictReader(f)
        for record in reader:
            features = {name: float(record.get(name) or 0.0) for name in NUMERIC_FEATURES}
            rows.append(
                FeatureRow(
                    market_hash_name=record["market_hash_name"],
                    as_of=record["as_of"],
                    category=record.get("category", ""),
                    wear=record.get("wear", ""),
                    source=record.get("source", ""),
                    price=float(record.get("price") or features["price"]),
                    features=features,
                )
            )
    return rows


def _features_for(snapshot: MarketSnapshot, history: list[MarketSnapshot], category: str) -> dict[str, float]:
    price = snapshot.price or 0.0
    first_time = parse_time(history[0].captured_at)
    current_time = parse_time(snapshot.captured_at)
    features = {
        "price": price,
        "log_price": math.log(max(price, 0.01)),
        "quantity": float(snapshot.quantity or 0),
        "liquidity_score": _clamp((snapshot.quantity or 0) / 50.0, 0.0, 1.0),
        "spread_ratio": _spread_ratio(snapshot),
        "momentum_1d": _momentum(history, days=1),
        "momentum_7d": _momentum(history, days=7),
        "momentum_14d": _momentum(history, days=14),
        "momentum_30d": _momentum(history, days=30),
        "volatility_14d": _volatility(history, days=14),
        "history_points": float(len(history)),
        "history_span_days": max((current_time - first_time).days, 0),
    }
    for key in ("skin", "case", "sticker", "capsule", "agent", "knife", "gloves"):
        features[f"category_{key}"] = 1.0 if category == key else 0.0
    return features


def _momentum(history: list[MarketSnapshot], *, days: int) -> float:
    if len(history) < 2 or history[-1].price is None:
        return 0.0
    current = history[-1]
    current_time = parse_time(current.captured_at)
    candidates = [s for s in history[:-1] if (current_time - parse_time(s.captured_at)).days >= days and s.price]
    baseline = candidates[-1] if candidates else history[0]
    if not baseline.price or baseline.price <= 0:
        return 0.0
    return (current.price - baseline.price) / baseline.price


def _volatility(history: list[MarketSnapshot], *, days: int) -> float:
    if len(history) < 3:
        return 0.0
    current_time = parse_time(history[-1].captured_at)
    recent = [s for s in history if (current_time - parse_time(s.captured_at)).days <= days and s.price]
    returns = []
    for prev, cur in zip(recent, recent[1:]):
        if prev.price and cur.price:
            returns.append((cur.price - prev.price) / prev.price)
    if len(returns) < 2:
        return 0.0
    mean = sum(returns) / len(returns)
    variance = sum((r - mean) ** 2 for r in returns) / len(returns)
    return variance**0.5


def _spread_ratio(snapshot: MarketSnapshot) -> float:
    mid = snapshot.median_price or snapshot.mean_price or snapshot.price
    if not mid or mid <= 0 or not snapshot.min_price:
        return 0.0
    return abs(mid - snapshot.min_price) / mid


def _clamp(value: float, low: float, high: float) -> float:
    return min(max(value, low), high)
