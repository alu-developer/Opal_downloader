#!/usr/bin/env python3
"""Status line: model, context usage, rate limits.

Runs every 30s, so it must stay instant: it never fetches inline, it only reads
the cache and spawns the detached fetcher when the schedule says one is due.

The trust rules (is this number too old, has its window already rolled over)
come from ratelimit_core so there is exactly one copy of them - see the comment
at the top of that module for what happened when there were two.

Two ordering/robustness rules here, both learned the hard way:

- The fetcher is spawned BEFORE the payload is parsed. This process is the only
  thing that keeps the cache fresh, so a malformed payload must never be able to
  stop the refresh. If it could, one bad line of stdin would strand the readout
  on stale numbers indefinitely, and a status line that has silently stopped
  rendering is close to invisible.
- stdin is decoded as utf-8-sig and parsed defensively. A BOM on the input made
  json.load die on character one during verification on 2026-07-31.
"""

import json
import os
import subprocess
import sys
import time

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import ratelimit_core as core  # noqa: E402


def spawn_fetcher_if_due(cache, now):
    """DETACHED_PROCESS alone in creationflags: or-ing in CREATE_NO_WINDOW is
    rejected by Windows and the spawn then silently never happens, which is a
    very quiet way for this whole thing to stop working."""
    if now < core.next_attempt(cache, now):
        return
    try:
        kwargs = {"creationflags": 0x00000008} if os.name == "nt" else {"start_new_session": True}
        subprocess.Popen(
            [sys.executable, os.path.join(HERE, "ratelimit_fetch.py")],
            stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, stdin=subprocess.DEVNULL,
            **kwargs,
        )
    except OSError:
        pass


def read_payload():
    try:
        raw = sys.stdin.buffer.read()
    except (OSError, ValueError):
        return {}
    try:
        parsed = json.loads(raw.decode("utf-8-sig"))
    except (ValueError, UnicodeDecodeError):
        return {}
    return parsed if isinstance(parsed, dict) else {}


def rate_limit_parts(cache, now, fallback):
    """Past MAX_TRUST_AGE the cache is no longer presentable as current, so fall
    back to the CLI's own numbers marked with "?" - measured 2026-07-31, that
    block freezes after the first API response and stays frozen for the whole
    session, so it is a last resort and never a source of truth."""
    fresh = core.cache_age(cache, now) <= core.MAX_TRUST_AGE
    parts = []
    for label, key in (("5h", "five_hour"), ("7d", "seven_day")):
        if fresh:
            info = core.read_window(cache, key, now)
            if info["state"] == "ok":
                parts.append("%s %.0f%%" % (label, info["percent"]))
            elif info["state"] == "rolled_over":
                # The window ended after this reading was taken, so usage has
                # since reset. The old number here would read as "nearly out"
                # when the opposite is true.
                parts.append("%s --" % label)
        else:
            percent = core.as_number((fallback.get(key) or {}).get("used_percentage"))
            if percent is not None:
                parts.append("%s %.0f%%?" % (label, percent))
    return parts


def main():
    cache = core.load_cache()
    now = time.time()
    spawn_fetcher_if_due(cache, now)

    data = read_payload()
    model = (data.get("model") or {}).get("display_name") or "?"
    context_percent = core.as_number((data.get("context_window") or {}).get("used_percentage"))

    parts = ["[%s]" % model,
             "ctx %.0f%%" % context_percent if context_percent is not None else "ctx --"]
    parts += rate_limit_parts(cache, now, data.get("rate_limits") or {})
    print(" | ".join(parts))
    return 0


if __name__ == "__main__":
    sys.exit(main())
