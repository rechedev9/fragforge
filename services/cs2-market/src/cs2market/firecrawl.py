from __future__ import annotations

import json
import subprocess
from dataclasses import dataclass
from pathlib import Path
from typing import Any


@dataclass(frozen=True)
class FirecrawlCLI:
    command: tuple[str, ...]
    timeout_seconds: int = 90

    def scrape(
        self,
        url: str,
        *,
        formats: str = "markdown,json",
        schema: dict[str, Any] | None = None,
        output: Path | None = None,
    ) -> dict[str, Any]:
        args = [
            *self.command,
            "scrape",
            url,
            "--format",
            formats,
            "--only-main-content",
            "--json",
        ]
        if schema:
            args.extend(["--schema", json.dumps(schema)])
        if output:
            output.parent.mkdir(parents=True, exist_ok=True)
            args.extend(["--output", str(output)])
        return self._run_json(args)

    def search(self, query: str, *, limit: int = 5, output: Path | None = None) -> dict[str, Any]:
        args = [*self.command, "search", query, "--limit", str(limit), "--json"]
        if output:
            output.parent.mkdir(parents=True, exist_ok=True)
            args.extend(["--output", str(output)])
        return self._run_json(args)

    def _run_json(self, args: list[str]) -> dict[str, Any]:
        proc = subprocess.run(
            args,
            check=False,
            capture_output=True,
            text=True,
            timeout=self.timeout_seconds,
        )
        if proc.returncode != 0:
            raise RuntimeError(f"firecrawl failed ({proc.returncode}): {proc.stderr.strip()}")
        text = proc.stdout.strip()
        if not text:
            return {}
        try:
            return json.loads(text)
        except json.JSONDecodeError as err:
            raise RuntimeError(f"firecrawl returned non-json output: {text[:200]}") from err
