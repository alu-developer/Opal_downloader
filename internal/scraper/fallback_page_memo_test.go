package scraper

import "testing"

// The memo is the only thing standing between "reuse the page that is already
// open" and "search for a file's link on the wrong listing". These pin that it
// only ever reuses a page whose identity is fully confirmed.
func TestFallbackPageMemoCanReuse(t *testing.T) {
	const req = "https://opal.example/auth/RepositoryEntry/1/CourseNode/2/Vorlesung"
	const landed = req + "?1032"

	tests := []struct {
		name       string
		memo       fallbackPageMemo
		requested  string
		currentURL string
		want       bool
		why        string
	}{
		{
			name:       "same request, page still where we left it",
			memo:       fallbackPageMemo{RequestedURL: req, LandedURL: landed},
			requested:  req,
			currentURL: landed,
			want:       true,
		},
		{
			name:       "different section requested",
			memo:       fallbackPageMemo{RequestedURL: req, LandedURL: landed},
			requested:  "https://opal.example/auth/RepositoryEntry/1/CourseNode/9/Uebung",
			currentURL: landed,
			want:       false,
		},
		{
			name:       "page moved since it was recorded",
			memo:       fallbackPageMemo{RequestedURL: req, LandedURL: landed},
			requested:  req,
			currentURL: "https://opal.example/auth/home",
			want:       false,
			why:        "something else navigated the shared page",
		},
		{
			name:       "empty memo",
			memo:       fallbackPageMemo{},
			requested:  req,
			currentURL: landed,
			want:       false,
		},
		{
			name:       "current page url unknown",
			memo:       fallbackPageMemo{RequestedURL: req, LandedURL: landed},
			requested:  req,
			currentURL: "",
			want:       false,
			why:        "cannot confirm identity, so must not reuse",
		},
		{
			name:       "case and surrounding space are ignored",
			memo:       fallbackPageMemo{RequestedURL: req, LandedURL: landed},
			requested:  "  " + req + "  ",
			currentURL: landed,
			want:       true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.memo.canReuse(tc.requested, tc.currentURL); got != tc.want {
				t.Errorf("canReuse() = %v, want %v (%s)", got, tc.want, tc.why)
			}
		})
	}
}

func TestFallbackPageMemoLifecycle(t *testing.T) {
	m := &fallbackPageMemo{}
	const req = "https://opal.example/s1"
	const landed = req + "?7"

	m.record(req, landed, false)
	if !m.canReuse(req, landed) {
		t.Fatal("a recorded page should be reusable")
	}
	if m.Expanded {
		t.Error("record(expanded=false) must not mark the page expanded")
	}

	// A show-all click moves the Wicket page-instance counter, so the memo has
	// to follow the page or the very next reuse check fails.
	const afterExpand = req + "?8"
	m.markExpanded(afterExpand)
	if !m.Expanded {
		t.Error("markExpanded did not set Expanded")
	}
	if !m.canReuse(req, afterExpand) {
		t.Error("markExpanded must keep the landed URL in step with the page")
	}
	if m.canReuse(req, landed) {
		t.Error("the pre-expansion URL must no longer count as the current page")
	}

	m.invalidate()
	if m.canReuse(req, afterExpand) || m.Expanded {
		t.Error("invalidate must forget everything")
	}
}

func TestFallbackPageMemoNilSafe(t *testing.T) {
	var m *fallbackPageMemo
	if m.canReuse("a", "b") {
		t.Error("nil memo must never claim reuse")
	}
	m.record("a", "b", true)
	m.markExpanded("c")
	m.invalidate()
}
