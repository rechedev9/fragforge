from __future__ import annotations

import json
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "src"))

from fastapi.testclient import TestClient

from cs2market.api import create_app
from cs2market.config import Settings
from cs2market.render import render_short_videos
from cs2market.sources import SteamImageClient


class HTTPRenderAndSourcesTests(unittest.TestCase):
    def test_signals_route_uses_sqlite_connection_in_request_thread(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            settings = Settings(
                database_url=str(root / "market.sqlite3"),
                firecrawl_command=("npx",),
                data_dir=root / "data",
                output_dir=root / "shorts",
                skinport_base_url="https://example.test",
            )

            with TestClient(create_app(settings)) as client:
                response = client.get("/signals")

        self.assertEqual(response.status_code, 200)
        self.assertEqual(response.json(), [])

    def test_render_skips_manifest_without_skin_image_path(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            manifest_dir = root / "manifests"
            manifest_dir.mkdir()
            background = root / "background.png"
            background.write_bytes(b"not-used")
            (manifest_dir / "signal.assets.json").write_text(
                json.dumps({"artifacts": {"skin_image": ""}}),
                encoding="utf-8",
            )

            rendered = render_short_videos(manifest_dir, background=background, out_dir=root / "out")

        self.assertEqual(rendered, [])

    def test_empty_image_download_reports_item_name(self) -> None:
        class EmptyResponse:
            headers = {"Content-Type": "image/png"}

            def __enter__(self) -> "EmptyResponse":
                return self

            def __exit__(self, *args: object) -> None:
                return None

            def read(self) -> bytes:
                return b""

        with tempfile.TemporaryDirectory() as tmp:
            client = SteamImageClient(timeout_seconds=1)
            with patch("urllib.request.urlopen", return_value=EmptyResponse()):
                with self.assertRaisesRegex(RuntimeError, "AK-47"):
                    client._download_url(
                        "https://example.test/image.png",
                        Path(tmp) / "ak.png",
                        "AK-47 | Slate (Factory New)",
                    )


if __name__ == "__main__":
    unittest.main()
