from __future__ import annotations

from typing import Any


def weighted_mean(values: list[float], weights: list[float]) -> float:
    """Compute a weighted mean with NumPy for dashboard summary metrics."""
    if len(values) != len(weights):
        raise ValueError("values and weights must have the same length")
    if not values:
        return 0.0
    np = _numpy()
    weights_array = np.asarray(weights, dtype=float)
    total_weight = float(weights_array.sum())
    if total_weight <= 0:
        return 0.0
    return float(np.average(np.asarray(values, dtype=float), weights=weights_array))


def _numpy() -> Any:
    try:
        import numpy as np
    except ImportError as err:
        raise RuntimeError("market analytics requires numpy; run `python -m pip install -e services/cs2-market`") from err
    return np
