from __future__ import annotations

from pathlib import Path
from typing import Any


def read_features_frame(path: Path) -> Any:
    """Read feature rows with Polars.

    Polars is an analytics dependency, so this module imports it lazily. CLI
    help, scoring, and lightweight tests should not require the analytics stack
    to be installed.
    """
    pl = _polars()
    return pl.read_csv(path)


def _polars() -> Any:
    try:
        import polars as pl
    except ImportError as err:
        raise RuntimeError("market analytics requires polars; run `python -m pip install -e services/cs2-market`") from err
    return pl
