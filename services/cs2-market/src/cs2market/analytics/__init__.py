"""Market analytics boundaries for CS2 market dashboards and reports.

The analytics stack intentionally uses Polars for tabular ETL, NumPy for
numeric summaries, and Matplotlib for static charts embedded in generated
reports.
"""

from .dashboard import build_dashboard
from .types import DashboardInputs, DashboardResult

__all__ = ["DashboardInputs", "DashboardResult", "build_dashboard"]
