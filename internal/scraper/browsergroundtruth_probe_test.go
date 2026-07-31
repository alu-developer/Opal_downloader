package scraper

// Browser ground-truth probe for the sync-speed campaign. The HTTP probe
// (httpdiscovery_probe_test.go) showed HTTP discovery is much faster but
// finds fewer files than the browser (158 vs 207 on Softwaretechnologie).
// That gap is exactly what the campaign doc rejected HTTP-first over — and
// what it never diagnosed. This probe runs the REAL browser crawl for a
// single course and writes the discovered files (one per line, as
// "course<TAB>section_url<TAB>name") so the gap can be diffed against the
// HTTP probe's per-section detail (OPAL_HTTPPROBE_DETAIL) attribute-by-
// attribute, not just counted.
//
// It changes no behaviour: skips by default, opt-in via env.
//
// Usage:
//   OPAL_BROWSER_GROUNDTRUTH=Softwaretechnologie \
//     go test ./internal/scraper/ -run TestBrowserGroundTruthProbe -v -timeout 20m
//
// Writes the file list to OPAL_BROWSER_GROUNDTRUTH_OUT (default
// tmp/baseline/browser-groundtruth.txt in the repo root).

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/alu-developer/opal-downloader/internal/config"
)

func TestBrowserGroundTruthProbe(t *testing.T) {
	courseFilter := os.Getenv("OPAL_BROWSER_GROUNDTRUTH")
	if courseFilter == "" {
		t.Skip("set OPAL_BROWSER_GROUNDTRUTH=<course name substring>")
	}
	captureProbeLogs(t)

	const repo = `C:\07_Arbeitszeug\Open_github\Opal_downloader`
	loaded, err := config.Load(filepath.Join(repo, "config.yaml"))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	out := os.Getenv("OPAL_BROWSER_GROUNDTRUTH_OUT")
	if out == "" {
		out = filepath.Join(repo, "tmp", "baseline", "browser-groundtruth.txt")
	}

	sc := New(loaded.Credentials.URL, loaded.Credentials.StateFile)
	defer sc.Close()

	files, err := sc.ScrapeWithSavedSession(context.Background(), []string{courseFilter})
	if err != nil {
		t.Fatalf("scrape: %v", err)
	}

	sort.Slice(files, func(i, j int) bool {
		if files[i].SectionTitle != files[j].SectionTitle {
			return files[i].SectionTitle < files[j].SectionTitle
		}
		return files[i].Name < files[j].Name
	})

	var b strings.Builder
	seen := map[string]bool{}
	for _, f := range files {
		line := fmt.Sprintf("%s\t%s\t%s\n", f.Course, f.Path, f.Name)
		key := f.Course + "|" + f.Path + "|" + f.Name
		if !seen[key] {
			seen[key] = true
			b.WriteString(line)
		}
	}
	if err := os.WriteFile(out, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write out: %v", err)
	}

	distinct := map[string]bool{}
	for _, f := range files {
		distinct[f.Name] = true
	}
	t.Logf("BROWSER GROUNDTRUTH: %d files, %d distinct names for filter %q",
		len(files), len(distinct), courseFilter)
	t.Logf("  written to %s", out)
}
