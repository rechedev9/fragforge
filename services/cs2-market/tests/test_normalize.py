from __future__ import annotations

import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "src"))

from cs2market.normalize import infer_item, normalize_skinport_items, normalize_take_skin_history, normalize_take_skin_listing


class NormalizeTests(unittest.TestCase):
    def test_infers_skin_wear_and_refs(self) -> None:
        item = infer_item(
            "AK-47 | Redline (Field-Tested)",
            {"item_page": "https://example/item", "market_page": "https://example/market"},
        )

        self.assertEqual(item.category, "skin")
        self.assertEqual(item.wear, "Field-Tested")
        self.assertEqual(item.source_refs["item_page"], "https://example/item")

    def test_does_not_classify_non_weapon_pipe_items_as_skins(self) -> None:
        self.assertEqual(infer_item("Sticker Slab | Drug War Veteran").category, "sticker")
        self.assertEqual(infer_item("StatTrak™ Music Kit | TWERL and Ekko & Sidetrack, Under Bright Lights").category, "music_kit")
        self.assertEqual(infer_item("AK-47 | Slate (Minimal Wear)").category, "skin")

    def test_normalizes_skinport_price_fallback(self) -> None:
        snapshots = normalize_skinport_items(
            [
                {
                    "market_hash_name": "Revolution Case",
                    "currency": "USD",
                    "min_price": None,
                    "median_price": 1.25,
                    "quantity": 42,
                }
            ],
            captured_at="2026-05-01T00:00:00Z",
        )

        self.assertEqual(len(snapshots), 1)
        self.assertEqual(snapshots[0].price, 1.25)
        self.assertEqual(snapshots[0].quantity, 42)

    def test_normalizes_take_skin_listing_and_history(self) -> None:
        listing = normalize_take_skin_listing(
            [{"marketHashName": "AK-47 | Asiimov (Field-Tested)", "price": 34.2, "volume": 142}],
            captured_at="2026-05-01T00:00:00Z",
        )
        history = normalize_take_skin_history(
            {
                "marketHashName": "AK-47 | Asiimov (Field-Tested)",
                "currency": "USD",
                "data": [{"date": "2026-04-30", "price": 33.1, "volume": 98}],
            }
        )

        self.assertEqual(listing[0].source, "take.skin")
        self.assertEqual(listing[0].quantity, 142)
        self.assertEqual(history[0].source, "take.skin.history")
        self.assertEqual(history[0].captured_at, "2026-04-30T00:00:00Z")


if __name__ == "__main__":
    unittest.main()
