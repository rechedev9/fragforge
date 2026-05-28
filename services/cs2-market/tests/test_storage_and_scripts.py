from __future__ import annotations

import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "src"))

from cs2market.models import MarketSnapshot
from cs2market.scripts import build_short_script, export_short_scripts
from cs2market.scoring import score_snapshots
from cs2market.storage import SQLiteStore


class StorageAndScriptTests(unittest.TestCase):
    def test_store_round_trip_and_script_export(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            db_path = Path(tmp) / "market.sqlite3"
            store = SQLiteStore(db_path)
            try:
                store.init_schema()
                store.put_snapshots(
                    "skinport",
                    [
                        MarketSnapshot(
                            source="skinport",
                            market_hash_name="Fracture Case",
                            price=6.0,
                            currency="USD",
                            quantity=45,
                            min_price=5.9,
                            max_price=6.2,
                            median_price=6.0,
                            raw={"market_page": "https://example"},
                        )
                    ],
                )
                signals = store.replace_signals(score_snapshots(store.snapshots_for_scoring()))

                got = store.list_signals(limit=5)
                self.assertEqual(len(got), 1)
                self.assertIsNotNone(got[0].id)

                script = build_short_script(got[0], skin_image_path=Path(tmp) / "skin.png")
                self.assertIn("Fracture Case", script.title)
                self.assertIn("no es garantia", script.disclaimer)
                self.assertIn("Asset type", script.gpt_image_prompt)
                self.assertTrue(script.skin_image_path.endswith("skin.png"))

                written = export_short_scripts(signals, Path(tmp) / "shorts", {"Fracture Case": Path(tmp) / "skin.png"})
                self.assertEqual(len(written), 5)
                self.assertTrue(any(path.suffix == ".md" for path in written))
                self.assertTrue(any(path.name.endswith(".assets.json") for path in written))
            finally:
                store.close()


if __name__ == "__main__":
    unittest.main()
