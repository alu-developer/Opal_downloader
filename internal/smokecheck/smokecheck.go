// Package smokecheck implements the on-demand "does the crawl/sync still
// actually work against real OPAL" check described in queue task
// add-scraper-smoke-check: a local check a maintainer or queue-run can run
// right after touching internal/scraper or internal/syncer, reusing the
// existing saved session_state_file exactly the way `list`/`sync` already
// do - no new credential handling, no data leaving the machine.
//
// It evaluates two signals from a single discovery pass
// (Discoverer.ScrapeWithSavedSession - the same call `list` makes, so this
// package never drives its own separate crawl):
//
//   - Login/session validity: did establishing the session need to fall
//     back to an interactive re-login? This is reported (Result.LoginMessage/
//     UsedInteractiveLogin), not treated as a failure by itself - TU-Fast
//     completing a re-login automatically is the system working as
//     designed, not a regression. The caller (cmd/opal-downloader) is
//     expected to call scraper.EnsureTUFastPresent before ScrapeWithSavedSession,
//     the same pre-flight `sync --scheduled` already uses, so a session that
//     would need a *human* 2FA click fails fast instead of hanging up to
//     300s - see runSmokeCheck's doc comment.
//   - Discovery sanity: is the discovered total file count in the right
//     ballpark? Compared against a small, locally-persisted high-water-mark
//     baseline (see Baseline/EvaluateDiscovery) instead of a hand-maintained
//     magic number that would go stale the moment a course's real content
//     changes.
//
// A third possible check - a full `sync` that actually downloads files - is
// deliberately NOT part of Run: discovery already exercises the DOM-scraping
// and session-establishment code this check exists to protect, and a real
// download pass is slower, flakier, and (without care) would rewrite local
// download-path/manifest state. It is instead available as an explicit,
// separate opt-in (`sync --scheduled`-style callers can add it) - see
// cmd/opal-downloader's --full-sync flag, which runs a real sync against a
// disposable scratch directory rather than anything in this package.
package smokecheck

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/alu-developer/opal-downloader/internal/scraper"
	"github.com/alu-developer/opal-downloader/internal/statuslog"
)

// Discoverer is the subset of *scraper.OpalScraper's behavior Run depends
// on. Extracted as an interface so tests can exercise the baseline/pass-fail
// logic with a fake, without needing a real browser/Playwright session -
// mirrors internal/syncer.Downloader's approach for the same reason.
type Discoverer interface {
	ScrapeWithSavedSession(ctx context.Context, courseFilter []string) ([]scraper.RemoteFile, error)
	UsedInteractiveLogin() bool
}

// Baseline is the small, locally-persisted "expected file count" record
// smoke-check compares each run against. It intentionally carries no
// session/credential data - just counts and a timestamp - so it is safe to
// print or attach to a bug report verbatim.
type Baseline struct {
	UpdatedAt  time.Time      `json:"updated_at"`
	TotalFiles int            `json:"total_files"`
	Courses    map[string]int `json:"courses"`
}

// baselineFileName is the baseline file's name under ~/.opal-downloader/,
// alongside the other local-only state files (sync.lock,
// last-scheduled-run.json, the login profile).
const baselineFileName = "smoke-baseline.json"

// DefaultBaselinePath returns the real, single baseline file path used in
// production: ~/.opal-downloader/smoke-baseline.json.
func DefaultBaselinePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory for the smoke-check baseline file: %w", err)
	}
	return filepath.Join(home, ".opal-downloader", baselineFileName), nil
}

// LoadBaseline reads the baseline file at path. Any problem (missing file,
// permission error, corrupt/truncated JSON) is reported the same way, as
// (Baseline{}, false) - "no usable baseline yet" - mirroring
// internal/statuslog.Read's degrade-silently contract, since a first run on
// a fresh machine (or after --reset-baseline) is an expected, non-error
// case, not a failure.
func LoadBaseline(path string) (Baseline, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Baseline{}, false
	}
	var b Baseline
	if err := json.Unmarshal(data, &b); err != nil {
		return Baseline{}, false
	}
	return b, true
}

// SaveBaseline persists b to path, creating its parent directory if needed.
func SaveBaseline(path string, b Baseline) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating directory for smoke-check baseline file: %w", err)
	}
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding smoke-check baseline: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing smoke-check baseline file at %s: %w", path, err)
	}
	return nil
}

// DefaultDropThresholdPercent is how far (as a percentage of the recorded
// baseline) the current total file count may fall before EvaluateDiscovery
// reports failure. 20% comfortably tolerates ordinary content churn (a
// professor removing a handful of old files) while still catching the kind
// of wholesale loss seen in real incidents documented in
// docs/OPERATIONS.md's course_concurrency writeup (21-76% of files lost
// across whole courses).
const DefaultDropThresholdPercent = 20.0

// EvaluateDiscovery compares the current per-course discovery counts against
// baseline (ignored when hadBaseline is false) and decides pass/fail plus
// the Baseline value the caller should persist afterward (via SaveBaseline).
//
// The baseline is a high-water mark, not a rolling average or "last run"
// snapshot: it only ever moves up (current total >= baseline's), never down
// automatically. A run whose total dropped but stayed within
// thresholdPercent of the baseline still passes, but the returned Baseline
// keeps the higher historical total rather than lowering it. This
// deliberately avoids "boiling frog" drift, where a sequence of small,
// individually-tolerable drops would otherwise gradually redefine "normal"
// downward until a real regression could hide inside the accumulated slack.
// When a genuine, expected drop happens (e.g. semester rollover, a course
// archived), the caller resets deliberately (see cmd/opal-downloader's
// --reset-baseline flag) rather than this function silently accepting a
// lower number as the new normal.
//
// A total of exactly 0 is always a failure once a baseline exists (or even
// without one being strictly required to call it a failure - see the first
// branch below) - a genuinely empty, never-synced account would not have
// produced a prior baseline in the first place, so 0 files after having ever
// seen more is an unambiguous discovery-broke signal, never legitimate
// content churn.
func EvaluateDiscovery(current map[string]int, baseline Baseline, hadBaseline bool, thresholdPercent float64) (pass bool, message string, next Baseline) {
	total := 0
	for _, n := range current {
		total += n
	}
	now := Baseline{UpdatedAt: time.Now().UTC(), TotalFiles: total, Courses: current}

	if !hadBaseline {
		return true, fmt.Sprintf("no baseline found; established one at %d file(s) across %d course(s)", total, len(current)), now
	}

	if total == 0 {
		// Never ratchet the baseline down to zero - keep the last known-good
		// value so the next run still has something real to compare against.
		return false, fmt.Sprintf("found 0 files across %d course(s) (baseline: %d file(s) recorded %s) - discovery is likely broken", len(current), baseline.TotalFiles, baseline.UpdatedAt.Format(time.RFC3339)), baseline
	}

	if baseline.TotalFiles > 0 && total < baseline.TotalFiles {
		dropPercent := 100 * float64(baseline.TotalFiles-total) / float64(baseline.TotalFiles)
		if dropPercent > thresholdPercent {
			return false, fmt.Sprintf("file count dropped %.1f%% (%d -> %d files), exceeds the %.1f%% threshold (baseline recorded %s)", dropPercent, baseline.TotalFiles, total, thresholdPercent, baseline.UpdatedAt.Format(time.RFC3339)), baseline
		}
		// Within threshold: pass, but keep the higher historical baseline
		// rather than lowering it - see the doc comment above.
		kept := baseline
		kept.UpdatedAt = time.Now().UTC()
		return true, fmt.Sprintf("%d file(s) (%.1f%% below baseline of %d, within the %.1f%% threshold)", total, dropPercent, baseline.TotalFiles, thresholdPercent), kept
	}

	return true, fmt.Sprintf("%d file(s) (baseline was %d)", total, baseline.TotalFiles), now
}

// Options configures a Run call.
type Options struct {
	// ThresholdPercent overrides DefaultDropThresholdPercent when positive.
	ThresholdPercent float64
	// ResetBaseline discards any existing baseline before this run, so the
	// current count is recorded fresh as the new starting point instead of
	// being compared/failed against a stale one.
	ResetBaseline bool
	// BaselinePath overrides DefaultBaselinePath() - used by tests to avoid
	// touching the real ~/.opal-downloader directory.
	BaselinePath string
}

// Result is Run's outcome, safe to print verbatim (no session/credential
// content - see the package doc comment).
type Result struct {
	Pass                 bool
	LoginMessage         string
	DiscoveryMessage     string
	UsedInteractiveLogin bool
	TotalFiles           int
	Courses              map[string]int
	BaselineWriteWarning string
}

// Run performs the single discovery pass and both checks described in the
// package doc comment. It returns an error only when the discovery scrape
// itself fails outright (network/selector/session error) - a low or zero
// file count is reported via Result.Pass/DiscoveryMessage, not as a Go
// error, since that is smoke-check's actual job to detect and report
// clearly, not to treat as an exceptional condition.
func Run(ctx context.Context, sc Discoverer, opts Options) (Result, error) {
	// "*" deliberately, ignoring config.yaml's courses: filter (which Options
	// above does not even carry): this is an account-wide reachability
	// check, not a check of one config's subset - documented in the CLI's
	// --help (`smoke-check` line and its own option note) after a friction
	// walk (2026-08-18) found the two reading identically to `list`'s, which
	// does respect the filter.
	files, err := sc.ScrapeWithSavedSession(ctx, []string{"*"})
	if err != nil {
		return Result{}, err
	}

	counts := map[string]int{}
	total := 0
	for _, f := range files {
		counts[f.Course]++
		total++
	}

	result := Result{
		TotalFiles:           total,
		Courses:              counts,
		UsedInteractiveLogin: sc.UsedInteractiveLogin(),
	}
	if result.UsedInteractiveLogin {
		result.LoginMessage = "saved session had expired; an interactive relogin completed automatically (TU-Fast) - not itself a failure, but worth noticing if it happens on every run"
	} else {
		result.LoginMessage = "saved session was still valid; no relogin needed"
	}

	threshold := opts.ThresholdPercent
	if threshold <= 0 {
		threshold = DefaultDropThresholdPercent
	}

	baselinePath := opts.BaselinePath
	if baselinePath == "" {
		p, err := DefaultBaselinePath()
		if err != nil {
			return Result{}, err
		}
		baselinePath = p
	}

	var baseline Baseline
	hadBaseline := false
	if !opts.ResetBaseline {
		baseline, hadBaseline = LoadBaseline(baselinePath)
	}

	pass, msg, next := EvaluateDiscovery(counts, baseline, hadBaseline, threshold)
	result.Pass = pass
	result.DiscoveryMessage = msg

	if writeErr := SaveBaseline(baselinePath, next); writeErr != nil {
		// Non-fatal: the discovery result itself is still valid even if
		// persisting the baseline failed (e.g. disk full/permissions).
		// Sanitized the same way any other message is before it can ever
		// reach a terminal/log (see internal/statuslog.SanitizeMessage) -
		// belt-and-suspenders even though a baseline-file write error would
		// not normally contain session data.
		result.BaselineWriteWarning = statuslog.SanitizeMessage(writeErr.Error())
	}

	return result, nil
}
