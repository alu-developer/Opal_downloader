from __future__ import annotations

from dataclasses import dataclass
from fnmatch import fnmatch
from pathlib import Path
from typing import Any

import yaml


DEFAULT_WEBDAV_URL = "https://bildungsportal.sachsen.de/opal/webdav"
DEFAULT_ROOTS = ("coursefolders", "groupfolders", "home")


@dataclass
class WebDavCredentials:
    url: str
    username: str
    password: str


@dataclass
class AppConfig:
    download_path: Path
    courses: list[str]
    roots: list[str]
    sync: bool
    delete_removed: bool


@dataclass
class LoadedConfig:
    app: AppConfig
    credentials: WebDavCredentials


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

    webdav = secrets_data.get("webdav", {})
    if not isinstance(webdav, dict):
        raise ValueError("secrets.yaml: 'webdav' must be a mapping")

    username = webdav.get("username", "").strip()
    password = webdav.get("password", "")
    if not username or not password:
        raise ValueError(
            "secrets.yaml must define webdav.username and webdav.password"
        )

    download_path = Path(config_data.get("download_path", "./downloads")).expanduser()
    courses = config_data.get("courses", ["*"])
    if not isinstance(courses, list):
        raise ValueError("config.yaml: 'courses' must be a list of patterns")

    roots = config_data.get("roots", list(DEFAULT_ROOTS))
    if not isinstance(roots, list) or not roots:
        raise ValueError("config.yaml: 'roots' must be a non-empty list")

    return LoadedConfig(
        app=AppConfig(
            download_path=download_path,
            courses=[str(item) for item in courses],
            roots=[str(item).strip("/") for item in roots],
            sync=bool(config_data.get("sync", True)),
            delete_removed=bool(config_data.get("delete_removed", False)),
        ),
        credentials=WebDavCredentials(
            url=str(webdav.get("url", DEFAULT_WEBDAV_URL)).rstrip("/"),
            username=username,
            password=password,
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
