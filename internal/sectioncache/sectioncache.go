// Package sectioncache remembers what each OPAL section looked like last time,
// so an unchanged section can be skipped instead of crawled.
//
// It stores only sectionURL -> hash. Deliberately NOT the extractor's output:
// the 2026-07-21 attempt stored that and produced a 52 MB file for 276
// sections, 31 MB of it the same section text repeated. It never needed to -
// on a hit, that section's files are already known from the previous run.
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
const SchemaVersion = 1

// FileName sits beside the sync manifest, in the download root.
const FileName = ".opal-sync.sections.json"

type file struct {
	SchemaVersion   int               `json:"schema_version"`
	PatternsVersion int               `json:"patterns_version"`
	Sections        map[string]string `json:"sections"`
}

// Cache is safe for concurrent Lookup; Record is not, and the crawl records
// under its own lock.
type Cache struct {
	previous map[string]string
	current  map[string]string
}

// Load reads the cache from root. Any problem yields an empty cache and a nil
// error - the caller's correct response to every one of them is identical
// (crawl everything), and returning an error would invite a caller to abort a
// sync over a cache file it could simply ignore.
func Load(root string) *Cache {
	c := &Cache{previous: map[string]string{}, current: map[string]string{}}
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
		if v != "" {
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
	return c.previous[sectionURL] == hash
}

// Record stores what this run saw, for the next one.
func (c *Cache) Record(sectionURL, hash string) {
	if c == nil || hash == "" || sectionURL == "" {
		return
	}
	c.current[sectionURL] = hash
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
