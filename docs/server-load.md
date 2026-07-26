# Server load

opal-downloader runs against OPAL, which is operated by Bildungsportal Sachsen
for every student in the state. This project has no relationship with them, no
agreement, and no channel through which it would be told it is being a
nuisance — the first signal would be being blocked.

Raised by the maintainer on 2026-07-26: *"Auch Serverlast beachten: aufpassen,
dass wenn viele es nutzen, es nicht zu einer extremen Last für den Server wird
— das bitte langfristig einrichten, nicht nur kurzfristig prüfen."* This file
is the "langfristig einrichten" half. It is meant to be read before any change
that would make the tool ask OPAL for more, or ask faster.

## What the tool actually costs today

Measured on the maintainer's real account (6 courses, 284 sections, 345 files):

| | |
|---|---|
| Page loads per sync | ~284 (one per section) + a handful for discovery |
| Wall clock | ~210–230s serial |
| Effective request rate | **~1.2 requests/second** |
| Bytes | Only changed files are downloaded; a routine sync transfers almost nothing |

A single user syncing once a day is negligible. The question this file exists
for is what happens at 100 or 1000 users.

## The three things that actually bound the load

### 1. A rate ceiling that is not binding today (`internal/polite`)

Every navigation to OPAL passes through `OpalScraper.gotoPolitely`, which waits
on a shared `polite.Limiter`. The default is one request per 250ms — about 4/s,
roughly **three times looser than what the crawl does on its own**.

That looseness is deliberate, and it is the part most likely to be
misunderstood as an oversight:

- A limiter that binds during normal operation makes every future performance
  measurement a measurement of the limiter. That is how a safety limit quietly
  becomes the thing everyone tunes around instead of respecting.
- The ceiling's job is not to slow today's tool down. It is to make sure a
  *future* change cannot speed it up past a defensible rate by accident. Raising
  it has to be a decision somebody makes on purpose, in this file.

The limiter is shared across every browser tab, because OPAL cannot tell which
of this program's tabs a request came from, and neither should the ceiling.

**This interacts directly with `docs/sync-speed-campaign.md`**, which is a
standing effort to make syncs dramatically faster. Those two goals genuinely
pull against each other, and the honest position is:

> Making one user's sync finish sooner by doing the same work in less time is
> fine and does not increase total load. Making it finish sooner by asking for
> more things at once does increase peak load, and is bounded by the ceiling.

Every concurrency axis tried so far has been rejected on *correctness* grounds
anyway (silent file loss — see the campaign doc), so nothing has yet come close
to the ceiling. If something does, that is the moment to revisit this number
rather than to route around it.

### 2. Backing off when OPAL says it is struggling

A `429` or `503` steps the limiter's spacing up (2s → 10s → 30s → 60s between
requests); a clean response steps it back down. A transport error is
deliberately **not** treated as overload — a dropped connection says nothing
about the server's capacity, and backing off on flaky wifi would turn a bad
network into an ever-slower sync.

Backoff is also logged as a warning, so a sync that is slow because it is being
throttled does not look like a sync that is slow because the tool is bad.

### 3. Not all arriving at once (`scheduler.SuggestedTime`)

This is the highest-leverage item on the list and the cheapest.

The daily schedule used to propose `06:00` to everybody. A few hundred
installations all starting several hundred page loads on the same tick is a
load spike created entirely by a default, for no benefit — nobody cares whether
their sync runs at 06:00 or 06:37.

The proposed minute is now derived from the machine's hostname, scattering
installations across the hour. Derived rather than random so it is stable: a
user who opens the page twice sees the same time, and re-saving does not move a
working schedule. The hour is untouched, since the early-morning reasoning
(see `scheduler.DefaultTime`) still holds.

## What is deliberately not done

- **No shared/central backend.** It would flatten load beautifully and it is
  ruled out by the local-only principle in `CLAUDE.md`. Not revisited here.
- **No `robots.txt` handling.** This is an authenticated user fetching their own
  course materials through a browser, which is not what robots exclusion
  governs.
- **No global cap across users.** There is no mechanism to coordinate installs,
  and inventing one would mean a backend.
- **No artificial slowdown of the download phase.** Files are fetched only when
  they have actually changed, which on a routine sync is almost none of them.

## If you are about to change something here

Ask, in order:

1. Does this make one sync ask OPAL for **more things** than before, or the same
   things **faster**? Only the first increases total load.
2. Does it raise **peak** request rate — more in flight at once — or just reduce
   idle time between requests?
3. Would it still be defensible if a thousand people ran it on the same morning?

If the answer to (3) is uncomfortable, the change belongs behind the ceiling,
not around it.
