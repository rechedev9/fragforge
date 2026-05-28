from __future__ import annotations

import sys
import tempfile
import unittest
from datetime import UTC, datetime, timedelta
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "src"))

from cs2market.features import build_feature_rows
from cs2market.historical import load_historical_csv
from cs2market.labels import attach_labels
from cs2market.ml import train_logistic_model
from cs2market.models import MarketSnapshot


class MLPipelineTests(unittest.TestCase):
    def test_historical_csv_imports_snapshots_with_column_aliases(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "history.csv"
            path.write_text(
                "item_name,date,lowest_price,volume\n"
                "AK-47 | Slate (Factory New),2026-01-01,12.50,42\n"
                "AK-47 | Slate (Factory New),2026-01-08,14.10,39\n",
                encoding="utf-8",
            )

            snapshots = load_historical_csv(path, source="fixture", currency="USD")

        self.assertEqual(len(snapshots), 2)
        self.assertEqual(snapshots[0].market_hash_name, "AK-47 | Slate (Factory New)")
        self.assertEqual(snapshots[0].captured_at, "2026-01-01T00:00:00Z")
        self.assertEqual(snapshots[0].price, 12.5)
        self.assertEqual(snapshots[0].quantity, 42)

    def test_train_logistic_model_from_labeled_historical_snapshots(self) -> None:
        snapshots = _synthetic_snapshots()
        features = build_feature_rows(snapshots, categories=("skin",), min_price=1.0, max_price=50.0)
        labels = attach_labels(features, snapshots, horizon_days=7, target_return=0.05, fee_rate=0.0)

        model = train_logistic_model(labels, epochs=120, learning_rate=0.05)

        self.assertGreaterEqual(len(features), 60)
        self.assertGreaterEqual(len(labels), 40)
        self.assertGreater(sum(row.label for row in labels), 0)
        self.assertLess(sum(row.label for row in labels), len(labels))
        for row in features[:5]:
            got = model.predict_proba(row)
            self.assertGreaterEqual(got, 0.0)
            self.assertLessEqual(got, 1.0)


def _synthetic_snapshots() -> list[MarketSnapshot]:
    start = datetime(2026, 1, 1, tzinfo=UTC)
    snapshots: list[MarketSnapshot] = []
    for index in range(35):
        captured_at = (start + timedelta(days=index)).isoformat().replace("+00:00", "Z")
        snapshots.append(
            MarketSnapshot(
                source="fixture",
                market_hash_name="AK-47 | Growth (Factory New)",
                price=10.0 + index * 0.35,
                currency="USD",
                quantity=30 + index,
                captured_at=captured_at,
            )
        )
        snapshots.append(
            MarketSnapshot(
                source="fixture",
                market_hash_name="M4A1-S | Fadeout (Factory New)",
                price=20.0 - index * 0.18,
                currency="USD",
                quantity=35,
                captured_at=captured_at,
            )
        )
    return snapshots


if __name__ == "__main__":
    unittest.main()
