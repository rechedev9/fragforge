# CS2 Market Analytics Boundaries

This document defines the project boundary for CS2 market analytics and
dashboards inside FragForge.

## Scope

`services/cs2-market` owns market analysis code:

- public market ingestion
- normalized snapshots
- feature extraction
- labels, model metrics, predictions, and backtests
- rule-based signals
- static dashboards and analyst reports
- Shorts scripts derived from market signals

The Go demo/video pipeline does not own this code. The market service can feed
Shorts production with scripts and assets, but it should not record gameplay,
parse demos, render HLAE captures, or manage final upload packs.

## Runtime Boundary

The market analytics layer reads local artifacts under `data/market`:

- `signals/*.json`
- `ml/*.csv`
- `ml/*.json`
- `assets/skins/*`
- `shorts*/**/*.assets.json` when enriching content artifacts

It writes generated artifacts under:

- `data/market/dashboard/`
- `data/market/reports/`
- `data/market/processed/`

Raw external captures or exports should go under `data/market/raw/`.

## Code Boundary

Permanent code belongs under `services/cs2-market/src/cs2market`.

Current skeleton:

- `analytics/types.py`: data contracts for dashboard/report inputs and outputs
- `analytics/dashboard.py`: static dashboard build boundary
- `analytics/frames.py`: Polars-based dataframe extraction and transforms
- `analytics/numerics.py`: NumPy-based numeric summaries
- `analytics/plots.py`: Matplotlib chart export boundary
- `analytics/recommendations.py`: investment-research recommendation rules,
  thesis, invalidation, sizing, and risk gates
- `frontend/`: Svelte/Vite user interface for dashboards
- `cli.py`: user-facing command wiring, for example `cs2market dashboard build`

Future code should keep these boundaries:

- `sources.py` and `historical.py`: extraction from public data or local exports
- `storage.py`: durable tables and database access
- `features.py`, `labels.py`, `ml.py`, `backtest.py`: transformations and model
  evaluation
- `scoring.py`: deterministic rule-based signals
- `analytics/*`: dashboard/report assembly from already-normalized artifacts
- `render.py` and `scripts.py`: content production from selected signals

## ETL Contract

Extract:

- Read public market snapshots, local historical exports, and generated market
  artifacts.
- Do not bypass logins, paywalls, CAPTCHA, anti-bot controls, or marketplace
  rate limits.

Transform:

- Normalize item names, categories, wear, price, liquidity, spread, momentum,
  volatility, model probability, and signal state.
- Keep generated metrics reproducible from stored snapshots.
- Use Polars for table-scale transforms and joins.
- Use NumPy for weighted summaries, ranking math, and vectorized scoring
  helpers that do not belong in the rule-based scorer.
- Use Matplotlib with the non-interactive `Agg` backend for static chart images
  embedded in HTML reports.

Load:

- Store canonical market snapshots in the configured database.
- Store derived local files under `data/market/processed`, `data/market/ml`,
  or `data/market/dashboard`.
- Keep dashboard HTML static and reproducible from local inputs.
- Emit `dashboard-data.json` as the frontend contract.
- Build the Svelte frontend into `data/market/dashboard`.

## Out Of Scope

- Real trading execution
- Wallet, API-key, or credential management
- Live browser automation against marketplaces
- Financial advice or guaranteed-profit language
- Go demo parsing, HLAE recording, FFmpeg gameplay rendering, and final Shorts
  upload pack ownership

## Dependency Boundary

Analytics-only dependencies must stay behind `cs2market.analytics` modules.
General CLI commands such as `cs2market score`, `cs2market ingest`, and
`cs2market export-shorts` should not import Polars, NumPy, or Matplotlib unless
they explicitly enter an analytics/reporting path.

Frontend dependencies stay under `services/cs2-market/frontend`. The Python
dashboard command may invoke the frontend build, but the Svelte app must consume
only the generated `dashboard-data.json` contract and static assets.
