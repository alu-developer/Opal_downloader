// Package netcheck answers the question every "it did not work" message has
// to answer before any other: is this machine online at all?
//
// It exists because of what a user actually saw when the Wi-Fi was off
// (maintainer, 2026-08-10: "errormeldung von internet ist nicht da ist fuer
// normale Nutzer nicht aussagekraeftig"). The old path launched a browser,
// let it fail, and surfaced the raw Playwright error - a wall of
// "net::ERR_NAME_NOT_RESOLVED at https://... Call log: navigating to ..."
// which says nothing to someone who just wants to know their Wi-Fi is off.
//
// Two jobs, deliberately kept apart:
//
//   - Check dials, and classifies the failure into "this computer is
//     offline" vs "the host is not answering". Only the configured OPAL host
//     is ever contacted - no third-party "am I online" beacon, which would
//     mean telling some other server about every sync attempt.
//   - Describe turns that classification into one plain sentence a
//     non-technical user can act on, keeping the technical cause wrapped
//     underneath for logs and errors.Is.
package netcheck

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

// DefaultTimeout bounds a whole Check. Short on purpose: this runs before
// every crawl, and its entire value is being faster than the browser launch
// it replaces as the place a network problem is discovered. A working
// connection answers in milliseconds; a dead one is not going to come back
// within the seconds spent waiting for it.
const DefaultTimeout = 6 * time.Second

var (
	// ErrOffline means the host name could not even be looked up, which on a
	// normal machine means there is no working network connection at all.
	ErrOffline = errors.New("no internet connection")

	// ErrUnreachable means the name resolved but nothing accepted a
	// connection: the server is down, a firewall is in the way, or the
	// connection dropped between the lookup and the dial.
	ErrUnreachable = errors.New("host not answering")
)

// lookupHost and dialTCP are indirections so tests can simulate an offline
// machine without one - the same test-override convention internal/gui uses
// for its own package-level function values.
var (
	lookupHost = func(ctx context.Context, host string) ([]string, error) {
		return net.DefaultResolver.LookupHost(ctx, host)
	}
	dialTCP = func(ctx context.Context, addr string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "tcp", addr)
	}
)

// Check reports whether rawURL's host can be reached right now, returning
// nil when it can, and an error wrapping ErrOffline or ErrUnreachable when
// it cannot. A rawURL that cannot be parsed into a host returns a plain
// error wrapping neither - a broken opal_url is a config problem, not a
// network one, and must not be reported as "your Wi-Fi is off".
func Check(ctx context.Context, rawURL string) error {
	host, addr, err := hostPort(rawURL)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, DefaultTimeout)
	defer cancel()

	if _, err := lookupHost(ctx, host); err != nil {
		return fmt.Errorf("%w (could not look up %s: %v)", ErrOffline, host, err)
	}

	conn, err := dialTCP(ctx, addr)
	if err != nil {
		return fmt.Errorf("%w (could not connect to %s: %v)", ErrUnreachable, addr, err)
	}
	_ = conn.Close()
	return nil
}

// hostPort splits rawURL into its bare host name and a dialable host:port,
// defaulting the port from the scheme.
func hostPort(rawURL string) (host string, addr string, err error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", "", fmt.Errorf("could not read the OPAL address %q from your settings: %w", rawURL, err)
	}
	host = parsed.Hostname()
	if host == "" {
		return "", "", fmt.Errorf("the OPAL address in your settings (%q) has no host name in it", rawURL)
	}
	port := parsed.Port()
	if port == "" {
		port = "443"
		if parsed.Scheme == "http" {
			port = "80"
		}
	}
	return host, net.JoinHostPort(host, port), nil
}

// Describe is what callers show a user. It runs a Check and turns the result
// into one plain sentence, with cause (the underlying browser/HTTP error, if
// there is one) wrapped underneath so `errors.Is`, logs and bug reports keep
// everything they had before.
//
// cause may be nil, for the pre-flight case where nothing has failed yet and
// the check is simply being run ahead of opening a browser.
//
// Deliberately says nothing about profiles, Chromium, Playwright or
// selectors: the audience for this string is someone whose Wi-Fi is off, and
// the next thing they do should be turning it back on.
func Describe(ctx context.Context, rawURL string, cause error) error {
	checkErr := Check(ctx, rawURL)

	var sentence string
	switch {
	case checkErr == nil && cause == nil:
		return nil
	case errors.Is(checkErr, ErrOffline):
		sentence = "No internet connection. Your computer is not online right now - check your Wi-Fi or network cable, then try again."
	case errors.Is(checkErr, ErrUnreachable):
		sentence = fmt.Sprintf("Could not reach OPAL (%s). Your internet connection looks alive, so OPAL itself is most likely down or blocked by a firewall. Try again later.", displayHost(rawURL))
	case checkErr != nil:
		// hostPort failed: a malformed address in the settings, which is the
		// user's to fix and is not a network problem at all.
		return checkErr
	default:
		sentence = fmt.Sprintf("Could not open OPAL (%s), even though this computer is online. That usually means a temporary problem at OPAL, or that the OPAL address in Settings is wrong.", displayHost(rawURL))
	}

	detail := checkErr
	if cause != nil {
		detail = cause
	}
	if detail == nil {
		return errors.New(sentence)
	}
	// Both the classification and the underlying cause stay reachable
	// through errors.Is: the classification is what decides the process
	// exit code for an unattended run (exitCodeOffline in
	// cmd/opal-downloader), and the cause is what a bug report needs. A
	// single %w would have to drop one of them, and dropping the
	// classification was a real bug caught by this package's own test.
	return &describedError{sentence: sentence, class: checkErr, cause: detail}
}

// AppendSentence returns err with extra added to its user-facing sentence -
// that is, BEFORE the "(technical detail: ...)" marker rather than after it.
//
// This exists because appending with fmt.Errorf("%w ...", err, extra) puts the
// new text past that marker, and the GUI's banner (bannerChrome in
// internal/gui/chrome.go) splits on exactly that marker and folds everything
// after it into a collapsed <details>. A reassurance sentence appended the
// obvious way is therefore never visible by default - which is what happened
// to the scheduled-run give-up message: the banner showed only the raw
// technical cause and hid the "the next scheduled sync will try again" line
// the whole feature existed to show (weekly review, 2026-08-10).
//
// The marker's format is owned by this package, so the safe way to extend a
// message lives here too. Classification and cause are preserved, so
// errors.Is still finds ErrOffline/ErrUnreachable and the underlying cause.
// An error this package did not produce is appended to the old way, since
// there is no sentence to extend.
func AppendSentence(err error, extra string) error {
	if err == nil {
		return nil
	}
	extra = strings.TrimSpace(extra)
	if extra == "" {
		return err
	}
	var described *describedError
	if errors.As(err, &described) {
		return &describedError{
			sentence: strings.TrimSpace(described.sentence) + " " + extra,
			class:    described.class,
			cause:    described.cause,
		}
	}
	return fmt.Errorf("%w %s", err, extra)
}

// describedError carries the user-facing sentence, the ErrOffline/
// ErrUnreachable classification, and the technical cause as one error.
type describedError struct {
	sentence string
	class    error
	cause    error
}

func (e *describedError) Error() string {
	if e.cause == nil {
		return e.sentence
	}
	return fmt.Sprintf("%s (technical detail: %s)", e.sentence, e.cause)
}

// Unwrap returns both wrapped errors, which errors.Is walks in order.
func (e *describedError) Unwrap() []error {
	out := make([]error, 0, 2)
	if e.class != nil {
		out = append(out, e.class)
	}
	if e.cause != nil && e.cause != e.class {
		out = append(out, e.cause)
	}
	return out
}

// displayHost is the host name alone, for a message a person reads. Falls
// back to the raw string if it will not parse, so a message is never empty.
func displayHost(rawURL string) string {
	if host, _, err := hostPort(rawURL); err == nil {
		return host
	}
	return rawURL
}

// Online is Check reduced to a bool, for callers that only want to decide
// whether to wait and retry (see the scheduled-sync retry loop in
// cmd/opal-downloader).
func Online(ctx context.Context, rawURL string) bool {
	return Check(ctx, rawURL) == nil
}
