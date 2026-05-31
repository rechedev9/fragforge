from __future__ import annotations

from pathlib import Path
from typing import Any


def write_placeholder_plot(path: Path) -> None:
    """Write a small Matplotlib smoke chart for future dashboard embeds."""
    pyplot = _pyplot()
    path.parent.mkdir(parents=True, exist_ok=True)
    figure, axis = pyplot.subplots(figsize=(4, 2.4), dpi=140)
    axis.plot([0, 1, 2], [0, 1, 0.6], color="#0f766e", linewidth=2)
    axis.set_title("CS2 market signal trend")
    axis.set_xlabel("period")
    axis.set_ylabel("score")
    figure.tight_layout()
    figure.savefig(path)
    pyplot.close(figure)


def _pyplot() -> Any:
    try:
        import matplotlib

        matplotlib.use("Agg")
        from matplotlib import pyplot
    except ImportError as err:
        raise RuntimeError("market analytics requires matplotlib; run `python -m pip install -e services/cs2-market`") from err
    return pyplot
