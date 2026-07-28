package scraper

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/alu-developer/opal-downloader/internal/config"
	"github.com/alu-developer/opal-downloader/internal/sectioncache"
)

// The instrument for the one question that decides whether blocking inline file
// previews (previews.go) is safe: does the crawl still find exactly the same
// files?
//
// A file *count* is not enough and this repo has the scars to prove it - the
// 2026-07-26 concurrency work lost nine files while two runs reported counts
// inside normal variance, and the 2026-07-21 stability-poll change lost files
// "byte-for-byte identically to the unfixed code". So this writes every file's
// course, section, name and URL, sorted, to a file that can be diffed.
//
// Usage:
//
//	OPAL_FILELIST=before                             go test ./internal/scraper/ -run TestFileListSnapshot -v -timeout 30m
//	OPAL_FILELIST=after OPAL_BLOCK_FILE_PREVIEWS=1   go test ./internal/scraper/ -run TestFileListSnapshot -v -timeout 30m
//	diff "tmp/filelist-before.txt" "tmp/filelist-after.txt"
//
// An empty diff is the only acceptable result.
func TestFileListSnapshot(t *testing.T) {
	label := os.Getenv("OPAL_FILELIST")
	if label == "" {
		t.Skip("set OPAL_FILELIST=<label> to snapshot the real account's file list")
	}

	loaded, err := config.Load(filepath.Join("..", "..", "config.yaml"))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	sc := New(loaded.Credentials.URL, loaded.Credentials.StateFile)
	sc.SetCourseConcurrency(loaded.App.CourseConcurrency)
	sc.SetSectionConcurrency(loaded.App.SectionConcurrency)
	sc.SetSkipEnrollmentSections(loaded.App.SkipEnrollmentSections)
	defer sc.Close()

	// Reusable for the section-cache A/B the same way this probe already is
	// for the previews one: only touches the cache file when SectionCacheEnv
	// is set, mirroring syncer.go's own gate exactly (see its doc comment for
	// why an unconditional Load/Save would be its own hazard).
	if os.Getenv(SectionCacheEnv) != "" {
		cacheDir := filepath.Join("..", "..", "tmp")
		if err := os.MkdirAll(cacheDir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", cacheDir, err)
		}
		cache := sectioncache.Load(cacheDir)
		sc.SetSectionCache(cache)
		defer func() {
			if err := cache.Save(cacheDir); err != nil {
				t.Fatalf("save section cache: %v", err)
			}
		}()
	}

	files, err := sc.ScrapeWithSavedSession(context.Background(), loaded.App.Courses)
	if err != nil {
		t.Fatalf("scrape: %v", err)
	}

	lines := make([]string, 0, len(files))
	for _, f := range files {
		// Name and URL both: a run that finds the same names via different
		// URLs, or the same URLs under a different section, has still changed
		// something worth seeing.
		lines = append(lines, fmt.Sprintf("%s\t%s\t%s\t%s", f.Course, f.SectionTitle, f.Name, f.URL))
	}
	sort.Strings(lines)

	out := filepath.Join("..", "..", "tmp", "filelist-"+label+".txt")
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := fmt.Sprintf("%d files\n", len(files)) + strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(out, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", out, err)
	}
	t.Logf("%d files written to %s (previews blocked: %q)", len(files), out, os.Getenv(blockPreviewsEnv))
}
