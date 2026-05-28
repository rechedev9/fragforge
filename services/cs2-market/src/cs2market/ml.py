from __future__ import annotations

import json
import math
from dataclasses import asdict, dataclass
from pathlib import Path

from .features import FeatureRow, NUMERIC_FEATURES
from .labels import LabeledRow
from .models import isoformat


@dataclass(frozen=True)
class LogisticModel:
    feature_names: list[str]
    means: list[float]
    scales: list[float]
    weights: list[float]
    bias: float
    trained_at: str
    metrics: dict[str, float]

    def predict_proba(self, row: FeatureRow) -> float:
        z = self.bias
        for i, name in enumerate(self.feature_names):
            value = (row.features.get(name, 0.0) - self.means[i]) / self.scales[i]
            z += self.weights[i] * value
        return _sigmoid(z)

    def to_dict(self) -> dict:
        return asdict(self)


def train_logistic_model(
    rows: list[LabeledRow],
    *,
    feature_names: list[str] | None = None,
    epochs: int = 900,
    learning_rate: float = 0.06,
    l2: float = 0.001,
) -> LogisticModel:
    if len(rows) < 20:
        raise RuntimeError(f"need at least 20 labeled rows, got {len(rows)}")
    positives = sum(row.label for row in rows)
    if positives == 0 or positives == len(rows):
        raise RuntimeError("need both positive and negative labels to train")

    feature_names = feature_names or NUMERIC_FEATURES
    ordered = sorted(rows, key=lambda r: (r.feature_row.as_of, r.feature_row.market_hash_name))
    split = max(1, int(len(ordered) * 0.7))
    train = ordered[:split]
    test = ordered[split:] or ordered[:]
    means, scales = _standardizer(train, feature_names)
    weights = [0.0 for _ in feature_names]
    bias = 0.0

    for _ in range(epochs):
        grad_w = [0.0 for _ in feature_names]
        grad_b = 0.0
        for row in train:
            x = _vector(row.feature_row, feature_names, means, scales)
            pred = _sigmoid(bias + sum(w * value for w, value in zip(weights, x)))
            err = pred - row.label
            grad_b += err
            for i, value in enumerate(x):
                grad_w[i] += err * value
        n = float(len(train))
        bias -= learning_rate * grad_b / n
        for i in range(len(weights)):
            grad = (grad_w[i] / n) + (l2 * weights[i])
            weights[i] -= learning_rate * grad

    model = LogisticModel(
        feature_names=feature_names,
        means=means,
        scales=scales,
        weights=weights,
        bias=bias,
        trained_at=isoformat(),
        metrics={},
    )
    metrics = evaluate_model(model, test)
    return LogisticModel(**{**model.to_dict(), "metrics": metrics})


def evaluate_model(model: LogisticModel, rows: list[LabeledRow]) -> dict[str, float]:
    if not rows:
        return {"rows": 0.0}
    tp = fp = tn = fn = 0
    probs = []
    for row in rows:
        prob = model.predict_proba(row.feature_row)
        probs.append(prob)
        pred = 1 if prob >= 0.5 else 0
        if pred == 1 and row.label == 1:
            tp += 1
        elif pred == 1:
            fp += 1
        elif row.label == 1:
            fn += 1
        else:
            tn += 1
    total = tp + fp + tn + fn
    return {
        "rows": float(total),
        "positive_rate": sum(row.label for row in rows) / total,
        "avg_probability": sum(probs) / len(probs),
        "accuracy": (tp + tn) / total,
        "precision": tp / max(tp + fp, 1),
        "recall": tp / max(tp + fn, 1),
    }


def write_model(model: LogisticModel, path: Path) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(model.to_dict(), indent=2) + "\n", encoding="utf-8")


def read_model(path: Path) -> LogisticModel:
    return LogisticModel(**json.loads(path.read_text(encoding="utf-8")))


def write_predictions(model: LogisticModel, rows: list[FeatureRow], path: Path, *, limit: int = 50) -> None:
    scored = sorted(((model.predict_proba(row), row) for row in rows), key=lambda item: item[0], reverse=True)[:limit]
    payload = [
        {
            "market_hash_name": row.market_hash_name,
            "as_of": row.as_of,
            "category": row.category,
            "price": row.price,
            "ml_probability": round(prob, 4),
        }
        for prob, row in scored
    ]
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(payload, indent=2) + "\n", encoding="utf-8")


def _standardizer(rows: list[LabeledRow], feature_names: list[str]) -> tuple[list[float], list[float]]:
    means = []
    scales = []
    for name in feature_names:
        values = [row.feature_row.features.get(name, 0.0) for row in rows]
        mean = sum(values) / len(values)
        variance = sum((value - mean) ** 2 for value in values) / len(values)
        means.append(mean)
        scales.append(max(variance**0.5, 1e-9))
    return means, scales


def _vector(row: FeatureRow, feature_names: list[str], means: list[float], scales: list[float]) -> list[float]:
    return [(row.features.get(name, 0.0) - means[i]) / scales[i] for i, name in enumerate(feature_names)]


def _sigmoid(value: float) -> float:
    if value >= 0:
        z = math.exp(-value)
        return 1.0 / (1.0 + z)
    z = math.exp(value)
    return z / (1.0 + z)
