// Package sectioncache remembers what each OPAL section looked like last time,
// so an unchanged section can be skipped instead of crawled.
//
// It stores, per section, a hash of the page and the file rows that page
// produced. Deliberately NOT the extractor's raw candidates: the 2026-07-21
// attempt stored those and produced a 52 MB file for 276 sections, 31 MB of it
// the same section text repeated across 9,958 candidates. The resulting rows
// are ~345 of ~200 bytes, about 70 KB. It was the wrong thing being stored,
// not the storing.
//
// The rows are opaque JSON here. A parallel row type would have to be kept in
// step with scraper.FileRef by hand, and a field silently missing from the
// copy is precisely the kind of quiet loss this cache must not introduce.
//
// Every failure degrades to "crawl it": a missing file, unreadable JSON, a
// schema bump, a different sectionhash.PatternsVersion, an empty hash. That
// direction is the whole safety argument. A miss costs one section's crawl; a
// wrong hit means a changed section is reported unchanged and silently stops
// downloading, which is this project's worst failure mode.
package sectioncache

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/alu-developer/opal-downloader/internal/sectionhash"
)

// SchemaVersion covers the file's shape. sectionhash.PatternsVersion covers
// what the hashes mean; both must match for an entry to be trusted.
// Bumped to 2 on 2026-07-27 when entries gained their file rows. A v1 file
// carries hashes with no rows, and a hit that returns no files would drop that
// section's files entirely - the degrade-to-crawl path below rejects it.
const SchemaVersion = 2

// FileName sits beside the sync manifest, in the download root.
const FileName = ".opal-sync.sections.json"

// entry is what one section looked like last time.
type entry struct {
	Hash string `json:"hash"`
	// Files is nil when nothing was recorded and `[]` when the section
	// genuinely held none. The distinction matters: the first must not be a
	// hit, the second must.
	Files json.RawMessage `json:"files"`
}

type file struct {
	SchemaVersion   int              `json:"schema_version"`
	PatternsVersion int              `json:"patterns_version"`
	Sections        map[string]entry `json:"sections"`
}

// Cache is safe for concurrent Lookup; Record is not, and the crawl records
// under its own lock.
type Cache struct {
	previous map[string]entry
	current  map[string]entry
}

// Load reads the cache from root. Any problem yields an empty cache and a nil
// error - the caller's correct response to every one of them is identical
// (crawl everything), and returning an error would invite a caller to abort a
// sync over a cache file it could simply ignore.
func Load(root string) *Cache {
	c := &Cache{previous: map[string]entry{}, current: map[string]entry{}}
	raw, err := os.ReadFile(filepath.Join(root, FileName))
	if err != nil {
		return c
	}
	var f file
	if err := json.Unmarshal(raw, &f); err != nil {
		return c
	}
	if f.SchemaVersion != SchemaVersion || f.PatternsVersion != sectionhash.PatternsVersion {
		return c
	}
	for k, v := range f.Sections {
		// A hash with no rows cannot be a hit: returning it would report the
		// section unchanged and hand back nothing, dropping its files.
		// `!= nil` is not enough: a nil RawMessage marshals to `null` and comes
		// back as the four bytes "null", which is not nil. Caught by
		// TestAHashWithNoRowsIsNeverAHit, and without it a v1-shaped entry
		// would be a hit that returns no files.
		if v.Hash != "" && len(v.Files) > 0 && string(v.Files) != "null" {
			c.previous[k] = v
		}
	}
	return c
}

// Unchanged reports whether this section hashed the same as last time.
//
// An empty hash is never a match, even against another empty one: an empty
// hash means the fetch failed, and two failed fetches agreeing about nothing
// is not evidence that a section is unchanged.
func (c *Cache) Unchanged(sectionURL, hash string) bool {
	if c == nil || hash == "" || sectionURL == "" {
		return false
	}
	prev, ok := c.previous[sectionURL]
	return ok && prev.Hash == hash
}

// Files returns the rows recorded for a section, for the caller to unmarshal
// into its own type. Only meaningful after Unchanged reported true.
func (c *Cache) Files(sectionURL string) json.RawMessage {
	if c == nil {
		return nil
	}
	return c.previous[sectionURL].Files
}

// Record stores what this run saw, for the next one. files is marshalled here
// so callers pass their own type; a marshal failure records nothing rather than
// an entry with no rows, which the next run would have to reject anyway.
func (c *Cache) Record(sectionURL, hash string, files any) {
	if c == nil || hash == "" || sectionURL == "" {
		return
	}
	raw, err := json.Marshal(files)
	if err != nil {
		return
	}
	c.current[sectionURL] = entry{Hash: hash, Files: raw}
}

// Save writes what this run recorded. Sections not recorded are dropped rather
// than carried over, so a section that vanished from OPAL cannot keep
// answering "unchanged" forever.
func (c *Cache) Save(root string) error {
	if c == nil {
		return nil
	}
	body, err := json.Marshal(file{
		SchemaVersion:   SchemaVersion,
		PatternsVersion: sectionhash.PatternsVersion,
		Sections:        c.current,
	})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, FileName), body, 0o600)
}
