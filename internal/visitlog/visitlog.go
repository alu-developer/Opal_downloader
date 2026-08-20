// Package visitlog implements a persistent, cross-run log of which course
// sections were visited during a crawl and how many files each visit found,
// plus an aggregate report over that log.
//
// This is distinct from --debug-clicks (internal/scraper/audit.go): that
// logs raw .Click()/wait call sites for manual reading within a single run
// ("did this run do anything dumb"). This package answers a different,
// cross-run question the maintainer separately asked for: "which
// sections/pages are consistently a waste of time across many runs" - e.g.
// "this section has been empty on all 8 visits". See
// .claude/queue/todo/add-section-visit-effectiveness-log.md for the
// original ask.
//
// This is observational only. Nothing in this package changes crawl
// behavior or skips a section automatically - a prior task
// (research-structure-cache-and-priority-crawl.md) explicitly rejected
// silent skip-based-on-history caching, since OPAL can add new content to a
// previously-empty section at any time. This package only gives a human the
// data to decide (e.g. manually adding a section to a future exclude-list).
package visitlog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// FileName is the conventional log file name, meant to sit next to
// ".opal-sync.manifest.json" under a config's DownloadPath.
const FileName = ".opal-visit-log.json"

// Record is one section-visit observation: which course/section was
// visited, how many files that visit found, and when. Appended to the
// persistent log across runs (see Append) - never overwritten.
type Record struct {
	Course       string    `json:"course"`
	SectionTitle string    `json:"section_title"`
	SectionURL   string    `json:"section_url"`
	FilesFound   int       `json:"files_found"`
	Timestamp    time.Time `json:"timestamp"`
	// HadChildren records whether this visit's page had at least one
	// subsection/folder link queued (or skipped as a known non-file node
	// type) for further crawling - i.e. whether this node was structurally
	// a container, as opposed to a terminal leaf page. Older log entries
	// written before this field existed decode as false, which is
	// indistinguishable from a genuine leaf - see FormatReport's doc
	// comment on why that ambiguity is left visible rather than guessed at.
	HadChildren bool `json:"had_children"`
}

type logFile struct {
	Records []Record `json:"records"`
}

// Load reads path's existing records. A missing file is not an error - it
// returns an empty (nil) slice, matching syncer.LoadManifest's behavior for
// the sync manifest this log sits next to: the first run of any command
// that touches this log starts from nothing.
func Load(path string) ([]Record, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var payload logFile
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("visitlog: parsing %s: %w", path, err)
	}
	return payload.Records, nil
}

// Append loads path's existing records, adds newRecords, and writes the
// combined list back - this is what makes the log persistent/cross-run
// (each call adds to what's already there rather than overwriting it). A
// nil/empty newRecords is a no-op and leaves any existing file untouched.
func Append(path string, newRecords []Record) error {
	if len(newRecords) == 0 {
		return nil
	}

	existing, err := Load(path)
	if err != nil {
		return err
	}
	combined := make([]Record, 0, len(existing)+len(newRecords))
	combined = append(combined, existing...)
	combined = append(combined, newRecords...)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(logFile{Records: combined}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// SectionStat is one section's aggregate visit-effectiveness summary across
// every recorded visit, keyed by (Course, SectionURL) - see Aggregate.
type SectionStat struct {
	Course       string
	SectionTitle string
	SectionURL   string
	Visits       int
	EmptyVisits  int
	LastVisited  time.Time
	// EverHadChildren is true if any recorded visit to this section found
	// subsection/folder links to queue (see Record.HadChildren). A node
	// that is AlwaysEmpty() but EverHadChildren is a container whose own
	// page never lists a file directly - expected, not necessarily a
	// problem. A node that is AlwaysEmpty() and never EverHadChildren is a
	// true terminal leaf that has contributed nothing across every visit.
	// This does not decide which of the two is "wasteful" - see
	// FormatReport's doc comment - it only makes the distinction visible.
	EverHadChildren bool
}

// AlwaysEmpty reports whether every recorded visit to this section found 0
// files - the specific signal the maintainer asked for ("this section has
// been empty on all 8 visits").
func (s SectionStat) AlwaysEmpty() bool {
	return s.Visits > 0 && s.EmptyVisits == s.Visits
}

// Aggregate groups records by (Course, SectionURL) and summarizes visit
// counts, so a human can see at a glance how often a section is visited and
// how often that visit turned out to be worthless (0 files found).
//
// The returned slice is sorted so the most consistently-wasteful sections
// come first: sections that were empty on every recorded visit, most-visited
// first, then sections with the highest empty-visit count generally, with a
// final alphabetical tie-break for determinism.
func Aggregate(records []Record) []SectionStat {
	type key struct {
		course string
		url    string
	}
	index := map[key]*SectionStat{}
	order := make([]key, 0)

	for _, r := range records {
		k := key{course: r.Course, url: r.SectionURL}
		stat, ok := index[k]
		if !ok {
			stat = &SectionStat{Course: r.Course, SectionTitle: r.SectionTitle, SectionURL: r.SectionURL}
			index[k] = stat
			order = append(order, k)
		}
		stat.Visits++
		if r.FilesFound == 0 {
			stat.EmptyVisits++
		}
		if r.HadChildren {
			stat.EverHadChildren = true
		}
		if r.Timestamp.After(stat.LastVisited) {
			stat.LastVisited = r.Timestamp
			// Keep the most recently observed section title - OPAL section
			// names can change over time, and the latest visit's title is
			// the most useful one to show a human.
			if strings.TrimSpace(r.SectionTitle) != "" {
				stat.SectionTitle = r.SectionTitle
			}
		}
	}

	stats := make([]SectionStat, 0, len(order))
	for _, k := range order {
		stats = append(stats, *index[k])
	}

	sort.Slice(stats, func(i, j int) bool {
		ai, aj := stats[i].AlwaysEmpty(), stats[j].AlwaysEmpty()
		if ai != aj {
			return ai
		}
		if stats[i].EmptyVisits != stats[j].EmptyVisits {
			return stats[i].EmptyVisits > stats[j].EmptyVisits
		}
		if stats[i].Visits != stats[j].Visits {
			return stats[i].Visits > stats[j].Visits
		}
		if stats[i].Course != stats[j].Course {
			return stats[i].Course < stats[j].Course
		}
		return stats[i].SectionTitle < stats[j].SectionTitle
	})

	return stats
}

// FormatReport renders stats (as returned by Aggregate) as a plain-text
// table for CLI output.
//
// An AlwaysEmpty row whose visits ever had subsection links gets a
// "(container - files live in subsections)" suffix, so a human scanning the
// report can positively identify (not guess) the case that's expected and
// uninteresting: a folder page whose own node never lists a file directly
// because the files live in its subsections, which get their own rows.
// EverHadChildren can only prove "yes, this is a container" - it cannot
// prove "no, this is a true leaf", since a row whose visits all predate the
// Record.HadChildren field (added after this report's first version)
// decodes that field as false indistinguishably from a genuine
// never-had-children leaf. So the report adds the container label when it
// has positive evidence and otherwise says nothing further, rather than
// asserting "leaf" on data that might just be stale - see the package doc
// comment on why silent skip-based-on-history was rejected; the same
// caution applies to labeling based on absence of evidence. This still
// doesn't decide whether a genuine leaf is worth acting on, but it removes
// the container case from the "needs manual cross-referencing" pile.
func FormatReport(stats []SectionStat) string {
	if len(stats) == 0 {
		return "No section visits recorded yet. Run `list` or `sync` at least once to start building history.\n"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%-30s %-35s %6s %6s  %s\n", "Course", "Section", "Visits", "Empty", "Notes")
	for _, s := range stats {
		notes := ""
		if s.AlwaysEmpty() {
			notes = fmt.Sprintf("empty on all %d visit(s)", s.Visits)
			if s.EverHadChildren {
				notes += " (container - files live in subsections)"
			}
		} else if s.EmptyVisits > 0 {
			// The signal a maintainer actually needs: this section sometimes
			// has files and sometimes doesn't, which an always-empty note
			// would never flag and a bare number pair (92, 3) is easy to
			// read past among ~80% always-empty rows.
			notes = fmt.Sprintf("empty on %d of %d visit(s)", s.EmptyVisits, s.Visits)
		}
		fmt.Fprintf(&b, "%-30s %-35s %6d %6d  %s\n", truncate(s.Course, 30), truncate(s.SectionTitle, 35), s.Visits, s.EmptyVisits, notes)
	}
	return b.String()
}

func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 1 {
		return string(r[:max])
	}
	return string(r[:max-1]) + "…"
}
