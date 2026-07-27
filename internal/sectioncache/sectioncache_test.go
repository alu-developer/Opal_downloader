package sectioncache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/alu-developer/opal-downloader/internal/sectionhash"
)

const url = "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1/CourseNode/2"

func writeFile(t *testing.T, root string, f file) {
	t.Helper()
	body, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, FileName), body, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestRoundTrip(t *testing.T) {
	root := t.TempDir()
	c := Load(root)
	c.Record(url, "abc", []string{"Skript.pdf"})
	if err := c.Save(root); err != nil {
		t.Fatalf("save: %v", err)
	}

	next := Load(root)
	if !next.Unchanged(url, "abc") {
		t.Fatal("a section that hashed the same was not reported unchanged")
	}
	if next.Unchanged(url, "different") {
		t.Fatal("a changed hash was reported unchanged")
	}
}

// Every one of these must degrade to "crawl it". They are listed together
// because they share the single property that matters: the safe answer to a
// cache we cannot trust is always the same, and any of them answering
// "unchanged" would silently skip downloads.
func TestEveryUntrustworthyStateDegradesToCrawl(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, root string)
	}{
		{"no cache file at all", func(*testing.T, string) {}},
		{"unreadable JSON", func(t *testing.T, root string) {
			if err := os.WriteFile(filepath.Join(root, FileName), []byte("{not json"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"a newer schema", func(t *testing.T, root string) {
			writeFile(t, root, file{SchemaVersion: SchemaVersion + 1, PatternsVersion: sectionhash.PatternsVersion, Sections: map[string]entry{url: {Hash: "abc", Files: json.RawMessage(`[]`)}}})
		}},
		{
			// The dangerous one. Patterns changing means old hashes describe
			// different normalisation, so trusting them can match HTML it
			// should not.
			"hashes written by a different pattern set",
			func(t *testing.T, root string) {
				writeFile(t, root, file{SchemaVersion: SchemaVersion, PatternsVersion: sectionhash.PatternsVersion + 1, Sections: map[string]entry{url: {Hash: "abc", Files: json.RawMessage(`[]`)}}})
			},
		},
		{"an empty stored hash", func(t *testing.T, root string) {
			writeFile(t, root, file{SchemaVersion: SchemaVersion, PatternsVersion: sectionhash.PatternsVersion, Sections: map[string]entry{url: {Hash: "", Files: json.RawMessage(`[]`)}}})
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			tc.setup(t, root)
			if Load(root).Unchanged(url, "abc") {
				t.Fatal("reported unchanged from a cache that must not be trusted")
			}
		})
	}
}

// A failed fetch hashes to "". Two of them agreeing about nothing is not
// evidence a section is unchanged - without this, a section that cannot be
// read would be skipped forever.
func TestAFailedFetchIsNeverAMatch(t *testing.T) {
	root := t.TempDir()
	c := Load(root)
	c.Record(url, "", []string{"x"})
	if err := c.Save(root); err != nil {
		t.Fatal(err)
	}
	if Load(root).Unchanged(url, "") {
		t.Fatal("two empty hashes matched")
	}
}

// A section removed from OPAL must not keep answering "unchanged" out of a
// cache nobody rewrote.
func TestUnrecordedSectionsAreDropped(t *testing.T) {
	root := t.TempDir()
	first := Load(root)
	first.Record(url, "abc", []string{"a"})
	first.Record(url+"/gone", "def", []string{"b"})
	if err := first.Save(root); err != nil {
		t.Fatal(err)
	}

	second := Load(root)
	second.Record(url, "abc", []string{"a"}) // only this one is seen this run
	if err := second.Save(root); err != nil {
		t.Fatal(err)
	}

	third := Load(root)
	if third.Unchanged(url+"/gone", "def") {
		t.Fatal("a section not seen this run survived into the next cache")
	}
	if !third.Unchanged(url, "abc") {
		t.Fatal("a section that was seen did not survive")
	}
}

// A v1 entry has a hash and no rows. Trusting it would report the section
// unchanged and return nothing, dropping every file in it - the exact silent
// loss this cache exists to avoid causing.
func TestAHashWithNoRowsIsNeverAHit(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, file{
		SchemaVersion:   SchemaVersion,
		PatternsVersion: sectionhash.PatternsVersion,
		Sections:        map[string]entry{url: {Hash: "abc"}},
	})
	if Load(root).Unchanged(url, "abc") {
		t.Fatal("an entry carrying no file rows was reported as a hit")
	}
}

// A section that genuinely holds no files must still be a hit, or every empty
// section is re-crawled forever.
func TestASectionWithZeroFilesIsStillAHit(t *testing.T) {
	root := t.TempDir()
	c := Load(root)
	c.Record(url, "abc", []string{})
	if err := c.Save(root); err != nil {
		t.Fatal(err)
	}
	next := Load(root)
	if !next.Unchanged(url, "abc") {
		t.Fatal("a section with zero files was not a hit")
	}
	if string(next.Files(url)) != "[]" {
		t.Fatalf("expected an empty list back, got %q", next.Files(url))
	}
}

func TestFilesRoundTrip(t *testing.T) {
	type row struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	}
	root := t.TempDir()
	c := Load(root)
	c.Record(url, "abc", []row{{Name: "Skript.pdf", URL: "https://example/Skript.pdf"}})
	if err := c.Save(root); err != nil {
		t.Fatal(err)
	}

	var got []row
	if err := json.Unmarshal(Load(root).Files(url), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got) != 1 || got[0].Name != "Skript.pdf" || got[0].URL != "https://example/Skript.pdf" {
		t.Fatalf("rows did not survive the round trip: %+v", got)
	}
}

func TestNilCacheIsSafe(t *testing.T) {
	var c *Cache
	if c.Unchanged(url, "abc") {
		t.Fatal("a nil cache reported unchanged")
	}
	c.Record(url, "abc", []string{"Skript.pdf"})
	if err := c.Save(t.TempDir()); err != nil {
		t.Fatalf("saving a nil cache should be a no-op, got %v", err)
	}
}
