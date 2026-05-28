from __future__ import annotations

import os
import shlex
from dataclasses import dataclass
from pathlib import Path


def repo_root() -> Path:
    return Path(__file__).resolve().parents[4]


@dataclass(frozen=True)
class Settings:
    database_url: str
    firecrawl_command: tuple[str, ...]
    data_dir: Path
    output_dir: Path
    skinport_base_url: str

    @classmethod
    def from_env(cls) -> "Settings":
        root = repo_root()
        data_dir = Path(os.getenv("CS2MARKET_DATA_DIR", root / "data" / "market"))
        default_db = data_dir / "cs2market.sqlite3"
        firecrawl_command = os.getenv(
            "CS2MARKET_FIRECRAWL_COMMAND",
            "npx --yes firecrawl-cli@1.18.5",
        )
        return cls(
            database_url=os.getenv("CS2MARKET_DATABASE_URL", str(default_db)),
            firecrawl_command=tuple(shlex.split(firecrawl_command)),
            data_dir=data_dir,
            output_dir=Path(os.getenv("CS2MARKET_OUTPUT_DIR", data_dir / "shorts")),
            skinport_base_url=os.getenv("CS2MARKET_SKINPORT_BASE_URL", "https://api.skinport.com/v1"),
        )
