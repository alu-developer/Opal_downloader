#!/usr/bin/env python3
"""Shared rules for reading a rate-limit snapshot and deciding what may be shown.

There is one copy of these rules on purpose. The 2026-07-31 regression happened
because "a window past its reset time is meaningless" existed twice - once in
statusline.py's pick_window, once in the autopilot hooks - and when the hooks
were deleted and the PowerShell readout was rebuilt from scratch, the rebuild
came back without the rule. It then reported 87% used against a real ~0%.

Everything this tool reads is somebody else's undocumented internal: a beta
usage endpoint, the CLI's credential file, the shape of its rate_limits
payload. None of that has a contract, so it will keep changing. The goal here
is therefore NOT "always correct" - that is not achievable - but "never
confidently wrong": every path that cannot produce a trustworthy number must
say so instead of printing a plausible one.

`now` is always passed in rather than read from the clock, so the failure paths
can be tested without waiting hours for them to occur naturally.
"""

import json
import os
from datetime import datetime, timezone

MAX_TRUST_AGE = 900   # older than this is presented as stale, never as current
MIN_INTERVAL = 300    # minimum seconds between successful fetches
MAX_BACKOFF = 3600    # never back off further than an hour
BAR_WIDTH = 20


def home():
    """Directory holding the cache and credentials.

    Overridable so tests never touch the live cache. A test that backs up and
    restores the real file is one crash away from leaving a fabricated snapshot
    behind, and this tool only has value if its output can be trusted.
    """
    override = os.environ.get("CLAUDE_RATELIMIT_DIR")
    return override or os.path.join(os.path.expanduser("~"), ".claude")


def cache_path():
    return os.path.join(home(), "rate-limit-live.json")


def creds_path():
    return os.path.join(home(), ".credentials.json")


def load_json(path):
    """Read JSON, tolerating every way this file has actually been seen broken.

    utf-8-sig matters: PowerShell 5.1's `Set-Content -Encoding utf8` writes a
    BOM, and a plain json.load then dies on the first character with an error
    that looks exactly like a missing file. That cost a debugging session.
    """
    try:
        with open(path, "r", encoding="utf-8-sig") as handle:
            return json.load(handle)
    except (OSError, ValueError):
        return {}


def load_cache():
    data = load_json(cache_path())
    return data if isinstance(data, dict) else {}


def write_cache(state):
    """Atomic, so a reader never catches a half-written snapshot."""
    path = cache_path()
    tmp = path + ".tmp"
    with open(tmp, "w", encoding="utf-8") as handle:
        json.dump(state, handle)
    os.replace(tmp, path)


def as_number(value):
    """Accept only real numbers.

    A percentage arriving as a string, a bool or NaN means the payload shape
    changed underneath us. Coercing it would turn a detectable break into a
    number on screen that looks fine, which is the exact failure this module
    exists to prevent.
    """
    if isinstance(value, bool) or not isinstance(value, (int, float)):
        return None
    number = float(value)
    if number != number or number in (float("inf"), float("-inf")):
        return None
    return number


def parse_reset(value):
    """Return an aware UTC datetime, or None if the value cannot be believed.

    Both shapes occur in the wild: the usage endpoint sends ISO-8601 strings,
    the CLI's own snapshot uses epoch seconds. A naive string is treated as UTC
    because that is what the endpoint sends.
    """
    number = as_number(value)
    if number is not None:
        try:
            return datetime.fromtimestamp(number, timezone.utc)
        except (OverflowError, OSError, ValueError):
            return None
    if not isinstance(value, str):
        return None
    text = value.strip()
    if text.endswith(("Z", "z")):  # fromisoformat only learned "Z" in 3.11
        text = text[:-1] + "+00:00"
    try:
        parsed = datetime.fromisoformat(text)
    except ValueError:
        return None
    if parsed.tzinfo is None:
        return parsed.replace(tzinfo=timezone.utc)
    return parsed.astimezone(timezone.utc)


def read_window(cache, key, now):
    """Classify one window, saying how much of it may be believed.

    States:
      missing      - not in the snapshot at all
      rolled_over  - resets_at has passed, so this describes a window that has
                     already ended and whose usage has since reset to near zero
      no_percent   - present, but the utilization was null or not a number
      ok           - safe to display

    rolled_over deliberately outranks no_percent: an expired window is not
    merely incomplete, it is about a period that no longer exists.
    """
    window = cache.get(key)
    if not isinstance(window, dict):
        return {"state": "missing", "percent": None, "reset": None, "seconds_left": None}

    percent = as_number(window.get("used_percentage"))
    reset = parse_reset(window.get("resets_at"))
    seconds_left = None
    if reset is not None:
        seconds_left = (reset - datetime.fromtimestamp(now, timezone.utc)).total_seconds()

    if seconds_left is not None and seconds_left <= 0:
        state = "rolled_over"
    elif percent is None:
        state = "no_percent"
    else:
        state = "ok"
    return {"state": state, "percent": percent, "reset": reset, "seconds_left": seconds_left}


def cache_age(cache, now):
    """Seconds since the snapshot was taken.

    A missing timestamp counts as infinitely old, and so does one from the
    future: after a clock jump the age is meaningless, and meaningless must not
    render as "just now".
    """
    fetched = as_number(cache.get("fetched_at"))
    if fetched is None:
        return float("inf")
    age = now - fetched
    if age < -120:
        return float("inf")
    return max(0.0, age)


def next_attempt(cache, now):
    """When the fetcher is next allowed to try.

    An out-of-range value here is how this tool breaks permanently and in
    silence: one corrupt write or one forward clock jump parks next_attempt
    years out, the fetcher returns early on every single run, and the readout
    serves stale numbers forever with nothing ever fetching again.

    Such a value is *rejected*, not clamped. Clamping to `now + MAX_BACKOFF`
    looks like a fix and is not one: the ceiling moves forward with every call,
    so the deadline stays permanently in the future and the fetch still never
    happens. Since the fetcher never schedules further out than MAX_BACKOFF,
    anything beyond that is corrupt, and the safe reading of corrupt is "try
    now" rather than "wait".
    """
    value = as_number(cache.get("next_attempt"))
    if value is None or value > now + MAX_BACKOFF:
        return 0.0
    return value


def bar(percent):
    """Always exactly BAR_WIDTH characters, whatever the input."""
    if percent is None:
        return "?" * BAR_WIDTH
    clamped = max(0.0, min(100.0, percent))
    filled = int(round(clamped / 100.0 * BAR_WIDTH))
    return ("#" * filled).ljust(BAR_WIDTH, ".")


def humanise(seconds):
    minutes = int(round(seconds / 60.0))
    if minutes >= 120:
        return "%.1f h" % (minutes / 60.0)
    return "%d min" % minutes


def format_window(label, info):
    if info["state"] == "missing":
        return "%-8s %s no data" % (label, "?" * BAR_WIDTH)
    if info["state"] == "rolled_over":
        return "%-8s %s rolled over %s ago - no current reading" % (
            label, "?" * BAR_WIDTH, humanise(-info["seconds_left"]))
    if info["state"] == "no_percent":
        return "%-8s %s no percentage reported" % (label, "?" * BAR_WIDTH)

    tail = ""
    if info["reset"] is not None:
        local = info["reset"].astimezone()
        tail = "  resets in %s (%s)" % (humanise(info["seconds_left"]), local.strftime("%a %H:%M"))
    percent = info["percent"]
    return "%-8s %s %5.0f%% used / %3.0f%% free%s" % (label, bar(percent), percent, 100 - percent, tail)


def render(cache, now, plan=None):
    """The full readout, as a list of lines."""
    lines = []
    age = cache_age(cache, now)
    if age == float("inf"):
        measured = "age unknown"
    elif age < 60:
        measured = "measured just now"
    else:
        measured = "measured %.0f min ago" % (age / 60.0)

    header = "plan: %s   (%s)" % (plan or "unknown", measured)
    if age > MAX_TRUST_AGE:
        header += "   STALE"
    lines.append(header)

    failures = as_number(cache.get("consecutive_failures")) or 0
    if failures > 0:
        lines.append("  warning: last %.0f fetch(es) failed (%s) - numbers below are older than they look"
                     % (failures, cache.get("last_error") or "unknown error"))

    lines.append(format_window("5-hour", read_window(cache, "five_hour", now)))
    lines.append(format_window("7-day", read_window(cache, "seven_day", now)))
    return lines


def plan_name():
    creds = load_json(creds_path())
    oauth = creds.get("claudeAiOauth") if isinstance(creds, dict) else None
    if isinstance(oauth, dict):
        return oauth.get("subscriptionType")
    return None
