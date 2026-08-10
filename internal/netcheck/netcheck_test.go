package netcheck

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
)

// fakeNetwork replaces the package's real DNS/dial for one test and restores
// it afterwards. lookupErr simulates an offline machine (nothing resolves);
// dialErr simulates a host that resolves but does not answer.
func fakeNetwork(t *testing.T, lookupErr, dialErr error) {
	t.Helper()
	origLookup, origDial := lookupHost, dialTCP
	t.Cleanup(func() { lookupHost, dialTCP = origLookup, origDial })

	lookupHost = func(context.Context, string) ([]string, error) {
		if lookupErr != nil {
			return nil, lookupErr
		}
		return []string{"203.0.113.1"}, nil
	}
	dialTCP = func(context.Context, string) (net.Conn, error) {
		if dialErr != nil {
			return nil, dialErr
		}
		client, server := net.Pipe()
		_ = server.Close()
		return client, nil
	}
}

const testURL = "https://bildungsportal.sachsen.de/opal/"

func TestCheckOfflineWhenNameDoesNotResolve(t *testing.T) {
	fakeNetwork(t, errors.New("no such host"), nil)

	err := Check(context.Background(), testURL)
	if !errors.Is(err, ErrOffline) {
		t.Fatalf("Check() = %v, want it to wrap ErrOffline", err)
	}
}

func TestCheckUnreachableWhenNameResolvesButDialFails(t *testing.T) {
	fakeNetwork(t, nil, errors.New("connection refused"))

	err := Check(context.Background(), testURL)
	if !errors.Is(err, ErrUnreachable) {
		t.Fatalf("Check() = %v, want it to wrap ErrUnreachable", err)
	}
	// The two cases must stay distinguishable: one is "your Wi-Fi is off",
	// the other is "OPAL is down", and they get different advice.
	if errors.Is(err, ErrOffline) {
		t.Fatalf("a reachable-DNS/failed-dial result must not report as offline: %v", err)
	}
}

func TestCheckPassesWhenHostAnswers(t *testing.T) {
	fakeNetwork(t, nil, nil)

	if err := Check(context.Background(), testURL); err != nil {
		t.Fatalf("Check() = %v, want nil", err)
	}
}

// A malformed opal_url is the user's own setting being wrong, not a network
// problem, and must never be reported as "check your Wi-Fi".
func TestCheckRejectsAddressWithNoHost(t *testing.T) {
	fakeNetwork(t, errors.New("no such host"), nil)

	err := Check(context.Background(), "not a url at all")
	if err == nil {
		t.Fatal("Check() = nil, want an error about the address")
	}
	if errors.Is(err, ErrOffline) || errors.Is(err, ErrUnreachable) {
		t.Fatalf("a broken address must not be classified as a network failure: %v", err)
	}
}

// The whole point of this package: what the user is shown says what is wrong
// in words they can act on, before any technical detail.
func TestDescribeLeadsWithPlainLanguage(t *testing.T) {
	fakeNetwork(t, errors.New("lookup bildungsportal.sachsen.de: no such host"), nil)

	err := Describe(context.Background(), testURL, errors.New("net::ERR_NAME_NOT_RESOLVED at https://bildungsportal.sachsen.de/opal/"))
	if err == nil {
		t.Fatal("Describe() = nil, want an offline error")
	}
	msg := err.Error()
	if !strings.HasPrefix(msg, "No internet connection.") {
		t.Fatalf("message must open with the plain sentence, got: %s", msg)
	}
	if !strings.Contains(msg, "Wi-Fi") {
		t.Fatalf("message must tell the user what to check, got: %s", msg)
	}
	// The raw browser error stays available for logs and bug reports - just
	// not first, and not on its own.
	if !strings.Contains(msg, "net::ERR_NAME_NOT_RESOLVED") {
		t.Fatalf("technical cause should still be carried along, got: %s", msg)
	}
	if !errors.Is(err, ErrOffline) {
		t.Fatalf("Describe() must keep the ErrOffline sentinel for exit codes: %v", err)
	}
}

// The bug this guards (weekly review, 2026-08-10): the scheduled-run give-up
// message appended its reassurance with fmt.Errorf("%w ...", err, extra),
// which puts it AFTER the "(technical detail: " marker. bannerChrome
// (internal/gui/chrome.go) splits on that marker and folds everything past it
// into a collapsed <details>, so the one sentence the retry feature exists to
// show - that the next scheduled sync will try again - was never visible by
// default. Anything appended to a netcheck error has to land in the sentence.
func TestAppendSentenceKeepsExtraTextBeforeTheTechnicalDetail(t *testing.T) {
	fakeNetwork(t, errors.New("lookup bildungsportal.sachsen.de: no such host"), nil)

	base := Describe(context.Background(), testURL, errors.New("net::ERR_NAME_NOT_RESOLVED"))
	if base == nil {
		t.Fatal("Describe() = nil, want an offline error")
	}

	const extra = "Waited 15m0s for the connection to come back, then gave up - the next scheduled sync will try again on its own."
	err := AppendSentence(base, extra)
	msg := err.Error()

	marker := strings.Index(msg, "(technical detail: ")
	if marker < 0 {
		t.Fatalf("expected the technical-detail marker to survive, got: %s", msg)
	}
	at := strings.Index(msg, extra)
	if at < 0 {
		t.Fatalf("appended sentence missing entirely, got: %s", msg)
	}
	if at > marker {
		t.Fatalf("appended sentence landed after the technical-detail marker, so the GUI banner would hide it.\n got: %s", msg)
	}
	if !strings.HasPrefix(msg, "No internet connection.") {
		t.Fatalf("the plain sentence must still lead, got: %s", msg)
	}
	if !errors.Is(err, ErrOffline) {
		t.Fatalf("AppendSentence must preserve the classification for exit codes: %v", err)
	}
	if !strings.Contains(msg, "net::ERR_NAME_NOT_RESOLVED") {
		t.Fatalf("AppendSentence must preserve the technical cause, got: %s", msg)
	}
}

func TestAppendSentenceIsANoopWithoutText(t *testing.T) {
	if got := AppendSentence(nil, "anything"); got != nil {
		t.Fatalf("AppendSentence(nil, ...) = %v, want nil", got)
	}
	base := errors.New("plain")
	if got := AppendSentence(base, "   "); got != base {
		t.Fatalf("empty extra must return the original error unchanged, got %v", got)
	}
	// An error this package did not build has no sentence to extend; appending
	// the old way is correct there and must still carry errors.Is.
	got := AppendSentence(base, "more")
	if !errors.Is(got, base) || !strings.Contains(got.Error(), "more") {
		t.Fatalf("foreign error should be wrapped with the extra appended, got %v", got)
	}
}

func TestDescribeSeparatesOpalBeingDownFromBeingOffline(t *testing.T) {
	fakeNetwork(t, nil, errors.New("connection refused"))

	err := Describe(context.Background(), testURL, nil)
	if err == nil {
		t.Fatal("Describe() = nil, want an unreachable error")
	}
	msg := err.Error()
	if strings.Contains(msg, "No internet connection.") {
		t.Fatalf("a reachable network with a dead host must not blame the connection: %s", msg)
	}
	if !strings.Contains(msg, "bildungsportal.sachsen.de") {
		t.Fatalf("message should name the host it could not reach: %s", msg)
	}
}

func TestDescribeReturnsNilWhenOnlineAndNothingFailed(t *testing.T) {
	fakeNetwork(t, nil, nil)

	if err := Describe(context.Background(), testURL, nil); err != nil {
		t.Fatalf("Describe() = %v, want nil for a healthy pre-flight check", err)
	}
}

// A page navigation that failed while the network is demonstrably fine is a
// third case: not the user's connection, and not a dead host either.
func TestDescribeExplainsFailureDespiteWorkingNetwork(t *testing.T) {
	fakeNetwork(t, nil, nil)

	err := Describe(context.Background(), testURL, errors.New("net::ERR_ABORTED"))
	if err == nil {
		t.Fatal("Describe() = nil, want the cause to be reported")
	}
	if errors.Is(err, ErrOffline) {
		t.Fatalf("must not claim the user is offline when they are not: %v", err)
	}
	if !strings.Contains(err.Error(), "online") {
		t.Fatalf("message should say the connection itself is fine: %s", err.Error())
	}
}

func TestHostPortDefaultsPortFromScheme(t *testing.T) {
	for _, tc := range []struct {
		url  string
		want string
	}{
		{"https://example.org/opal/", "example.org:443"},
		{"http://example.org/opal/", "example.org:80"},
		{"https://example.org:8443/opal/", "example.org:8443"},
	} {
		_, addr, err := hostPort(tc.url)
		if err != nil {
			t.Fatalf("hostPort(%q) error: %v", tc.url, err)
		}
		if addr != tc.want {
			t.Fatalf("hostPort(%q) = %q, want %q", tc.url, addr, tc.want)
		}
	}
}
