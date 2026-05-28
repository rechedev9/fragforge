from __future__ import annotations

from dataclasses import asdict, dataclass, field
from datetime import UTC, datetime
from typing import Any, Literal


Action = Literal["buy", "sell", "watch", "avoid"]


def now_utc() -> datetime:
    return datetime.now(UTC).replace(microsecond=0)


def isoformat(dt: datetime | None = None) -> str:
    return (dt or now_utc()).isoformat().replace("+00:00", "Z")


def parse_time(raw: str) -> datetime:
    return datetime.fromisoformat(raw.replace("Z", "+00:00"))


@dataclass(frozen=True)
class MarketItem:
    market_hash_name: str
    category: str = "unknown"
    wear: str = ""
    collection: str = ""
    source_refs: dict[str, str] = field(default_factory=dict)

    def to_dict(self) -> dict[str, Any]:
        return asdict(self)


@dataclass(frozen=True)
class MarketSnapshot:
    source: str
    market_hash_name: str
    price: float | None
    currency: str
    quantity: int | None = None
    min_price: float | None = None
    max_price: float | None = None
    mean_price: float | None = None
    median_price: float | None = None
    captured_at: str = field(default_factory=isoformat)
    raw: dict[str, Any] = field(default_factory=dict)

    def to_dict(self) -> dict[str, Any]:
        return asdict(self)


@dataclass(frozen=True)
class Signal:
    id: int | None
    market_hash_name: str
    action: Action
    score: float
    confidence: float
    horizon: str
    price: float | None
    currency: str
    reasons: list[str]
    risks: list[str]
    generated_at: str = field(default_factory=isoformat)

    def to_dict(self) -> dict[str, Any]:
        return asdict(self)


@dataclass(frozen=True)
class ShortScript:
    signal_id: int | None
    market_hash_name: str
    action: Action
    title: str
    hook: str
    narration: str
    caption: str
    disclaimer: str
    gpt_image_prompt: str
    skin_image_path: str = ""
    asset_manifest_path: str = ""
    generated_at: str = field(default_factory=isoformat)

    def to_dict(self) -> dict[str, Any]:
        return asdict(self)
