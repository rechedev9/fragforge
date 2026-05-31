# CS2 Market Intelligence

Python microservice for public CS2 market research. It ingests free/public data,
stores snapshots, scores 2-8 week swing opportunities, and exports YouTube
Shorts scripts.

This service does not execute trades and must not bypass paywalls, logins,
CAPTCHA, anti-bot controls, or marketplace rate limits.

## Setup

```powershell
cd services\cs2-market
python -m venv .venv
.\.venv\Scripts\Activate.ps1
python -m pip install -e .
```

Optional Firecrawl CLI access uses `npx` by default:

```powershell
$env:FIRECRAWL_API_KEY = "fc-..."
```

The default database is SQLite at `data/market/cs2market.sqlite3` from the repo
root. To use Postgres:

```powershell
$env:CS2MARKET_DATABASE_URL = "postgres://zackvideo:zackvideo@localhost:5432/zackvideo?sslmode=disable"
```

## CLI

```powershell
cs2market init-db
cs2market ingest skinport --currency USD
cs2market ingest takeskin --pages 1 --history-limit 10
cs2market ingest historical-csv data\market\historical\prices.csv --source legacy-prices
cs2market score --category skin --limit 25 --write data\market\signals\latest.json
cs2market download-images --signals data\market\signals\latest.json --out data\market\assets\skins
cs2market export-shorts --signals data\market\signals\latest.json --out data\market\shorts --include-images
cs2market render-videos --manifest-dir data\market\shorts\<YYYYMMDD> --background data\market\gpt-image\cs2-skin-investment-short-bg.png --out data\market\shorts-rendered
cs2market dashboard build --out data\market\dashboard\index.html
cs2market serve --host 127.0.0.1 --port 8090
```

For offline development, ingest a captured Skinport payload:

```powershell
cs2market ingest skinport --sample-file fixtures\skinport-items.json
```

## Machine learning

The ML path is deterministic and uses the same `market_snapshots` table as the
rule-based scorer. Historical CSVs should be imported first so labels can be
computed from real future prices instead of today's snapshot.

Supported historical CSV aliases include:

- item name: `market_hash_name`, `hash_name`, `item_name`, `name`, `skin_name`
- timestamp: `captured_at`, `timestamp`, `time`, `date`, `created_at`
- price: `price`, `lowest_price`, `min_price`, `median_price`, `mean_price`
- quantity: `quantity`, `listings`, `listing_count`, `volume`, `sales_volume`

Use explicit column names when a dataset uses different headers:

```powershell
cs2market ingest historical-csv data\market\historical\prices.csv `
  --source steam-history `
  --name-column item `
  --time-column day `
  --price-column close `
  --quantity-column volume
```

Then build features, labels, and a small logistic model:

```powershell
cs2market ml status --category skin --horizon-days 30
cs2market ml features --category skin --min-price 5 --max-price 50 --out data\market\ml\features.csv
cs2market ml labels --features data\market\ml\features.csv --horizon-days 30 --target-return 0.08 --fee-rate 0.12 --out data\market\ml\labels.csv
cs2market ml train --labels data\market\ml\labels.csv --model-out data\market\ml\model.json --metrics-out data\market\ml\metrics.json
cs2market ml predict --model data\market\ml\model.json --features data\market\ml\features.csv --out data\market\ml\predictions.json
cs2market ml backtest --labels data\market\ml\labels.csv --top-k 25 --out data\market\ml\backtest.json
```

Training requires both winning and losing labeled examples. If the database only
contains current prices, `ml train` will write an `insufficient_data` report
instead of pretending the model is ready.

## Analytics dashboards

Dashboard code lives inside this service, under `src/cs2market/analytics`.
Generated dashboard artifacts live under `data/market/dashboard`.
The analytics stack uses Polars for tabular ETL, NumPy for numeric summaries,
and Matplotlib for static charts.
The interactive frontend lives under `frontend/` and is built with Svelte/Vite.

Build the Svelte dashboard from local market artifacts:

```powershell
cs2market dashboard build --out data\market\dashboard\index.html
```

The command writes both `index.html` and `dashboard-data.json`. The JSON is also
embedded into the generated HTML so the dashboard can be opened directly from
disk, while still supporting HTTP serving.

For frontend development:

```powershell
cd services\cs2-market\frontend
npm install
npm run dev
```

The project boundary is documented in
[`docs/analytics-boundaries.md`](docs/analytics-boundaries.md).

## HTTP API

- `GET /health`
- `POST /ingest/run`
- `POST /signals/refresh`
- `GET /signals?limit=20&min_confidence=0.4`
- `GET /signals/{id}/short-script`

## Signal Policy

Signals use direct `buy`, `sell`, `watch`, or `avoid` labels, but each signal
includes timestamp, confidence, risk notes, and price context. Shorts should
state that this is CS2 market research, not guaranteed profit.

## Data and image sources

- Skinport `/v1/items`: no auth, cached for 5 minutes, 8 requests per 5 minutes.
- Take.Skin public API: no auth, 60 requests/hour, up to 30 days of history.
- SteamApis image endpoint: no API key, redirects to Steam CDN item images.
