// Package sessionstate answers "am I still logged in to OPAL, and until
// when?" by reading the saved Playwright storage-state file - the same JSON
// internal/scraper writes with saveState and reloads via StorageStatePath.
//
// It never opens a browser and never touches the network. Everything here is
// one small file read, so it is safe to call on every render of a GUI page
// (which is exactly what internal/gui does with it).
//
// # Why this exists
//
// The landing page used to say "Logged in (session saved <mtime>). May still
// need a fresh login if it expired." - which is a file timestamp dressed up
// as a session status. It cannot distinguish a session saved ten minutes ago
// from one saved last month, and the honest half of the sentence ("may still
// need") is the only part carrying information.
//
// There is something better in the file. OPAL sets a cookie literally called
// `authenticated-marker` on bildungsportal.sachsen.de with a real expiry
// timestamp, and Playwright persists it. Measured on this machine 2026-08-03:
// state file written 11:17:33, marker expiring 2026-08-06 11:17 - 72 hours to
// the minute. Nothing here depends on that duration being 72 hours; it reads
// whatever expiry the cookie actually carries. The 72h figure is recorded
// only as the evidence that the cookie is a genuine lifetime and not, say, a
// far-future placeholder.
//
// # What this can and cannot tell you
//
// The marker is an *upper bound*, and the distinction matters enough that
// callers should phrase it the way this package's fields do:
//
//   - Past the marker's expiry, you are definitely logged out. That is solid:
//     the browser will not send an expired cookie at all.
//   - Before it, you are *probably* still logged in. The rest of the session
//     is carried by JSESSIONID and the Shibboleth cookies
//     (__Host-shib_idp_session, _shibsession_*), all of which are session
//     cookies with no expiry stored in the file, and all of which have
//     server-side idle and absolute timeouts this side cannot see. OPAL can
//     drop the session well before the marker lapses.
//
// So `Expired` is a fact and `ValidUntil` is a deadline, but "not expired" is
// not a promise. This is not a problem worth solving with a network probe:
// per CLAUDE.md a stale session is never a blocker anyway - TU-Fast completes
// login and 2FA unattended - so the practical value here is telling the user
// where they stand, not gating anything on it.
package sessionstate

import (
	"encoding/json"
	"os"
	"strings"
	"time"
)

// authMarkerCookie is OPAL's own name for the cookie, not one this project
// invented. It is the only cookie in the saved state that carries both a real
// expiry and a plausible claim to represent "authenticated" - see the package
// doc for the full cookie inventory and why the others are unusable here.
const authMarkerCookie = "authenticated-marker"

// opalCookieDomain scopes the marker lookup. A cookie of the same name from
// any other host would be somebody else's, and reading its expiry as OPAL's
// would be worse than reporting nothing.
const opalCookieDomain = "bildungsportal.sachsen.de"

// Status is what one look at the saved state file can establish.
type Status struct {
	// Present reports that a saved session file exists and could be read.
	// False means "never logged in on this machine, or the file is gone".
	Present bool

	// Saved is the state file's modification time - when a login or a sync
	// last wrote cookies back. Kept because it is still the only thing that
	// answers "when did this last work?", and because it is the fallback
	// display when the marker is missing.
	Saved time.Time

	// ValidUntil is the authenticated-marker cookie's expiry. Zero when
	// KnownExpiry is false.
	ValidUntil time.Time

	// KnownExpiry reports that the marker was found with a usable expiry.
	// False is a perfectly normal state - a state file written by a future
	// OPAL that renamed the cookie, or a hand-made one in a test - and
	// callers should degrade to "logged in, expiry unknown" rather than to
	// "logged out".
	KnownExpiry bool

	// Expired reports that ValidUntil is in the past. Only ever true when
	// KnownExpiry is true: not knowing an expiry is not evidence of one.
	Expired bool
}

// Remaining is how long the marker is still good for, or 0 once it has
// lapsed or was never found. Callers rendering a countdown should check
// KnownExpiry rather than treating 0 as "expires now".
func (s Status) Remaining() time.Duration {
	if !s.KnownExpiry || s.Expired {
		return 0
	}
	return time.Until(s.ValidUntil)
}

// storageState is the subset of Playwright's storage-state JSON this needs.
// Everything else in the file (cookie values, localStorage origins) is
// deliberately not modelled - there is no reason for this package to hold a
// session cookie's value in memory.
type storageState struct {
	Cookies []struct {
		Name   string `json:"name"`
		Domain string `json:"domain"`
		// Playwright writes expires as a unix-seconds number, using -1 for a
		// session cookie. It is a float because sub-second precision is
		// allowed by the format, not because it is ever meaningfully
		// fractional.
		Expires float64 `json:"expires"`
	} `json:"cookies"`
}

// Inspect reads stateFile and reports what it says about the session.
//
// Every failure mode collapses to the same answer - a zero Status, meaning
// "not logged in" - and none of them is an error worth returning. A missing
// file is the ordinary first-run case; an unreadable or malformed one is
// indistinguishable from it as far as any caller can act on it, since the
// remedy in both cases is the same login that would happen anyway.
func Inspect(stateFile string) Status {
	info, err := os.Stat(stateFile)
	if err != nil || info.IsDir() {
		return Status{}
	}

	status := Status{Present: true, Saved: info.ModTime()}

	raw, err := os.ReadFile(stateFile)
	if err != nil {
		return status
	}
	var state storageState
	if err := json.Unmarshal(raw, &state); err != nil {
		return status
	}

	for _, cookie := range state.Cookies {
		if cookie.Name != authMarkerCookie {
			continue
		}
		// Playwright stores the domain with a leading dot for host-spanning
		// cookies, so match on suffix rather than equality.
		if !strings.HasSuffix(strings.ToLower(cookie.Domain), opalCookieDomain) {
			continue
		}
		if cookie.Expires <= 0 {
			// A session cookie, or an explicit "no expiry". Either way there
			// is no deadline to report.
			continue
		}
		status.ValidUntil = time.Unix(int64(cookie.Expires), 0)
		status.KnownExpiry = true
		status.Expired = time.Now().After(status.ValidUntil)
		break
	}

	return status
}
