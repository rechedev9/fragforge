from __future__ import annotations

import csv
import html
import json
import os
import re
import subprocess
from pathlib import Path
from typing import Any

from .recommendations import build_recommendations
from .types import DashboardInputs, DashboardResult


def build_dashboard(inputs: DashboardInputs, out: Path, *, build_frontend: bool = False) -> DashboardResult:
    """Build the static market dashboard from normalized local market artifacts."""
    data = build_dashboard_data(inputs)
    payload = data["summary"]
    data_path = out.parent / "dashboard-data.json"

    out.parent.mkdir(parents=True, exist_ok=True)
    if build_frontend:
        _build_svelte_frontend(out.parent)
    else:
        out.write_text(_render_dashboard(payload, data["recommendations"], inputs), encoding="utf-8")
    data_path.write_text(json.dumps(data, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    if build_frontend:
        _inject_dashboard_data(out, data)
    return DashboardResult(
        output_path=out,
        data_path=data_path,
        source_paths=tuple(path for path in inputs.source_paths() if path.exists()),
        signal_count=payload["signals"],
        feature_count=payload["features"],
        prediction_count=payload["predictions"],
    )


def build_dashboard_data(inputs: DashboardInputs) -> dict[str, Any]:
    signals = _read_json_list(inputs.signals_path)
    predictions = _read_json_list(inputs.predictions_path)
    feature_stats = _feature_stats(inputs.features_path)
    metrics = _read_json_object(inputs.metrics_path)
    backtest = _read_json_object(inputs.backtest_path)
    recommendations = build_recommendations(inputs, limit=8)
    summary = {
        "signals": len(signals),
        "features": feature_stats["rows"],
        "latest_as_of": feature_stats["latest_as_of"],
        "skin_rows": feature_stats["skin_rows"],
        "avg_price": feature_stats["avg_price"],
        "median_price": feature_stats["median_price"],
        "predictions": len(predictions),
        "metrics_rows": metrics.get("rows", 0),
        "backtest_rows": backtest.get("rows", 0),
        "backtest_precision": backtest.get("precision_at_k", 0),
        "backtest_return": backtest.get("avg_net_return_at_k", 0),
    }
    return {
        "title": "CS2 Skin Investment Research",
        "summary": summary,
        "kpis": _kpis(summary),
        "recommendations": [_recommendation_dict(rec, inputs) for rec in recommendations],
        "heatmap": _heatmap(inputs.features_path, recommendations),
        "risk_gate": _risk_gate(summary),
        "sources": {
            "signals": str(inputs.signals_path),
            "features": str(inputs.features_path),
            "predictions": str(inputs.predictions_path),
            "metrics": str(inputs.metrics_path),
            "backtest": str(inputs.backtest_path),
        },
    }


def _read_json_list(path: Path) -> list[dict[str, Any]]:
    if not path.exists():
        return []
    payload = json.loads(path.read_text(encoding="utf-8"))
    return payload if isinstance(payload, list) else []


def _read_json_object(path: Path) -> dict[str, Any]:
    if not path.exists():
        return {}
    payload = json.loads(path.read_text(encoding="utf-8"))
    return payload if isinstance(payload, dict) else {}


def _count_csv_rows(path: Path) -> int:
    if not path.exists():
        return 0
    with path.open("r", encoding="utf-8-sig", newline="") as f:
        return sum(1 for _ in csv.DictReader(f))


def _feature_stats(path: Path) -> dict[str, Any]:
    if not path.exists():
        return {"rows": 0, "skin_rows": 0, "latest_as_of": "", "avg_price": 0.0, "median_price": 0.0}
    prices: list[float] = []
    rows = 0
    skin_rows = 0
    latest_as_of = ""
    with path.open("r", encoding="utf-8-sig", newline="") as f:
        for row in csv.DictReader(f):
            rows += 1
            if row.get("category") == "skin":
                skin_rows += 1
            latest_as_of = max(latest_as_of, row.get("as_of") or "")
            price = _float(row.get("price"))
            if price > 0:
                prices.append(price)
    prices.sort()
    median = prices[len(prices) // 2] if prices else 0.0
    avg = sum(prices) / len(prices) if prices else 0.0
    return {"rows": rows, "skin_rows": skin_rows, "latest_as_of": latest_as_of, "avg_price": avg, "median_price": median}


def _build_svelte_frontend(out_dir: Path) -> None:
    frontend_dir = Path(__file__).resolve().parents[3] / "frontend"
    if not frontend_dir.exists():
        raise RuntimeError(f"Svelte frontend directory is missing: {frontend_dir}")
    npm = "npm.cmd" if os.name == "nt" else "npm"
    cmd = [npm, "run", "build", "--", "--outDir", str(out_dir.resolve())]
    try:
        subprocess.run(cmd, cwd=frontend_dir, check=True, capture_output=True, text=True)
    except FileNotFoundError as err:
        raise RuntimeError("npm is required to build the Svelte dashboard") from err
    except subprocess.CalledProcessError as err:
        output = "\n".join(part for part in (err.stdout, err.stderr) if part)
        raise RuntimeError(f"Svelte dashboard build failed:\n{output}") from err


def _inject_dashboard_data(index_path: Path, data: dict[str, Any]) -> None:
    if not index_path.exists():
        raise RuntimeError(f"Svelte build did not write dashboard index: {index_path}")
    text = index_path.read_text(encoding="utf-8")
    payload = (
        json.dumps(data, sort_keys=True)
        .replace("<", "\\u003c")
        .replace("&", "\\u0026")
        .replace("</", "<\\/")
    )
    script = f'<script id="dashboard-data" type="application/json">{payload}</script>'
    marker = '<div id="app"></div>'
    if marker not in text:
        raise RuntimeError("Svelte dashboard index is missing the app mount point")
    text = text.replace(marker, marker + "\n    " + script, 1)
    index_path.write_text(text, encoding="utf-8")


def _kpis(summary: dict[str, Any]) -> list[dict[str, str]]:
    return [
        {"label": "Signals", "value": str(summary["signals"]), "detail": "active skin recommendations"},
        {"label": "Skin rows", "value": str(summary["skin_rows"]), "detail": "normalized feature rows"},
        {"label": "Median price", "value": _money(summary["median_price"]), "detail": "current research universe"},
        {"label": "Predictions", "value": str(summary["predictions"]), "detail": "ML watchlist rows"},
        {"label": "Precision@K", "value": _pct(summary["backtest_precision"]), "detail": "latest local backtest"},
        {"label": "Net return@K", "value": _pct(summary["backtest_return"]), "detail": "after modeled fees"},
    ]


def _recommendation_dict(rec: Any, inputs: DashboardInputs) -> dict[str, Any]:
    return {
        "market_hash_name": rec.market_hash_name,
        "decision": rec.decision,
        "action": rec.action,
        "grade": rec.grade,
        "risk_level": rec.risk_level,
        "price": rec.price,
        "currency": rec.currency,
        "score": rec.score,
        "confidence": rec.confidence,
        "model_probability": rec.model_probability,
        "quantity": rec.quantity,
        "liquidity_score": rec.liquidity_score,
        "spread_ratio": rec.spread_ratio,
        "momentum_7d": rec.momentum_7d,
        "momentum_30d": rec.momentum_30d,
        "suggested_allocation": rec.suggested_allocation,
        "thesis": rec.thesis,
        "invalidation": rec.invalidation,
        "evidence": list(rec.evidence),
        "risks": list(rec.risks),
        "image": _image_for(rec.market_hash_name, inputs.assets_dir),
    }


def _heatmap(path: Path, recommendations: list[Any]) -> list[dict[str, Any]]:
    if not path.exists():
        return []
    rec_by_name = {rec.market_hash_name: rec for rec in recommendations}
    rows: list[dict[str, Any]] = []
    with path.open("r", encoding="utf-8-sig", newline="") as f:
        for row in csv.DictReader(f):
            if row.get("category") != "skin":
                continue
            name = row.get("market_hash_name") or ""
            price = _float(row.get("price"))
            quantity = _float(row.get("quantity"))
            liquidity = _float(row.get("liquidity_score"))
            spread = _float(row.get("spread_ratio"))
            momentum = _float(row.get("momentum_7d"))
            rec = rec_by_name.get(name)
            rows.append(
                {
                    "name": name,
                    "wear": row.get("wear") or "Unknown",
                    "price": price,
                    "quantity": quantity,
                    "liquidity_score": liquidity,
                    "spread_ratio": spread,
                    "momentum_7d": momentum,
                    "action": rec.action if rec else "watch",
                    "grade": rec.grade if rec else "",
                    "score": rec.score if rec else 0.0,
                    "weight": max(1.0, min(100.0, quantity or price)),
                }
            )
    rows.sort(key=lambda item: (item["score"], item["liquidity_score"], item["quantity"]), reverse=True)
    return rows[:80]


def _risk_gate(summary: dict[str, Any]) -> dict[str, Any]:
    net_return = _float(summary["backtest_return"])
    return {
        "status": "Speculative" if net_return < 0 else "Tradable research",
        "message": "Backtest net return@K is negative, so recommendations are capped as paper-trade/watchlist ideas."
        if net_return < 0
        else "Backtest is non-negative; still refresh live liquidity before execution.",
        "rules": [
            {"label": "Entry", "text": "Only consider entries near reference price with acceptable spread and visible listed quantity."},
            {"label": "Sizing", "text": "Use suggested allocation as a maximum research cap, not a mandate."},
            {"label": "Exit", "text": "Invalidate immediately when spread, liquidity, or price-chasing triggers are hit."},
            {"label": "Review", "text": "Refresh snapshots before publishing any recommendation or buying anything."},
        ],
    }


def _render_dashboard(payload: dict[str, Any], recommendations: list[Any], inputs: DashboardInputs) -> str:
    return f"""<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>CS2 Market Analytics</title>
  <style>
    :root {{
      --bg:#f4f6f8; --panel:#ffffff; --text:#111827; --muted:#64748b; --border:#d9e0e8;
      --accent:#0f766e; --blue:#2563eb; --amber:#b45309; --red:#b42318; --green:#15803d;
      --ink:#172033;
    }}
    * {{ box-sizing:border-box; }}
    body {{ margin:0; background:var(--bg); color:var(--text); font:14px/1.45 system-ui, -apple-system, "Segoe UI", sans-serif; }}
    header {{ background:#fff; border-bottom:1px solid var(--border); padding:20px 28px 18px; }}
    h1 {{ margin:0; font-size:25px; letter-spacing:0; }}
    h2 {{ margin:0 0 12px; font-size:16px; letter-spacing:0; }}
    h3 {{ margin:0 0 6px; font-size:15px; letter-spacing:0; }}
    main {{ max-width:1380px; margin:0 auto; padding:20px; }}
    .subtle {{ color:var(--muted); font-size:12px; margin-top:5px; }}
    .grid {{ display:grid; grid-template-columns:repeat(6,minmax(130px,1fr)); gap:12px; }}
    .card {{ background:var(--panel); border:1px solid var(--border); border-radius:8px; padding:14px; }}
    .card b {{ display:block; font-size:23px; line-height:1.1; margin-bottom:4px; }}
    .card span {{ color:var(--muted); font-size:12px; }}
    .section {{ margin-top:16px; }}
    .layout {{ display:grid; grid-template-columns:minmax(0,1.35fr) minmax(340px,.65fr); gap:16px; margin-top:16px; }}
    .rec-grid {{ display:grid; grid-template-columns:repeat(2,minmax(320px,1fr)); gap:12px; }}
    .rec {{ border:1px solid var(--border); border-radius:8px; padding:13px; background:#fff; display:grid; grid-template-columns:72px minmax(0,1fr); gap:12px; }}
    .rec-img {{ width:72px; height:92px; border:1px solid var(--border); border-radius:6px; background:#eef2f6; display:flex; align-items:center; justify-content:center; overflow:hidden; }}
    .rec-img img {{ width:100%; height:100%; object-fit:contain; padding:5px; }}
    .no-img {{ color:var(--muted); font-size:11px; text-align:center; }}
    .rec-top {{ display:flex; justify-content:space-between; gap:8px; align-items:start; margin-bottom:6px; }}
    .badge {{ display:inline-flex; align-items:center; justify-content:center; border-radius:999px; padding:3px 8px; font-size:11px; font-weight:700; border:1px solid transparent; }}
    .buy {{ background:#dcfce7; color:#065f46; border-color:#bbf7d0; }}
    .watch {{ background:#fef3c7; color:#92400e; border-color:#fde68a; }}
    .avoid {{ background:#fee2e2; color:#991b1b; border-color:#fecaca; }}
    .grade {{ background:#e0f2fe; color:#075985; border-color:#bae6fd; }}
    .risk {{ background:#fff7ed; color:#9a3412; border-color:#fed7aa; }}
    .decision {{ color:var(--ink); font-size:13px; font-weight:700; }}
    .rec-metrics {{ display:grid; grid-template-columns:repeat(4,1fr); gap:7px; margin:10px 0; }}
    .mini {{ background:#f8fafc; border:1px solid #e5eaf0; border-radius:6px; padding:7px; min-height:48px; }}
    .mini b {{ font-size:14px; margin:0; }}
    .mini span {{ display:block; }}
    .rec p {{ margin:7px 0; color:#344054; }}
    .evidence {{ display:flex; flex-wrap:wrap; gap:5px; margin-top:8px; }}
    .chip {{ border:1px solid var(--border); border-radius:999px; color:#475569; font-size:11px; padding:2px 7px; background:#fbfcfd; }}
    .committee {{ display:grid; gap:10px; }}
    .note {{ border-left:4px solid var(--amber); background:#fff7ed; color:#7c2d12; padding:10px 12px; border-radius:6px; }}
    .rule {{ display:grid; grid-template-columns:96px minmax(0,1fr); gap:10px; border-bottom:1px solid var(--border); padding:9px 0; }}
    .rule:last-child {{ border-bottom:0; }}
    table {{ width:100%; border-collapse:collapse; }}
    th,td {{ border-bottom:1px solid var(--border); padding:9px 8px; text-align:left; vertical-align:top; }}
    th {{ color:var(--muted); font-size:12px; }}
    code {{ background:#eef2f6; border-radius:4px; padding:2px 5px; }}
    @media (max-width:1080px) {{ .grid {{ grid-template-columns:repeat(3,1fr); }} .layout,.rec-grid {{ grid-template-columns:1fr; }} }}
    @media (max-width:760px) {{ main {{ padding:14px; }} header {{ padding:16px; }} .grid,.rec-metrics {{ grid-template-columns:1fr 1fr; }} .rec {{ grid-template-columns:1fr; }} .rec-img {{ width:100%; height:150px; }} }}
  </style>
</head>
<body>
  <header>
    <h1>CS2 Skin Investment Research</h1>
    <div class="subtle">Professional recommendation section from local market artifacts. Snapshot: {html.escape(str(payload["latest_as_of"] or "n/a"))}. Research only; no guaranteed profit or financial advice.</div>
  </header>
  <main>
    <section class="grid" aria-label="Market analytics inputs">
      {_metric("Signals", payload["signals"])}
      {_metric("Feature rows", payload["features"])}
      {_metric("Skin rows", payload["skin_rows"])}
      {_metric("Median price", _money(payload["median_price"]))}
      {_metric("Predictions", payload["predictions"])}
      {_metric("Net return@K", _pct(payload["backtest_return"]))}
    </section>

    <section class="layout">
      <div class="card">
        <h2>Investment Committee Recommendations</h2>
        <div class="rec-grid">
          {_recommendation_cards(recommendations, inputs)}
        </div>
      </div>
      <aside class="committee">
        <div class="card">
          <h2>Risk Gate</h2>
          <div class="note">Backtest net return@K is {_pct(payload["backtest_return"])}. Recommendations are capped as speculative watchlist when local backtest evidence is negative.</div>
          <div class="rule"><b>Entry</b><span>Only consider entries near reference price with acceptable spread and visible listed quantity.</span></div>
          <div class="rule"><b>Sizing</b><span>Use the suggested allocation as a maximum research cap, not a mandate.</span></div>
          <div class="rule"><b>Exit</b><span>Invalidate immediately when spread, liquidity, or price-chasing triggers are hit.</span></div>
          <div class="rule"><b>Review</b><span>Refresh snapshots before publishing any recommendation or buying anything.</span></div>
        </div>
        <div class="card">
          <h2>Sources</h2>
          <p>Signals: <code>{html.escape(str(inputs.signals_path))}</code></p>
          <p>Features: <code>{html.escape(str(inputs.features_path))}</code></p>
          <p>Predictions: <code>{html.escape(str(inputs.predictions_path))}</code></p>
        </div>
      </aside>
    </section>
  </main>
</body>
</html>
"""


def _metric(label: str, value: Any) -> str:
    return f'<div class="card"><b>{html.escape(str(value))}</b><span>{html.escape(label)}</span></div>'


def _recommendation_cards(recommendations: list[Any], inputs: DashboardInputs) -> str:
    if not recommendations:
        return '<p class="subtle">No recommendations available. Build skin signals first.</p>'
    cards = []
    for rec in recommendations:
        name = str(rec.get("market_hash_name") or "")
        image = str(rec.get("image") or _image_for(name, inputs.assets_dir))
        image_html = f'<img src="{html.escape(image)}" alt="{html.escape(name)}">' if image else '<div class="no-img">No image</div>'
        evidence = "".join(f'<span class="chip">{html.escape(str(item))}</span>' for item in rec.get("evidence", ()))
        risks = "".join(f"<li>{html.escape(str(item))}</li>" for item in rec.get("risks", [])[:2])
        if not risks:
            risks = "<li>Refresh live liquidity and spread before action.</li>"
        cards.append(
            f"""
            <article class="rec">
              <div class="rec-img">{image_html}</div>
              <div>
                <div class="rec-top">
                  <div>
                    <div class="decision">{html.escape(str(rec.get("decision") or ""))}</div>
                    <h3>{html.escape(name)}</h3>
                  </div>
                  <div><span class="badge {_action_class(str(rec.get("action") or ""))}">{html.escape(str(rec.get("action") or "").upper())}</span> <span class="badge grade">Grade {html.escape(str(rec.get("grade") or ""))}</span></div>
                </div>
                <div class="rec-metrics">
                  {_mini("Price", _money(rec.get("price"), str(rec.get("currency") or "USD")))}
                  {_mini("Score", f"{_float(rec.get("score")):.1f}")}
                  {_mini("Confidence", _pct(rec.get("confidence")))}
                  {_mini("Risk", str(rec.get("risk_level") or ""))}
                </div>
                <p><strong>Thesis:</strong> {html.escape(str(rec.get("thesis") or ""))}</p>
                <p><strong>Allocation:</strong> {html.escape(str(rec.get("suggested_allocation") or ""))}</p>
                <p><strong>Invalidation:</strong> {html.escape(str(rec.get("invalidation") or ""))}</p>
                <ul>{risks}</ul>
                <div class="evidence">{evidence}</div>
              </div>
            </article>
            """
        )
    return "".join(cards)


def _mini(label: str, value: str) -> str:
    return f'<div class="mini"><b>{html.escape(value)}</b><span>{html.escape(label)}</span></div>'


def _image_for(name: str, assets_dir: Path) -> str:
    candidate = assets_dir / f"{_slug(name)}.png"
    if not candidate.exists():
        return ""
    try:
        market_dir = assets_dir.parents[1]
        return (Path("..") / candidate.relative_to(market_dir)).as_posix()
    except ValueError:
        return candidate.as_posix()


def _slug(value: str) -> str:
    text = value.lower().replace("™", "")
    text = re.sub(r"[^a-z0-9]+", "_", text)
    return text.strip("_")


def _action_class(action: str) -> str:
    if action in {"avoid", "sell"}:
        return "avoid"
    if action == "buy":
        return "buy"
    return "watch"


def _money(value: Any, currency: str = "USD") -> str:
    return f"{currency} {_float(value):,.2f}"


def _pct(value: Any) -> str:
    return f"{_float(value) * 100:.1f}%"


def _float(value: Any) -> float:
    try:
        return float(value or 0.0)
    except (TypeError, ValueError):
        return 0.0
