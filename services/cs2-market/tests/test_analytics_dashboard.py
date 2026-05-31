from __future__ import annotations

import json
import sys
import tempfile
import unittest
from contextlib import redirect_stdout
from io import StringIO
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "src"))

from cs2market.analytics import DashboardInputs, build_dashboard
from cs2market.cli import main


class AnalyticsDashboardTests(unittest.TestCase):
    def test_build_dashboard_shell_from_local_artifacts(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            inputs = write_market_artifacts(root)
            out = root / "dashboard" / "index.html"

            result = build_dashboard(inputs, out)

            self.assertEqual(result.signal_count, 1)
            self.assertEqual(result.feature_count, 2)
            self.assertEqual(result.prediction_count, 1)
            self.assertTrue(out.exists())
            self.assertTrue(result.data_path.exists())
            html = out.read_text(encoding="utf-8")
            self.assertIn("CS2 Skin Investment Research", html)
            self.assertIn("Feature rows", html)
            self.assertIn("Investment Committee Recommendations", html)
            self.assertIn("Speculative watchlist", html)
            self.assertIn("Allocation:", html)
            self.assertIn("Invalidation:", html)

    def test_dashboard_build_command_writes_html(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            inputs = write_market_artifacts(root)
            out = root / "out" / "index.html"

            with redirect_stdout(StringIO()):
                code = main(
                    [
                        "dashboard",
                        "build",
                        "--signals",
                        str(inputs.signals_path),
                        "--features",
                        str(inputs.features_path),
                        "--predictions",
                        str(inputs.predictions_path),
                        "--metrics",
                        str(inputs.metrics_path),
                        "--backtest",
                        str(inputs.backtest_path),
                    "--assets-dir",
                    str(inputs.assets_dir),
                    "--no-frontend",
                    "--out",
                    str(out),
                ]
                )

            self.assertEqual(code, 0)
            self.assertTrue(out.exists())


def write_market_artifacts(root: Path) -> DashboardInputs:
    signals = root / "signals.json"
    features = root / "features.csv"
    predictions = root / "predictions.json"
    metrics = root / "metrics.json"
    backtest = root / "backtest.json"
    assets = root / "assets"
    assets.mkdir()
    signals.write_text(
        json.dumps(
            [
                {
                    "market_hash_name": "AK-47 | Slate (Factory New)",
                    "action": "buy",
                    "score": 72.0,
                    "confidence": 0.64,
                    "price": 12.5,
                    "currency": "USD",
                    "reasons": ["latest public price is 12.50 USD", "liquidity is strong"],
                    "risks": ["limited history, confidence is lower until more snapshots accumulate"],
                }
            ]
        ),
        encoding="utf-8",
    )
    features.write_text(
        "market_hash_name,as_of,category,wear,source,price,quantity,liquidity_score,spread_ratio,momentum_7d,momentum_30d\n"
        "AK-47 | Slate (Factory New),2026-05-01T00:00:00Z,skin,Factory New,skinport,12.5,40,0.8,0.18,0.05,0.08\n"
        "M4A1-S | Fadeout (Factory New),2026-05-01T00:00:00Z,skin,Factory New,skinport,22.5,25,0.5,0.32,-0.02,0.01\n",
        encoding="utf-8",
    )
    predictions.write_text(
        json.dumps([{"market_hash_name": "AK-47 | Slate (Factory New)", "ml_probability": 0.61}]),
        encoding="utf-8",
    )
    metrics.write_text(json.dumps({"rows": 30}), encoding="utf-8")
    backtest.write_text(json.dumps({"rows": 30, "precision_at_k": 0.16, "avg_net_return_at_k": -0.09}), encoding="utf-8")
    return DashboardInputs(
        signals_path=signals,
        features_path=features,
        predictions_path=predictions,
        metrics_path=metrics,
        backtest_path=backtest,
        assets_dir=assets,
    )


if __name__ == "__main__":
    unittest.main()
