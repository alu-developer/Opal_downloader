// Package statuslog persists the outcome of the most recent `sync
// --scheduled` run to a small local JSON file, so a human has something to
// read the next time they open the GUI even though an unattended,
// Task-Scheduler-triggered run has no terminal/GUI window for them to watch
// live (see docs/scheduled-sync-plan.md section 3, "Failure detection and
// notification"). This is the core mechanism behind the GUI's
// scheduled-sync banner (internal/gui/scheduled_status.go).
//
// Only the single most recent run is kept - Write always overwrites the
// file in place. This is deliberate (see the queue task's "Non-goals": "No
// historical log of every past scheduled run - just the most recent
// outcome").
package statuslog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Outcome classifies how a scheduled run went.
type Outcome string

const (
	OutcomeSuccess Outcome = "success"
	OutcomePartial Outcome = "partial"
	OutcomeFailure Outcome = "failure"
)

// LoginPath records which of ensureSession's two branches (see
// internal/scraper/session.go) a scheduled run took to establish its OPAL
// session. This directly answers docs/scheduled-sync-plan.md section 1's
// open question ("what fraction of daily runs need re-login") empirically,
// once real scheduled runs have accumulated a few weeks of data - instead
// of guessing, the field is recorded on day one.
type LoginPath string

const (
	// LoginPathHeadlessOnly means the saved session state was still valid:
	// ensureSession never launched a visible browser or touched the
	// dedicated login profile - the common case for a healthy daily run.
	LoginPathHeadlessOnly LoginPath = "headless-only"
	// LoginPathInteractiveRelogin means the saved session had expired (or
	// didn't exist), so ensureSession fell through to the interactive
	// login branch - a visible browser flashed briefly while TU-Fast
	// completed the Shibboleth/2FA exchange with no human click needed -
	// before relaunching headless for the actual crawl.
	LoginPathInteractiveRelogin LoginPath = "interactive-relogin"
	// LoginPathUnknown is used when the run never got far enough to
	// establish a session at all (e.g. EnsureTUFastPresent's pre-flight
	// check failed before any scraper was created).
	LoginPathUnknown LoginPath = "unknown"
)

// Status is the JSON shape written to and read from the status file.
//
// Hard requirement (CLAUDE.md: "credentials and session data never leave
// the machine unscrubbed" - this file is local-only, but there is still
// nothing sensitive to put in it): Message is always passed through
// SanitizeMessage before being written (see Write), so even a caller that
// forgot to sanitize a raw error string cannot accidentally persist
// cookies/tokens/session-state content here. See statuslog_test.go's
// TestWriteNeverIncludesSessionStateContent.
type Status struct {
	Timestamp time.Time `json:"timestamp"`
	Outcome   Outcome   `json:"outcome"`
	Message   string    `json:"message"`

	// FilesDownloaded/FilesSkipped/FilesErrored mirror
	// internal/syncer.Stats's Downloaded/Skipped/Errors fields for the run
	// this status describes - reused as-is rather than re-derived, per the
	// queue task's "however internal/syncer already reports this" guidance.
	FilesDownloaded int `json:"files_downloaded"`
	FilesSkipped    int `json:"files_skipped"`
	FilesErrored    int `json:"files_errored"`

	LoginPath LoginPath `json:"login_path"`
}

// fileName is the status file's name under ~/.opal-downloader/.
const fileName = "last-scheduled-run.json"

// DefaultPath returns the real, single status-file path used in
// production: ~/.opal-downloader/last-scheduled-run.json.
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory for the scheduled-run status file: %w", err)
	}
	return filepath.Join(home, ".opal-downloader", fileName), nil
}

// WriteDefault writes status to DefaultPath(). This is what production code
// (cmd/opal-downloader's runSync) calls; tests exercise Write(path, status)
// directly against a temp directory instead.
func WriteDefault(status Status) error {
	path, err := DefaultPath()
	if err != nil {
		return err
	}
	return Write(path, status)
}

// Write persists status to path, creating its parent directory if needed.
// status.Message is always re-sanitized here (see SanitizeMessage) even
// though callers are expected to have already produced a safe message -
// this is the last line of defense before anything touches disk.
func Write(path string, status Status) error {
	status.Message = SanitizeMessage(status.Message)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating directory for scheduled-run status file: %w", err)
	}

	data, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding scheduled-run status: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing scheduled-run status file at %s: %w", path, err)
	}
	return nil
}

// ReadDefault reads the status file at DefaultPath(). See Read's doc
// comment for the "degrade silently" contract callers rely on.
func ReadDefault() (Status, bool) {
	path, err := DefaultPath()
	if err != nil {
		return Status{}, false
	}
	return Read(path)
}

// Read reads and parses the status file at path. Per the queue task's
// acceptance criterion ("If the status file doesn't exist ... or is
// malformed/unreadable, the GUI must degrade silently - no banner, no
// error shown to the user, no crash"), this never returns an error: any
// problem (missing file, permission error, corrupt/truncated JSON) is
// reported the same way, as (Status{}, false), for the caller to treat as
// "nothing to report".
func Read(path string) (Status, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Status{}, false
	}
	var status Status
	if err := json.Unmarshal(data, &status); err != nil {
		return Status{}, false
	}
	if status.Outcome == "" {
		return Status{}, false
	}
	return status, true
}

// maxMessageLen bounds how long a sanitized message can be, so a runaway
// or unexpectedly verbose upstream error can't bloat the status file
// (or a rendered GUI banner) indefinitely.
const maxMessageLen = 500

// credentialHeaderPattern matches header-shaped "Cookie: ...",
// "Set-Cookie: ...", "Authorization: ..." fragments that should never
// appear in a user-facing message but are defended against here as a
// backstop in case some deeper layer ever echoes raw HTTP header text into
// an error.
var credentialHeaderPattern = regexp.MustCompile(`(?i)\b(cookie|set-cookie|authorization|bearer)\s*[:=]\s*\S+`)

// tokenLikePattern matches long runs of base64/hex-alphabet characters -
// the shape of session tokens, cookie values, and similar credential-like
// strings - so they get redacted even if they show up somewhere unexpected
// rather than a header-labelled fragment. 32 characters is comfortably
// longer than any normal English word or filename segment, so this should
// not usually trigger on legitimate error text (course/file names, URLs
// with short path segments, etc.).
var tokenLikePattern = regexp.MustCompile(`[A-Za-z0-9+/_\-]{32,}`)

// sessionStateMarkers are lowercase substrings that indicate a message may
// contain raw Playwright storage-state content (cookies/localStorage) or a
// known OPAL/Shibboleth session-cookie name, rather than one of this
// codebase's already-wrapped, human-readable error strings. Unlike
// credentialHeaderPattern/tokenLikePattern (which redact just the matched
// piece), finding any of these markers discards the *entire* message in
// favor of a generic placeholder (see SanitizeMessage) - surgically
// redacting only the "value" fields of an arbitrary JSON blob is fragile
// (short field names, nested structures, unknown key names), so once a
// message looks like it might be session-state-shaped at all, the safest
// thing is to not show any of it rather than risk a partial leak.
var sessionStateMarkers = []string{
	`"cookies"`,
	`"localstorage"`,
	`"storagestate"`,
	"jsessionid",
	"set-cookie",
}

// looksLikeSessionState reports whether msg contains any marker from
// sessionStateMarkers (case-insensitively).
func looksLikeSessionState(msg string) bool {
	lower := strings.ToLower(msg)
	for _, marker := range sessionStateMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// redactedSessionStateMessage is what SanitizeMessage substitutes for any
// message that trips looksLikeSessionState.
const redactedSessionStateMessage = "sync failed (error details omitted: the underlying message looked like it might contain session/cookie data)"

// SanitizeMessage is the sanitization boundary every message written to
// the status file passes through (see Write). It is defense-in-depth on
// top of this codebase's existing convention of already wrapping errors in
// human-readable, credential-free text at the point they're raised (e.g.
// scraper.isAuthenticated's "could not reach OPAL at %s...",
// scraper.EnsureTUFastPresent's messages, synclock.ErrHeld's "a sync is
// already running (PID N, ...)") - see those callers' doc comments. Never
// pass a raw, unwrapped Playwright/lower-level error straight through
// without going through this.
func SanitizeMessage(msg string) string {
	if looksLikeSessionState(msg) {
		return redactedSessionStateMessage
	}
	msg = credentialHeaderPattern.ReplaceAllString(msg, "$1: [redacted]")
	msg = tokenLikePattern.ReplaceAllString(msg, "[redacted]")
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return "no further detail available"
	}
	if len(msg) > maxMessageLen {
		msg = msg[:maxMessageLen] + "... [truncated]"
	}
	return msg
}
