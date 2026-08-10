// Package statuslog persists the outcome of the most recent `sync
// --scheduled` run to a small local JSON file, so a human has something to
// read the next time they open the GUI even though an unattended,
// Task-Scheduler-triggered run has no terminal/GUI window for them to watch
// live (see docs/scheduled-sync-plan.md section 3, "Failure detection and
// notification"). This is the core mechanism behind the GUI's
// scheduled-sync banner (internal/gui/scheduled_status.go).
//
// WriteDefault overwrites the single-run status file in place (used by the
// GUI banner, which only ever wants "the most recent outcome") but also
// appends the same status to a small bounded rolling history file (see
// AppendHistory/historyFileName). That reverses this package's original
// "Non-goals: no historical log" call - the "sync already running" 4
// seconds after another started bug (docs/BACKLOG.md, "Blocked / needs
// evidence") turned out to be undiagnosable precisely because only the
// latest run's outcome survived a re-registration, so the next occurrence
// needs more than one data point to go on.
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

	// OutcomeSkipped means this run never actually attempted a sync because
	// another opal-downloader process already held the sync lock (see
	// synclock.ErrHeld) - most commonly the GUI running its own in-process
	// "Sync now" job at the same moment Task Scheduler's daily trigger fired.
	// Deliberately distinct from OutcomeFailure: nothing went wrong, another
	// instance is (or just was) doing the work, and there is nothing for the
	// user to fix. Both the scheduled-failure toast notification and the GUI
	// banner treat this the same as OutcomeSuccess (see notify_on_scheduled_
	// failure's gate in cmd/opal-downloader/root.go and
	// internal/gui/scheduled_status.go) - surfacing it as a "failure" is
	// exactly the noise reported live on the maintainer's machine (2026-07-23):
	// a scheduled run 4 seconds behind another one is routine overlap, not an
	// incident.
	OutcomeSkipped Outcome = "skipped"
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

// historyFileName is the rolling-history file's name under
// ~/.opal-downloader/. JSON Lines (one Status object per line) rather than a
// single JSON array, so appending never requires reading and re-parsing the
// whole file first, and one corrupt/truncated line (e.g. a write that
// crashed mid-flush) can be skipped without losing every other entry.
const historyFileName = "scheduled-run-history.jsonl"

// maxHistoryEntries bounds the rolling history to a "small" log per its
// purpose (giving the next occurrence of an intermittent bug something to
// mine, not a permanent audit trail) - 30 entries covers a month of daily
// runs, comfortably past the "a run every day" cadence this feature is
// built around, while keeping the file trivially small.
const maxHistoryEntries = 30

// DefaultPath returns the real, single status-file path used in
// production: ~/.opal-downloader/last-scheduled-run.json.
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory for the scheduled-run status file: %w", err)
	}
	return filepath.Join(home, ".opal-downloader", fileName), nil
}

// DefaultHistoryPath returns the real rolling-history file path used in
// production: ~/.opal-downloader/scheduled-run-history.jsonl.
func DefaultHistoryPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory for the scheduled-run history file: %w", err)
	}
	return filepath.Join(home, ".opal-downloader", historyFileName), nil
}

// dismissFileName holds the timestamp of the failure the user has already
// dismissed in the GUI banner, under ~/.opal-downloader/.
//
// This used to live in the browser's localStorage, which looked like the
// smaller solution and was in fact broken: the GUI binds an *ephemeral* port
// (net.Listen on 127.0.0.1:0 - see gui.Options.Port), so every launch serves
// from a different origin, and localStorage is scoped per origin. Dismissing
// therefore held only until the window was closed, which is exactly what the
// maintainer reported (2026-08-10: "wenn dismiss geclicked wird, dann ist es
// beim naechsten oeffnen wieder da"). A file next to the status file it
// refers to is immune to that, and to the user clearing browser data.
const dismissFileName = "dismissed-scheduled-run.json"

// dismissRecord is the JSON shape of the dismiss file: the timestamp of the
// dismissed run, formatted exactly as the status file writes it. Storing the
// timestamp rather than a bare "dismissed" boolean is what makes a *new*
// failure show up again on its own - see IsDismissed.
type dismissRecord struct {
	Timestamp time.Time `json:"timestamp"`
}

// DefaultDismissPath returns the real dismiss-marker path used in
// production: ~/.opal-downloader/dismissed-scheduled-run.json.
func DefaultDismissPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory for the dismissed-run file: %w", err)
	}
	return filepath.Join(home, ".opal-downloader", dismissFileName), nil
}

// WriteDismissed records that the run stamped at ts has been dismissed.
func WriteDismissed(path string, ts time.Time) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating directory for the dismissed-run file: %w", err)
	}
	data, err := json.MarshalIndent(dismissRecord{Timestamp: ts}, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding the dismissed-run marker: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing the dismissed-run file at %s: %w", path, err)
	}
	return nil
}

// ReadDismissed returns the timestamp last passed to WriteDismissed, or the
// zero time when there is none. Degrades the same way Read does: a missing,
// unreadable or corrupt file is simply "nothing dismissed", never an error -
// the worst case being that a banner the user already dismissed comes back,
// which is the state this whole file exists to improve on and not something
// worth failing a page load over.
func ReadDismissed(path string) time.Time {
	data, err := os.ReadFile(path)
	if err != nil {
		return time.Time{}
	}
	var rec dismissRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return time.Time{}
	}
	return rec.Timestamp
}

// WriteDismissedDefault is WriteDismissed against DefaultDismissPath().
func WriteDismissedDefault(ts time.Time) error {
	path, err := DefaultDismissPath()
	if err != nil {
		return err
	}
	return WriteDismissed(path, ts)
}

// ReadDismissedDefault is ReadDismissed against DefaultDismissPath().
func ReadDismissedDefault() time.Time {
	path, err := DefaultDismissPath()
	if err != nil {
		return time.Time{}
	}
	return ReadDismissed(path)
}

// IsDismissed reports whether the run stamped at ts is the one that was
// dismissed. Compared by instant (Time.Equal), not by formatted string, so a
// timezone or formatting difference between writer and reader cannot
// silently un-dismiss a banner. A *newer* run never matches an older
// dismissal, which is the point: dismissing today's failure hides today's
// failure, and tomorrow's failure is news again.
func IsDismissed(dismissed, ts time.Time) bool {
	return !dismissed.IsZero() && dismissed.Equal(ts)
}

// AppendHistory adds status as one more line to the rolling history file at
// path, trimming to the most recent maxHistoryEntries afterward. status.
// Message goes through the same SanitizeMessage boundary as Write - this
// file is just as local-only as the single-status one, but nothing here
// should ever carry credential/session-shaped content either.
//
// Not safe against two callers racing on the same path: this is a plain
// read-modify-write, not a locked append, so two concurrent calls can lose
// one entry to a last-write-wins overwrite. Accepted rather than fixed with
// real file locking: WriteDefault (this function's only caller) only ever
// runs from `sync --scheduled`, so the sole way to hit this race is two
// scheduled runs actually overlapping - precisely the rare event this log
// exists to help diagnose, and losing one entry from a best-effort
// diagnostic aid is a far smaller cost than the cross-platform locking
// complexity would be for what's meant to stay "a small rolling log", not a
// hardened audit trail.
func AppendHistory(path string, status Status) error {
	status.Message = SanitizeMessage(status.Message)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating directory for scheduled-run history file: %w", err)
	}

	line, err := json.Marshal(status)
	if err != nil {
		return fmt.Errorf("encoding scheduled-run history entry: %w", err)
	}

	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading scheduled-run history file at %s: %w", path, err)
	}

	lines := splitNonEmptyLines(existing)
	lines = append(lines, string(line))
	if len(lines) > maxHistoryEntries {
		lines = lines[len(lines)-maxHistoryEntries:]
	}

	data := []byte(strings.Join(lines, "\n") + "\n")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing scheduled-run history file at %s: %w", path, err)
	}
	return nil
}

// splitNonEmptyLines splits raw JSONL content into its non-blank lines,
// tolerating a trailing newline (or no content at all).
func splitNonEmptyLines(raw []byte) []string {
	var lines []string
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

// ReadHistoryDefault reads the rolling history file at DefaultHistoryPath().
func ReadHistoryDefault() ([]Status, error) {
	path, err := DefaultHistoryPath()
	if err != nil {
		return nil, err
	}
	return ReadHistory(path)
}

// ReadHistory reads and parses the rolling history file at path, oldest
// entry first. A line that fails to parse (partial write, disk corruption)
// is skipped rather than failing the whole read - consistent with this
// package's "degrade rather than crash" contract for Read, except here
// there may be good entries on either side of a bad one worth keeping. A
// missing file returns an empty slice, not an error.
func ReadHistory(path string) ([]Status, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading scheduled-run history file at %s: %w", path, err)
	}

	var out []Status
	for _, line := range splitNonEmptyLines(raw) {
		var status Status
		if err := json.Unmarshal([]byte(line), &status); err != nil {
			continue
		}
		if status.Outcome == "" {
			continue
		}
		out = append(out, status)
	}
	return out, nil
}

// WriteDefault writes status to DefaultPath() and appends it to
// DefaultHistoryPath(). This is what production code (cmd/opal-downloader's
// runSync) calls; tests exercise Write(path, status) directly against a temp
// directory instead.
//
// A history-append failure is deliberately not returned as an error: the
// single-run status file (which the GUI banner depends on) is the
// load-bearing write, and history is a best-effort diagnostic aid on top of
// it, not something that should make a scheduled run report failure to the
// user over.
func WriteDefault(status Status) error {
	path, err := DefaultPath()
	if err != nil {
		return err
	}
	if err := Write(path, status); err != nil {
		return err
	}

	if historyPath, err := DefaultHistoryPath(); err == nil {
		_ = AppendHistory(historyPath, status)
	}
	return nil
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
