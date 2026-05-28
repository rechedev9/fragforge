from __future__ import annotations

from collections.abc import Iterator
from contextlib import asynccontextmanager, contextmanager
from pathlib import Path

from .config import Settings
from .scripts import build_short_script
from .service import MarketService


def create_app(settings: Settings | None = None):
    try:
        from fastapi import FastAPI, HTTPException, Query
    except ImportError as err:
        raise RuntimeError("FastAPI is required for HTTP mode; run `python -m pip install -e services/cs2-market`") from err

    settings = settings or Settings.from_env()

    @asynccontextmanager
    async def lifespan(_app):
        with _service(settings) as service:
            service.init_db()
        yield

    app = FastAPI(title="CS2 Market Intelligence", version="0.1.0", lifespan=lifespan)

    @app.get("/health")
    def health() -> dict[str, str]:
        return {"status": "ok"}

    @app.post("/ingest/run")
    def ingest_run(source: str = "skinport", currency: str = "USD") -> dict[str, int | str]:
        with _service(settings) as service:
            if source == "skinport":
                count = service.ingest_skinport(currency=currency)
            elif source in ("takeskin", "take.skin"):
                count = service.ingest_take_skin()
                source = "take.skin"
            else:
                raise HTTPException(status_code=400, detail="source must be skinport or take.skin")
        return {"source": source, "snapshots": count}

    @app.post("/signals/refresh")
    def signals_refresh(limit: int = Query(default=50, ge=1, le=500), category: list[str] | None = Query(default=None)) -> list[dict]:
        write = settings.data_dir / "signals" / "latest.json"
        with _service(settings) as service:
            return [s.to_dict() for s in service.refresh_signals(limit=limit, write=write, categories=tuple(category or []))]

    @app.get("/signals")
    def signals(limit: int = Query(default=20, ge=1, le=200), min_confidence: float = 0.0) -> list[dict]:
        with _service(settings) as service:
            return [s.to_dict() for s in service.list_signals(limit=limit, min_confidence=min_confidence)]

    @app.get("/signals/{signal_id}/short-script")
    def short_script(signal_id: int) -> dict:
        with _service(settings) as service:
            signal = service.store.get_signal(signal_id)
            if signal is None:
                raise HTTPException(status_code=404, detail="signal not found")
            return build_short_script(signal).to_dict()

    @app.post("/context/scrape")
    def context_scrape(urls: list[str]) -> dict[str, list[str]]:
        with _service(settings) as service:
            paths = service.scrape_context(urls)
        return {"files": [str(Path(path)) for path in paths]}

    return app


@contextmanager
def _service(settings: Settings) -> Iterator[MarketService]:
    service = MarketService(settings)
    try:
        yield service
    finally:
        service.close()
