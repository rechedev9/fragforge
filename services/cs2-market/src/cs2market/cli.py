from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

from .config import Settings
from .models import Signal
from .service import MarketService


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="cs2market")
    parser.add_argument("--database-url", help="Override CS2MARKET_DATABASE_URL")
    sub = parser.add_subparsers(dest="command", required=True)

    sub.add_parser("init-db")

    ingest = sub.add_parser("ingest")
    ingest_sub = ingest.add_subparsers(dest="source", required=True)
    skinport = ingest_sub.add_parser("skinport")
    skinport.add_argument("--currency", default="USD")
    skinport.add_argument("--sample-file", type=Path)
    takeskin = ingest_sub.add_parser("takeskin")
    takeskin.add_argument("--pages", type=int, default=1)
    takeskin.add_argument("--limit", type=int, default=100)
    takeskin.add_argument("--history-days", type=int, default=30)
    takeskin.add_argument("--history-limit", type=int, default=10)
    firecrawl = ingest_sub.add_parser("firecrawl")
    firecrawl.add_argument("urls", nargs="+")
    historical = ingest_sub.add_parser("historical-csv")
    historical.add_argument("path", type=Path)
    historical.add_argument("--source", dest="dataset_source", default="historical.csv")
    historical.add_argument("--currency", default="USD")
    historical.add_argument("--name-column")
    historical.add_argument("--time-column")
    historical.add_argument("--price-column")
    historical.add_argument("--quantity-column")

    score = sub.add_parser("score")
    score.add_argument("--limit", type=int, default=50)
    score.add_argument("--write", type=Path)
    score.add_argument("--category", action="append", default=[])

    export = sub.add_parser("export-shorts")
    export.add_argument("--signals", type=Path)
    export.add_argument("--out", type=Path)
    export.add_argument("--limit", type=int, default=20)
    export.add_argument("--include-images", action="store_true")

    images = sub.add_parser("download-images")
    images.add_argument("--signals", type=Path)
    images.add_argument("--out", type=Path)
    images.add_argument("--limit", type=int, default=20)

    render = sub.add_parser("render-videos")
    render.add_argument("--manifest-dir", type=Path, required=True)
    render.add_argument("--background", type=Path, required=True)
    render.add_argument("--out", type=Path, required=True)
    render.add_argument("--ffmpeg", default="ffmpeg")
    render.add_argument("--duration", type=float, default=12.0)

    dashboard = sub.add_parser("dashboard")
    dashboard_sub = dashboard.add_subparsers(dest="dashboard_command", required=True)
    dashboard_build = dashboard_sub.add_parser("build")
    dashboard_build.add_argument("--out", type=Path, default=Path("data/market/dashboard/index.html"))
    dashboard_build.add_argument("--signals", type=Path)
    dashboard_build.add_argument("--features", type=Path)
    dashboard_build.add_argument("--predictions", type=Path)
    dashboard_build.add_argument("--metrics", type=Path)
    dashboard_build.add_argument("--backtest", type=Path)
    dashboard_build.add_argument("--assets-dir", type=Path)
    dashboard_build.add_argument("--no-frontend", action="store_true", help="write the Python fallback HTML without building Svelte")

    ml = sub.add_parser("ml")
    ml_sub = ml.add_subparsers(dest="ml_command", required=True)

    ml_status = ml_sub.add_parser("status")
    ml_status.add_argument("--category", action="append", default=[])
    ml_status.add_argument("--min-price", type=float, default=0.0)
    ml_status.add_argument("--max-price", type=float, default=1_000_000.0)
    ml_status.add_argument("--horizon-days", type=int, default=30)
    ml_status.add_argument("--target-return", type=float, default=0.08)
    ml_status.add_argument("--fee-rate", type=float, default=0.12)

    ml_features = ml_sub.add_parser("features")
    ml_features.add_argument("--category", action="append", default=[])
    ml_features.add_argument("--min-price", type=float, default=0.0)
    ml_features.add_argument("--max-price", type=float, default=1_000_000.0)
    ml_features.add_argument("--out", type=Path, default=Path("data/market/ml/features.csv"))

    ml_labels = ml_sub.add_parser("labels")
    ml_labels.add_argument("--features", type=Path)
    ml_labels.add_argument("--category", action="append", default=[])
    ml_labels.add_argument("--min-price", type=float, default=0.0)
    ml_labels.add_argument("--max-price", type=float, default=1_000_000.0)
    ml_labels.add_argument("--horizon-days", type=int, default=30)
    ml_labels.add_argument("--target-return", type=float, default=0.08)
    ml_labels.add_argument("--fee-rate", type=float, default=0.12)
    ml_labels.add_argument("--out", type=Path, default=Path("data/market/ml/labels.csv"))

    ml_train = ml_sub.add_parser("train")
    ml_train.add_argument("--labels", type=Path, default=Path("data/market/ml/labels.csv"))
    ml_train.add_argument("--model-out", type=Path, default=Path("data/market/ml/model.json"))
    ml_train.add_argument("--metrics-out", type=Path, default=Path("data/market/ml/metrics.json"))
    ml_train.add_argument("--epochs", type=int, default=900)
    ml_train.add_argument("--learning-rate", type=float, default=0.06)
    ml_train.add_argument("--l2", type=float, default=0.001)

    ml_predict = ml_sub.add_parser("predict")
    ml_predict.add_argument("--model", type=Path, default=Path("data/market/ml/model.json"))
    ml_predict.add_argument("--features", type=Path)
    ml_predict.add_argument("--category", action="append", default=[])
    ml_predict.add_argument("--min-price", type=float, default=0.0)
    ml_predict.add_argument("--max-price", type=float, default=1_000_000.0)
    ml_predict.add_argument("--limit", type=int, default=50)
    ml_predict.add_argument("--out", type=Path, default=Path("data/market/ml/predictions.json"))

    ml_backtest = ml_sub.add_parser("backtest")
    ml_backtest.add_argument("--labels", type=Path, default=Path("data/market/ml/labels.csv"))
    ml_backtest.add_argument("--top-k", type=int, default=10)
    ml_backtest.add_argument("--out", type=Path, default=Path("data/market/ml/backtest.json"))

    serve = sub.add_parser("serve")
    serve.add_argument("--host", default="127.0.0.1")
    serve.add_argument("--port", type=int, default=8090)

    args = parser.parse_args(argv)
    settings = Settings.from_env()
    if args.database_url:
        settings = Settings(
            database_url=args.database_url,
            firecrawl_command=settings.firecrawl_command,
            data_dir=settings.data_dir,
            output_dir=settings.output_dir,
            skinport_base_url=settings.skinport_base_url,
        )

    if args.command == "serve":
        from .api import create_app
        import uvicorn

        uvicorn.run(create_app(settings), host=args.host, port=args.port)
        return 0

    if args.command == "dashboard":
        return _run_dashboard(args, settings)

    service = MarketService(settings)
    try:
        if args.command == "init-db":
            service.init_db()
            print(f"initialized {settings.database_url}")
            return 0
        if args.command == "ingest" and args.source == "skinport":
            count = service.ingest_skinport(currency=args.currency, sample_file=args.sample_file)
            print(json.dumps({"source": "skinport", "snapshots": count}))
            return 0
        if args.command == "ingest" and args.source == "takeskin":
            count = service.ingest_take_skin(
                pages=args.pages,
                limit=args.limit,
                history_days=args.history_days,
                history_limit=args.history_limit,
            )
            print(json.dumps({"source": "take.skin", "snapshots": count}))
            return 0
        if args.command == "ingest" and args.source == "firecrawl":
            paths = service.scrape_context(args.urls)
            print(json.dumps({"files": [str(p) for p in paths]}, indent=2))
            return 0
        if args.command == "ingest" and args.source == "historical-csv":
            count = service.ingest_historical_csv(
                args.path,
                source=args.dataset_source,
                currency=args.currency,
                name_column=args.name_column,
                time_column=args.time_column,
                price_column=args.price_column,
                quantity_column=args.quantity_column,
            )
            print(json.dumps({"source": args.dataset_source, "snapshots": count, "path": str(args.path)}))
            return 0
        if args.command == "score":
            write = args.write or settings.data_dir / "signals" / "latest.json"
            signals = service.refresh_signals(limit=args.limit, write=write, categories=tuple(args.category))
            print(json.dumps({"signals": len(signals), "path": str(write)}, indent=2))
            return 0
        if args.command == "export-shorts":
            signals = _load_signals(args.signals) if args.signals else service.list_signals(limit=args.limit)
            paths = service.export_shorts(signals=signals, out_dir=args.out, include_images=args.include_images)
            print(json.dumps({"files": [str(p) for p in paths]}, indent=2))
            return 0
        if args.command == "download-images":
            signals = _load_signals(args.signals) if args.signals else service.list_signals(limit=args.limit)
            paths = service.download_signal_images(signals=signals, out_dir=args.out)
            print(json.dumps({"images": {name: str(path) for name, path in paths.items()}}, indent=2))
            return 0
        if args.command == "render-videos":
            from .render import render_short_videos

            paths = render_short_videos(
                args.manifest_dir,
                background=args.background,
                out_dir=args.out,
                ffmpeg=args.ffmpeg,
                duration_seconds=args.duration,
            )
            print(json.dumps({"videos": [str(path) for path in paths]}, indent=2))
            return 0
        if args.command == "ml":
            return _run_ml(args, service)
    finally:
        service.close()
    parser.error("unknown command")
    return 2


def _load_signals(path: Path) -> list[Signal]:
    data = json.loads(path.read_text(encoding="utf-8"))
    return [Signal(**item) for item in data]


def _run_ml(args: argparse.Namespace, service: MarketService) -> int:
    from .backtest import deterministic_backtest, write_backtest
    from .features import build_feature_rows, read_features_csv, write_features_csv
    from .labels import attach_labels, read_labels_csv, write_labels_csv
    from .ml import read_model, train_logistic_model, write_model, write_predictions

    service.store.init_schema()
    snapshots = service.store.snapshots_for_scoring()

    if args.ml_command == "status":
        rows = build_feature_rows(
            snapshots,
            categories=tuple(args.category),
            min_price=args.min_price,
            max_price=args.max_price,
        )
        labeled = attach_labels(
            rows,
            snapshots,
            horizon_days=args.horizon_days,
            target_return=args.target_return,
            fee_rate=args.fee_rate,
        )
        positives = sum(row.label for row in labeled)
        payload = {
            "snapshots": len(snapshots),
            "items": len({snapshot.market_hash_name for snapshot in snapshots}),
            "feature_rows": len(rows),
            "labeled_rows": len(labeled),
            "positive_labels": positives,
            "negative_labels": len(labeled) - positives,
            "trainable": len(labeled) >= 20 and 0 < positives < len(labeled),
        }
        print(json.dumps(payload, indent=2))
        return 0

    if args.ml_command == "features":
        rows = build_feature_rows(
            snapshots,
            categories=tuple(args.category),
            min_price=args.min_price,
            max_price=args.max_price,
        )
        write_features_csv(rows, args.out)
        print(json.dumps({"feature_rows": len(rows), "path": str(args.out)}, indent=2))
        return 0

    if args.ml_command == "labels":
        rows = read_features_csv(args.features) if args.features else build_feature_rows(
            snapshots,
            categories=tuple(args.category),
            min_price=args.min_price,
            max_price=args.max_price,
        )
        labeled = attach_labels(
            rows,
            snapshots,
            horizon_days=args.horizon_days,
            target_return=args.target_return,
            fee_rate=args.fee_rate,
        )
        write_labels_csv(labeled, args.out)
        positives = sum(row.label for row in labeled)
        print(
            json.dumps(
                {
                    "labeled_rows": len(labeled),
                    "positive_labels": positives,
                    "negative_labels": len(labeled) - positives,
                    "path": str(args.out),
                },
                indent=2,
            )
        )
        return 0

    if args.ml_command == "train":
        rows = read_labels_csv(args.labels)
        try:
            model = train_logistic_model(
                rows,
                epochs=args.epochs,
                learning_rate=args.learning_rate,
                l2=args.l2,
            )
        except RuntimeError as err:
            metrics = {
                "status": "insufficient_data",
                "reason": str(err),
                "labeled_rows": len(rows),
                "positive_labels": sum(row.label for row in rows),
            }
            args.metrics_out.parent.mkdir(parents=True, exist_ok=True)
            args.metrics_out.write_text(json.dumps(metrics, indent=2) + "\n", encoding="utf-8")
            print(json.dumps(metrics, indent=2))
            return 0
        write_model(model, args.model_out)
        args.metrics_out.parent.mkdir(parents=True, exist_ok=True)
        args.metrics_out.write_text(json.dumps(model.metrics, indent=2) + "\n", encoding="utf-8")
        print(json.dumps({"status": "trained", "model": str(args.model_out), "metrics": str(args.metrics_out)}, indent=2))
        return 0

    if args.ml_command == "predict":
        model = read_model(args.model)
        rows = read_features_csv(args.features) if args.features else build_feature_rows(
            snapshots,
            categories=tuple(args.category),
            min_price=args.min_price,
            max_price=args.max_price,
        )
        write_predictions(model, rows, args.out, limit=args.limit)
        print(json.dumps({"predictions": min(len(rows), args.limit), "path": str(args.out)}, indent=2))
        return 0

    if args.ml_command == "backtest":
        rows = read_labels_csv(args.labels)
        metrics = deterministic_backtest(rows, top_k=args.top_k)
        write_backtest(metrics, args.out)
        print(json.dumps({**metrics, "path": str(args.out)}, indent=2))
        return 0

    raise ValueError(f"unknown ml command: {args.ml_command}")


def _run_dashboard(args: argparse.Namespace, settings: Settings) -> int:
    from .analytics import DashboardInputs, build_dashboard

    inputs = DashboardInputs.from_settings(settings)
    inputs = DashboardInputs(
        signals_path=args.signals or inputs.signals_path,
        features_path=args.features or inputs.features_path,
        predictions_path=args.predictions or inputs.predictions_path,
        metrics_path=args.metrics or inputs.metrics_path,
        backtest_path=args.backtest or inputs.backtest_path,
        assets_dir=args.assets_dir or inputs.assets_dir,
    )
    if args.dashboard_command == "build":
        result = build_dashboard(inputs, args.out, build_frontend=not args.no_frontend)
        print(
            json.dumps(
                {
                    "dashboard": str(result.output_path),
                    "data": str(result.data_path),
                    "signals": result.signal_count,
                    "features": result.feature_count,
                    "predictions": result.prediction_count,
                    "sources": [str(path) for path in result.source_paths],
                },
                indent=2,
            )
        )
        return 0
    raise ValueError(f"unknown dashboard command: {args.dashboard_command}")


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
