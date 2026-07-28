package scraper

import (
	"errors"
	"testing"

	"github.com/alu-developer/opal-downloader/internal/sectioncache"
)

// A section whose HTML the fetch seam returns unchanged from what was
// recorded last time must be a cache hit that replays the recorded
// candidates and show-all fields exactly - the whole point of the cache is
// to make that replay indistinguishable from a fresh crawl's own result.
func TestCheckSectionCacheHitsAndReplaysExactly(t *testing.T) {
	t.Setenv(SectionCacheEnv, "1")
	s := &OpalScraper{sectionCache: sectioncache.Load(t.TempDir())}

	const url = "https://example/opal/CourseNode/1"
	const html = "<html>unchanged section content</html>"
	s.sectionCacheFetch = func(got string) (string, error) {
		if got != url {
			t.Fatalf("fetched %q, want %q", got, url)
		}
		return html, nil
	}

	candidates := []map[string]string{{"href": "/a.pdf", "text": "a.pdf", "rootText": "a.pdf 1MB"}}
	first := s.checkSectionCache(url)
	if first.hit {
		t.Fatal("first probe against an empty cache must be a miss")
	}
	if first.hash == "" {
		t.Fatal("a successful fetch must produce a non-empty hash")
	}
	s.recordSectionCache(url, first.hash, candidates, "https://example/show-all", "https://example/expanded?7", true)

	// A fresh Cache instance, loaded from what was just saved - this is the
	// shape a real second sync would see, not the same in-memory object.
	dir := t.TempDir()
	if err := s.sectionCache.Save(dir); err != nil {
		t.Fatalf("Save: %v", err)
	}
	s.sectionCache = sectioncache.Load(dir)

	second := s.checkSectionCache(url)
	if !second.hit {
		t.Fatal("identical HTML on the next run must be a cache hit")
	}
	if len(second.visit.candidates) != 1 || second.visit.candidates[0]["href"] != "/a.pdf" || second.visit.candidates[0]["rootText"] != "a.pdf 1MB" {
		t.Fatalf("candidates did not replay exactly: %#v", second.visit.candidates)
	}
	if second.visit.showAllURL != "https://example/show-all" || second.visit.expandedPageURL != "https://example/expanded?7" || !second.visit.expandedShowAll {
		t.Fatalf("show-all fields did not replay exactly: %#v", second.visit)
	}
}

// Changed HTML must never be reported as a hit - this is the asymmetric
// safety property the whole cache rests on. A miss costs one crawl; a false
// hit silently stops downloading a section that changed.
func TestCheckSectionCacheMissesOnChangedContent(t *testing.T) {
	t.Setenv(SectionCacheEnv, "1")
	s := &OpalScraper{sectionCache: sectioncache.Load(t.TempDir())}
	const url = "https://example/opal/CourseNode/1"

	s.sectionCacheFetch = func(string) (string, error) { return "<html>version A</html>", nil }
	first := s.checkSectionCache(url)
	s.recordSectionCache(url, first.hash, nil, "", "", false)

	s.sectionCacheFetch = func(string) (string, error) { return "<html>version B, a new file appeared</html>", nil }
	second := s.checkSectionCache(url)
	if second.hit {
		t.Fatal("changed HTML must not be reported as a hit")
	}
}

// Every way a probe can fail to positively confirm "unchanged" must degrade
// to hit=false rather than error out - the caller's uniform response is to
// crawl the section normally, exactly as if this feature did not exist.
func TestCheckSectionCacheDegradesToMissOnFailure(t *testing.T) {
	t.Setenv(SectionCacheEnv, "1")
	url := "https://example/opal/CourseNode/1"

	t.Run("fetch error", func(t *testing.T) {
		s := &OpalScraper{sectionCache: sectioncache.Load(t.TempDir())}
		s.sectionCacheFetch = func(string) (string, error) { return "", errors.New("network down") }
		probe := s.checkSectionCache(url)
		if probe.hit || probe.hash != "" {
			t.Fatalf("a fetch error must produce a completely empty probe, got %#v", probe)
		}
	})

	t.Run("empty body", func(t *testing.T) {
		s := &OpalScraper{sectionCache: sectioncache.Load(t.TempDir())}
		s.sectionCacheFetch = func(string) (string, error) { return "", nil }
		probe := s.checkSectionCache(url)
		if probe.hit || probe.hash != "" {
			t.Fatalf("an empty body must produce a completely empty probe (sectionhash.Of(\"\") is \"\"), got %#v", probe)
		}
	})

	t.Run("no cache installed", func(t *testing.T) {
		s := &OpalScraper{}
		s.sectionCacheFetch = func(string) (string, error) { return "<html>content</html>", nil }
		probe := s.checkSectionCache(url)
		if probe.hit {
			t.Fatal("a nil cache must never report a hit")
		}
	})
}

// The env flag is the whole off-by-default gate: without it, a scrape must
// behave exactly as if SetSectionCache had never been called, even though a
// caller wired up a real cache and a working fetch seam.
func TestSectionCacheDisabledWithoutEnvFlag(t *testing.T) {
	s := &OpalScraper{sectionCache: sectioncache.Load(t.TempDir())}
	fetchCalled := false
	s.sectionCacheFetch = func(string) (string, error) {
		fetchCalled = true
		return "<html>content</html>", nil
	}

	probe := s.checkSectionCache("https://example/opal/CourseNode/1")
	if probe.hit || probe.hash != "" {
		t.Fatalf("disabled cache must produce a completely empty probe, got %#v", probe)
	}
	if fetchCalled {
		t.Fatal("disabled cache must not even attempt a fetch")
	}

	// recordSectionCache must likewise no-op, not just checkSectionCache.
	s.recordSectionCache("https://example/opal/CourseNode/1", "somehash", nil, "", "", false)
	if err := s.sectionCache.Save(t.TempDir()); err != nil {
		t.Fatalf("Save: %v", err)
	}
}

// A hash match against rows that fail to unmarshal must not be trusted as a
// hit - the cache file could have been hand-edited or partially written.
func TestCheckSectionCacheDoesNotTrustUnreadableRows(t *testing.T) {
	t.Setenv(SectionCacheEnv, "1")
	dir := t.TempDir()
	s := &OpalScraper{sectionCache: sectioncache.Load(dir)}
	const url = "https://example/opal/CourseNode/1"
	const html = "<html>content</html>"

	s.sectionCacheFetch = func(string) (string, error) { return html, nil }
	probe := s.checkSectionCache(url)
	// Record something that is valid JSON but not a cachedSection - Record
	// itself can't produce this (packSection always yields a valid shape),
	// so this exercises what a corrupted cache file would do on Load.
	s.sectionCache.Record(url, probe.hash, 42)

	second := s.checkSectionCache(url)
	if second.hit {
		t.Fatal("a hash match with unreadable rows must not be treated as a hit")
	}
	if second.hash != probe.hash {
		t.Fatalf("the fresh hash must still be reported so the caller can record correctly after crawling, got %q want %q", second.hash, probe.hash)
	}
}
