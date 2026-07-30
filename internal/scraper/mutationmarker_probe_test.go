package scraper

// Feasibility probe for the one lead docs/sync-speed-campaign.md names as
// still unexplored (2026-07-27 entry, "Consequence for the ~84s debounce
// toll"): "a DOM-level completion marker Wicket itself sets, if one exists...
// Neither has been looked for." The network-layer half of that sentence (a
// different OPAL view) needs a human to go looking at OPAL's own UI for an
// alternate representation and is out of scope for an automated probe; this
// one answers the DOM half, which is machine-checkable.
//
// The settle-wait debounce (waitForInteractiveLinks, navigation.go) infers
// "done" from 300ms of silence on a MutationObserver, because no positive
// signal has ever been found. This probe installs its OWN MutationObserver
// before navigating to a real section - via page.AddInitScript, so it is
// attached before Wicket's own render even starts - and records every
// mutation's target/type/timestamp for the section's full render sequence.
// If the render's last mutation (or a small stable set of them) always
// targets the same element/attribute across sections, that would BE the
// positive signal this campaign has been looking for. If it doesn't, that is
// a real, useful negative result: no DOM-level marker exists either, and the
// unexplored lead in the campaign doc can be closed rather than left open
// implying an easy win nobody has checked.
//
// Usage: OPAL_MUTATION_MARKER_TRACE=1 go test ./internal/scraper/ -run TestMutationMarkerAtSectionSettle -v -timeout 5m
import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alu-developer/opal-downloader/internal/config"
	"github.com/mxschmitt/playwright-go"
)

// mutationObserverInitScript is injected before every navigation on the
// probe's page (AddInitScript re-runs it on each Goto), so the observer is
// always attached before Wicket's own render begins - the same ordering
// requirement waitForInteractiveLinks itself depends on.
const mutationObserverInitScript = `
(() => {
  window.__mlog = [];
  window.__mstart = performance.now();
  function attach() {
    const obs = new MutationObserver((mutations) => {
      const t = performance.now() - window.__mstart;
      for (const m of mutations) {
        const tgt = m.target;
        const tag = tgt && tgt.tagName ? tgt.tagName.toLowerCase() : (tgt && tgt.nodeName) || 'unknown';
        const id = (tgt && tgt.id) || '';
        const cls = (tgt && typeof tgt.className === 'string') ? tgt.className : '';
        let attrVal = '';
        if (m.type === 'attributes' && tgt && tgt.getAttribute) {
          attrVal = tgt.getAttribute(m.attributeName);
          if (attrVal === null) { attrVal = '(removed)'; }
        }
        window.__mlog.push({
          t: Math.round(t * 100) / 100,
          type: m.type,
          attr: m.attributeName || '',
          attrVal: attrVal,
          tag: tag,
          id: id,
          cls: cls,
          added: m.addedNodes ? m.addedNodes.length : 0,
          removed: m.removedNodes ? m.removedNodes.length : 0
        });
      }
    });
    obs.observe(document.documentElement, {childList: true, subtree: true, attributes: true, characterData: true});
  }
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', attach);
  } else {
    attach();
  }
})();
`

type mutationRecord struct {
	T       float64 `json:"t"`
	Type    string  `json:"type"`
	Attr    string  `json:"attr"`
	AttrVal string  `json:"attrVal"`
	Tag     string  `json:"tag"`
	ID      string  `json:"id"`
	Cls     string  `json:"cls"`
	Added   int     `json:"added"`
	Removed int     `json:"removed"`
}

func (m mutationRecord) label() string {
	desc := m.tag_or_unknown()
	if m.ID != "" {
		desc += "#" + m.ID
	} else if m.Cls != "" {
		desc += "." + strings.ReplaceAll(strings.TrimSpace(m.Cls), " ", ".")
	}
	if m.Type == "attributes" {
		desc += "[" + m.Attr + "=" + m.AttrVal + "]"
	}
	if m.Added > 0 || m.Removed > 0 {
		desc += fmt.Sprintf(" (+%d/-%d nodes)", m.Added, m.Removed)
	}
	return desc
}

func (m mutationRecord) tag_or_unknown() string {
	if m.Tag == "" {
		return "unknown"
	}
	return m.Tag
}

func TestMutationMarkerAtSectionSettle(t *testing.T) {
	if os.Getenv("OPAL_MUTATION_MARKER_TRACE") == "" {
		t.Skip("set OPAL_MUTATION_MARKER_TRACE=1 to run the real-account mutation-marker probe")
	}
	captureProbeLogs(t) // see probelogging_test.go for the day a suppressed Warn cost

	loaded, err := config.Load(filepath.Join("..", "..", "config.yaml"))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	courseName := os.Getenv("OPAL_MUTATION_MARKER_COURSE")
	if courseName == "" {
		courseName = "Algorithmen und Datenstrukturen"
	}

	sc := New(loaded.Credentials.URL, loaded.Credentials.StateFile)
	defer sc.Close()

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
	t.Logf("probing course %q (%s)", course.Title, course.URL)

	page := sc.getPage()
	if page == nil {
		t.Fatalf("no page available after ensureSession")
	}

	script := mutationObserverInitScript
	if err := page.AddInitScript(playwright.Script{Content: &script}); err != nil {
		t.Fatalf("AddInitScript: %v", err)
	}

	var report []string
	say := func(format string, args ...any) {
		line := fmt.Sprintf(format, args...)
		report = append(report, line)
		t.Log(line)
	}

	// analyse visits one section, reads back the mutation log AddInitScript
	// resets on every navigation, and reports its tail. Shared between the
	// root section and (below) one real subfolder section, so the same
	// question - is the tail content-shaped or bookkeeping-shaped - gets
	// asked identically for both kinds of section. Returns the visit so the
	// caller can reuse its candidates (finding a subfolder URL) without a
	// second navigation to the same section.
	analyse := func(label, url, title string) sectionVisit {
		newPage, visit := sc.visitSection(page, url, title)
		page = newPage
		if visit.failed {
			say("%s: visitSection FAILED for %s - nothing to analyse", label, url)
			return visit
		}

		raw, err := page.Evaluate(`() => JSON.stringify(window.__mlog || [])`)
		if err != nil {
			say("%s: reading mutation log failed: %v", label, err)
			return visit
		}
		rawStr, _ := raw.(string)
		var mutations []mutationRecord
		if rawStr != "" {
			if err := json.Unmarshal([]byte(rawStr), &mutations); err != nil {
				say("%s: unmarshal mutation log (%d bytes) failed: %v", label, len(rawStr), err)
				return visit
			}
		}

		say("%s %q (%s): %d files/candidates extracted, %d mutations recorded", label, title, url, len(visit.candidates), len(mutations))

		if len(mutations) == 0 {
			say("RESULT: no mutations recorded at all - either the section rendered with zero DOM changes after DOMContentLoaded (unlikely given the settle wait exists for a reason) or the observer failed to attach in time. Treat as inconclusive, not as 'no marker exists'.")
			return visit
		}

		first, last := mutations[0], mutations[len(mutations)-1]
		say("first mutation at t=%.2fms: %s", first.T, first.label())
		say("last  mutation at t=%.2fms: %s", last.T, last.label())

		const tail = 8
		start := len(mutations) - tail
		if start < 0 {
			start = 0
		}
		say("--- last %d mutations ---", len(mutations)-start)
		for i := start; i < len(mutations); i++ {
			m := mutations[i]
			say("  [%d] t=%.2fms %s type=%s %s", i, m.T, m.tag_or_unknown(), m.Type, m.label())
		}

		// The actual question: is the tail dominated by content (the file
		// table itself changing) or by bookkeeping (Wicket's own chrome -
		// counters, ids, the ajax-busy indicator)? A tail that is
		// content-shaped has no marker to key off, because "the content
		// changed" IS the thing being waited for. A tail that is
		// consistently bookkeeping-shaped, on the other hand, is a candidate:
		// if Wicket always touches the same non-content element last, that
		// touch could be the positive signal.
		say("RESULT: recorded %d mutations spanning t=%.2fms to t=%.2fms. Read the tail above by hand - this probe reports the data, it does not itself judge whether a stable non-content marker exists, since that needs the section's actual markup for context (which element ids/classes mean 'file table' vs 'Wicket chrome' is not something this probe knows).", len(mutations), first.T, last.T)
		return visit
	}

	rootVisit := analyse("ROOT section", course.URL, course.Title)

	// Whether the jsTree completion signal (found 2026-07-30 on 4 course
	// roots) also fires on a non-root section is the first open question
	// docs/RESUME.md records for this lead. Reusing appendSectionFolderTargets
	// - the exact function the real crawl uses to turn the root section's own
	// candidates into its BFS queue - finds a real subfolder URL from the
	// root visit already done above, with no second navigation needed just to
	// re-derive it.
	say("--- looking for a non-root section to test the same question ---")
	if !rootVisit.failed && len(rootVisit.candidates) > 0 {
		queue, _ := appendSectionFolderTargets(nil, map[string]struct{}{}, map[string]struct{}{sectionKey(course.URL, course.RepoID): {}},
			rootVisit.candidates, sc.opalURL, course.RepoID, course.URL, course.URL, course.Title, map[string]string{}, false)
		if len(queue) > 0 {
			analyse("NON-ROOT section", queue[0], "(subfolder, title unknown to this probe)")
		} else {
			say("no subfolder URL found from the root section's own candidates - this course may be flat (no subfolders), which this probe cannot help with")
		}
	} else {
		say("root section failed or had no candidates - cannot derive a subfolder queue")
	}

	outDir := filepath.Join("..", "..", "tmp")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Logf("could not create %s, result is in this log only: %v", outDir, err)
	} else {
		outPath := filepath.Join(outDir, "mutation-marker-probe.txt")
		header := "mutation marker probe recorded " + time.Now().Format(time.RFC3339) + "\n\n"
		if err := os.WriteFile(outPath, []byte(header+strings.Join(report, "\n")+"\n"), 0o644); err != nil {
			t.Logf("could not write %s, result is in this log only: %v", outPath, err)
		} else {
			t.Logf("findings written to %s", outPath)
		}
	}
}
