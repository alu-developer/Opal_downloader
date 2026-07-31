#!/usr/bin/env python3
"""Print the rate-limit readout. `ratelimit.ps1` is a thin wrapper around this.

  --force      fetch even if the cache is still fresh
  --no-fetch   show the cache as it is, never touch the network

Exits 1 when neither window could be shown, so a caller can tell "no data" from
a real reading without parsing the text.
"""

import sys
import time

import ratelimit_core as core
import ratelimit_fetch


def main(argv):
    force = "--force" in argv
    no_fetch = "--no-fetch" in argv

    now = time.time()
    cache = core.load_cache()

    if not no_fetch and (force or core.cache_age(cache, now) > core.MIN_INTERVAL):
        ratelimit_fetch.run_once(now=now, force=force)
        cache = core.load_cache()
        now = time.time()

    for line in core.render(cache, now, core.plan_name()):
        print(line)

    usable = any(core.read_window(cache, key, now)["state"] == "ok"
                 for key in ("five_hour", "seven_day"))
    return 0 if usable else 1


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
