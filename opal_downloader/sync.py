from __future__ import annotations

import json
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path

from opal_downloader.config import AppConfig, course_matches
from opal_downloader.webdav import OpalWebDavClient, RemoteEntry


@dataclass
class SyncStats:
    downloaded: int = 0
    skipped: int = 0
    deleted: int = 0
    errors: int = 0


@dataclass(frozen=True)
class FileRecord:
    etag: str | None
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
                etag=record.get("etag"),
                size=record.get("size"),
                modified=record.get("modified"),
            )

    def save(self) -> None:
        self.path.parent.mkdir(parents=True, exist_ok=True)
        payload = {
            "updated_at": datetime.now(timezone.utc).isoformat(),
            "files": {
                path: {
                    "etag": record.etag,
                    "size": record.size,
                    "modified": record.modified,
                }
                for path, record in sorted(self.files.items())
            },
        }
        with self.path.open("w", encoding="utf-8") as handle:
            json.dump(payload, handle, indent=2)


def sync_courses(
    client: OpalWebDavClient,
    config: AppConfig,
    *,
    force: bool = False,
) -> SyncStats:
    stats = SyncStats()
    manifest_path = config.download_path / ".opal-sync.manifest.json"
    manifest = Manifest(manifest_path)
    config.download_path.mkdir(parents=True, exist_ok=True)

    remote_files = _collect_remote_files(client, config)
    seen_paths: set[str] = set()

    for remote_path, entry in sorted(remote_files.items()):
        seen_paths.add(remote_path)
        local_path = config.download_path / Path(*remote_path.split("/"))
        previous = manifest.files.get(remote_path)
        changed = force or _entry_changed(entry, previous)

        if local_path.exists() and not changed:
            stats.skipped += 1
            continue

        try:
            local_path.parent.mkdir(parents=True, exist_ok=True)
            client.download_file(remote_path, str(local_path))
            manifest.files[remote_path] = FileRecord(
                etag=entry.etag,
                size=entry.size,
                modified=entry.modified,
            )
            stats.downloaded += 1
            print(f"  downloaded: {remote_path}")
        except Exception as exc:
            stats.errors += 1
            print(f"  error: {remote_path} ({exc})")

    if config.delete_removed:
        for remote_path in list(manifest.files):
            if remote_path in seen_paths:
                continue
            local_path = config.download_path / Path(*remote_path.split("/"))
            if local_path.exists():
                local_path.unlink()
            del manifest.files[remote_path]
            stats.deleted += 1
            print(f"  deleted local: {remote_path}")

    manifest.save()
    return stats


def list_available_courses(client: OpalWebDavClient, roots: list[str]) -> None:
    for root in roots:
        print(f"\n[{root}]")
        try:
            entries = client.list_dir(root)
        except Exception as exc:
            print(f"  unavailable: {exc}")
            continue
        if not entries:
            print("  (empty or no access)")
            continue
        for entry in sorted(entries, key=lambda item: item.path.casefold()):
            suffix = "/" if entry.is_dir else ""
            print(f"  {entry.path}{suffix}")


def _collect_remote_files(
    client: OpalWebDavClient,
    config: AppConfig,
) -> dict[str, RemoteEntry]:
    selected: dict[str, RemoteEntry] = {}
    filter_roots = {"coursefolders", "groupfolders"}

    for root in config.roots:
        try:
            top_level = client.list_dir(root)
        except Exception:
            continue

        for entry in top_level:
            if not entry.is_dir:
                selected[entry.path] = entry
                continue

            folder_name = entry.path.split("/")[-1]
            if root in filter_roots and not course_matches(folder_name, config.courses):
                continue

            for file_entry in client.walk_files([entry.path]):
                if not file_entry.is_dir:
                    selected[file_entry.path] = file_entry

    return selected


def _entry_changed(entry: RemoteEntry, previous: FileRecord | None) -> bool:
    if previous is None:
        return True
    if entry.etag and previous.etag and entry.etag != previous.etag:
        return True
    if entry.size is not None and previous.size is not None and entry.size != previous.size:
        return True
    if entry.modified and previous.modified and entry.modified != previous.modified:
        return True
    return False
