from __future__ import annotations

from dataclasses import dataclass
from fnmatch import fnmatch
from pathlib import Path
from typing import Any

import yaml


DEFAULT_OPAL_URL = "https://bildungsportal.sachsen.de/opal/"


@dataclass
class OpalCredentials:
    url: str


@dataclass
class AppConfig:
    download_path: Path
    courses: list[str]
    sync: bool


@dataclass
class LoadedConfig:
    app: AppConfig
    credentials: OpalCredentials


def _load_yaml(path: Path) -> dict[str, Any]:
    if not path.exists():
        raise FileNotFoundError(f"Config file not found: {path}")
    with path.open(encoding="utf-8") as handle:
        data = yaml.safe_load(handle) or {}
    if not isinstance(data, dict):
        raise ValueError(f"Expected mapping in {path}")
    return data


def load_config(
    config_path: Path,
    secrets_path: Path,
) -> LoadedConfig:
    config_data = _load_yaml(config_path)
    secrets_data = _load_yaml(secrets_path)

    opal_url = str(secrets_data.get("opal_url", DEFAULT_OPAL_URL)).rstrip("/") + "/"
    
    download_path = Path(config_data.get("download_path", "./downloads")).expanduser()
    courses = config_data.get("courses", ["*"])
    if not isinstance(courses, list):
        raise ValueError("config.yaml: 'courses' must be a list of patterns")

    return LoadedConfig(
        app=AppConfig(
            download_path=download_path,
            courses=[str(item) for item in courses],
            sync=bool(config_data.get("sync", True)),
        ),
        credentials=OpalCredentials(
            url=opal_url,
        ),
    )


def course_matches(name: str, patterns: list[str]) -> bool:
    if not patterns or patterns == ["*"]:
        return True
    normalized = name.casefold()
    for pattern in patterns:
        if fnmatch(normalized, pattern.casefold()):
            return True
    return False
