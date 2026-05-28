from __future__ import annotations

import json
from pathlib import Path

from .labels import LabeledRow


def deterministic_backtest(rows: list[LabeledRow], *, top_k: int = 10) -> dict[str, float]:
    if not rows:
        return {"rows": 0.0}
    ranked = sorted(rows, key=lambda row: row.feature_row.features.get("liquidity_score", 0.0), reverse=True)[:top_k]
    wins = sum(row.label for row in ranked)
    avg_net = sum(row.net_return for row in ranked) / len(ranked)
    return {
        "rows": float(len(rows)),
        "top_k": float(len(ranked)),
        "precision_at_k": wins / len(ranked),
        "avg_net_return_at_k": avg_net,
        "positive_rate": sum(row.label for row in rows) / len(rows),
    }


def write_backtest(metrics: dict[str, float], path: Path) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(metrics, indent=2) + "\n", encoding="utf-8")
