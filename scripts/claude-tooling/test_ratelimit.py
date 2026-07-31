#!/usr/bin/env python3
"""Tests for the rate-limit readout.

Run:  python C:\\Users\\alois\\.claude\\test_ratelimit.py

These deliberately spend almost all their effort on the paths that only happen
on a bad day. Every previous break in this tool (five in eleven days) was found
by the maintainer noticing a wrong number, never by a test, because the only
thing ever exercised was the happy path - which is by definition the state that
was just fixed.

The governing rule is not "the number is correct" - it cannot be, the inputs are
undocumented externals that change without notice - but "a number that cannot be
trusted is never printed as if it could be".

CLAUDE_RATELIMIT_DIR points every module at a temp directory, so no test can
touch the live cache or the real credentials.
"""

import json
import os
import shutil
import subprocess
import sys
import tempfile
import unittest
import urllib.error
from datetime import datetime, timedelta, timezone

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)

TMP = tempfile.mkdtemp(prefix="ratelimit-test-")
os.environ["CLAUDE_RATELIMIT_DIR"] = TMP

import ratelimit_core as core  # noqa: E402
import ratelimit_fetch  # noqa: E402


NOW = 1_800_000_000.0


def iso(**delta):
    return (datetime.fromtimestamp(NOW, timezone.utc) + timedelta(**delta)).isoformat()


def write_cache(**payload):
    with open(core.cache_path(), "w", encoding="utf-8") as handle:
        json.dump(payload, handle)


def snapshot(five=41.0, seven=87.0, age=10.0, **extra):
    payload = {
        "fetched_at": NOW - age,
        "next_attempt": NOW + core.MIN_INTERVAL,
        "consecutive_failures": 0,
        "five_hour": {"used_percentage": five, "resets_at": iso(hours=1)},
        "seven_day": {"used_percentage": seven, "resets_at": iso(hours=9)},
    }
    payload.update(extra)
    return payload


class CacheReading(unittest.TestCase):
    """Every way this file has actually been found broken."""

    def setUp(self):
        for name in os.listdir(TMP):
            os.unlink(os.path.join(TMP, name))

    def test_missing_file(self):
        self.assertEqual(core.load_cache(), {})

    def test_empty_file(self):
        open(core.cache_path(), "w").close()
        self.assertEqual(core.load_cache(), {})

    def test_malformed_json(self):
        with open(core.cache_path(), "w") as handle:
            handle.write("{not json")
        self.assertEqual(core.load_cache(), {})

    def test_utf8_bom(self):
        """PowerShell 5.1 writes a BOM; a plain json.load dies on character one
        and the failure is indistinguishable from a missing file."""
        with open(core.cache_path(), "w", encoding="utf-8-sig") as handle:
            json.dump({"fetched_at": NOW}, handle)
        self.assertEqual(core.load_cache().get("fetched_at"), NOW)

    def test_top_level_not_an_object(self):
        with open(core.cache_path(), "w") as handle:
            json.dump([1, 2, 3], handle)
        self.assertEqual(core.load_cache(), {})


class NumberGuard(unittest.TestCase):
    def test_rejects_strings_bools_and_nan(self):
        for value in ("41", True, False, None, float("nan"), float("inf"), [], {}):
            self.assertIsNone(core.as_number(value), repr(value))

    def test_accepts_ints_and_floats_including_zero(self):
        self.assertEqual(core.as_number(0), 0.0)
        self.assertEqual(core.as_number(41), 41.0)
        self.assertEqual(core.as_number(-3.5), -3.5)


class ResetParsing(unittest.TestCase):
    def test_iso_with_offset(self):
        parsed = core.parse_reset("2026-07-31T20:40:00+00:00")
        self.assertEqual(parsed.year, 2026)
        self.assertEqual(parsed.tzinfo, timezone.utc)

    def test_iso_with_z_suffix(self):
        """fromisoformat only learned "Z" in 3.11 and this must not depend on
        which Python happens to be on PATH."""
        self.assertEqual(core.parse_reset("2026-07-31T20:40:00Z"),
                         core.parse_reset("2026-07-31T20:40:00+00:00"))

    def test_naive_string_treated_as_utc(self):
        self.assertEqual(core.parse_reset("2026-07-31T20:40:00").tzinfo, timezone.utc)

    def test_epoch_seconds(self):
        """The CLI's own snapshot uses epoch numbers, not ISO strings."""
        self.assertEqual(core.parse_reset(NOW), datetime.fromtimestamp(NOW, timezone.utc))

    def test_unparseable_values_return_none(self):
        for value in ("", "not a date", "2026-13-45T99:99:99", None, [], {}, "  "):
            self.assertIsNone(core.parse_reset(value), repr(value))

    def test_absurd_epoch_does_not_crash(self):
        self.assertIsNone(core.parse_reset(1e30))


class WindowTrust(unittest.TestCase):
    """The rule that was lost in the rewrite, pinned down."""

    def test_healthy_window_is_ok(self):
        info = core.read_window(snapshot(), "five_hour", NOW)
        self.assertEqual(info["state"], "ok")
        self.assertEqual(info["percent"], 41.0)

    def test_expired_window_is_rolled_over(self):
        cache = snapshot()
        cache["five_hour"]["resets_at"] = iso(minutes=-40)
        self.assertEqual(core.read_window(cache, "five_hour", NOW)["state"], "rolled_over")

    def test_window_expiring_exactly_now_is_rolled_over(self):
        cache = snapshot()
        cache["five_hour"]["resets_at"] = iso(seconds=0)
        self.assertEqual(core.read_window(cache, "five_hour", NOW)["state"], "rolled_over")

    def test_window_one_second_out_is_still_ok(self):
        cache = snapshot()
        cache["five_hour"]["resets_at"] = iso(seconds=1)
        self.assertEqual(core.read_window(cache, "five_hour", NOW)["state"], "ok")

    def test_both_windows_can_roll_over_independently(self):
        cache = snapshot()
        cache["five_hour"]["resets_at"] = iso(minutes=-5)
        self.assertEqual(core.read_window(cache, "five_hour", NOW)["state"], "rolled_over")
        self.assertEqual(core.read_window(cache, "seven_day", NOW)["state"], "ok")

    def test_expired_beats_missing_percentage(self):
        """An expired window is not merely incomplete - it is about a period
        that no longer exists, and that is the more important thing to say."""
        cache = snapshot()
        cache["five_hour"] = {"used_percentage": None, "resets_at": iso(minutes=-5)}
        self.assertEqual(core.read_window(cache, "five_hour", NOW)["state"], "rolled_over")

    def test_null_percentage(self):
        cache = snapshot()
        cache["five_hour"]["used_percentage"] = None
        self.assertEqual(core.read_window(cache, "five_hour", NOW)["state"], "no_percent")

    def test_percentage_as_string_is_refused(self):
        cache = snapshot()
        cache["five_hour"]["used_percentage"] = "41"
        self.assertEqual(core.read_window(cache, "five_hour", NOW)["state"], "no_percent")

    def test_missing_window(self):
        cache = snapshot()
        del cache["five_hour"]
        self.assertEqual(core.read_window(cache, "five_hour", NOW)["state"], "missing")

    def test_window_of_wrong_type(self):
        self.assertEqual(core.read_window({"five_hour": "nope"}, "five_hour", NOW)["state"], "missing")

    def test_missing_reset_still_usable(self):
        """No countdown is a smaller loss than no reading."""
        cache = snapshot()
        cache["five_hour"] = {"used_percentage": 41.0}
        info = core.read_window(cache, "five_hour", NOW)
        self.assertEqual(info["state"], "ok")
        self.assertIsNone(info["seconds_left"])


class Ageing(unittest.TestCase):
    def test_fresh(self):
        self.assertAlmostEqual(core.cache_age(snapshot(age=42), NOW), 42)

    def test_missing_timestamp_is_infinitely_old(self):
        self.assertEqual(core.cache_age({}, NOW), float("inf"))

    def test_timestamp_from_the_future_is_not_fresh(self):
        """After a clock jump the age is meaningless, and meaningless must not
        render as "just now"."""
        self.assertEqual(core.cache_age({"fetched_at": NOW + 7200}, NOW), float("inf"))

    def test_small_forward_skew_is_tolerated(self):
        self.assertEqual(core.cache_age({"fetched_at": NOW + 30}, NOW), 0.0)

    def test_out_of_range_next_attempt_is_rejected(self):
        """An unbounded next_attempt is how this breaks permanently and in
        silence: nothing would ever fetch again. Beyond MAX_BACKOFF it can only
        be corrupt, and corrupt has to mean "try now", not "wait"."""
        self.assertEqual(core.next_attempt({"next_attempt": NOW + 10 ** 9}, NOW), 0.0)

    def test_next_attempt_at_the_ceiling_is_kept(self):
        self.assertEqual(core.next_attempt({"next_attempt": NOW + core.MAX_BACKOFF}, NOW),
                         NOW + core.MAX_BACKOFF)

    def test_next_attempt_in_the_past_allows_a_fetch(self):
        self.assertEqual(core.next_attempt({"next_attempt": NOW - 500}, NOW), NOW - 500)

    def test_missing_next_attempt_allows_a_fetch(self):
        self.assertEqual(core.next_attempt({}, NOW), 0.0)

    def test_garbage_next_attempt_allows_a_fetch(self):
        self.assertEqual(core.next_attempt({"next_attempt": "soon"}, NOW), 0.0)


class Rendering(unittest.TestCase):
    def test_bar_is_always_exactly_twenty_characters(self):
        for percent in (None, -50, -0.4, 0, 0.1, 41, 99.6, 100, 137.5):
            self.assertEqual(len(core.bar(percent)), core.BAR_WIDTH, repr(percent))

    def test_bar_endpoints(self):
        self.assertEqual(core.bar(0), "." * 20)
        self.assertEqual(core.bar(100), "#" * 20)

    def test_out_of_range_percentages_do_not_crash(self):
        for percent in (-5.0, 137.5):
            cache = snapshot(five=percent)
            line = core.format_window("5-hour", core.read_window(cache, "five_hour", NOW))
            self.assertIn("used", line)

    def test_rolled_over_window_prints_no_percentage(self):
        """The 2026-07-31 regression, pinned. It showed "87% used ... resets in
        -40 min" when real usage had reset to ~0."""
        cache = snapshot(five=87.0)
        cache["five_hour"]["resets_at"] = iso(minutes=-40)
        line = core.format_window("5-hour", core.read_window(cache, "five_hour", NOW))
        self.assertNotIn("87", line)
        self.assertNotIn("%", line)
        self.assertIn("rolled over", line)

    def test_no_line_ever_shows_a_negative_countdown(self):
        for offset in (-1, -40, -60 * 24):
            cache = snapshot()
            cache["five_hour"]["resets_at"] = iso(minutes=offset)
            for line in core.render(cache, NOW, "pro"):
                self.assertNotIn("resets in -", line)

    def test_stale_cache_is_labelled(self):
        lines = core.render(snapshot(age=4000), NOW, "pro")
        self.assertIn("STALE", lines[0])

    def test_fresh_cache_is_not_labelled_stale(self):
        self.assertNotIn("STALE", core.render(snapshot(age=30), NOW, "pro")[0])

    def test_failure_warning_names_the_error(self):
        lines = core.render(snapshot(consecutive_failures=3, last_error="HTTP 401"), NOW, "pro")
        self.assertTrue(any("HTTP 401" in line for line in lines))

    def test_empty_cache_renders_without_crashing(self):
        lines = core.render({}, NOW, None)
        self.assertEqual(len(lines), 3)
        self.assertTrue(all("no data" in line for line in lines[1:]))

    def test_hours_and_minutes_thresholds(self):
        self.assertEqual(core.humanise(90 * 60), "90 min")
        self.assertEqual(core.humanise(120 * 60), "2.0 h")


class Fetching(unittest.TestCase):
    def setUp(self):
        for name in os.listdir(TMP):
            os.unlink(os.path.join(TMP, name))

    def test_respects_the_schedule(self):
        write_cache(**snapshot(next_attempt=NOW + 200))
        calls = []
        ratelimit_fetch.run_once(now=NOW, fetcher=lambda now: calls.append(now))
        self.assertEqual(calls, [])

    def test_force_overrides_the_schedule(self):
        write_cache(**snapshot(next_attempt=NOW + 10 ** 9))
        ratelimit_fetch.run_once(
            now=NOW, force=True,
            fetcher=lambda now: {"five_hour": {"utilization": 12, "resets_at": iso(hours=1)}})
        self.assertEqual(core.load_cache()["five_hour"]["used_percentage"], 12.0)

    def test_poisoned_next_attempt_is_ignored_not_clamped(self):
        """A next_attempt beyond MAX_BACKOFF is corrupt and must not delay the
        fetch at all. Clamping it to `now + MAX_BACKOFF` looks like a fix but
        moves the ceiling forward on every call, so the deadline stays in the
        future forever and nothing ever fetches again - the tool would sit there
        serving stale numbers with no way to notice."""
        write_cache(**snapshot(next_attempt=NOW + 10 ** 9))
        ratelimit_fetch.run_once(
            now=NOW,
            fetcher=lambda now: {"five_hour": {"utilization": 5, "resets_at": iso(hours=1)}})
        self.assertEqual(core.load_cache()["five_hour"]["used_percentage"], 5.0)

    def test_legitimate_next_attempt_at_the_ceiling_is_respected(self):
        write_cache(**snapshot(next_attempt=NOW + core.MAX_BACKOFF))
        calls = []
        ratelimit_fetch.run_once(now=NOW, fetcher=lambda now: calls.append(now))
        self.assertEqual(calls, [])

    def _fail_with(self, exc, state=None):
        write_cache(**(state or snapshot(next_attempt=0)))
        def boom(now):
            raise exc
        ratelimit_fetch.run_once(now=NOW, fetcher=boom)
        return core.load_cache()

    def test_failure_keeps_the_last_known_values(self):
        cache = self._fail_with(urllib.error.HTTPError("u", 500, "err", {}, None))
        self.assertEqual(cache["five_hour"]["used_percentage"], 41.0)
        self.assertEqual(cache["fetched_at"], NOW - 10)

    def test_401_retries_soon_rather_than_backing_off_for_an_hour(self):
        cache = self._fail_with(urllib.error.HTTPError("u", 401, "unauth", {}, None))
        self.assertEqual(cache["next_attempt"], NOW + 120)
        self.assertEqual(cache["last_error"], "HTTP 401")

    def test_429_honours_retry_after_when_it_is_longer(self):
        cache = self._fail_with(
            urllib.error.HTTPError("u", 429, "slow down", {"Retry-After": "1800"}, None))
        self.assertEqual(cache["next_attempt"], NOW + 1800)

    def test_429_without_retry_after_backs_off_exponentially(self):
        cache = self._fail_with(urllib.error.HTTPError("u", 429, "slow down", {}, None))
        self.assertEqual(cache["next_attempt"], NOW + core.MIN_INTERVAL * 2)

    def test_garbage_retry_after_does_not_crash(self):
        cache = self._fail_with(
            urllib.error.HTTPError("u", 429, "slow", {"Retry-After": "Wed, 21 Oct"}, None))
        self.assertGreater(cache["next_attempt"], NOW)

    def test_backoff_is_capped(self):
        cache = self._fail_with(OSError("offline"),
                                state=snapshot(next_attempt=0, consecutive_failures=20))
        self.assertEqual(cache["next_attempt"], NOW + core.MAX_BACKOFF)

    def test_network_error_is_named_with_its_message(self):
        self.assertEqual(self._fail_with(OSError("offline"))["last_error"], "OSError: offline")

    def test_error_without_a_message_still_names_its_type(self):
        self.assertEqual(self._fail_with(OSError())["last_error"], "OSError")

    def test_success_clears_the_failure_counter(self):
        write_cache(**snapshot(next_attempt=0, consecutive_failures=4, last_error="HTTP 429"))
        ratelimit_fetch.run_once(
            now=NOW, fetcher=lambda now: {"five_hour": {"utilization": 7, "resets_at": iso(hours=1)}})
        self.assertEqual(core.load_cache()["consecutive_failures"], 0)

    def test_renamed_payload_field_counts_as_a_failure(self):
        """If the endpoint renames `utilization`, the old code wrote an empty
        snapshot that rendered as a confident "no data" and lost the last real
        reading. It has to look like the break it is."""
        write_cache(**snapshot(next_attempt=0))
        ratelimit_fetch.run_once(now=NOW, fetcher=lambda now: {"five_hour": {"usage_pct": 41}})
        cache = core.load_cache()
        self.assertEqual(cache["five_hour"]["used_percentage"], 41.0)
        self.assertIn("no usable window", cache["last_error"])

    def test_null_utilization_counts_as_a_failure(self):
        write_cache(**snapshot(next_attempt=0))
        ratelimit_fetch.run_once(
            now=NOW, fetcher=lambda now: {"five_hour": {"utilization": None},
                                          "seven_day": {"utilization": None}})
        self.assertEqual(core.load_cache()["consecutive_failures"], 1)

    def test_partial_response_keeps_what_arrived(self):
        write_cache(**snapshot(next_attempt=0))
        ratelimit_fetch.run_once(
            now=NOW, fetcher=lambda now: {"five_hour": {"utilization": 9, "resets_at": iso(hours=1)}})
        cache = core.load_cache()
        self.assertEqual(cache["five_hour"]["used_percentage"], 9.0)
        self.assertNotIn("seven_day", cache)

    def test_non_dict_response_counts_as_a_failure(self):
        write_cache(**snapshot(next_attempt=0))
        ratelimit_fetch.run_once(now=NOW, fetcher=lambda now: "<html>login</html>")
        self.assertEqual(core.load_cache()["consecutive_failures"], 1)

    def test_held_lock_prevents_a_concurrent_fetch(self):
        write_cache(**snapshot(next_attempt=0))
        path = ratelimit_fetch.lock_path()
        open(path, "w").close()
        os.utime(path, (NOW, NOW))  # the lock's age is measured against `now`
        calls = []
        ratelimit_fetch.run_once(now=NOW, fetcher=lambda now: calls.append(now))
        self.assertEqual(calls, [])

    def test_stale_lock_is_broken(self):
        """A fetch killed mid-flight must not disable the tool forever."""
        write_cache(**snapshot(next_attempt=0))
        path = ratelimit_fetch.lock_path()
        open(path, "w").close()
        old = os.path.getmtime(path) - 3600
        os.utime(path, (old, old))
        ratelimit_fetch.run_once(
            now=NOW, fetcher=lambda now: {"five_hour": {"utilization": 3, "resets_at": iso(hours=1)}})
        self.assertEqual(core.load_cache()["five_hour"]["used_percentage"], 3.0)

    def test_lock_is_released_after_a_failure(self):
        self._fail_with(OSError("offline"))
        self.assertFalse(os.path.exists(ratelimit_fetch.lock_path()))


class EndToEnd(unittest.TestCase):
    """Through the real ratelimit.ps1, with --no-fetch so nothing touches the
    network. This is the artifact the maintainer actually runs."""

    def setUp(self):
        if not shutil.which("powershell"):
            self.skipTest("powershell not available")
        for name in os.listdir(TMP):
            os.unlink(os.path.join(TMP, name))

    def run_script(self):
        result = subprocess.run(
            ["powershell", "-NoProfile", "-File", os.path.join(HERE, "ratelimit.ps1"), "-NoFetch"],
            capture_output=True, text=True, env=dict(os.environ, CLAUDE_RATELIMIT_DIR=TMP), timeout=120)
        return result.stdout

    def test_healthy_cache_shows_both_windows(self):
        now = __import__("time").time()
        write_cache(fetched_at=now - 5, next_attempt=now + 300, consecutive_failures=0,
                    five_hour={"used_percentage": 41.0,
                               "resets_at": (datetime.now(timezone.utc) + timedelta(hours=1)).isoformat()},
                    seven_day={"used_percentage": 87.0,
                               "resets_at": (datetime.now(timezone.utc) + timedelta(hours=9)).isoformat()})
        out = self.run_script()
        self.assertIn("41% used", out)
        self.assertIn("87% used", out)
        self.assertNotIn("resets in -", out)

    def test_rolled_over_window_shows_no_number(self):
        now = __import__("time").time()
        write_cache(fetched_at=now - 10800, next_attempt=now + 3600, consecutive_failures=3,
                    last_error="HTTP 401",
                    five_hour={"used_percentage": 87.0,
                               "resets_at": (datetime.now(timezone.utc) - timedelta(minutes=40)).isoformat()},
                    seven_day={"used_percentage": 87.0,
                               "resets_at": (datetime.now(timezone.utc) + timedelta(hours=9)).isoformat()})
        out = self.run_script()
        self.assertIn("rolled over", out)
        self.assertIn("STALE", out)
        self.assertIn("HTTP 401", out)
        self.assertNotIn("resets in -", out)

    def test_missing_cache_says_no_data(self):
        self.assertIn("no data", self.run_script())


class StatusLine(unittest.TestCase):
    """The status line is the only thing that keeps the cache fresh, so it must
    survive any input at all. If it crashes, no fetcher is ever spawned and the
    readout quietly strands on stale numbers - and a status line that has
    stopped rendering is close to invisible."""

    def setUp(self):
        for name in os.listdir(TMP):
            os.unlink(os.path.join(TMP, name))
        now = __import__("time").time()
        write_cache(fetched_at=now - 5, next_attempt=now + 300, consecutive_failures=0,
                    five_hour={"used_percentage": 41.0,
                               "resets_at": (datetime.now(timezone.utc) + timedelta(hours=1)).isoformat()},
                    seven_day={"used_percentage": 87.0,
                               "resets_at": (datetime.now(timezone.utc) + timedelta(hours=9)).isoformat()})

    def run_line(self, payload):
        result = subprocess.run(
            [sys.executable, os.path.join(HERE, "statusline.py")],
            input=payload, capture_output=True,
            env=dict(os.environ, CLAUDE_RATELIMIT_DIR=TMP), timeout=60)
        self.assertEqual(result.returncode, 0, result.stderr.decode(errors="replace"))
        return result.stdout.decode(errors="replace")

    def test_normal_payload(self):
        out = self.run_line(b'{"model":{"display_name":"Opus 5"},"context_window":{"used_percentage":42}}')
        self.assertIn("Opus 5", out)
        self.assertIn("ctx 42%", out)
        self.assertIn("5h 41%", out)
        self.assertIn("7d 87%", out)

    def test_payload_with_bom(self):
        self.assertIn("5h 41%", self.run_line(b"\xef\xbb\xbf" + b'{"model":{"display_name":"O"}}'))

    def test_malformed_payload_still_renders(self):
        self.assertIn("5h 41%", self.run_line(b"{not json at all"))

    def test_empty_stdin_still_renders(self):
        self.assertIn("5h 41%", self.run_line(b""))

    def test_payload_that_is_not_an_object(self):
        self.assertIn("5h 41%", self.run_line(b'["unexpected"]'))

    def test_nulled_fields_do_not_crash(self):
        out = self.run_line(b'{"model":null,"context_window":null,"rate_limits":null}')
        self.assertIn("ctx --", out)

    def test_rolled_over_window_shows_no_number(self):
        now = __import__("time").time()
        write_cache(fetched_at=now - 5, next_attempt=now + 300, consecutive_failures=0,
                    five_hour={"used_percentage": 87.0,
                               "resets_at": (datetime.now(timezone.utc) - timedelta(minutes=5)).isoformat()},
                    seven_day={"used_percentage": 87.0,
                               "resets_at": (datetime.now(timezone.utc) + timedelta(hours=9)).isoformat()})
        out = self.run_line(b'{"model":{"display_name":"O"}}')
        self.assertIn("5h --", out)
        self.assertNotIn("5h 87%", out)

    def test_stale_cache_falls_back_to_the_cli_numbers_marked(self):
        now = __import__("time").time()
        write_cache(fetched_at=now - 99999, next_attempt=now + 300,
                    five_hour={"used_percentage": 41.0,
                               "resets_at": (datetime.now(timezone.utc) + timedelta(hours=1)).isoformat()})
        out = self.run_line(b'{"rate_limits":{"five_hour":{"used_percentage":26}}}')
        self.assertIn("5h 26%?", out)


if __name__ == "__main__":
    try:
        unittest.main(verbosity=2, exit=False)
    finally:
        shutil.rmtree(TMP, ignore_errors=True)
