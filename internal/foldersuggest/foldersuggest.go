// Package foldersuggest proposes a per-course download folder by looking at
// the folder tree the user already keeps their coursework in.
//
// Filling in course_folders by hand is the fiddliest part of setup: every
// course needs its own path typed or browsed to, and the names rarely line
// up — a folder called "AlgData" belongs to a course OPAL calls "Algorithmen
// und Datenstrukturen", and one called "NuMa" to a course whose title runs
// to eleven words. Substring matching finds almost none of those, so the
// matching here is deliberately abbreviation-aware (see Score).
//
// The governing rule is that a wrong suggestion is worse than none: a folder
// is only offered when it scores well *and* is clearly ahead of the
// runner-up. Everything this package does is a proposal for a form field the
// user still has to save, never a change on disk.
package foldersuggest

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DefaultMaxDepth bounds how deep under the download root a candidate can
// sit. Three levels covers the layout this is aimed at —
// "<root>/<semester>/<course>/Downloads" — without walking a whole OneDrive.
const DefaultMaxDepth = 3

// DefaultMaxDirs caps the scan so pointing the tool at a large tree cannot
// turn a settings-page button into a minutes-long stall. Whatever was found
// before the cap is still used; a partial candidate list produces fewer
// suggestions, not wrong ones.
const DefaultMaxDirs = 20000

// Candidate is one folder that could serve as a course's download
// destination.
//
// Path is what gets suggested; MatchName is what the course title is
// compared against, and the two differ for the "…/Downloads" convention: for
// "…/AlgData/Downloads" the downloads folder is the destination but
// "AlgData" is the name that identifies the course.
type Candidate struct {
	Path      string
	MatchName string
}

// genericFolderNames are leaf names that describe a folder's role rather
// than its subject. A folder called "Downloads" tells us nothing about which
// course it belongs to — its parent does — but it is exactly where the
// downloads should go.
var genericFolderNames = map[string]bool{
	"downloads": true, "download": true, "dl": true,
	"files": true, "dateien": true,
	"material": true, "materialien": true, "materials": true,
	"unterlagen": true, "dokumente": true, "documents": true,
}

// skippedFolderNames never contain coursework and can be large.
var skippedFolderNames = map[string]bool{
	"node_modules": true, "$recycle.bin": true, "appdata": true,
	"system volume information": true,
}

func isGenericFolderName(name string) bool {
	return genericFolderNames[strings.ToLower(strings.TrimSpace(name))]
}

func shouldSkipFolder(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	return skippedFolderNames[strings.ToLower(name)]
}

// Scan collects the candidate destination folders under root using the
// default limits.
func Scan(root string) ([]Candidate, error) {
	return ScanLimited(root, DefaultMaxDepth, DefaultMaxDirs)
}

// ScanLimited is Scan with explicit limits, so tests (and any future caller
// with a differently shaped tree) can set their own.
//
// A folder that holds a generically-named child ("AlgData" holding
// "Downloads") is dropped in favour of that child: both mean the same
// course, and the child is the more specific answer. Unreadable
// subdirectories are skipped rather than failing the scan — a permission
// error somewhere in a big tree should cost one branch, not the feature.
func ScanLimited(root string, maxDepth, maxDirs int) ([]Candidate, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, errors.New("no download folder is configured yet")
	}

	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("could not resolve %s: %w", root, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("could not read %s: %w", abs, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a folder", abs)
	}

	var candidates []Candidate
	supersededBy := map[string]bool{}
	budget := maxDirs

	var walk func(dir, inherited string, depth int)
	walk = func(dir, inherited string, depth int) {
		if depth > maxDepth || budget <= 0 {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, entry := range entries {
			if !entry.IsDir() || shouldSkipFolder(entry.Name()) {
				continue
			}
			if budget <= 0 {
				return
			}
			budget--

			path := filepath.Join(dir, entry.Name())
			matchName := inherited
			if isGenericFolderName(entry.Name()) {
				if inherited != "" {
					supersededBy[dir] = true
				}
			} else {
				matchName = entry.Name()
			}

			if matchName != "" {
				candidates = append(candidates, Candidate{Path: path, MatchName: matchName})
			}
			walk(path, matchName, depth+1)
		}
	}
	walk(abs, "", 1)

	kept := candidates[:0]
	for _, c := range candidates {
		if !supersededBy[c.Path] {
			kept = append(kept, c)
		}
	}
	return kept, nil
}

// Suggest proposes at most one folder per course name.
//
// taken lists folders already spoken for (courses the user has configured by
// hand); they are excluded so a suggestion never sends two courses to one
// folder. The same rule applies within a single call: if one folder is the
// best answer for two courses, only the better-scoring course gets it, and
// the other is left blank rather than pointed somewhere plausible-looking.
//
// Courses with no confident match are simply absent from the result.
func Suggest(courses []string, candidates []Candidate, taken []string) map[string]string {
	excluded := map[string]bool{}
	for _, t := range taken {
		if key := folderKey(t); key != "" {
			excluded[key] = true
		}
	}

	type claim struct {
		course string
		path   string
		score  float64
	}
	claimed := map[string]claim{}

	sorted := append([]string(nil), courses...)
	sort.Strings(sorted)

	for _, course := range sorted {
		if strings.TrimSpace(course) == "" {
			continue
		}
		path, score, ok := bestCandidate(course, candidates, excluded)
		if !ok {
			continue
		}
		key := folderKey(path)
		if held, exists := claimed[key]; exists && held.score >= score {
			continue
		}
		claimed[key] = claim{course: course, path: path, score: score}
	}

	suggestions := make(map[string]string, len(claimed))
	for _, c := range claimed {
		suggestions[c.course] = c.path
	}
	return suggestions
}

// bestCandidate returns the highest-scoring folder for one course, provided
// it clears MinScore and leads the next-best *different* folder by
// MinMargin. Two folders that tie are the case this is built to refuse.
func bestCandidate(course string, candidates []Candidate, excluded map[string]bool) (string, float64, bool) {
	bestPath := ""
	bestScore := 0.0
	runnerUp := 0.0

	for _, c := range candidates {
		if excluded[folderKey(c.Path)] {
			continue
		}
		score := Score(course, c.MatchName)
		switch {
		case score > bestScore:
			runnerUp = bestScore
			bestScore, bestPath = score, c.Path
		case score > runnerUp:
			runnerUp = score
		}
	}

	if bestPath == "" || bestScore < MinScore || bestScore-runnerUp < MinMargin {
		return "", 0, false
	}
	return bestPath, bestScore, true
}

// folderKey normalises a path for comparison. Windows paths are
// case-insensitive and may be written with either separator, and the same
// folder reached two ways must not look like two folders.
func folderKey(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return ""
	}
	cleaned := filepath.Clean(strings.ReplaceAll(trimmed, "\\", string(filepath.Separator)))
	return strings.ToLower(strings.TrimRight(cleaned, string(filepath.Separator)))
}
