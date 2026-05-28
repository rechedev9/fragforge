from __future__ import annotations

import gzip
import json
import re
import urllib.parse
import urllib.request
from dataclasses import dataclass
from pathlib import Path
from typing import Any


@dataclass(frozen=True)
class SkinportClient:
    base_url: str = "https://api.skinport.com/v1"
    timeout_seconds: int = 60

    def items(self, *, currency: str = "USD", tradable: bool = True) -> list[dict[str, Any]]:
        params = urllib.parse.urlencode(
            {
                "app_id": 730,
                "currency": currency,
                "tradable": "1" if tradable else "0",
            }
        )
        url = f"{self.base_url.rstrip('/')}/items?{params}"
        request = urllib.request.Request(
            url,
            headers={
                "Accept": "application/json",
                "Accept-Encoding": "br, gzip",
                "User-Agent": "zackvideo-cs2market/0.1",
            },
        )
        with urllib.request.urlopen(request, timeout=self.timeout_seconds) as response:
            body = response.read()
            encoding = response.headers.get("Content-Encoding", "").lower()
            if encoding == "br":
                try:
                    import brotli
                except ImportError as err:
                    raise RuntimeError("Skinport /v1/items requires Brotli; install the `brotli` Python package") from err
                body = brotli.decompress(body)
            elif encoding == "gzip":
                body = gzip.decompress(body)
        data = json.loads(body.decode("utf-8"))
        if not isinstance(data, list):
            raise RuntimeError("Skinport /v1/items returned a non-list payload")
        return data


@dataclass(frozen=True)
class TakeSkinClient:
    base_url: str = "https://take.skin/api/public/v1"
    timeout_seconds: int = 45

    def skins(self, *, page: int = 0, limit: int = 100, search: str = "") -> dict[str, Any]:
        params: dict[str, Any] = {"page": page, "limit": limit}
        if search:
            params["search"] = search
        return self._get_json("/skins", params)

    def cases(self) -> dict[str, Any]:
        return self._get_json("/cases", {})

    def price_history(self, market_hash_name: str, *, days: int = 30) -> dict[str, Any]:
        encoded = urllib.parse.quote(market_hash_name, safe="")
        return self._get_json(f"/skins/{encoded}/price-history", {"days": days})

    def _get_json(self, path: str, params: dict[str, Any]) -> dict[str, Any]:
        query = urllib.parse.urlencode(params)
        url = f"{self.base_url.rstrip('/')}{path}"
        if query:
            url += f"?{query}"
        request = urllib.request.Request(
            url,
            headers={"Accept": "application/json", "User-Agent": "zackvideo-cs2market/0.1"},
        )
        with urllib.request.urlopen(request, timeout=self.timeout_seconds) as response:
            data = json.loads(response.read().decode("utf-8"))
        if not isinstance(data, dict):
            raise RuntimeError(f"Take.Skin returned a non-object payload for {path}")
        return data


@dataclass(frozen=True)
class SteamImageClient:
    base_url: str = "https://steamapis.com/image/item"
    timeout_seconds: int = 45

    def download_item_image(self, market_hash_name: str, out_dir: Path) -> Path:
        out_dir.mkdir(parents=True, exist_ok=True)
        target = out_dir / f"{_slug(market_hash_name)}.png"
        if target.exists() and target.stat().st_size > 0:
            return target
        encoded = urllib.parse.quote(market_hash_name, safe="")
        try:
            return self._download_url(f"{self.base_url.rstrip('/')}/730/{encoded}", target, market_hash_name)
        except Exception:
            image_url = self._steam_market_image_url(market_hash_name)
            return self._download_url(image_url, target, market_hash_name)

    def _download_url(self, url: str, target: Path, market_hash_name: str) -> Path:
        request = urllib.request.Request(url, headers={"User-Agent": "zackvideo-cs2market/0.1"})
        with urllib.request.urlopen(request, timeout=self.timeout_seconds) as response:
            body = response.read()
            content_type = response.headers.get("Content-Type", "").lower()
        if not body:
            raise RuntimeError(f"empty image response for {market_hash_name} from {url}")
        if "jpeg" in content_type or "jpg" in content_type:
            target = target.with_suffix(".jpg")
        elif "webp" in content_type:
            target = target.with_suffix(".webp")
        target.write_bytes(body)
        return target

    def _steam_market_image_url(self, market_hash_name: str) -> str:
        encoded = urllib.parse.quote(market_hash_name, safe="")
        url = f"https://steamcommunity.com/market/listings/730/{encoded}"
        request = urllib.request.Request(url, headers={"User-Agent": "Mozilla/5.0 zackvideo-cs2market/0.1"})
        with urllib.request.urlopen(request, timeout=self.timeout_seconds) as response:
            html = response.read().decode("utf-8", "ignore")
        matches = re.findall(r"https://community\.steamstatic\.com/economy/image/[^\"'<>\\s]+", html)
        if not matches:
            matches = re.findall(r"https://steamcommunity-a\.akamaihd\.net/economy/image/[^\"'<>\\s]+", html)
        icon_matches = re.findall(r'icon_url\\+":\\+"([^"\\]+)', html)
        matches.extend(f"https://community.steamstatic.com/economy/image/{icon}" for icon in icon_matches)
        if not matches:
            raise RuntimeError(f"no Steam CDN image found for {market_hash_name}")
        return matches[-1]


def _slug(value: str) -> str:
    out: list[str] = []
    last_underscore = False
    for char in value.lower():
        if char.isascii() and char.isalnum():
            out.append(char)
            last_underscore = False
        elif not last_underscore:
            out.append("_")
            last_underscore = True
    return "".join(out).strip("_")[:100] or "item"
