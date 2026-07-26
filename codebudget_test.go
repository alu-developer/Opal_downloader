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
const codeLineBudget = 11181

func TestCodeSizeStaysWithinBudget(t *testing.T) {
	out, err := exec.Command("git", "ls-files", "*.go").Output()
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
