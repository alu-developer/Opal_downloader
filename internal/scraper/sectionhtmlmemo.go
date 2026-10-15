package scraper

import (
	"sync"
)

// sectionHTMLMemo caches one fetch of a section page's HTML for the length
// of a download phase, so N files from the same section cost one fetch of
// that page instead of N.
//
// WHY THIS IS A CORRECTNESS FIX, NOT ONLY A SPEED ONE
// ---------------------------------------------------
// The counter-refresh (download_refresh.go) re-fetches a file's section page
// to find that file's Wicket click behaviour. Doing that once per *file*
// means a 48-file section is fetched 48 times in a few seconds, and every
// fetch makes OPAL mint a **new server-side Wicket page instance** - the
// `?36` in `.../Part-3?36-2.1-…`. The click URL a worker extracts is bound
// to the instance it came from, and OPAL keeps only a bounded number of
// instances per session, so under `download_concurrency` workers racing on
// one section the instance a worker is about to invoke can already have been
// evicted by the fetches its neighbours made in the meantime. The invoke
// then answers without a download target, the counter-refresh reports a
// miss, and the file falls through to the serialized browser-click path.
//
// That is the mechanism behind docs/sync-speed-model.md's Question 44, open
// since 2026-08-12 and the single largest cost in a no-op sync. It explains
// every observation that question had already collected and could not
// reconcile:
//
//   - Sequential replay of all 48 Part-3 files: 0 failures. One worker never
//     outruns its own page instance.
//   - 3 workers on that one section in isolation: 0 failures. Few enough
//     instances, nothing evicted.
//   - The full 6-course run: fails every time, same distribution. Only there
//     do enough sections churn instances at once to push one out.
//   - The audit trail's "section URL re-fetched under different Wicket
//     container ids (?1065 then ?1109) within ~20s, then the earlier
//     container's click fails" - that is this, written down a year before it
//     was understood.
//
// Fetching each section once removes the churn at its source: one instance
// per section, reused by every file in it, exactly as a real browser does
// (a browser loads the folder page once and clicks each row on that one
// page).
//
// A memoized page can still go stale - a long download phase, or an instance
// evicted for some other reason - so this is a cache with an invalidation
// path, not a promise: refreshCounterURL drops the entry and re-fetches once
// when a lookup against memoized HTML fails. That keeps the worst case at
// today's behaviour (one extra fetch) rather than turning a stale cache into
// a new failure class.
type sectionHTMLMemo struct {
	mu      sync.Mutex
	entries map[string]*sectionHTMLEntry
}

// sectionHTMLEntry is one section's cached fetch. The once/ready pair means
// several workers arriving at the same uncached section issue one fetch
// between them and all wait on it, rather than each starting their own -
// which would recreate the instance churn this exists to remove.
type sectionHTMLEntry struct {
	once   sync.Once
	ready  chan struct{}
	status int
	body   string
	err    error
}

func newSectionHTMLMemo() *sectionHTMLMemo {
	return &sectionHTMLMemo{entries: map[string]*sectionHTMLEntry{}}
}

// get returns url's HTML, fetching it with fetch at most once across all
// callers. fresh reports whether this call did the fetching, which the
// caller uses to decide whether a failed lookup is worth retrying: a result
// that was already cached may be stale and is worth one retry, a result just
// fetched is not.
func (m *sectionHTMLMemo) get(url string, fetch func() (int, string, error)) (status int, body string, fresh bool, err error) {
	if m == nil {
		status, body, err = fetch()
		return status, body, true, err
	}

	m.mu.Lock()
	entry := m.entries[url]
	if entry == nil {
		entry = &sectionHTMLEntry{ready: make(chan struct{})}
		m.entries[url] = entry
	}
	m.mu.Unlock()

	did := false
	entry.once.Do(func() {
		did = true
		entry.status, entry.body, entry.err = fetch()
		close(entry.ready)
	})
	<-entry.ready

	return entry.status, entry.body, did, entry.err
}

// invalidate drops url's cached entry so the next get re-fetches it.
func (m *sectionHTMLMemo) invalidate(url string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	delete(m.entries, url)
	m.mu.Unlock()
}

// reset empties the memo. Called when a run's download phase ends, so a
// long-lived scraper (the GUI keeps one across syncs) never serves a page
// from a previous run.
func (m *sectionHTMLMemo) reset() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.entries = map[string]*sectionHTMLEntry{}
	m.mu.Unlock()
}
