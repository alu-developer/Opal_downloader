package scraper

import (
	"testing"

	"github.com/alu-developer/opal-downloader/internal/sectioncache"
	"github.com/alu-developer/opal-downloader/internal/sectionhash"
)

// This is the correctness argument for the whole cache made concrete: a
// course whose every section is a cache hit must be crawled to completion -
// files found, subfolders discovered and followed - without ever touching
// the Playwright page. The page passed in is a fakePage (session_test.go)
// that embeds a nil playwright.Page and overrides nothing relevant here, so
// if the wiring regressed to visiting a cache-hit section with the browser
// anyway, s.visitSection would call a method on that nil embedded interface
// and this test would panic rather than merely fail.
//
// It also directly exercises the two traps 09059e4 found by reading the
// crawl rather than assuming: the root section's cached candidates must
// still feed appendSectionFolderTargets (so the child section gets queued at
// all - trap 1), and the child section's own cache hit must be found and
// replayed too (proving the cache works across BFS levels, not just for a
// single-section course).
func TestCollectCourseFilesSkipsTheBrowserOnAFullyCachedCourse(t *testing.T) {
	t.Setenv(SectionCacheEnv, "1")

	const opalURL = "https://bildungsportal.sachsen.de/opal/"
	const repoID = "53290106881"
	const rootURL = "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/53290106881"
	const childURL = "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/53290106881/CourseNode/1775615795226691003"

	rootCandidates := []map[string]string{
		{
			"href":     childURL,
			"title":    "Übungen",
			"text":     "Übungen",
			"rootText": "Übungen",
		},
	}
	childCandidates := []map[string]string{
		{
			"href":    "/opal/goto.php?target=file_55&cmd=sendfile",
			"title":   "Folien Woche 1.pdf",
			"text":    "Folien Woche 1.pdf",
			"rowText": "Folien Woche 1.pdf 245 KB 01.07.2026, 09:06 Uhr",
		},
	}

	html := map[string]string{
		rootURL:  "<html>root section, unchanged</html>",
		childURL: "<html>child section, unchanged</html>",
	}
	hash := map[string]string{
		rootURL:  sectionhash.Of(html[rootURL]),
		childURL: sectionhash.Of(html[childURL]),
	}

	// Record populates the cache's "what this run saw" side, but a hit is
	// read from "what was recorded last time" (sectioncache.Cache.Unchanged/
	// Files) - so priming a test's cache needs the same Save-then-Load round
	// trip a real second sync would go through, not a direct Record.
	dir := t.TempDir()
	seed := sectioncache.Load(dir)
	seed.Record(rootURL, hash[rootURL], packSection(rootCandidates, "", "", false))
	seed.Record(childURL, hash[childURL], packSection(childCandidates, "", "", false))
	if err := seed.Save(dir); err != nil {
		t.Fatalf("seeding the cache: %v", err)
	}
	cache := sectioncache.Load(dir)

	s := &OpalScraper{
		opalURL:            opalURL,
		downloadCandidates: map[string]downloadCandidate{},
		sectionTiming:      &sectionTiming{},
		stall:              &stallWatch{},
		sectionCache:       cache,
	}
	s.sectionCacheFetch = func(url string) (string, error) {
		body, ok := html[url]
		if !ok {
			t.Fatalf("fetched an unexpected URL %q - the crawl should only ever probe rootURL and childURL", url)
		}
		return body, nil
	}

	course := CourseRef{RepoID: repoID, Title: "Algorithmen und Datenstrukturen", URL: rootURL}
	_, files, _, err := s.collectCourseFiles(&fakePage{name: "root"}, course)
	if err != nil {
		t.Fatalf("collectCourseFiles: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected the child section's one file to be found via the cache, got %d: %#v", len(files), files)
	}
	if files[0].Name != "Folien Woche 1.pdf" {
		t.Fatalf("unexpected file: %#v", files[0])
	}
	if files[0].Size == nil || *files[0].Size != 245*1024 {
		t.Fatalf("file metadata did not survive the cache replay: %#v", files[0].Size)
	}
}
