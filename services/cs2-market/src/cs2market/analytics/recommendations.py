from __future__ import annotations

import csv
import json
from pathlib import Path
from typing import Any

from .types import DashboardInputs, InvestmentRecommendation


def build_recommendations(inputs: DashboardInputs, *, limit: int = 12) -> list[InvestmentRecommendation]:
    signals = _read_json_list(inputs.signals_path)
    features = _latest_features(inputs.features_path)
    predictions = _latest_predictions(inputs.predictions_path)
    backtest = _read_json_object(inputs.backtest_path)
    backtest_return = _float(backtest.get("avg_net_return_at_k"))
    backtest_precision = _float(backtest.get("precision_at_k"))

    recommendations: list[InvestmentRecommendation] = []
    for signal in signals:
        name = str(signal.get("market_hash_name") or "").strip()
        if not name:
            continue
        feature = features.get(name, {})
        prediction = predictions.get(name, {})
        rec = _recommendation_from_signal(
            signal=signal,
            feature=feature,
            prediction=prediction,
            backtest_return=backtest_return,
            backtest_precision=backtest_precision,
        )
        recommendations.append(rec)

    recommendations.sort(key=lambda item: (_grade_rank(item.grade), item.score, item.confidence), reverse=True)
    return recommendations[:limit]


def _recommendation_from_signal(
    *,
    signal: dict[str, Any],
    feature: dict[str, str],
    prediction: dict[str, Any],
    backtest_return: float,
    backtest_precision: float,
) -> InvestmentRecommendation:
    name = str(signal.get("market_hash_name") or "")
    action = str(signal.get("action") or "watch").lower()
    score = _float(signal.get("score"))
    confidence = _float(signal.get("confidence"))
    price = _float(signal.get("price") or feature.get("price"))
    currency = str(signal.get("currency") or "USD")
    quantity = int(round(_float(feature.get("quantity"))))
    liquidity = _float(feature.get("liquidity_score"))
    spread = _float(feature.get("spread_ratio"))
    momentum_7d = _float(feature.get("momentum_7d"))
    momentum_30d = _float(feature.get("momentum_30d"))
    model_probability = _float(prediction.get("ml_probability"))

    grade = _grade(
        action=action,
        score=score,
        confidence=confidence,
        liquidity=liquidity,
        spread=spread,
        model_probability=model_probability,
        backtest_return=backtest_return,
    )
    risk_level = _risk_level(spread=spread, liquidity=liquidity, backtest_return=backtest_return, model_probability=model_probability)
    decision = _decision(action=action, grade=grade, backtest_return=backtest_return)
    allocation = _allocation(grade=grade, risk_level=risk_level, backtest_return=backtest_return)
    thesis = _thesis(signal=signal, liquidity=liquidity, spread=spread, momentum_7d=momentum_7d, model_probability=model_probability)
    invalidation = _invalidation(price=price, quantity=quantity, spread=spread, liquidity=liquidity)
    evidence = _evidence(
        signal=signal,
        liquidity=liquidity,
        spread=spread,
        momentum_7d=momentum_7d,
        momentum_30d=momentum_30d,
        model_probability=model_probability,
        backtest_precision=backtest_precision,
        backtest_return=backtest_return,
    )
    risks = tuple(str(item) for item in signal.get("risks") or ())

    return InvestmentRecommendation(
        market_hash_name=name,
        decision=decision,
        action=action,
        grade=grade,
        risk_level=risk_level,
        price=price,
        currency=currency,
        score=score,
        confidence=confidence,
        model_probability=model_probability,
        quantity=quantity,
        liquidity_score=liquidity,
        spread_ratio=spread,
        momentum_7d=momentum_7d,
        momentum_30d=momentum_30d,
        suggested_allocation=allocation,
        thesis=thesis,
        invalidation=invalidation,
        evidence=evidence,
        risks=risks,
    )


def _grade(
    *,
    action: str,
    score: float,
    confidence: float,
    liquidity: float,
    spread: float,
    model_probability: float,
    backtest_return: float,
) -> str:
    if action in {"avoid", "sell"}:
        return "D"
    if backtest_return < 0:
        if score >= 58 and confidence >= 0.6 and liquidity >= 0.8 and spread <= 0.35:
            return "B"
        if score >= 50 and confidence >= 0.45:
            return "C"
        return "D"
    if score >= 70 and confidence >= 0.65 and liquidity >= 0.8 and spread <= 0.25 and model_probability >= 0.55:
        return "A"
    if score >= 58 and confidence >= 0.55 and liquidity >= 0.6:
        return "B"
    if score >= 45:
        return "C"
    return "D"


def _risk_level(*, spread: float, liquidity: float, backtest_return: float, model_probability: float) -> str:
    if backtest_return < 0 or spread >= 0.4 or liquidity < 0.35:
        return "High"
    if spread >= 0.25 or liquidity < 0.75 or (model_probability and model_probability < 0.45):
        return "Medium"
    return "Controlled"


def _decision(*, action: str, grade: str, backtest_return: float) -> str:
    if action in {"avoid", "sell"} or grade == "D":
        return "Avoid"
    if backtest_return < 0:
        return "Speculative watchlist"
    if grade == "A":
        return "High-conviction buy"
    if grade == "B":
        return "Research buy"
    return "Watchlist"


def _allocation(*, grade: str, risk_level: str, backtest_return: float) -> str:
    if grade == "D":
        return "0%"
    if backtest_return < 0:
        return "0.25%-0.75% max; paper-trade preferred"
    if risk_level == "High":
        return "0.25%-0.50% max"
    if grade == "A":
        return "1.00%-2.00% max"
    if grade == "B":
        return "0.50%-1.00% max"
    return "0.25%-0.50% max"


def _thesis(
    *,
    signal: dict[str, Any],
    liquidity: float,
    spread: float,
    momentum_7d: float,
    model_probability: float,
) -> str:
    reasons = [str(item) for item in signal.get("reasons") or []]
    primary = reasons[0] if reasons else "local signal model selected this skin"
    context = []
    if liquidity >= 0.8:
        context.append("strong listed liquidity")
    if 0 < spread <= 0.25:
        context.append("acceptable spread")
    if momentum_7d > 0:
        context.append(f"positive 7d momentum ({momentum_7d:.1%})")
    if model_probability > 0:
        context.append(f"ML probability {model_probability:.1%}")
    suffix = "; ".join(context) if context else "limited confirmation from secondary factors"
    return f"{primary}; {suffix}."


def _invalidation(*, price: float, quantity: int, spread: float, liquidity: float) -> str:
    triggers = []
    if price > 0:
        triggers.append(f"do not chase above {price * 1.10:.2f} reference price")
    if quantity > 0:
        triggers.append(f"recheck if listed quantity falls below {max(5, quantity // 2)}")
    if spread > 0:
        triggers.append("skip if spread widens above 35%")
    if liquidity < 0.5:
        triggers.append("wait for liquidity score above 50%")
    return "; ".join(triggers) + "."


def _evidence(
    *,
    signal: dict[str, Any],
    liquidity: float,
    spread: float,
    momentum_7d: float,
    momentum_30d: float,
    model_probability: float,
    backtest_precision: float,
    backtest_return: float,
) -> tuple[str, ...]:
    items = [
        f"Signal score {float(signal.get('score') or 0):.1f}, confidence {float(signal.get('confidence') or 0):.0%}",
        f"Liquidity {liquidity:.0%}, spread {spread:.0%}",
        f"Momentum 7d {momentum_7d:.1%}, 30d {momentum_30d:.1%}",
    ]
    if model_probability:
        items.append(f"ML probability {model_probability:.1%}")
    if backtest_precision or backtest_return:
        items.append(f"Backtest precision@K {backtest_precision:.1%}, net return@K {backtest_return:.1%}")
    return tuple(items)


def _latest_features(path: Path) -> dict[str, dict[str, str]]:
    if not path.exists():
        return {}
    latest: dict[str, dict[str, str]] = {}
    with path.open("r", encoding="utf-8-sig", newline="") as f:
        for row in csv.DictReader(f):
            name = row.get("market_hash_name") or ""
            if not name:
                continue
            prev = latest.get(name)
            if prev is None or (row.get("as_of") or "") >= (prev.get("as_of") or ""):
                latest[name] = row
    return latest


def _latest_predictions(path: Path) -> dict[str, dict[str, Any]]:
    predictions = _read_json_list(path)
    latest: dict[str, dict[str, Any]] = {}
    for row in predictions:
        name = str(row.get("market_hash_name") or "")
        if not name:
            continue
        prev = latest.get(name)
        if prev is None or _float(row.get("ml_probability")) >= _float(prev.get("ml_probability")):
            latest[name] = row
    return latest


def _read_json_list(path: Path) -> list[dict[str, Any]]:
    if not path.exists():
        return []
    payload = json.loads(path.read_text(encoding="utf-8"))
    return payload if isinstance(payload, list) else []


def _read_json_object(path: Path) -> dict[str, Any]:
    if not path.exists():
        return {}
    payload = json.loads(path.read_text(encoding="utf-8"))
    return payload if isinstance(payload, dict) else {}


def _float(value: Any) -> float:
    try:
        return float(value or 0.0)
    except (TypeError, ValueError):
        return 0.0


def _grade_rank(grade: str) -> int:
    return {"A": 4, "B": 3, "C": 2, "D": 1}.get(grade, 0)
