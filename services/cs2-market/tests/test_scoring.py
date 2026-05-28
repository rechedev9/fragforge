from __future__ import annotations

import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "src"))

from cs2market.models import MarketSnapshot
from cs2market.scoring import score_snapshots


class ScoringTests(unittest.TestCase):
    def test_scores_liquid_positive_momentum_as_buy(self) -> None:
        snapshots = [
            snap("Kilowatt Case", 8.0, 80, "2026-04-01T00:00:00Z"),
            snap("Kilowatt Case", 9.0, 72, "2026-04-20T00:00:00Z"),
            snap("Kilowatt Case", 10.0, 60, "2026-05-01T00:00:00Z"),
        ]

        signals = score_snapshots(snapshots)

        self.assertEqual(signals[0].action, "buy")
        self.assertGreaterEqual(signals[0].score, 70)
        self.assertIn("visible supply is tightening", signals[0].reasons)

    def test_filters_out_large_ticket_items(self) -> None:
        signals = score_snapshots([snap("★ Karambit | Doppler (Factory New)", 800.0, 12)])

        self.assertEqual(signals, [])

    def test_marks_low_quantity_as_avoid(self) -> None:
        signals = score_snapshots([snap("Sticker | Rare Thing", 12.0, 1)])

        self.assertEqual(signals[0].action, "avoid")
        self.assertTrue(any("low listed quantity" in risk for risk in signals[0].risks))


def snap(
    name: str,
    price: float,
    quantity: int,
    captured_at: str = "2026-05-01T00:00:00Z",
) -> MarketSnapshot:
    return MarketSnapshot(
        source="skinport",
        market_hash_name=name,
        price=price,
        currency="USD",
        quantity=quantity,
        min_price=price * 0.98,
        max_price=price * 1.04,
        median_price=price,
        captured_at=captured_at,
        raw={},
    )


if __name__ == "__main__":
    unittest.main()
