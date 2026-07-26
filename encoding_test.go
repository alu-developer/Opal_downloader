package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// Mojibake is what you get when UTF-8 bytes are read as Windows-1252 (or
// Latin-1) and then saved as UTF-8 again: a "u" umlaut turns into two junk
// characters, an em-dash into three. It is a one-way trip, it happens to
// whichever editor or shell touched the file last, and - the reason this test
// exists - it is invisible in review, because the reviewer's terminal renders
// the damaged bytes as exactly the characters that end up printed on screen
// for the user. Three of these shipped, in the GUI and in config.go's
// comments, before a human noticed one of them in the running program
// (2026-07-26).
//
// The detection is deliberately narrow rather than clever. Instead of trying
// to recognise mojibake in general, it looks for the lead characters that
// produce essentially all of it in practice, in the combinations that cannot
// occur in this repo's real content:
//
//   - U+00C3 leads every mangled Latin-1 letter - the umlauts, the sharp s
//     and the accented vowels all pass through it. It is a legitimate
//     Portuguese letter, but this repo writes German and English only, so any
//     occurrence here is damage.
//   - U+00E2 followed by U+20AC is the signature of every mangled "general
//     punctuation" character: em-dash, en-dash, curly quotes, ellipsis. The
//     lead alone is left alone.
//   - U+00C2 followed by a non-letter is a mangled non-breaking space or
//     similar. The lead alone is left alone.
//
// Written as numeric code points on purpose: spelling these characters
// literally would put mojibake in the very file that forbids it, and the test
// would flag itself. That is not hypothetical - it happened twice while
// writing this.
const (
	latin1Lead = rune(0x00C3)
	punctLead  = rune(0x00E2)
	euroSign   = rune(0x20AC)
	nbspLead   = rune(0x00C2)
)

// scannedExtensions are the text formats this repo authors. An allowlist
// rather than a blocklist, so a new binary fixture format is not silently
// scanned as if it were prose.
var scannedExtensions = map[string]bool{
	".go": true, ".md": true, ".ps1": true, ".psm1": true,
	".yaml": true, ".yml": true, ".json": true, ".txt": true,
	".html": true, ".css": true, ".js": true,
}

func TestNoMojibakeInSourceFiles(t *testing.T) {
	// Git-tracked files, not a directory walk: "content this repo authors" is
	// exactly "what is committed". A plain walk also picks up the gitignored
	// tmp/ dumps of real OPAL pages, which are captured bytes rather than
	// authored text - 77 findings that are not this repo's to fix, and which
	// would have made the guard useless on its first run.
	var findings []string
	for _, path := range trackedFiles(t) {
		if !scannedExtensions[strings.ToLower(filepath.Ext(path))] {
			continue
		}

		data, err := os.ReadFile(path)
		if err != nil {
			// A tracked file that cannot be read is a deleted-but-staged
			// working state, not a finding.
			continue
		}
		// Invalid UTF-8 is a different defect, and it would make the rune
		// scan below meaningless, so report it rather than scan garbage.
		if !utf8.Valid(data) {
			findings = append(findings, fmt.Sprintf("%s: not valid UTF-8", path))
			continue
		}

		for i, line := range strings.Split(string(data), "\n") {
			if what, ok := findMojibake(line); ok {
				findings = append(findings, fmt.Sprintf("%s:%d: %s\n    %s", path, i+1, what, escapeNonASCII(line)))
			}
		}
	}

	if len(findings) > 0 {
		t.Errorf("%d line(s) contain mojibake - UTF-8 text that was read as Latin-1/Windows-1252 and re-saved.\n"+
			"Re-type the affected characters in a UTF-8 editor, or use an HTML entity such as &mdash; in a template.\n\n%s",
			len(findings), strings.Join(findings, "\n"))
	}
}

// trackedFiles lists the repo's git-tracked paths. Skips the test rather than
// failing it when git is unavailable or this is not a checkout: in that
// situation the guard cannot tell authored text from captured bytes, and a
// guard that guesses is worse than one that says it did not run.
func trackedFiles(t *testing.T) []string {
	t.Helper()

	out, err := exec.Command("git", "ls-files", "-z").Output()
	if err != nil {
		t.Skipf("cannot list git-tracked files (%v) - mojibake guard did not run", err)
	}

	var paths []string
	for _, p := range strings.Split(string(out), "\x00") {
		if p != "" {
			paths = append(paths, filepath.FromSlash(p))
		}
	}
	if len(paths) == 0 {
		t.Skip("git reported no tracked files - mojibake guard did not run")
	}
	return paths
}

// findMojibake reports the first damaged sequence in s, naming the characters
// by code point so the report is readable in a terminal that would otherwise
// render the damaged bytes as whatever they were mistaken for.
func findMojibake(s string) (string, bool) {
	runes := []rune(s)
	for i, r := range runes {
		var next rune
		if i+1 < len(runes) {
			next = runes[i+1]
		}

		switch {
		case r == latin1Lead,
			r == punctLead && next == euroSign,
			r == nbspLead && next != 0 && !isLetter(next):
			return fmt.Sprintf("U+%04X followed by U+%04X", r, next), true
		}
	}
	return "", false
}

func isLetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r > 0x00bf
}

// escapeNonASCII renders the offending line without reproducing its bytes.
func escapeNonASCII(s string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(s) {
		if r < 128 {
			b.WriteRune(r)
		} else {
			fmt.Fprintf(&b, "<U+%04X>", r)
		}
	}
	return b.String()
}
