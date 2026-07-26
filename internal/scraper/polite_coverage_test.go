package scraper

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every request to OPAL has to go through the politeness ceiling, and the way
// that breaks is not a bug in the limiter - it is somebody adding a new
// download path that quietly does not use it.
//
// That already happened once. The ceiling was introduced covering page
// navigations only, and docs/server-load.md described it as bounding how hard
// the tool hits OPAL, while the actual file downloads went straight out via
// reqCtx.Get. Those are the requests that run concurrently and move real
// bytes: the ceiling was bounding the half nobody was worried about.
//
// So this is a structural check rather than a behavioural one. It reads the
// package's own source and insists the raw calls appear in exactly one place.
func TestEveryHTTPRequestGoesThroughTheLimiter(t *testing.T) {
	const (
		rawNavigation = ".Goto("
		rawRequest    = "reqCtx.Get("
	)

	// The two doors, and the only files allowed to hold a raw call.
	allowed := map[string]bool{"scraper.go": true}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}

	var offenders []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			code := strings.TrimSpace(line)
			// Comments discuss these calls at length; only real code counts.
			if strings.HasPrefix(code, "//") {
				continue
			}
			if !strings.Contains(code, rawNavigation) && !strings.Contains(code, rawRequest) {
				continue
			}
			// The helpers themselves are where the raw calls belong.
			if allowed[name] {
				continue
			}
			offenders = append(offenders, name+":"+itoa(i+1)+": "+code)
		}
	}

	if len(offenders) > 0 {
		t.Errorf("these requests bypass the politeness ceiling (see internal/polite and docs/server-load.md).\n"+
			"Use s.gotoPolitely for navigations and s.getPolitely for API requests:\n\n%s",
			strings.Join(offenders, "\n"))
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
