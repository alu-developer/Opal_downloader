package scraper

import (
	"encoding/json"
	"os"
	"sort"
	"sync"

	"github.com/mxschmitt/playwright-go"

	"github.com/alu-developer/opal-downloader/internal/logging"
)

// The settle wait and the stability poll cost 143.7s of a 210s run between
// them, against 4.3s of actual extraction (measured 2026-07-27). Both infer
// "the page is done" from the absence of change, which costs the full debounce
// window whether the page took 20ms or 2s.
//
// Every previous attempt in docs/sync-speed-campaign.md tried to make that wait
// shorter or cheaper. None asked whether it is needed, and there is now a
// measured reason to think it might not be: the same day's network trace found
// that an ordinary section's initial render fires no AJAX at all, which means
// the file table is in the initial document rather than arriving later.
//
// This probe answers that without changing behaviour. It reads the section
// immediately - before any settling - and compares that byte-for-byte against
// what the full wait eventually returns, in the same run and the same page
// load, so no run-to-run variance is involved.
//
// A file count is deliberately NOT the comparison. Lowering
// sectionContentRequiredStableReads from 4 to 1 lost files byte-for-byte while
// looking perfectly healthy, and both of this project's known losses would have
// passed a count. Only an exact diff is evidence here.
//
//	OPAL_EARLY_READ_PROBE=1 go test ./internal/scraper/ -run TestFileListSnapshot -v -timeout 30m
const earlyReadProbeEnv = "OPAL_EARLY_READ_PROBE"

type earlyReadResult struct {
	Section    string `json:"section"`
	URL        string `json:"url"`
	EarlyCount int    `json:"early_count"`
	FinalCount int    `json:"final_count"`
	Identical  bool   `json:"identical"`
}

type earlyReadProbe struct {
	enabled bool
	mu      sync.Mutex
	results []earlyReadResult
}

func newEarlyReadProbe() *earlyReadProbe {
	return &earlyReadProbe{enabled: os.Getenv(earlyReadProbeEnv) != ""}
}

// read takes the immediate, pre-settle reading. Returns nil (and costs one
// map lookup) when the probe is off, so the shipped crawl is unaffected.
func (p *earlyReadProbe) read(s *OpalScraper, page playwright.Page) []map[string]string {
	if p == nil || !p.enabled {
		return nil
	}
	// An error here is itself a result - the page was not ready - so it
	// becomes an empty reading rather than dropping the section.
	candidates, _ := s.extractSectionContentCandidates(page)
	return candidates
}

func (p *earlyReadProbe) compare(section, url string, early, final []map[string]string) {
	if p == nil || !p.enabled {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.results = append(p.results, earlyReadResult{
		Section:    section,
		URL:        url,
		EarlyCount: len(early),
		FinalCount: len(final),
		Identical:  canonicalCandidates(early) == canonicalCandidates(final),
	})
}

// canonicalCandidates renders a reading in a form where equality means "the
// same rows with the same fields", independent of the order the DOM happened
// to yield them in. Order-independence matters: a reordering is not a loss,
// and counting it as one would bury the losses that are real.
func canonicalCandidates(candidates []map[string]string) string {
	rows := make([]string, 0, len(candidates))
	for _, c := range candidates {
		keys := make([]string, 0, len(c))
		for k := range c {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		row := ""
		for _, k := range keys {
			row += k + "=" + c[k] + "\x1f"
		}
		rows = append(rows, row)
	}
	sort.Strings(rows)
	out := ""
	for _, r := range rows {
		out += r + "\x1e"
	}
	return out
}

// report writes the verdict. Sections where the two readings differ are the
// entire finding: none means the 143.7s of waiting is removable, few means
// they are individually inspectable. Detail goes to a file rather than the
// console because 280 rows is not something to read in a terminal.
func (p *earlyReadProbe) report() {
	if p == nil || !p.enabled {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	identical := 0
	for _, r := range p.results {
		if r.Identical {
			identical++
		}
	}
	logging.User("Early-read probe: %d/%d sections identical with no settle wait at all", identical, len(p.results))

	body, err := json.MarshalIndent(p.results, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll("tmp", 0o755); err != nil {
		return
	}
	if err := os.WriteFile("tmp/early-read-probe.json", body, 0o600); err == nil {
		logging.User("Early-read probe detail written to tmp/early-read-probe.json")
	}
}
