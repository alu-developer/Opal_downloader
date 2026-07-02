from __future__ import annotations

from dataclasses import dataclass
from fnmatch import fnmatch
from pathlib import Path
import re
from typing import Any
import unicodedata

import yaml


DEFAULT_OPAL_URL = "https://bildungsportal.sachsen.de/opal/"
DEFAULT_STATE_FILE = "~/.opal_storage_state.json"


@dataclass
class OpalCredentials:
    url: str
    state_file: Path
    browser_executable: Path | None
    browser_user_data_dir: Path | None


@dataclass
class AppConfig:
    download_path: Path
    courses: list[str]
    sync: bool
    default_course_folder: str
    course_folders: dict[str, str]


@dataclass
class LoadedConfig:
    app: AppConfig
    credentials: OpalCredentials


def load_credentials(secrets_path: Path) -> OpalCredentials:
    secrets_data = _load_yaml(secrets_path)
    opal_url = str(secrets_data.get("opal_url", DEFAULT_OPAL_URL)).rstrip("/") + "/"
    state_file_value = str(secrets_data.get("session_state_file", DEFAULT_STATE_FILE))
    state_file = Path(state_file_value).expanduser()
    browser_executable_value = str(secrets_data.get("browser_executable", "")).strip()
    browser_user_data_dir_value = str(secrets_data.get("browser_user_data_dir", "")).strip()
    browser_executable = Path(browser_executable_value).expanduser() if browser_executable_value else None
    browser_user_data_dir = Path(browser_user_data_dir_value).expanduser() if browser_user_data_dir_value else None
    return OpalCredentials(
        url=opal_url,
        state_file=state_file,
        browser_executable=browser_executable,
        browser_user_data_dir=browser_user_data_dir,
    )


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
    credentials = load_credentials(secrets_path)
    
    download_path = Path(config_data.get("download_path", "./downloads")).expanduser()
    courses = config_data.get("courses", ["*"])
    if not isinstance(courses, list):
        raise ValueError("config.yaml: 'courses' must be a list of patterns")

    default_course_folder = str(config_data.get("default_course_folder", "")).strip()
    course_folders = config_data.get("course_folders", {})
    if not isinstance(course_folders, dict):
        raise ValueError("config.yaml: 'course_folders' must be a mapping of patterns to folder names")

    return LoadedConfig(
        app=AppConfig(
            download_path=download_path,
            courses=[str(item) for item in courses],
            sync=bool(config_data.get("sync", True)),
            default_course_folder=default_course_folder,
            course_folders={str(pattern): str(folder).strip() for pattern, folder in course_folders.items()},
        ),
        credentials=credentials,
    )


def resolve_course_folder(config: AppConfig, course_name: str) -> tuple[str, bool]:
    """Return the folder for a course and whether it came from an explicit rule."""
    for pattern, folder in config.course_folders.items():
        if course_matches(course_name, [pattern]):
            return folder, True

    if config.default_course_folder:
        return config.default_course_folder, False

    return _sanitize_path_component(course_name), False


def course_matches(name: str, patterns: list[str]) -> bool:
    if not patterns or patterns == ["*"]:
        return True

    normalized = _normalize_match_text(name)
    for pattern in patterns:
        if _pattern_matches_course(normalized, pattern):
            return True
    return False


def _normalize_match_text(value: str) -> str:
    lowered = value.casefold().strip()
    decomposed = unicodedata.normalize("NFKD", lowered)
    without_diacritics = "".join(ch for ch in decomposed if not unicodedata.combining(ch))
    return " ".join(without_diacritics.split())


def _pattern_matches_course(normalized_course: str, raw_pattern: str) -> bool:
    normalized_pattern = _normalize_match_text(str(raw_pattern))
    if not normalized_pattern:
        return False

    has_glob = any(token in normalized_pattern for token in ("*", "?", "["))
    if has_glob:
        return fnmatch(normalized_course, normalized_pattern)

    return normalized_pattern in normalized_course


def _sanitize_path_component(value: str) -> str:
    cleaned = re.sub(r'[<>:"/\\|?*\x00-\x1F]', "_", value.strip())
    cleaned = re.sub(r"\s+", " ", cleaned)
    cleaned = cleaned.rstrip(". ")
    if not cleaned:
        return "unnamed"
    if cleaned.upper() in {"CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9"}:
        return f"_{cleaned}"
    return cleaned
