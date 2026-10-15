package scraper

import (
	"errors"
	"sync"
	"testing"
)

func TestSectionHTMLMemoFetchesOncePerURL(t *testing.T) {
	memo := newSectionHTMLMemo()
	calls := 0

	for i := 0; i < 5; i++ {
		status, body, fresh, err := memo.get("https://opal/sectionA", func() (int, string, error) {
			calls++
			return 200, "<html>A</html>", nil
		})
		if err != nil {
			t.Fatalf("get %d: %v", i, err)
		}
		if status != 200 || body != "<html>A</html>" {
			t.Fatalf("get %d returned %d %q", i, status, body)
		}
		if wantFresh := i == 0; fresh != wantFresh {
			t.Errorf("get %d: fresh = %v, want %v", i, fresh, wantFresh)
		}
	}
	if calls != 1 {
		t.Errorf("fetched %d times, want 1 - the whole point is one Wicket page instance per section", calls)
	}

	if _, _, _, err := memo.get("https://opal/sectionB", func() (int, string, error) {
		calls++
		return 200, "<html>B</html>", nil
	}); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Errorf("a second, distinct URL should fetch: calls = %d, want 2", calls)
	}
}

// TestSectionHTMLMemoConcurrentCallersShareOneFetch is the property that
// matters under download_concurrency: workers arriving together at an
// uncached section must not each start their own fetch, because that would
// recreate exactly the page-instance churn the memo exists to remove.
func TestSectionHTMLMemoConcurrentCallersShareOneFetch(t *testing.T) {
	memo := newSectionHTMLMemo()

	var mu sync.Mutex
	calls := 0
	release := make(chan struct{})

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, body, _, err := memo.get("https://opal/section", func() (int, string, error) {
				mu.Lock()
				calls++
				mu.Unlock()
				<-release // hold the fetch open so the others pile up behind it
				return 200, "<html>shared</html>", nil
			})
			if err != nil || body != "<html>shared</html>" {
				t.Errorf("worker got %q, %v", body, err)
			}
		}()
	}
	close(release)
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Errorf("8 concurrent callers caused %d fetches, want 1", calls)
	}
}

func TestSectionHTMLMemoInvalidateForcesRefetch(t *testing.T) {
	memo := newSectionHTMLMemo()
	bodies := []string{"first", "second"}
	calls := 0
	fetch := func() (int, string, error) {
		body := bodies[calls]
		calls++
		return 200, body, nil
	}

	if _, body, _, _ := memo.get("u", fetch); body != "first" {
		t.Fatalf("got %q", body)
	}
	memo.invalidate("u")
	_, body, fresh, _ := memo.get("u", fetch)
	if body != "second" {
		t.Errorf("after invalidate got %q, want the re-fetched %q", body, "second")
	}
	if !fresh {
		t.Error("a re-fetch after invalidate should report fresh=true")
	}
}

func TestSectionHTMLMemoResetClearsEverything(t *testing.T) {
	memo := newSectionHTMLMemo()
	calls := 0
	fetch := func() (int, string, error) { calls++; return 200, "x", nil }

	_, _, _, _ = memo.get("a", fetch)
	_, _, _, _ = memo.get("b", fetch)
	memo.reset()
	_, _, _, _ = memo.get("a", fetch)
	_, _, _, _ = memo.get("b", fetch)

	if calls != 4 {
		t.Errorf("calls = %d, want 4 - reset must drop every entry", calls)
	}
}

// TestSectionHTMLMemoCachesErrors documents the deliberate choice: a failed
// fetch is cached like any other result for the rest of the run, so a
// section that is genuinely unreachable is not re-requested once per file.
// refreshCounterURL's invalidate-and-retry is what gives a transient failure
// its second chance.
func TestSectionHTMLMemoCachesErrors(t *testing.T) {
	memo := newSectionHTMLMemo()
	calls := 0
	fetch := func() (int, string, error) { calls++; return 0, "", errors.New("boom") }

	for i := 0; i < 3; i++ {
		if _, _, _, err := memo.get("u", fetch); err == nil {
			t.Fatal("expected the cached error")
		}
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

// TestSectionHTMLMemoNilIsUsable keeps the zero-value OpalScraper the tests
// build from panicking: a nil memo simply fetches every time.
func TestSectionHTMLMemoNilIsUsable(t *testing.T) {
	var memo *sectionHTMLMemo
	calls := 0
	for i := 0; i < 3; i++ {
		_, body, fresh, err := memo.get("u", func() (int, string, error) { calls++; return 200, "x", nil })
		if err != nil || body != "x" || !fresh {
			t.Fatalf("nil memo returned %q, fresh=%v, err=%v", body, fresh, err)
		}
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
	memo.invalidate("u")
	memo.reset()
}
