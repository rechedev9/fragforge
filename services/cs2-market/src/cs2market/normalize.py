from __future__ import annotations

import re
from typing import Any

from .models import MarketItem, MarketSnapshot, isoformat


WEAR_NAMES = {
    "Factory New",
    "Minimal Wear",
    "Field-Tested",
    "Well-Worn",
    "Battle-Scarred",
}

WEAPON_PREFIXES = {
    "ak-47",
    "aug",
    "awp",
    "cz75-auto",
    "desert eagle",
    "dual berettas",
    "famas",
    "five-seven",
    "g3sg1",
    "galil ar",
    "glock-18",
    "m249",
    "m4a1-s",
    "m4a4",
    "mac-10",
    "mag-7",
    "mp5-sd",
    "mp7",
    "mp9",
    "negev",
    "nova",
    "p2000",
    "p250",
    "p90",
    "pp-bizon",
    "r8 revolver",
    "sawed-off",
    "scar-20",
    "sg 553",
    "ssg 08",
    "tec-9",
    "ump-45",
    "usp-s",
    "xm1014",
}


def infer_item(market_hash_name: str, raw: dict[str, Any] | None = None) -> MarketItem:
    raw = raw or {}
    wear = ""
    match = re.search(r"\(([^)]+)\)$", market_hash_name)
    if match and match.group(1) in WEAR_NAMES:
        wear = match.group(1)

    lower = market_hash_name.lower()
    prefix = lower.split("|", 1)[0].strip()
    if lower.startswith("sticker |") or lower.startswith("sticker slab |"):
        category = "sticker"
    elif lower.startswith("patch |"):
        category = "patch"
    elif lower.startswith("music kit |") or lower.startswith("stattrak™ music kit |"):
        category = "music_kit"
    elif "capsule" in lower:
        category = "capsule"
    elif "case" in lower and "|" not in market_hash_name:
        category = "case"
    elif lower.endswith(" pin") or " pin |" in lower:
        category = "pin"
    elif lower.startswith("agent |"):
        category = "agent"
    elif lower.startswith("★") and "gloves" in lower:
        category = "gloves"
    elif lower.startswith("★"):
        category = "knife"
    elif "|" in market_hash_name and prefix in WEAPON_PREFIXES:
        category = "skin"
    else:
        category = "unknown"

    refs = {}
    for key in ("item_page", "market_page"):
        if raw.get(key):
            refs[key] = str(raw[key])
    return MarketItem(
        market_hash_name=market_hash_name,
        category=category,
        wear=wear,
        collection=str(raw.get("collection", "")),
        source_refs=refs,
    )


def normalize_skinport_items(items: list[dict[str, Any]], captured_at: str | None = None) -> list[MarketSnapshot]:
    captured_at = captured_at or isoformat()
    snapshots: list[MarketSnapshot] = []
    for item in items:
        name = str(item.get("market_hash_name", "")).strip()
        if not name:
            continue
        price = _first_number(item, "min_price", "median_price", "mean_price", "suggested_price")
        snapshots.append(
            MarketSnapshot(
                source="skinport",
                market_hash_name=name,
                price=price,
                currency=str(item.get("currency", "USD")),
                quantity=_int_or_none(item.get("quantity")),
                min_price=_float_or_none(item.get("min_price")),
                max_price=_float_or_none(item.get("max_price")),
                mean_price=_float_or_none(item.get("mean_price")),
                median_price=_float_or_none(item.get("median_price")),
                captured_at=captured_at,
                raw=item,
            )
        )
    return snapshots


def normalize_take_skin_listing(items: list[dict[str, Any]], captured_at: str | None = None) -> list[MarketSnapshot]:
    captured_at = captured_at or isoformat()
    snapshots: list[MarketSnapshot] = []
    for item in items:
        name = str(item.get("marketHashName") or item.get("market_hash_name") or "").strip()
        if not name:
            continue
        snapshots.append(
            MarketSnapshot(
                source="take.skin",
                market_hash_name=name,
                price=_float_or_none(item.get("price")),
                currency="USD",
                quantity=_int_or_none(item.get("volume")),
                captured_at=captured_at,
                raw=item,
            )
        )
    return snapshots


def normalize_take_skin_history(payload: dict[str, Any]) -> list[MarketSnapshot]:
    name = str(payload.get("marketHashName", "")).strip()
    currency = str(payload.get("currency", "USD"))
    if not name:
        return []
    snapshots: list[MarketSnapshot] = []
    for row in payload.get("data", []):
        date = str(row.get("date", "")).strip()
        price = _float_or_none(row.get("price"))
        if not date or price is None:
            continue
        snapshots.append(
            MarketSnapshot(
                source="take.skin.history",
                market_hash_name=name,
                price=price,
                currency=currency,
                quantity=_int_or_none(row.get("volume")),
                captured_at=f"{date}T00:00:00Z",
                raw=row,
            )
        )
    return snapshots


def _first_number(data: dict[str, Any], *keys: str) -> float | None:
    for key in keys:
        value = _float_or_none(data.get(key))
        if value is not None and value > 0:
            return value
    return None


def _float_or_none(value: Any) -> float | None:
    if value is None or value == "":
        return None
    try:
        return float(value)
    except (TypeError, ValueError):
        return None


def _int_or_none(value: Any) -> int | None:
    if value is None or value == "":
        return None
    try:
        return int(value)
    except (TypeError, ValueError):
        return None
