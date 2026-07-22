package syncer

import (
	"context"
	"strings"
	"testing"

	"github.com/alu-developer/opal-downloader/internal/config"
	"github.com/alu-developer/opal-downloader/internal/scraper"
)

func TestFormatDiscoveryProgress(t *testing.T) {
	cases := []struct {
		name  string
		in    scraper.DiscoveryProgress
		want  string
		empty bool
	}{
		{
			name: "courses found",
			in:   scraper.DiscoveryProgress{Phase: scraper.PhaseCoursesFound, TotalCourses: 6},
			want: "Found 6 course(s) - starting to scan them",
		},
		{
			name: "course started with a known total",
			in:   scraper.DiscoveryProgress{Phase: scraper.PhaseCourseStarted, Course: "Analysis", CourseIndex: 2, TotalCourses: 6},
			want: "Scanning course 2 of 6: Analysis",
		},
		{
			name: "course started without a total still names the course",
			in:   scraper.DiscoveryProgress{Phase: scraper.PhaseCourseStarted, Course: "Analysis"},
			want: "Scanning course: Analysis",
		},
		{
			name: "section",
			in:   scraper.DiscoveryProgress{Phase: scraper.PhaseSection, Course: "Analysis", Section: "Übungsblätter", SectionsDone: 3},
			want: "Analysis - section 3: Übungsblätter",
		},
		{
			name: "section with no title falls back to a count",
			in:   scraper.DiscoveryProgress{Phase: scraper.PhaseSection, Course: "Analysis", SectionsDone: 3},
			want: "Analysis - scanned 3 section(s)",
		},
		{
			name: "course done",
			in:   scraper.DiscoveryProgress{Phase: scraper.PhaseCourseDone, Course: "Analysis", FileCount: 30},
			want: "Finished Analysis - found 30 file(s)",
		},
		{
			name:  "unknown phase renders nothing",
			in:    scraper.DiscoveryProgress{Phase: "something-else"},
			empty: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := formatDiscoveryProgress(tc.in)
			if tc.empty {
				if got != "" {
					t.Fatalf("expected no line for an unrenderable event, got %q", got)
				}
				return
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// progressReportingDownloader is a fake that implements BOTH Downloader and
// the optional discoveryProgressReporter, so the relay wiring can be
// exercised without a browser.
type progressReportingDownloader struct {
	fn       func(scraper.DiscoveryProgress)
	setCalls int
	cleared  bool
}

func (d *progressReportingDownloader) SetDiscoveryProgress(fn func(scraper.DiscoveryProgress)) {
	d.setCalls++
	if fn == nil {
		d.cleared = true
	}
	d.fn = fn
}

func (d *progressReportingDownloader) ScrapeWithSavedSession(ctx context.Context, courseFilter []string) ([]scraper.RemoteFile, error) {
	// Emit while "crawling", exactly as the real scraper does.
	if d.fn != nil {
		d.fn(scraper.DiscoveryProgress{Phase: scraper.PhaseCoursesFound, TotalCourses: 1})
		d.fn(scraper.DiscoveryProgress{Phase: scraper.PhaseCourseStarted, Course: "Analysis", CourseIndex: 1, TotalCourses: 1})
		d.fn(scraper.DiscoveryProgress{Phase: scraper.PhaseSection, Course: "Analysis", Section: "Material", SectionsDone: 1})
	}
	return nil, nil
}

func (d *progressReportingDownloader) DownloadFile(fileURL, localPath string) error { return nil }

// The whole point of this plumbing: discovery must report progress *while*
// it runs, not only after it has finished.
func TestSyncRelaysDiscoveryProgress(t *testing.T) {
	dl := &progressReportingDownloader{}
	var lines []string
	_, err := SyncCoursesWithProgress(context.Background(), dl, config.App{DownloadPath: t.TempDir()}, false, func(e Event) {
		if e.Type == EventDiscovery {
			lines = append(lines, e.Message)
		}
	})
	if err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	if len(lines) != 3 {
		t.Fatalf("expected 3 discovery lines, got %d: %v", len(lines), lines)
	}
	if !strings.Contains(lines[1], "Scanning course 1 of 1: Analysis") {
		t.Fatalf("unexpected course line: %q", lines[1])
	}
	if !strings.Contains(lines[2], "Material") {
		t.Fatalf("expected the section name to reach the caller, got %q", lines[2])
	}
	// The callback must be torn down, or a later call publishes into a stale
	// closure belonging to a finished run.
	if !dl.cleared {
		t.Fatal("expected the discovery callback to be cleared after discovery finished")
	}
}

// A downloader that does NOT implement the optional interface must still
// sync normally - that is why it is a separate interface from Downloader.
func TestSyncWorksWithoutProgressReporting(t *testing.T) {
	var plain Downloader = &countingDownloader{}
	if _, err := SyncCoursesWithProgress(context.Background(), plain, config.App{DownloadPath: t.TempDir()}, false, nil); err != nil {
		t.Fatalf("sync with a non-reporting downloader failed: %v", err)
	}
}

type countingDownloader struct{}

func (c *countingDownloader) ScrapeWithSavedSession(ctx context.Context, courseFilter []string) ([]scraper.RemoteFile, error) {
	return nil, nil
}
func (c *countingDownloader) DownloadFile(fileURL, localPath string) error { return nil }
