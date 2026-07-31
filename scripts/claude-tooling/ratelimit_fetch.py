#!/usr/bin/env python3
"""Fetch live rate-limit utilization and cache it.

Runs detached, spawned by statusline.py when the cache is due. It must never be
called inline: the status line re-renders every 30s and an HTTP round-trip in
that path would make the whole prompt feel sluggish.

Why it exists: the `rate_limits` block Claude Code hands the status line is
documented as "only present after first API response", and measured on
2026-07-31 it then stays frozen for the rest of the session -- the snapshot was
rewritten every 30s while its numbers sat at 26%/72% for ten minutes as real
usage climbed to 39%. The OAuth usage endpoint is the only source that tracks.

That endpoint has its own rate limit and it is not generous: polling every 30s
for roughly eight minutes earned a 429 (measured the same day), and the cooldown
outlasted 20 minutes. Hence MIN_INTERVAL plus exponential backoff -- never poll
it tightly.

Run with --force to bypass the interval (manual use only).
"""

import json
import os
import subprocess
import sys
import time
import urllib.error
import urllib.request

import ratelimit_core as core

LOCK_STALE_SECONDS = 60  # a fetch that died must not block the next one forever
USAGE_URL = "https://api.anthropic.com/api/oauth/usage"


def lock_path():
    return core.cache_path() + ".lock"


def acquire_lock(now):
    path = lock_path()
    try:
        if now - os.path.getmtime(path) > LOCK_STALE_SECONDS:
            os.unlink(path)
    except OSError:
        pass
    try:
        os.close(os.open(path, os.O_CREAT | os.O_EXCL | os.O_WRONLY))
        return True
    except OSError:
        return False


def release_lock():
    try:
        os.unlink(lock_path())
    except OSError:
        pass


def refresh_token_if_expired(now):
    """The stored access token lives ~8h and nothing rewrites it on its own.

    Measured 2026-07-31: it expired at 19:02 and .credentials.json still held
    the dead token 89 minutes later, so every fetch came back 401. Any cheap
    `claude` subcommand re-authenticates and rewrites the file; `mcp list` does
    it with no inference and therefore no usage cost (measured: exit 0 in 1.4s).

    We deliberately do NOT perform the OAuth refresh ourselves: refresh tokens
    rotate, and a botched write to .credentials.json logs the user out of Claude
    Code entirely. Letting the CLI own its own credentials is the safe side of
    that trade.
    """
    creds = core.load_json(core.creds_path()).get("claudeAiOauth") or {}
    expires_at = (core.as_number(creds.get("expiresAt")) or 0) / 1000.0
    if expires_at > now + 60:
        return creds
    try:
        subprocess.run(
            ["claude", "mcp", "list"], timeout=60, shell=(os.name == "nt"),
            stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, stdin=subprocess.DEVNULL,
        )
    except (OSError, subprocess.SubprocessError):
        pass
    return core.load_json(core.creds_path()).get("claudeAiOauth") or {}


def fetch(now):
    token = refresh_token_if_expired(now).get("accessToken")
    if not token:
        raise RuntimeError("no access token in credentials")
    request = urllib.request.Request(
        USAGE_URL,
        headers={
            "Authorization": "Bearer " + token,
            "anthropic-beta": "oauth-2025-04-20",
            "Content-Type": "application/json",
        },
    )
    with urllib.request.urlopen(request, timeout=10) as response:
        return json.load(response)


def backoff_delay(failures, exc):
    delay = min(core.MIN_INTERVAL * (2 ** failures), core.MAX_BACKOFF)
    if isinstance(exc, urllib.error.HTTPError):
        if exc.code == 401:
            # Not overload - the refresh above did not take. Retry soon rather
            # than backing off for an hour: the CLI may rewrite the credentials
            # file at any moment.
            return 120
        try:
            retry_after = int(exc.headers.get("Retry-After", 0))
        except (TypeError, ValueError, AttributeError):
            retry_after = 0
        delay = max(delay, min(retry_after, core.MAX_BACKOFF))
    return delay


def describe(exc):
    """Short enough for one status line, specific enough to act on.

    The message matters: a bare "RuntimeError" tells the reader nothing, while
    "RuntimeError: no usable window in response" says the payload shape changed
    and points straight at the cause.
    """
    if isinstance(exc, urllib.error.HTTPError):
        return "HTTP %d" % exc.code
    detail = str(exc).strip()
    return "%s: %s" % (type(exc).__name__, detail) if detail else type(exc).__name__


def record_failure(state, now, exc):
    """Keep the previous values and their fetched_at; only the schedule changes.

    Ageing the old numbers out is the honest behaviour and the readout already
    does it -- overwriting them with nothing would throw away the last thing we
    actually knew.
    """
    failures = int(core.as_number(state.get("consecutive_failures")) or 0) + 1
    state["consecutive_failures"] = failures
    state["last_error"] = describe(exc)
    state["next_attempt"] = now + backoff_delay(failures, exc)
    core.write_cache(state)
    return 1


def extract(data):
    """Pull the windows we understand out of the response."""
    windows = {}
    if not isinstance(data, dict):
        return windows
    for key in ("five_hour", "seven_day"):
        window = data.get(key)
        if not isinstance(window, dict):
            continue
        percent = core.as_number(window.get("utilization"))
        if percent is None:
            continue
        windows[key] = {"used_percentage": percent, "resets_at": window.get("resets_at")}
    return windows


def run_once(now=None, force=False, fetcher=None):
    now = time.time() if now is None else now
    state = core.load_cache()

    if not force and now < core.next_attempt(state, now):
        return 0
    if not acquire_lock(now):
        return 0

    try:
        try:
            data = (fetcher or fetch)(now)
        except Exception as exc:  # noqa: BLE001 - every failure backs off alike
            return record_failure(state, now, exc)

        windows = extract(data)
        if not windows:
            # The call succeeded but carried nothing we recognise, which means
            # the payload shape changed. Treat it as a failure so the last known
            # numbers survive and the error is visible, instead of writing an
            # empty snapshot that renders as a confident "no data".
            return record_failure(state, now, RuntimeError("no usable window in response"))

        snapshot = {"fetched_at": now, "next_attempt": now + core.MIN_INTERVAL,
                    "consecutive_failures": 0}
        snapshot.update(windows)
        core.write_cache(snapshot)
        return 0
    finally:
        release_lock()


if __name__ == "__main__":
    sys.exit(run_once(force="--force" in sys.argv))
