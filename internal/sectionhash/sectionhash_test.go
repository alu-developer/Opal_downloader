package sectionhash

import "testing"

// The asymmetry is the whole design: a hash that differs for no reason costs
// one crawl, a hash that matches when content changed silently stops
// downloading. So these tests are lopsided on purpose - one case for "ignores
// bookkeeping", many for "never ignores content".

func TestIgnoresWicketBookkeeping(t *testing.T) {
	// Both sides are the shapes measured live on 2026-07-27, not invented ones.
	a := `<a id="id35a0c" href="/opal/x?2284-1.0-header-link">f</a>` +
		`<button id="tableMultiAction_VFSItemTable_9072download">d</button>` +
		`<script>Wicket.Ajax.baseUrl="auth/x?2286";</script>`
	b := `<a id="id35a85" href="/opal/x?2285-1.0-header-link">f</a>` +
		`<button id="tableMultiAction_VFSItemTable_9079download">d</button>` +
		`<script>Wicket.Ajax.baseUrl="auth/x?2287";</script>`

	if Of(a) != Of(b) {
		t.Fatalf("two fetches differing only in Wicket bookkeeping hashed differently:\n  %s\n  %s", Normalize(a), Normalize(b))
	}
}

func TestNeverIgnoresContent(t *testing.T) {
	base := `<table id="id35a0c"><tr><td><a href="/opal/auth/RepositoryEntry/1/CourseNode/2/Skript.pdf">Skript.pdf</a></td><td>1,2 MB</td></tr></table>`

	cases := []struct {
		name    string
		changed string
	}{
		{
			"a file is renamed",
			`<table id="id35a0c"><tr><td><a href="/opal/auth/RepositoryEntry/1/CourseNode/2/Skript.pdf">Skript_v2.pdf</a></td><td>1,2 MB</td></tr></table>`,
		},
		{
			"a file's URL changes",
			`<table id="id35a0c"><tr><td><a href="/opal/auth/RepositoryEntry/1/CourseNode/2/Skript_v2.pdf">Skript.pdf</a></td><td>1,2 MB</td></tr></table>`,
		},
		{
			"a file's size changes",
			`<table id="id35a0c"><tr><td><a href="/opal/auth/RepositoryEntry/1/CourseNode/2/Skript.pdf">Skript.pdf</a></td><td>1,3 MB</td></tr></table>`,
		},
		{
			"a file is added",
			`<table id="id35a0c"><tr><td><a href="/opal/auth/RepositoryEntry/1/CourseNode/2/Skript.pdf">Skript.pdf</a></td><td>1,2 MB</td></tr><tr><td><a href="/opal/auth/RepositoryEntry/1/CourseNode/2/Neu.pdf">Neu.pdf</a></td><td>9 KB</td></tr></table>`,
		},
		{
			"a file is removed",
			`<table id="id35a0c"></table>`,
		},
		{
			// The case a file count cannot see. One section did exactly this in
			// a single real run on 2026-07-27.
			"a row is swapped for a different one, same count",
			`<table id="id35a0c"><tr><td><a href="/opal/auth/RepositoryEntry/1/CourseNode/2/Andere.pdf">Andere.pdf</a></td><td>1,2 MB</td></tr></table>`,
		},
		{
			"a single character changes",
			`<table id="id35a0c"><tr><td><a href="/opal/auth/RepositoryEntry/1/CourseNode/2/Skript.pdf">Skript.pdf</a></td><td>1,2 MB</td></tr></table> `,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if Of(base) == Of(tc.changed) {
				t.Fatalf("a real change hashed identically - a cache on this would silently stop downloading.\n  before: %s\n  after:  %s", Normalize(base), Normalize(tc.changed))
			}
		})
	}
}

// A digit-run long enough to look like an epoch stamp sits inside real file
// names on this account (course-node ids appear in every file URL). This pins
// that the epoch pattern cannot swallow one, which would make two different
// files hash alike.
func TestFileURLsSurviveTheEpochPattern(t *testing.T) {
	a := `<a href="/opal/auth/RepositoryEntry/50999590912/CourseNode/1757212677096374003/Analysis01.pdf">Analysis01.pdf</a>`
	b := `<a href="/opal/auth/RepositoryEntry/50999590912/CourseNode/1757212677096374003/Analysis02.pdf">Analysis02.pdf</a>`
	if Of(a) == Of(b) {
		t.Fatal("two different files in the same course node hashed identically")
	}
}

func TestEmptyInputIsNotAHash(t *testing.T) {
	// A failed fetch stored as a legitimate value would make the NEXT failed
	// fetch a false match, i.e. "unchanged" for a section nobody could read.
	if Of("") != "" {
		t.Fatalf("empty HTML must not produce a storable hash, got %q", Of(""))
	}
	if Of("x") == "" {
		t.Fatal("non-empty HTML must produce a hash")
	}
}

func TestHashIsStableForIdenticalInput(t *testing.T) {
	const html = `<div id="id35a0c">same</div>`
	if Of(html) != Of(html) {
		t.Fatal("hashing is not deterministic")
	}
}
