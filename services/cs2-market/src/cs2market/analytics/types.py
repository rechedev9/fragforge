from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path

from cs2market.config import Settings


@dataclass(frozen=True)
class DashboardInputs:
    signals_path: Path
    features_path: Path
    predictions_path: Path
    metrics_path: Path
    backtest_path: Path
    assets_dir: Path

    @classmethod
    def from_settings(cls, settings: Settings) -> "DashboardInputs":
        data_dir = settings.data_dir
        return cls(
            signals_path=data_dir / "signals" / "skins-latest.json",
            features_path=data_dir / "ml" / "features.csv",
            predictions_path=data_dir / "ml" / "predictions-h1-smoke.json",
            metrics_path=data_dir / "ml" / "metrics-h1-smoke.json",
            backtest_path=data_dir / "ml" / "backtest-h1-smoke.json",
            assets_dir=data_dir / "assets" / "skins",
        )

    def source_paths(self) -> tuple[Path, ...]:
        return (
            self.signals_path,
            self.features_path,
            self.predictions_path,
            self.metrics_path,
            self.backtest_path,
        )


@dataclass(frozen=True)
class DashboardResult:
    output_path: Path
    data_path: Path
    source_paths: tuple[Path, ...]
    signal_count: int
    feature_count: int
    prediction_count: int


@dataclass(frozen=True)
class InvestmentRecommendation:
    market_hash_name: str
    decision: str
    action: str
    grade: str
    risk_level: str
    price: float
    currency: str
    score: float
    confidence: float
    model_probability: float
    quantity: int
    liquidity_score: float
    spread_ratio: float
    momentum_7d: float
    momentum_30d: float
    suggested_allocation: str
    thesis: str
    invalidation: str
    evidence: tuple[str, ...]
    risks: tuple[str, ...]
