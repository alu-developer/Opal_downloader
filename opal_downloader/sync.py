from __future__ import annotations

import difflib
import json
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path

from opal_downloader.config import AppConfig, course_matches, resolve_course_folder
from opal_downloader.scraper import OpalScraper, RemoteFile


@dataclass
class SyncStats:
    downloaded: int = 0
    skipped: int = 0
    errors: int = 0


@dataclass(frozen=True)
class FileRecord:
    size: int | None
    modified: str | None


class Manifest:
    def __init__(self, path: Path) -> None:
        self.path = path
        self.files: dict[str, FileRecord] = {}
        self._load()

    def _load(self) -> None:
        if not self.path.exists():
            return
        with self.path.open(encoding="utf-8") as handle:
            data = json.load(handle)
        raw_files = data.get("files", {})
        if not isinstance(raw_files, dict):
            return
        for remote_path, record in raw_files.items():
            if not isinstance(record, dict):
                continue
            self.files[remote_path] = FileRecord(
                size=record.get("size"),
                modified=record.get("modified"),
            )

    def save(self) -> None:
        self.path.parent.mkdir(parents=True, exist_ok=True)
        payload = {
            "updated_at": datetime.now(timezone.utc).isoformat(),
            "files": {
                path: {
                    "size": record.size,
                    "modified": record.modified,
                }
                for path, record in sorted(self.files.items())
            },
        }
        with self.path.open("w", encoding="utf-8") as handle:
            json.dump(payload, handle, indent=2)


async def sync_courses(
    scraper: OpalScraper,
    config: AppConfig,
    *,
    force: bool = False,
) -> SyncStats:
    stats = SyncStats()
    manifest_path = config.download_path / ".opal-sync.manifest.json"
    manifest = Manifest(manifest_path)
    config.download_path.mkdir(parents=True, exist_ok=True)

    # Scrape OPAL for files (saved session first, interactive fallback)
    remote_files = await scraper.scrape_with_saved_session(config.courses)
    print(f"Discovered {len(remote_files)} remote files. Comparing against local manifest...")
    _print_unmatched_course_patterns(remote_files, config.courses)

    for remote_file in sorted(remote_files, key=lambda f: f.path):
        target_path = _resolve_remote_target_path(config, remote_file)
        target_key = target_path.as_posix()
        local_path = config.download_path / target_path if not target_path.is_absolute() else target_path
        previous = manifest.files.get(target_key)
        changed = force or _file_changed(remote_file, previous)

        if local_path.exists() and not changed:
            stats.skipped += 1
            continue

        try:
            local_path.parent.mkdir(parents=True, exist_ok=True)
            await scraper.download_file(remote_file.url, str(local_path))
            manifest.files[target_key] = FileRecord(
                size=remote_file.size,
                modified=remote_file.modified,
            )
            stats.downloaded += 1
            print(f"  downloaded: {target_key}")
        except Exception as exc:
            stats.errors += 1
            print(f"  error: {target_key} ({exc})")

    manifest.save()
    return stats


async def list_available_courses(scraper: OpalScraper, config: AppConfig) -> None:
    """
    List available courses by scraping OPAL.
    Uses saved session state if available, otherwise requires manual browser login.
    """
    print("Fetching courses from OPAL...")
    files = await scraper.scrape_with_saved_session(["*"])
    
    # Group files by course
    courses: dict[str, list[RemoteFile]] = {}
    for file in files:
        if file.course not in courses:
            courses[file.course] = []
        courses[file.course].append(file)
    
    print(f"\nFound {len(courses)} courses:\n")
    for course in sorted(courses.keys()):
        file_count = len(courses[course])
        print(f"  [{course}] ({file_count} files)")


def _file_changed(remote_file: RemoteFile, previous: FileRecord | None) -> bool:
    """Check if a remote file has changed since last sync."""
    if previous is None:
        return True
    if remote_file.size is not None and previous.size is not None and remote_file.size != previous.size:
        return True
    if remote_file.modified and previous.modified and remote_file.modified != previous.modified:
        return True
    return False


def _print_unmatched_course_patterns(remote_files: list[RemoteFile], patterns: list[str]) -> None:
    if not patterns or patterns == ["*"]:
        return

    discovered_courses = sorted({file.course for file in remote_files})
    if not discovered_courses:
        print("Warning: no courses discovered for the configured filters.")
        return

    unmatched = [pattern for pattern in patterns if not any(course_matches(name, [pattern]) for name in discovered_courses)]
    if not unmatched:
        return

    print("Warning: some configured course patterns did not match discovered courses:")
    for pattern in unmatched:
        suggestions = difflib.get_close_matches(pattern, discovered_courses, n=3, cutoff=0.25)
        if suggestions:
            print(f"  - {pattern} -> maybe: {', '.join(suggestions)}")
        else:
            print(f"  - {pattern}")


def _resolve_remote_target_path(config: AppConfig, remote_file: RemoteFile) -> Path:
    folder, is_explicit = resolve_course_folder(config, remote_file.course)
    folder_path = Path(folder)

    if is_explicit:
        return folder_path / remote_file.name

    if config.default_course_folder:
        return folder_path / remote_file.course / remote_file.name

    return folder_path / remote_file.name
