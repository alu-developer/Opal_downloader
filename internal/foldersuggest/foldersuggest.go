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
	"time"
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

	// DownloadsLeaf marks a folder that is named for its role rather than
	// its subject and sits under one that is named for the subject - the
	// "<course>/Downloads" convention. When a user organises that way,
	// being such a leaf is evidence that this is where files are meant to
	// go, which decides ties a name alone cannot.
	DownloadsLeaf bool

	// Modified is the folder's own modification time, used to prefer a
	// course being worked on now over one from an earlier semester with the
	// same name.
	Modified time.Time
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

// Options tunes a scan. The zero value is what Scan uses.
type Options struct {
	// MaxDepth and MaxDirs override DefaultMaxDepth/DefaultMaxDirs when
	// non-zero, so tests and differently-shaped trees can set their own.
	MaxDepth int
	MaxDirs  int

	// Ignore lists folder subtrees that must never be offered, given as
	// paths (absolute, or relative to the scan root).
	//
	// This exists because of a folder opal-downloader creates itself. With
	// default_course_folder set, every course that has no mapping yet gets
	// dumped into "<default>/<exact course name>" — which then scores a
	// perfect match against that course name and beats the abbreviated
	// folder the user actually keeps their work in.
	//
	// Measured on the maintainer's real tree before this existed: of six
	// courses, two were confidently pointed at the dumping ground and the
	// other four were withheld, because the tool's own folder tied with the
	// real one and the margin rule refused to choose. Nought out of six.
	// Suggesting that folder is circular anyway — it is where the files
	// already go when nothing is configured.
	Ignore []string
}

// Scan collects the candidate destination folders under root using the
// default limits and no exclusions.
func Scan(root string) ([]Candidate, error) {
	return ScanWith(root, Options{})
}

// ScanWith is Scan with explicit options.
//
// A folder that holds a generically-named child ("AlgData" holding
// "Downloads") is dropped in favour of that child: both mean the same
// course, and the child is the more specific answer. Unreadable
// subdirectories are skipped rather than failing the scan — a permission
// error somewhere in a big tree should cost one branch, not the feature.
func ScanWith(root string, opts Options) ([]Candidate, error) {
	maxDepth, maxDirs := opts.MaxDepth, opts.MaxDirs
	if maxDepth <= 0 {
		maxDepth = DefaultMaxDepth
	}
	if maxDirs <= 0 {
		maxDirs = DefaultMaxDirs
	}
	return scan(root, maxDepth, maxDirs, opts.Ignore)
}

func scan(root string, maxDepth, maxDirs int, ignore []string) ([]Candidate, error) {
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

	ignored := make(map[string]bool, len(ignore))
	for _, path := range ignore {
		trimmed := strings.TrimSpace(path)
		if trimmed == "" {
			continue
		}
		if !filepath.IsAbs(trimmed) {
			trimmed = filepath.Join(abs, filepath.FromSlash(trimmed))
		}
		if key := folderKey(trimmed); key != "" {
			ignored[key] = true
		}
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
			// Skipping the whole subtree, not just the folder itself: the
			// children of a dumping ground are exactly the problem.
			if ignored[folderKey(filepath.Join(dir, entry.Name()))] {
				continue
			}
			if budget <= 0 {
				return
			}
			budget--

			path := filepath.Join(dir, entry.Name())
			matchName := inherited
			downloadsLeaf := false
			if isGenericFolderName(entry.Name()) {
				if inherited != "" {
					supersededBy[dir] = true
					downloadsLeaf = true
				}
			} else {
				matchName = entry.Name()
			}

			if matchName != "" {
				var modified time.Time
				if info, err := entry.Info(); err == nil {
					modified = info.ModTime()
				}
				candidates = append(candidates, Candidate{
					Path:          path,
					MatchName:     matchName,
					DownloadsLeaf: downloadsLeaf,
					Modified:      modified,
				})
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

// bestCandidate returns the best folder for one course, or refuses.
//
// A name is often not enough to decide, and the interesting cases are all
// near-ties: last semester's "Analysis" against this semester's, or a backup
// tree mirroring the real one folder for folder. Measured against a real
// tree, name-only scoring got two of six right and one confidently wrong.
//
// So everything within TieBand of the leader is treated as
// indistinguishable-on-name and decided on two structural signals instead,
// in order:
//
//  1. The "<course>/Downloads" convention. A user who organises that way has
//     told us where files go; a candidate that is such a leaf beats one that
//     is not.
//  2. Recency. The course you are syncing is the one you have touched; an
//     identically-named folder from an earlier semester has been sitting
//     still for months. Required to be decisively newer (MinAgeGap), so two
//     folders in current use stay a genuine tie.
//
// Still ambiguous after both means no suggestion, which remains the whole
// point: an empty field costs seconds, a wrong one silently files a course
// where nobody will look for it.
func bestCandidate(course string, candidates []Candidate, excluded map[string]bool) (string, float64, bool) {
	type scored struct {
		candidate Candidate
		score     float64
	}

	var best float64
	var pool []scored
	for _, c := range candidates {
		if excluded[folderKey(c.Path)] {
			continue
		}
		score := Score(course, c.MatchName)
		if score < MinScore {
			continue
		}
		if score > best {
			best = score
		}
		pool = append(pool, scored{candidate: c, score: score})
	}
	if len(pool) == 0 {
		return "", 0, false
	}

	contenders := pool[:0:0]
	for _, s := range pool {
		if best-s.score <= TieBand {
			contenders = append(contenders, s)
		}
	}

	if len(contenders) > 1 {
		var leaves []scored
		for _, s := range contenders {
			if s.candidate.DownloadsLeaf {
				leaves = append(leaves, s)
			}
		}
		// Only let the convention decide when it actually discriminates:
		// some leaves and some non-leaves. All-or-none leaves nothing to cut.
		if len(leaves) > 0 && len(leaves) < len(contenders) {
			contenders = leaves
		}
	}

	if len(contenders) > 1 {
		sort.Slice(contenders, func(i, j int) bool {
			return contenders[i].candidate.Modified.After(contenders[j].candidate.Modified)
		})
		newest, next := contenders[0].candidate.Modified, contenders[1].candidate.Modified
		if newest.IsZero() || newest.Sub(next) < MinAgeGap {
			return "", 0, false
		}
		contenders = contenders[:1]
	}

	// The winner still has to lead the also-rans *outside* the tie band by
	// MinMargin - the original refuse-on-a-close-call rule. Rivals inside the
	// band are not also-rans: they were in contention and lost on structure
	// (leaf/recency), so their nearby score is expected, not a reason to
	// refuse. Checking them here would undo the whole tie-break, since the
	// case it exists for is two folders with an identical score.
	winner := contenders[0]
	for _, s := range pool {
		if best-s.score <= TieBand {
			continue
		}
		if winner.score-s.score < MinMargin {
			return "", 0, false
		}
	}
	return winner.candidate.Path, winner.score, true
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
