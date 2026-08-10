package scraper

// docs/sync-speed-model.md, Question 18 ("Next experiment", written
// 2026-08-03): is a section being silently truncated on every run, at the
// shipping default?
//
// CourseNode/1775529461522481011 (Algorithmen und Datenstrukturen) emits
// warnShowAllTruncated in all 11 archived run logs, at course_concurrency=1
// and 2 alike, and its reason string alternates between "17 rows before, 17
// after" and "17 rows before, 14 after". A count that goes *down* after an
// expansion has at least two explanations needing opposite fixes:
//
//   - the expansion dropped rows that were really there (a bug losing files),
//   - or the collapsed list held rows that stop matching once expanded - the
//     "show all" control is itself one of the candidates and disappears when
//     activated (harmless, and it accounts for exactly one row, not three).
//
// Counts cannot separate those. This probe records the hrefs, so the answer
// is a list of what actually vanished rather than a subtraction.
//
// WHY THIS RUNS WITHOUT A COMPARISON BASELINE, unlike every other probe in
// this package: the failure under investigation is *permanent*. It reproduces
// identically in every archived run, which is precisely why eight
// byte-identical runs across Questions 14 and 15 reported "no deviation"
// while it was happening in all eight. A diff cannot see it. One run that
// prints the contents is worth more here than any number of runs compared
// against each other.
//
// Usage:
//
//	OPAL_SHOWALL_HREF_TRACE=1 go test ./internal/scraper/ -run TestShowAllHrefDelta -v -timeout 15m
//
// Read-only: no config is written, no default changed, nothing downloaded.
import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/alu-developer/opal-downloader/internal/config"
)

// showAllEvent is one section's expansion, captured whole.
type showAllEvent struct {
	sectionURL string
	before     []map[string]string
	after      []map[string]string
}

// candidateKey identifies a row across the before/after boundary. The link
// target is the only field that is both stable and meaningful here - text can
// be re-rendered and class names change when a table expands, so keying on
// either would manufacture differences that are not real.
func candidateKey(candidate map[string]string) string {
	target := extractLinkTarget(candidate["href"], candidate["onclick"], candidate["dataHref"], candidate["dataUrl"])
	if strings.TrimSpace(target) == "" {
		// A row with no resolvable target still has to be counted, or the
		// tallies stop matching warnShowAllTruncated's numbers and the two
		// cannot be compared. Fall back to its text.
		return "(no target) " + strings.TrimSpace(candidate["text"])
	}
	return target
}

func TestShowAllHrefDelta(t *testing.T) {
	if os.Getenv("OPAL_SHOWALL_HREF_TRACE") == "" {
		t.Skip("set OPAL_SHOWALL_HREF_TRACE=1 to run the real-account show-all href probe (docs/sync-speed-model.md Question 18)")
	}
	beginLiveProbe(t) // see probelogging_test.go for the day a suppressed Warn cost

	loaded, err := config.Load(filepath.Join("..", "..", "config.yaml"))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	courseName := os.Getenv("OPAL_SHOWALL_HREF_COURSE")
	if courseName == "" {
		courseName = "Algorithmen und Datenstrukturen"
	}

	sc := New(loaded.Credentials.URL, loaded.Credentials.StateFile)
	defer sc.Close()

	var events []showAllEvent
	sc.showAllProbe = func(sectionURL string, before, after []map[string]string) {
		// Copy: the crawl reuses and mutates these slices after the callback
		// returns, so holding the originals would report whatever they became
		// later rather than what they were at expansion time.
		event := showAllEvent{sectionURL: sectionURL}
		event.before = append(event.before, before...)
		event.after = append(event.after, after...)
		events = append(events, event)
	}

	if err := sc.ensureSession(false); err != nil {
		t.Fatalf("ensureSession: %v", err)
	}

	courses, err := sc.discoverCourseLinks([]string{courseName})
	if err != nil {
		t.Fatalf("discoverCourseLinks: %v", err)
	}
	if len(courses) == 0 {
		t.Fatalf("course %q not found", courseName)
	}
	course := courses[0]

	page := sc.getPage()
	if page == nil {
		t.Fatalf("no page available after ensureSession")
	}

	var report []string
	say := func(format string, args ...any) {
		line := fmt.Sprintf(format, args...)
		report = append(report, line)
		t.Log(line)
	}

	say("show-all href probe (Question 18) - course %q (%s)", course.Title, course.URL)

	_, files, _, err := sc.collectCourseFiles(page, course)
	if err != nil {
		t.Fatalf("collectCourseFiles: %v", err)
	}
	say("crawl finished: %d files, %d section(s) offered a \"show all\" control", len(files), len(events))

	if len(events) == 0 {
		say("VERDICT: no section in this course offered a \"show all\" control at all.")
		say("  That is not the expected shape - the archived logs show this course's")
		say("  CourseNode/1775529461522481011 warning on every run. Either the course")
		say("  content changed, or the control is no longer being detected, and the")
		say("  second of those would itself explain a permanent truncation.")
	}

	shrank := 0
	for _, event := range events {
		// Keyed by link target but keeping the whole row: classifying a lost
		// row needs its text as well as its href (looksLikeFileLink rejects
		// an empty name outright), so a set of bare keys is not enough.
		beforeKeys := map[string]map[string]string{}
		for _, candidate := range event.before {
			beforeKeys[candidateKey(candidate)] = candidate
		}
		afterKeys := map[string]map[string]string{}
		for _, candidate := range event.after {
			afterKeys[candidateKey(candidate)] = candidate
		}

		var lost, gained []string
		for key := range beforeKeys {
			if _, ok := afterKeys[key]; !ok {
				lost = append(lost, key)
			}
		}
		for key := range afterKeys {
			if _, ok := beforeKeys[key]; !ok {
				gained = append(gained, key)
			}
		}
		sort.Strings(lost)
		sort.Strings(gained)

		say("")
		say("section %s", event.sectionURL)
		say("  %d row(s) before, %d after; %d lost, %d gained",
			len(event.before), len(event.after), len(lost), len(gained))

		if len(lost) > 0 {
			shrank++
			// The interesting list. A row present before the expansion and
			// absent after it was visible to the crawler and then thrown
			// away - unless it is the control itself, which is why each line
			// is labelled rather than just printed.
			say("  LOST (present before the expansion, gone after):")
			for _, key := range lost {
				row := beforeKeys[key]
				label := ""
				switch {
				case looksLikeShowAllControl(key, row["text"], row["title"], row["className"]):
					label = "  <- the \"show all\" control itself, expected to disappear"
				case looksLikeFileLink(key, row["text"]):
					label = "  <- LOOKS LIKE A FILE - this is a real loss"
				}
				say("    %s [text: %q]%s", key, strings.TrimSpace(row["text"]), label)
			}
		}
		if len(gained) > 0 {
			say("  gained (%d): first few below", len(gained))
			for i, key := range gained {
				if i >= 5 {
					say("    ... and %d more", len(gained)-5)
					break
				}
				say("    %s", key)
			}
		}
	}

	say("")
	if shrank > 0 {
		say("VERDICT: %d section(s) lost at least one row to their own expansion.", shrank)
		say("  Check the LOST lines above: any entry marked \"LOOKS LIKE A FILE\" is a")
		say("  file the crawler had already found and then discarded, which is a bug in")
		say("  expandShowAllInSection returning `expanded` unconditionally (crawl.go).")
		say("  Entries that are only the show-all control are expected and harmless.")
	} else {
		say("VERDICT: no section lost rows to its expansion.")
		say("  Then the \"17 before, 14 after\" archived reason strings are NOT rows")
		say("  being dropped, and the truncation question moves to what the section")
		say("  holds in a browser versus what the crawler ever sees - the by-hand")
		say("  count is then the deciding step, not more instrumentation.")
	}

	if outDir := strings.TrimSpace(os.Getenv("OPAL_PROBE_OUT_DIR")); outDir != "" {
		outPath := filepath.Join(outDir, "showall-href-probe.txt")
		body := strings.Join(report, "\n") + "\n"
		if err := os.WriteFile(outPath, []byte(body), 0o644); err != nil {
			t.Logf("could not write %s: %v", outPath, err)
		} else {
			t.Logf("wrote %s", outPath)
		}
	}
}
