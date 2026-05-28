from __future__ import annotations

from collections import defaultdict
from dataclasses import dataclass

from .models import MarketSnapshot, Signal, parse_time
from .normalize import infer_item


@dataclass(frozen=True)
class ScoringConfig:
    min_ticket: float = 5.0
    max_ticket: float = 50.0
    min_quantity: int = 5
    horizon: str = "2-8 weeks"
    categories: tuple[str, ...] = ()


def score_snapshots(snapshots: list[MarketSnapshot], config: ScoringConfig | None = None) -> list[Signal]:
    config = config or ScoringConfig()
    grouped: dict[str, list[MarketSnapshot]] = defaultdict(list)
    for snapshot in snapshots:
        grouped[snapshot.market_hash_name].append(snapshot)

    signals: list[Signal] = []
    for name, item_snapshots in grouped.items():
        if config.categories and infer_item(name, item_snapshots[-1].raw).category not in config.categories:
            continue
        ordered = sorted(item_snapshots, key=lambda s: s.captured_at)
        latest = _latest_priced(ordered)
        if latest is None or latest.price is None:
            continue
        if not (config.min_ticket <= latest.price <= config.max_ticket):
            continue

        history = [s for s in ordered if s.price is not None and s.price > 0]
        momentum_7 = _momentum(history, days=7)
        momentum_30 = _momentum(history, days=30)
        liquidity = _liquidity_score(latest.quantity)
        spread_penalty = _spread_penalty(latest)
        volatility_penalty = _volatility_penalty(history)
        supply_bonus = _supply_bonus(history)

        score = 45.0
        score += liquidity * 18.0
        score += _clamp(momentum_7 * 170.0, -18.0, 18.0)
        score += _clamp(momentum_30 * 120.0, -16.0, 16.0)
        score += supply_bonus
        score -= spread_penalty
        score -= volatility_penalty
        score = round(_clamp(score, 0.0, 100.0), 2)

        confidence = _confidence(history, latest.quantity, spread_penalty)
        action = _action(score, confidence, latest.quantity, config)
        reasons = _reasons(latest, momentum_7, momentum_30, liquidity, supply_bonus)
        risks = _risks(latest, history, spread_penalty, volatility_penalty, config)
        signals.append(
            Signal(
                id=None,
                market_hash_name=name,
                action=action,
                score=score,
                confidence=confidence,
                horizon=config.horizon,
                price=latest.price,
                currency=latest.currency,
                reasons=reasons,
                risks=risks,
            )
        )

    return sorted(signals, key=lambda s: (s.score, s.confidence), reverse=True)


def _latest_priced(snapshots: list[MarketSnapshot]) -> MarketSnapshot | None:
    for snapshot in reversed(snapshots):
        if snapshot.price is not None and snapshot.price > 0:
            return snapshot
    return None


def _momentum(history: list[MarketSnapshot], *, days: int) -> float:
    if len(history) < 2:
        return 0.0
    latest = history[-1]
    latest_time = parse_time(latest.captured_at)
    candidates = [s for s in history[:-1] if (latest_time - parse_time(s.captured_at)).days >= days]
    baseline = candidates[-1] if candidates else history[0]
    if baseline.price is None or baseline.price <= 0 or latest.price is None:
        return 0.0
    return (latest.price - baseline.price) / baseline.price


def _liquidity_score(quantity: int | None) -> float:
    if quantity is None:
        return 0.25
    return _clamp(quantity / 50.0, 0.0, 1.0)


def _spread_penalty(snapshot: MarketSnapshot) -> float:
    if not snapshot.min_price:
        return 8.0
    mid = snapshot.median_price or snapshot.mean_price or snapshot.price
    if not mid or mid <= 0:
        return 10.0
    if snapshot.max_price and not snapshot.median_price and not snapshot.mean_price:
        spread = (snapshot.max_price - snapshot.min_price) / mid
    else:
        spread = abs(mid - snapshot.min_price) / mid
    return _clamp(spread * 40.0, 0.0, 22.0)


def _volatility_penalty(history: list[MarketSnapshot]) -> float:
    if len(history) < 4:
        return 4.0
    returns = []
    for prev, cur in zip(history, history[1:]):
        if prev.price and cur.price:
            returns.append((cur.price - prev.price) / prev.price)
    if len(returns) < 3:
        return 4.0
    mean = sum(returns) / len(returns)
    variance = sum((r - mean) ** 2 for r in returns) / len(returns)
    return _clamp((variance ** 0.5) * 90.0, 0.0, 18.0)


def _supply_bonus(history: list[MarketSnapshot]) -> float:
    if len(history) < 2:
        return 0.0
    first = next((s for s in history if s.quantity is not None), None)
    latest = next((s for s in reversed(history) if s.quantity is not None), None)
    if not first or not latest or not first.quantity or first.quantity <= 0:
        return 0.0
    change = (latest.quantity - first.quantity) / first.quantity
    if change < -0.1:
        return _clamp(abs(change) * 20.0, 0.0, 8.0)
    if change > 0.25:
        return -_clamp(change * 12.0, 0.0, 8.0)
    return 0.0


def _confidence(history: list[MarketSnapshot], quantity: int | None, spread_penalty: float) -> float:
    history_score = _clamp(len(history) / 12.0, 0.1, 1.0)
    liquidity_score = _liquidity_score(quantity)
    spread_score = 1.0 - _clamp(spread_penalty / 22.0, 0.0, 1.0)
    return round(_clamp((history_score * 0.4) + (liquidity_score * 0.4) + (spread_score * 0.2), 0.0, 1.0), 2)


def _action(score: float, confidence: float, quantity: int | None, config: ScoringConfig) -> str:
    if quantity is not None and quantity < config.min_quantity:
        return "avoid"
    if confidence < 0.35:
        return "watch" if score >= 65 else "avoid"
    if score >= 55 and confidence >= 0.6:
        return "buy"
    if score >= 70:
        return "buy"
    if score <= 35:
        return "sell"
    if score < 45:
        return "avoid"
    return "watch"


def _reasons(
    latest: MarketSnapshot,
    momentum_7: float,
    momentum_30: float,
    liquidity: float,
    supply_bonus: float,
) -> list[str]:
    reasons = [
        f"latest public price is {latest.price:.2f} {latest.currency}" if latest.price else "latest public price is unavailable",
        f"listed quantity signal is {latest.quantity}" if latest.quantity is not None else "listed quantity is missing",
    ]
    if momentum_7 > 0.03:
        reasons.append(f"7-day momentum is positive at {momentum_7:.1%}")
    elif momentum_7 < -0.03:
        reasons.append(f"7-day momentum is negative at {momentum_7:.1%}")
    if momentum_30 > 0.05:
        reasons.append(f"30-day trend is positive at {momentum_30:.1%}")
    elif momentum_30 < -0.05:
        reasons.append(f"30-day trend is negative at {momentum_30:.1%}")
    if liquidity >= 0.7:
        reasons.append("liquidity is strong for a small-ticket signal")
    if supply_bonus > 0:
        reasons.append("visible supply is tightening")
    return reasons


def _risks(
    latest: MarketSnapshot,
    history: list[MarketSnapshot],
    spread_penalty: float,
    volatility_penalty: float,
    config: ScoringConfig,
) -> list[str]:
    risks: list[str] = []
    if len(history) < 8:
        risks.append("limited history, confidence is lower until more snapshots accumulate")
    if latest.quantity is None or latest.quantity < config.min_quantity:
        risks.append("low listed quantity can make exits hard")
    if spread_penalty > 10:
        risks.append("wide min/max spread can erase expected edge")
    if volatility_penalty > 10:
        risks.append("recent price moves are volatile")
    risks.append("marketplace fees and stale listings are not fully modeled")
    return risks


def _clamp(value: float, low: float, high: float) -> float:
    return min(max(value, low), high)
