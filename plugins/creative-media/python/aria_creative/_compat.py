"""Standalone config path helper so aria_creative has zero dependency on aria-code."""
from __future__ import annotations

import os
from pathlib import Path


def config_dir() -> Path:
    return Path(os.getenv("ARIA_CONFIG_DIR", str(Path.home() / ".arthera"))).expanduser()


def providers_file() -> Path:
    return config_dir() / "providers.json"
