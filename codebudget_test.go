package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// A ratchet on how much code this project contains.
//
// WHY A FAILING TEST AND NOT A REPORTED NUMBER
// --------------------------------------------
// The first version of this idea was "print net lines added and removed, and
// justify it". The maintainer rejected it on the spot, correctly: a number I
// have to defend is a number I write a paragraph about. Nothing changes, the
// paragraph gets longer.
//
// What works instead is a check with a right answer. This one has exactly two
// outcomes: the code fits in the budget, or it does not and the build fails.
// Raising the budget is possible and sometimes correct - but it is a one-line
// diff with a number going up, which is visible in review and greppable in
// history. That is the whole mechanism: not that growth is forbidden, but that
// it cannot happen quietly.
//
// The failure this exists to catch is specific and observed. Over one session
// the assistant added several packages and deleted essentially nothing, and
// then described two pure file-moves as "cleanup" - shuffling bytes between
// files while the total went up. A per-package budget would have called that a
// success. A single total does not, which is why it is a single total.
//
// WHAT IS COUNTED
// ---------------
// Non-test Go code: blank lines and comments excluded. Comments are left out
// deliberately - this repo wants thorough ones, and a budget that punished
// them would fight the codebase's own style and teach exactly the wrong
// lesson. Code volume is what carries complexity; prose about it does not.
//
// HOW TO RESPOND WHEN THIS FAILS
// ------------------------------
// In order of preference:
//
//  1. Delete something. First choice for anything added recently: if it was
//     added this week and is not carrying its weight, it goes.
//  2. Write the change smaller.
//  3. Raise the budget, in the same commit as the change, and say in the
//     commit message what the new code buys. This is a legitimate answer for a
//     real feature. It is not a legitimate answer for "the test was in the way".
//
// What this does NOT license is deleting whatever is cheapest to delete.
// Anything that predates the change being made needs evidence before it goes -
// this repo has lost files before by removing something whose purpose was not
// obvious from reading it (see sectionContentRequiredStableReads in
// internal/scraper/crawl.go, which looks arbitrary and is load-bearing).
// Raised 2026-07-27 (was 11181) for internal/scraper/sectiontiming.go: the
// per-section timing measurement the sync-speed question needs. Bought: an
// answer to "where does each section's ~1s actually go" that is a number from
// a real run rather than constants read off the source - it found that 63% of
// in-section time is a 300ms debounce. Trimmed once before raising this; the
// per-section worst-case tracking went.
//
// The first version of that raise said +10 and was wrong: the new file was
// still untracked, so the check could not see it. The real cost was 50 lines.
// Hence the --others flag above.
//
// Raised 2026-07-27 (was 11241) for internal/gui/logs.go and the folder
// picker's encoding fix. Bought, in order of what it is worth:
//   - The diagnostic log is reachable at all. It was written to disk, named in
//     the CLI's --help, and mentioned nowhere in the GUI - which is how most
//     people use this, and where a windowed app's stdout goes nowhere. Most of
//     the cost is a page: path, tail, and a button that reveals the file.
//   - A picked folder containing "Ü" is no longer stored as a path that points
//     at nothing. That part is one line of PowerShell; the rest of its cost is
//     the comment explaining a code-page bug nobody would guess from the code.
//
// Trimmed before raising: a one-line urlQueryEscape wrapper around
// template.URLQueryEscaper.
//
// Raised 2026-07-27 (was 11417) for internal/scraper/previews.go. Bought: the
// ability to not download ~30 MB of file previews per course per discovery
// pass. Most of the file is the argument rather than the code - which two
// conditions make it safe, and the measured result that made it opt-in: the
// file lists came back byte-for-byte identical, and the run came back 31%
// slower. A future reader deciding whether to turn it on needs both numbers
// more than they need the six lines of logic.
// Raised 2026-07-27 (was 11477) for discovery.go telling "every course-listing
// page failed" apart from "this account has no courses". Bought: a run that
// read nothing can no longer finish as "Found 0 course links / Discovered 0
// remote files", which is what a healthy sync of an empty account looks like.
// Observed live the same day - the developer-mode browser window was gone by
// the time discovery ran, all three sources failed with "target closed", and
// the run reported success. Most of the cost is the two predicates, which are
// small but need to be separately testable: one decides whether to fail at
// all, the other decides whether the user is told their window closed, and
// getting the second wrong sends them looking in the wrong place.
//
// Trimmed before raising: a redundant third substring check in
// isClosedBrowserError that the second already subsumed.
// Raised 2026-07-27 (was 11504) for preferCancellation. Five lines, and they
// buy the difference between "you stopped this" and "this is broken": a cancel
// kills the browser, so discovery fails, so the user who pressed Cancel was
// being handed a course-listing failure and told to leave their browser window
// open. Small enough to be tempting to inline; kept separate because the
// wiring is the part that breaks, and a call site is what a mutation test can
// remove.
// Raised 2026-07-27 (was 11509) for the log download handler and the `go run`
// message. Bought: /feedback stops asking people to attach a file it gives
// them no way to obtain - it linked to a viewer and left them to go find the
// file themselves - and a `go run` build is told it is a `go run` build
// instead of "a temporary location", which reads as a fault when it is the
// expected state.
//
// Trimmed before raising: two <h2> sections on /settings that contained no
// settings, only pointers to other pages, collapsed into one line.
// Lowered back to 11534 on 2026-07-27, the same day it was raised to 11630 for
// internal/scraper/earlyread.go. That entry said the probe was an instrument
// with an expiry and would be deleted once its question was answered. The
// question was answered - the settle wait is load-bearing, 3 of 278 sections
// identical without it - so the probe is gone and the budget is back. Recorded
// because an expiry nobody honours is worse than no expiry: it makes every
// future "temporary" raise cheaper to argue for.
//
// Re-adding it is one `git show` away if a future attempt wants to re-measure.
// Raised 2026-07-27 (was 11534) for internal/sectionhash. Bought: the first
// piece of the change-detection cache, which is the only approach measured
// capable of a large sync-speed win (210.3s -> ~93s projected). Pure, no
// integration, and useless on its own until the cache is built on it - added
// separately because it is the piece the whole design's SAFETY rests on, and
// it deserves to be reviewable without a crawl attached.
//
// Most of the file is the pattern list and the argument for each pattern being
// narrow. That is proportionate: a pattern one character too wide turns a
// changed section into an unchanged one, which stops downloads silently.
const codeLineBudget = 11563

func TestCodeSizeStaysWithinBudget(t *testing.T) {
	// --others --exclude-standard includes files that are new and not yet
	// staged. Without it a brand-new .go file is invisible to this check until
	// it is committed - which is exactly the case the budget most needs to
	// catch, and it went unnoticed once: sectiontiming.go passed the budget
	// while untracked and put the repo 50 lines over the moment it landed.
	out, err := exec.Command("git", "ls-files", "--cached", "--others", "--exclude-standard", "*.go").Output()
	if err != nil {
		t.Skipf("cannot list git-tracked files (%v) - code budget not checked", err)
	}

	perPackage := map[string]int{}
	total := 0
	for _, f := range strings.Split(string(out), "\n") {
		f = strings.TrimSpace(f)
		if f == "" || strings.HasSuffix(f, "_test.go") {
			continue
		}
		n, err := countCodeLines(filepath.FromSlash(f))
		if err != nil {
			continue // staged-but-deleted, or otherwise not on disk
		}
		pkg := filepath.ToSlash(filepath.Dir(f))
		if pkg == "" {
			pkg = "."
		}
		perPackage[pkg] += n
		total += n
	}

	if total <= codeLineBudget {
		return
	}

	pkgs := make([]string, 0, len(perPackage))
	for p := range perPackage {
		pkgs = append(pkgs, p)
	}
	sort.Slice(pkgs, func(i, j int) bool { return perPackage[pkgs[i]] > perPackage[pkgs[j]] })

	var b strings.Builder
	for _, p := range pkgs {
		fmt.Fprintf(&b, "\n  %-32s %5d", p, perPackage[p])
	}

	t.Errorf("this project now holds %d lines of non-test code, over its budget of %d (+%d).\n\n"+
		"Before raising the budget, try in this order: delete something added recently, "+
		"or write the change smaller. Raising it is a fair answer for a real feature - "+
		"do it in the same commit and say what the new code buys.\n\n"+
		"Do not delete whatever is cheapest. Anything predating this change needs evidence first.\n"+
		"\nBy package:%s\n", total, codeLineBudget, total-codeLineBudget, b.String())
}

// countCodeLines counts lines that are neither blank nor comment.
func countCodeLines(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}

	n := 0
	inBlock := false
	for _, line := range strings.Split(string(data), "\n") {
		s := strings.TrimSpace(line)
		if s == "" {
			continue
		}
		if inBlock {
			if strings.Contains(s, "*/") {
				inBlock = false
			}
			continue
		}
		if strings.HasPrefix(s, "/*") {
			if !strings.Contains(s, "*/") {
				inBlock = true
			}
			continue
		}
		if strings.HasPrefix(s, "//") {
			continue
		}
		n++
	}
	return n, nil
}
