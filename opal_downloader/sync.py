from __future__ import annotations

import asyncio
import json
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path

from opal_downloader.config import AppConfig, course_matches
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

    # Scrape OPAL for files
    remote_files = await scraper.login_and_get_courses(config.courses)
    seen_paths: set[str] = set()

    for remote_file in sorted(remote_files, key=lambda f: f.path):
        seen_paths.add(remote_file.path)
        local_path = config.download_path / remote_file.path
        previous = manifest.files.get(remote_file.path)
        changed = force or _file_changed(remote_file, previous)

        if local_path.exists() and not changed:
            stats.skipped += 1
            continue

        try:
            local_path.parent.mkdir(parents=True, exist_ok=True)
            await scraper.download_file(remote_file.url, str(local_path))
            manifest.files[remote_file.path] = FileRecord(
                size=remote_file.size,
                modified=remote_file.modified,
            )
            stats.downloaded += 1
            print(f"  downloaded: {remote_file.path}")
        except Exception as exc:
            stats.errors += 1
            print(f"  error: {remote_file.path} ({exc})")

    manifest.save()
    return stats


async def list_available_courses(scraper: OpalScraper, config: AppConfig) -> None:
    """
    List available courses by scraping OPAL.
    Requires manual login.
    """
    print("Logging in to OPAL to fetch available courses...")
    files = await scraper.login_and_get_courses(["*"])
    
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
