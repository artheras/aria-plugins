"""Minimal standalone artifact writer for aria-creative-media."""
from __future__ import annotations

import re
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Dict, Optional

from aria_creative._compat import config_dir


@dataclass
class UserArtifact:
    path: Path
    kind: str
    topic: str


def create_user_artifact(
    kind: str,
    *,
    topic: str = "output",
    ext: str = ".bin",
    meta: Optional[Dict[str, Any]] = None,
) -> UserArtifact:
    slug = re.sub(r"[^a-zA-Z0-9_-]+", "_", str(topic or "output")).strip("_")[:48] or "output"
    suffix = ext if str(ext).startswith(".") else f".{ext}"
    root = config_dir() / "artifacts" / kind
    root.mkdir(parents=True, exist_ok=True)
    path = root / f"{int(time.time())}_{slug}{suffix}"
    return UserArtifact(path=path, kind=kind, topic=topic)
