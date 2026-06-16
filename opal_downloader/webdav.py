from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime, timezone
from typing import Iterable

from webdav3.client import Client


@dataclass(frozen=True)
class RemoteEntry:
    path: str
    is_dir: bool
    size: int | None = None
    etag: str | None = None
    modified: str | None = None


class OpalWebDavClient:
    def __init__(self, url: str, username: str, password: str) -> None:
        self._client = Client(
            {
                "webdav_hostname": url,
                "webdav_login": username,
                "webdav_password": password,
                "webdav_timeout": 120,
            }
        )

    def check_connection(self) -> None:
        if not self._client.check("/"):
            raise ConnectionError("WebDAV authentication failed. Check secrets.yaml.")

    def list_dir(self, remote_path: str) -> list[RemoteEntry]:
        remote_path = remote_path.strip("/")
        listing = self._client.list(remote_path or "/", get_info=True)
        entries: list[RemoteEntry] = []
        for item in listing:
            if not isinstance(item, dict):
                continue
            name = str(item.get("name", "")).strip("/")
            if not name or name == remote_path:
                continue
            full_path = f"{remote_path}/{name}" if remote_path else name
            entries.append(
                RemoteEntry(
                    path=full_path,
                    is_dir=bool(item.get("isdir")),
                    size=_coerce_int(item.get("size")),
                    etag=_normalize_etag(item.get("etag")),
                    modified=_normalize_modified(item.get("modified")),
                )
            )
        return entries

    def walk_files(self, roots: Iterable[str]) -> list[RemoteEntry]:
        files: list[RemoteEntry] = []
        for root in roots:
            self._walk(root.strip("/"), files)
        return files

    def _walk(self, remote_path: str, files: list[RemoteEntry]) -> None:
        try:
            entries = self.list_dir(remote_path)
        except Exception:
            return
        for entry in entries:
            if entry.is_dir:
                self._walk(entry.path, files)
            else:
                files.append(entry)

    def download_file(self, remote_path: str, local_path: str) -> None:
        self._client.download_sync(remote_path=remote_path, local_path=local_path)


def _coerce_int(value: object) -> int | None:
    if value is None:
        return None
    try:
        return int(value)
    except (TypeError, ValueError):
        return None


def _normalize_etag(value: object) -> str | None:
    if value is None:
        return None
    text = str(value).strip()
    return text or None


def _normalize_modified(value: object) -> str | None:
    if value is None:
        return None
    if isinstance(value, datetime):
        if value.tzinfo is None:
            value = value.replace(tzinfo=timezone.utc)
        return value.isoformat()
    text = str(value).strip()
    return text or None
