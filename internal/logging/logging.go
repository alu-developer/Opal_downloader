// Package logging is this project's output path: one place that decides
// where a message goes, so call sites do not have to.
//
// WHY THIS EXISTS
// ---------------
// Before this, the tool had exactly one channel - fmt.Printf to stdout - used
// for two completely different jobs. "Opening OPAL at ..." is for the person
// running the program. `Warning: skipping section "Woche 07" (https://...):
// navigation failed after retry: ...` is for whoever has to work out why files
// went missing three weeks later. Sharing a channel served neither: the user
// read text written for a developer, and the developer's text vanished the
// moment the terminal scrolled - or was never visible at all, since the GUI
// runs as a window and nobody sees its stdout.
//
// Raised by the maintainer (2026-07-26), relaying their father's point that a
// long-lived project needs real logging with more than one layer. It is also
// what the reliability principle in CLAUDE.md needs in practice: "a missing
// file is an incident to fix" is not achievable if the evidence was printed to
// a console that is gone.
//
// THE TWO AXES
// ------------
// Severity alone does not answer "should the user see this?". A skipped
// section is a genuine warning and also completely uninteresting to a student
// who wants their slides. So a record carries both:
//
//   - a level (Debug/Info/Warn/Error), which is about how bad it is, and
//   - an audience (user or diagnostic), which is about who it is for.
//
// Two sinks then read those independently:
//
//   - The console shows user-facing records, plus every error regardless of
//     audience - an error the user never hears about is how a broken tool
//     looks like a working one. Verbose mode adds the diagnostics.
//   - The file takes everything, with timestamps and levels, and keeps a few
//     rotations. This is the layer that survives the terminal closing.
//
// SCRUBBING
// ---------
// Everything written to the file is scrubbed first (see scrub.go). The file is
// exactly what someone would attach to a bug report, so it has to be safe to
// attach without anyone remembering to check - see CLAUDE.md on credentials
// and session data never leaving the machine unscrubbed.
//
// THE FACADE IS PRINTF-SHAPED ON PURPOSE
// --------------------------------------
// slog does the work underneath (levels, handler fan-out, a structured file
// format that a future bug-report reader can parse), but the functions here
// take a format string, because that is what all 166 existing call sites look
// like and what the maintainer reads comfortably. Making every one of them
// learn key-value pairs would have bought nothing at the call site.
package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

// audienceKey marks which sink a record is meant for. An attribute rather
// than a custom level, because it is genuinely a second dimension: a
// diagnostic can be a warning and a user-facing line can be routine.
const audienceKey = "audience"

const (
	audienceUser       = "user"
	audienceDiagnostic = "diag"
)

var (
	mu      sync.RWMutex
	current = slog.New(newConsoleHandler(os.Stdout, false))
	closers []io.Closer
)

// logger returns the active logger. Reads are cheap and concurrent; Setup is
// the only writer, and it runs once at startup.
func logger() *slog.Logger {
	mu.RLock()
	defer mu.RUnlock()
	return current
}

// Options configures where output goes. The zero value is usable: console
// only, no file, quiet about diagnostics - which is the right behaviour for a
// test binary or any caller that never calls Setup.
type Options struct {
	// Console receives user-facing records. Defaults to os.Stdout.
	Console io.Writer

	// FilePath is the diagnostic log. Empty disables the file sink entirely,
	// which is what tests want and what a machine with an unwritable home
	// directory falls back to.
	FilePath string

	// Verbose additionally shows diagnostics on the console. This is the
	// "turn it up" switch the tool did not have: dev mode meant "visible
	// browser", never "more logging".
	Verbose bool
}

// Setup installs the process-wide logger and returns a function that closes
// the file sink.
//
// A file that cannot be opened is reported and then ignored: failing to write
// a log is never a reason to fail the run the user actually asked for.
func Setup(opts Options) (func(), error) {
	console := opts.Console
	if console == nil {
		console = os.Stdout
	}

	handlers := []slog.Handler{newConsoleHandler(console, opts.Verbose)}

	var openErr error
	var toClose []io.Closer
	if opts.FilePath != "" {
		w, err := openRotating(opts.FilePath)
		if err != nil {
			openErr = fmt.Errorf("diagnostic log disabled: %w", err)
		} else {
			handlers = append(handlers, newFileHandler(w))
			toClose = append(toClose, w)
		}
	}

	mu.Lock()
	current = slog.New(fanOut(handlers))
	closers = toClose
	mu.Unlock()

	return closeAll, openErr
}

func closeAll() {
	mu.Lock()
	cs := closers
	closers = nil
	mu.Unlock()
	for _, c := range cs {
		_ = c.Close()
	}
}

// DefaultLogPath is where the diagnostic log lives: beside the login profile,
// under the user's home directory. Returns "" when the home directory cannot
// be determined, which disables the file sink rather than guessing at a path.
func DefaultLogPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".opal-downloader", "logs", "opal-downloader.log")
}

// User reports something the person running the tool wants to know: what it
// is doing, what it found, what it needs from them.
func User(format string, args ...any) {
	emit(slog.LevelInfo, audienceUser, format, args...)
}

// Detail records something only useful when working out why a run behaved the
// way it did. It never reaches the console unless Verbose is set.
func Detail(format string, args ...any) {
	emit(slog.LevelDebug, audienceDiagnostic, format, args...)
}

// Warn records something that went wrong but did not stop the run - a
// retried navigation, a section that could not be read. Diagnostic by
// default: these are frequent, individually unactionable, and a user who sees
// a stream of warnings on a run that succeeded learns to ignore all warnings.
func Warn(format string, args ...any) {
	emit(slog.LevelWarn, audienceDiagnostic, format, args...)
}

// Error records something the user has to know about, because it changes what
// they got. Always reaches the console, whatever the verbosity.
func Error(format string, args ...any) {
	emit(slog.LevelError, audienceUser, format, args...)
}

func emit(level slog.Level, audience, format string, args ...any) {
	l := logger()
	if !l.Enabled(context.Background(), level) {
		return
	}
	l.LogAttrs(context.Background(), level, fmt.Sprintf(format, args...),
		slog.String(audienceKey, audience))
}

// fanOut sends every record to each handler, letting each decide for itself
// whether to write it. Composition rather than one handler with two writers:
// the console and the file disagree about both what to include and how to
// format it, and that disagreement is the whole point.
func fanOut(handlers []slog.Handler) slog.Handler {
	if len(handlers) == 1 {
		return handlers[0]
	}
	return &multiHandler{handlers: handlers}
}

type multiHandler struct{ handlers []slog.Handler }

func (m *multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range m.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (m *multiHandler) Handle(ctx context.Context, r slog.Record) error {
	var firstErr error
	for _, h := range m.handlers {
		if !h.Enabled(ctx, r.Level) {
			continue
		}
		// Each handler gets its own copy: Record carries attrs by value but
		// shares backing storage once appended to, and a handler is free to
		// add its own.
		if err := h.Handle(ctx, r.Clone()); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (m *multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		out[i] = h.WithAttrs(attrs)
	}
	return &multiHandler{handlers: out}
}

func (m *multiHandler) WithGroup(name string) slog.Handler {
	out := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		out[i] = h.WithGroup(name)
	}
	return &multiHandler{handlers: out}
}

// consoleHandler writes the way this tool has always written to a terminal:
// the message, nothing else. No timestamp (the user is watching it happen),
// no level tag on ordinary lines, and a plain "Error: " prefix where it
// matters. A CLI that suddenly started emitting `time=... level=INFO msg=...`
// would be a downgrade for the person reading it.
type consoleHandler struct {
	mu      *sync.Mutex
	w       io.Writer
	verbose bool
	attrs   []slog.Attr
}

func newConsoleHandler(w io.Writer, verbose bool) *consoleHandler {
	return &consoleHandler{mu: &sync.Mutex{}, w: w, verbose: verbose}
}

func (c *consoleHandler) Enabled(_ context.Context, level slog.Level) bool {
	if c.verbose {
		return true
	}
	return level >= slog.LevelInfo
}

func (c *consoleHandler) Handle(_ context.Context, r slog.Record) error {
	if !c.shows(r) {
		return nil
	}

	prefix := ""
	switch {
	case r.Level >= slog.LevelError:
		prefix = "Error: "
	case r.Level >= slog.LevelWarn:
		prefix = "Warning: "
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	_, err := fmt.Fprintln(c.w, prefix+r.Message)
	return err
}

// shows decides whether the console is the right place for a record: it is,
// if the record was written for the user, or if it is an error (which the
// user has to hear about however it was tagged), or if verbose mode has asked
// for everything.
func (c *consoleHandler) shows(r slog.Record) bool {
	if c.verbose || r.Level >= slog.LevelError {
		return true
	}
	return audienceOf(r, c.attrs) == audienceUser
}

func (c *consoleHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clone := *c
	clone.attrs = append(append([]slog.Attr{}, c.attrs...), attrs...)
	return &clone
}

func (c *consoleHandler) WithGroup(string) slog.Handler { return c }

// newFileHandler writes the full record - time, level, audience, message - as
// text. Text rather than JSON: the first reader of this file is a student
// trying to see what happened, not a log aggregator, and `tail` should be
// enough.
func newFileHandler(w io.Writer) slog.Handler {
	return &fileHandler{inner: slog.NewTextHandler(w, &slog.HandlerOptions{Level: slog.LevelDebug})}
}

// fileHandler scrubs before delegating. This is the sanitization boundary for
// the log file: the file is what someone attaches to a bug report, so it has
// to be safe to attach without anyone remembering to check it first.
type fileHandler struct{ inner slog.Handler }

func (f *fileHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return f.inner.Enabled(ctx, level)
}

func (f *fileHandler) Handle(ctx context.Context, r slog.Record) error {
	scrubbed := r.Clone()
	scrubbed.Message = scrubForFile(r.Message)
	return f.inner.Handle(ctx, scrubbed)
}

func (f *fileHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &fileHandler{inner: f.inner.WithAttrs(attrs)}
}

func (f *fileHandler) WithGroup(name string) slog.Handler {
	return &fileHandler{inner: f.inner.WithGroup(name)}
}

// audienceOf finds the audience attribute on a record, falling back to the
// handler's own bound attrs (set via WithAttrs) and finally to diagnostic.
// Defaulting to diagnostic rather than user is deliberate: a message that
// forgot to say who it is for should not surprise the user, and the file
// keeps it either way.
func audienceOf(r slog.Record, bound []slog.Attr) string {
	found := audienceDiagnostic
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == audienceKey {
			found = a.Value.String()
			return false
		}
		return true
	})
	if found != audienceDiagnostic {
		return found
	}
	for _, a := range bound {
		if a.Key == audienceKey {
			return a.Value.String()
		}
	}
	return found
}
