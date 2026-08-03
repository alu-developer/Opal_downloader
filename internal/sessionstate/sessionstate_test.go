package sessionstate

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeState builds a storage-state file shaped like the real one Playwright
// produces. The cookie set is copied from the actual file on the maintainer's
// machine (2026-08-03) - values redacted, names and domains kept - because
// the point of these tests is that Inspect picks the right cookie out of a
// realistic crowd, not out of a one-element list.
func writeState(t *testing.T, markerExpires string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	body := fmt.Sprintf(`{
  "cookies": [
    {"name": "idpsite-presel", "value": "x", "domain": "bildungsportal.sachsen.de", "path": "/", "expires": 1794132000},
    {"name": "authenticated-marker", "value": "x", "domain": "bildungsportal.sachsen.de", "path": "/", "expires": %s},
    {"name": "JSESSIONID", "value": "x", "domain": "bildungsportal.sachsen.de", "path": "/", "expires": -1},
    {"name": "__Host-shib_idp_session", "value": "x", "domain": "idp.tu-dresden.de", "path": "/", "expires": -1}
  ],
  "origins": []
}`, markerExpires)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}
	return path
}

func TestInspectReadsMarkerExpiry(t *testing.T) {
	want := time.Now().Add(48 * time.Hour).Truncate(time.Second)
	path := writeState(t, fmt.Sprintf("%d", want.Unix()))

	got := Inspect(path)

	if !got.Present {
		t.Fatal("expected Present for an existing state file")
	}
	if !got.KnownExpiry {
		t.Fatal("expected the authenticated-marker cookie's expiry to be found")
	}
	if !got.ValidUntil.Equal(want) {
		t.Errorf("ValidUntil = %v, want %v", got.ValidUntil, want)
	}
	if got.Expired {
		t.Error("a marker 48h in the future must not report Expired")
	}
	if remaining := got.Remaining(); remaining < 47*time.Hour || remaining > 48*time.Hour {
		t.Errorf("Remaining() = %v, want just under 48h", remaining)
	}
}

func TestInspectReportsExpired(t *testing.T) {
	past := time.Now().Add(-2 * time.Hour)
	got := Inspect(writeState(t, fmt.Sprintf("%d", past.Unix())))

	if !got.Present || !got.KnownExpiry {
		t.Fatalf("expected a present state file with a known expiry, got %+v", got)
	}
	if !got.Expired {
		t.Error("a marker 2h in the past must report Expired")
	}
	if got.Remaining() != 0 {
		t.Errorf("Remaining() = %v for an expired session, want 0", got.Remaining())
	}
}

// The one that would actually bite: reporting "logged out" because the cookie
// was not found is a strictly worse answer than reporting "expiry unknown".
// Not knowing when a session ends is not evidence that it has ended.
func TestInspectWithoutMarkerIsUnknownNotExpired(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	body := `{"cookies": [{"name": "JSESSIONID", "value": "x", "domain": "bildungsportal.sachsen.de", "path": "/", "expires": -1}], "origins": []}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}

	got := Inspect(path)

	if !got.Present {
		t.Error("a state file with no marker is still a saved session")
	}
	if got.KnownExpiry {
		t.Error("expected KnownExpiry to be false with no marker cookie")
	}
	if got.Expired {
		t.Error("expected Expired to stay false when the expiry is simply unknown")
	}
}

// A marker from some other host must not be read as OPAL's.
func TestInspectIgnoresMarkerFromAnotherDomain(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	body := fmt.Sprintf(
		`{"cookies": [{"name": "authenticated-marker", "value": "x", "domain": "example.invalid", "path": "/", "expires": %d}], "origins": []}`,
		time.Now().Add(72*time.Hour).Unix())
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}

	if got := Inspect(path); got.KnownExpiry {
		t.Error("a marker cookie from another domain must not be read as OPAL's")
	}
}

func TestInspectMissingAndMalformedFiles(t *testing.T) {
	if got := Inspect(filepath.Join(t.TempDir(), "nope.json")); got.Present {
		t.Error("a missing state file must report Present=false")
	}

	dir := t.TempDir()
	if got := Inspect(dir); got.Present {
		t.Error("a directory must not be mistaken for a state file")
	}

	broken := filepath.Join(dir, "broken.json")
	if err := os.WriteFile(broken, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}
	got := Inspect(broken)
	if !got.Present {
		t.Error("an unreadable state file still exists, so Present stays true")
	}
	if got.KnownExpiry || got.Expired {
		t.Error("a malformed state file must not produce an expiry claim")
	}
}
