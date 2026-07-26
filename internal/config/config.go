package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
	"gopkg.in/yaml.v3"
)

const (
	DefaultOPALURL   = "https://bildungsportal.sachsen.de/opal/"
	DefaultStateFile = "~/.opal_storage_state.json"

	// DefaultDownloadConcurrency is the default number of files downloaded
	// concurrently during sync via the fast HTTP path. Live-tested
	// 2026-07-12 against the real TU Dresden OPAL account: no
	// rate-limiting/bot-detection signal was observed at 3, but a separate,
	// unrelated bug (some files fail the fast path and then also fail the
	// serialized browser-fallback download, each failure costing many
	// seconds) dominated wall-clock time badly enough that higher values
	// couldn't be cleanly compared in the time available. Left unchanged at
	// 3 pending a re-test once that fallback bug is fixed.
	DefaultDownloadConcurrency = 3

	// DefaultCourseConcurrency is the default number of courses crawled
	// concurrently during discovery, each on its own browser tab/page.
	//
	// STAYS AT 1. Queue task fix-concurrent-crawl-ajax-race-and-raise-
	// concurrency (2026-07-13) root-caused and fixed several real,
	// distinct bugs behind the original 2026-07-12 finding below (not
	// rate-limiting - a genuine AJAX-render race in concurrent course
	// crawling), live-verified each one against the real TU Dresden
	// account (8 courses, 341 real files, serial ground truth):
	//
	//  1. internal/scraper/navigation.go's per-section content wait used a
	//     single fixed-duration sleep (contentFallbackWaitMs) tuned against
	//     serial rendering. Under concurrent crawling, competing tabs'
	//     renders delay each other, so a fixed wait sometimes elapsed
	//     before a section's content had actually finished rendering -
	//     confirmed live to silently drop a whole course to 0 files when
	//     this hit the root section specifically. Fixed by replacing the
	//     fixed wait with condition-based polling (candidateStabilityPoll,
	//     navigation.go; waitForStableSectionContent, crawl.go) that keeps
	//     re-extracting until the read count stops growing.
	//  2. That polling initially stopped at the first non-growing read,
	//     which was NOT enough on its own - live A/B tested and still lost
	//     the same files as the unfixed code. Root cause: OPAL/Wicket
	//     renders a section in stages (the row list appears, then a later
	//     stage adds the pagination control), and a single non-growing read
	//     can catch a coincidental plateau between those stages. Fixed by
	//     requiring several *consecutive* non-growing reads before trusting
	//     "stable" (sectionContentRequiredStableReads /
	//     showAllExpansionRequiredStableReads, internal/scraper/crawl.go).
	//  3. The "show all" pagination control's Click() timeout (3s) was
	//     sometimes too short for the control to become actionable under
	//     concurrent rendering load, silently leaving a section truncated
	//     to its first page with no error. Fixed with a longer timeout and
	//     retries (showAllClickTimeoutMs/showAllClickMaxAttempts,
	//     internal/scraper/crawl.go).
	//  4. Several workers opening a fresh browser tab at the same instant
	//     (a common pattern right after a couple of short courses finish
	//     around the same time) measurably worsened the render race. Fixed
	//     by serializing and staggering tab creation across workers
	//     (newPageMu/newPageStaggerMs, internal/scraper/scraper.go and
	//     orchestrator.go).
	//
	// These fixes are real and were individually live-confirmed to resolve
	// the race in light-to-moderate concurrent load (multiple courses
	// concurrently, including the account's largest 198-file course paired
	// with just one or two others).
	//
	// WHY THE DEFAULT ISN'T RAISED ANYWAY: even with all four fixes above,
	// repeated live re-tests of the *full* real account (8 courses,
	// including that same 198-file course running concurrently with
	// several smaller ones that have paginated sections) at
	// course_concurrency 2 and 3 still intermittently lost a small number
	// of files (2-8 of 341, roughly 1-2%) - a large improvement over the
	// original 21%-76% loss, but not a byte-for-byte match with the serial
	// ground truth every time, so the "raise it once verified safe"
	// acceptance bar was not met. Pushing the stability-poll budgets even
	// higher (tried live: sectionContentRequiredStableReads=8 with an
	// 8s+ ceiling) did not reliably close the remaining gap either, while
	// roughly doubling total crawl wall-clock time even in the case where
	// nothing needed the extra patience. The per-section extra-patience
	// cost is now scoped to only apply when actually running with
	// course_concurrency>1 (see requiredStableReads, crawl.go) specifically
	// so this investigation's fixes don't regress the course_concurrency=1
	// default's own speed (an earlier unscoped version of this fix
	// live-measured ~999s for the same serial 341-file crawl that takes
	// ~471-602s without or with only the scoped version of it).
	//
	// Net effect for anyone who explicitly opts into course_concurrency>1
	// today (via config.yaml or --course-concurrency): meaningfully safer
	// than before this task (loses ~1-2% of files in the worst case tested,
	// down from 21-76%), but still not proven byte-for-byte safe for every
	// real course mix - most likely to matter for accounts with one course
	// whose crawl time (many minutes) dwarfs the others', running
	// concurrently with smaller courses that have paginated ("show
	// all") sections. Follow-up investigation worth trying if this is
	// revisited: a size/duration-aware scheduler that avoids running the
	// single largest/slowest course concurrently with everything else
	// (e.g. give it a dedicated pass, or at least stagger its tab creation
	// further behind the others), since pure per-section wait-tuning has
	// diminishing returns against this specific contention pattern.
	//
	// STILL AT 1 after re-verification (2026-07-17, queue task
	// close-residual-concurrent-crawl-ajax-race): PR #81 (2026-07-16)
	// replaced navigation.go's fixed 1100ms per-section content wait with a
	// MutationObserver-based debounce (see waitForInteractiveLinks) and got
	// a clean 344/344 on its own single live test at course_concurrency=2 -
	// promising, but PR #73/#78's own residual loss was itself intermittent
	// (2 of 4 repeated runs, not every run), so one clean run couldn't
	// distinguish "the race is closed" from "this run got lucky". Repeating
	// the same full-account live methodology (`list --dev --debug-clicks
	// --profile --course-concurrency 2`, serial ground truth 345 files/7
	// content-bearing courses vs. concurrent, four consecutive runs against
	// the real TU Dresden account) found the race is NOT closed - and this
	// sample was worse than PR #78's: all 4 of 4 runs lost files (337, 339,
	// 331, 333 of 345), not 2 of 4. The specific courses lost were the same
	// two flagged by PR #73/#78 (Analysis: -8 files in 3 of 4 runs;
	// Softwaretechnologie, the account's largest course: -6 files in 2 of 4
	// runs; Algorithmen und Datenstrukturen: -4 files in 1 of 4 runs) -
	// never a different course, and the lost-file count for a given course
	// was identical every time it was lost (a full page's worth, not a
	// random flaky amount).
	//
	// Crucially, --debug-clicks showed the MutationObserver wait itself is
	// NOT the failure point: across all 4 runs it resolved via
	// "settled-no-mutations" every single time (zero "hard-cap"
	// resolutions, zero wait-fixed-fallback injection failures), and every
	// one of the 5 "show all" pagination clicks in each run succeeded on
	// try 1/3 - identically across all 4 runs, including in the runs where
	// Analysis/Softwaretechnologie still lost files. So the click and the
	// new load-completion signal both fired normally every time; the loss
	// happens downstream, in waitForStableSectionContent/
	// candidateStabilityPoll (crawl.go) - unchanged by PR #81, per that
	// task's own scope. Those polls were seen settling on a *stable* (not
	// hard-cap-truncated) read that was nonetheless short of the serial
	// ground truth's count, meaning the section's client-side render
	// itself plateaus at an incomplete state under concurrent contention,
	// not that the poll gave up early (sectionContentMaxPolls=20 was never
	// exhausted in any of the four runs). This was believed (PR #84,
	// 2026-07-17) to implicate candidateStabilityPoll generically - see the
	// RAISED TO 2 entry below for why that was half right: it named the
	// right *engine*, but the wrong *call site*.
	//
	// RAISED TO 2 (2026-07-19, queue task
	// fix-candidate-stability-poll-concurrent-crawl-race): PR #84 only ever
	// looked at "section-content-poll" (waitForStableSectionContent's own
	// --debug-clicks trace, the MAIN per-section content wait after a
	// Goto) and found it innocent - correctly, but that is a different poll
	// from the one actually at fault. This task added equivalent per-poll
	// logging to waitForStableExpandedCandidates (the "show all" pagination
	// EXPANSION poll, run after a click rather than a Goto - see
	// expandShowAllInSection, internal/scraper/crawl.go) - previously the
	// only candidateStabilityPoll caller with no trace at all - and that
	// immediately exposed the real mechanism on the first re-run that
	// reproduced the loss: every lost section's expansion poll read the
	// exact same (pre-expansion, un-expanded) candidate count on every
	// single poll, poll #1 through the end - flat from the start, not a
	// plateau reached after partial growth. Cross-referencing against
	// per-section counts from a same-window serial run confirmed the MAIN
	// content poll's numbers matched serial exactly, section for section;
	// only the post-click expansion differed. The trigger: every lost
	// section's post-click content-settle wait (waitForInteractiveLinks)
	// had hit the recoverable "execution context was destroyed" condition
	// and retried - and this correlation was perfect across all observed
	// data (12/12 "show all" clicks across 3 live concurrent runs: every
	// section whose post-click wait retried lost files; every section whose
	// post-click wait did not retry kept its full count). Mechanism: unlike
	// a Goto (a real navigation, whose destroyed-context event is expected
	// and whose response already contains the complete - already expanded,
	// for the direct-navigate branch - content), a click's AJAX response is
	// tied to the execution context that existed when the click fired; if
	// OPAL/Wicket tears that context down before the response is applied
	// (observed under concurrent-tab rendering contention, with the URL
	// never changing), the response has nowhere left to land and the
	// expansion is silently dropped for good - no amount of further polling
	// can recover content that was never going to arrive, which is exactly
	// why sectionContentMaxPolls/showAllExpansionMaxPolls never showing
	// exhaustion in PR #84's data didn't rule this out. Fixed by having
	// expandShowAllInSection (crawl.go) re-issue the "show all" click
	// whenever this specific signal fires right after its own click (not
	// after the direct-navigate branch, which doesn't need it) - see
	// waitForInteractiveLinks's updated doc comment (navigation.go) and
	// expandShowAllInSection's re-click branch for the full mechanism.
	// Live-verified clean (byte-for-byte 339/339, matching a same-day serial
	// baseline) across 4 consecutive full-account runs at
	// course_concurrency=2 and a further 2 consecutive runs at
	// course_concurrency=3 - one of the concurrency=2 runs directly caught
	// the fix engaging: the same two sections that had lost files in every
	// prior unfixed run hit the retry signal, got re-clicked, and came back
	// with their full correct counts, not just an absence of contention
	// that run.
	//
	// WALL-CLOCK CAVEAT (2026-07-19, when this was raised to 2) - this
	// account's course mix (one ~200-file course that alone takes ~7
	// minutes, dwarfing the other five combined) made concurrency=2/3 the
	// same wall-clock or *slower* than course_concurrency=1 in the
	// correctness-verification runs (516-551s at concurrency=2, ~500s at
	// concurrency=3, versus 312s serial) - the wider MutationObserver/
	// stability-poll budgets applied to every section at concurrency>1 (see
	// contentSettleWaitBudget/requiredStableReads) are a fixed per-section
	// tax paid whether or not that section was ever actually contended.
	// Raised anyway because this constant's own stated bar was correctness
	// ("verified byte-for-byte safe"), not speed, and the race that kept it
	// at 1 is now closed - but the PR explicitly flagged the speed question
	// as unresolved: "worth a second look if it doesn't hold for other
	// course-size distributions", since the one-dominant-course structure
	// of this account could plausibly be masking a real parallelism win
	// that a more evenly-sized course load would show.
	//
	// REVERTED TO 1 (2026-07-20, queue task
	// investigate-course-concurrency-wallclock-benefit): that follow-up
	// found the speed loss is NOT specific to the one-dominant-course
	// structure - it holds, actually more starkly, on an evenly-sized
	// course load too, so the default is back to 1.
	//
	// Full-account live crawls are expensive to repeat (~5-8 minutes each),
	// so rather than more full-account runs, this task isolated the
	// concurrency effect from the one-dominant-course effect directly: the
	// real TU Dresden account's one ~200-file course (Softwaretechnologie)
	// was excluded, and the remaining 5 courses - 39/36/30/17/14 files, a
	// genuinely even spread with no course more than ~2.8x any other, a
	// plausible stand-in for a typical semester course load without one
	// outlier - were crawled with a small throwaway harness
	// (scraper.OpalScraper.ScrapeWithSavedSession with an explicit course
	// filter; `list` itself always crawls every discovered course
	// regardless of config.yaml's courses filter, so it couldn't be used
	// directly for this) at course_concurrency 1, 2, and 3, two repeated
	// runs each of 1 and 2:
	//
	//   - concurrency=1 (serial): 126.7s and 127.0s (136/136 files) -
	//     tightly reproducible.
	//   - concurrency=2: 190.95s and 187.16s (136/136 files) - not faster,
	//     ~49% *slower* than serial, consistently across both runs.
	//   - concurrency=3: 120.3s (136/136 files) - roughly on par with
	//     serial (~5% faster, within the noise band the repeated serial
	//     runs already show), not the ~2-3x speedup naive parallelism
	//     would suggest for 5 courses over 3 workers.
	//
	// Correctness held in every run (136/136 byte-for-byte, matching the
	// serial ground truth) - this is purely a speed finding, PR #94's
	// correctness fix is not in question.
	//
	// Root cause of the non-benefit: every individual course's own crawl
	// time roughly 2.3-2.7x'd under concurrency>1 versus its own serial
	// time (e.g. "2026 LA20" 34.3s serial -> ~89s concurrent; "Analysis"
	// 31.6s -> ~79-80s; "So26 Programmieren..." 34.5s -> ~93-97s;
	// "TUDMATH..." 13.6s -> ~33-35s; "Algorithmen und Datenstrukturen"
	// 6.9s -> ~16-18s) - a consistent multiplier across courses of very
	// different sizes, not a large-course-specific effect. This points at
	// the fixed per-section concurrency>1 tax (requiredStableReads/
	// contentSettleWaitBudget, applied unconditionally whenever
	// effectiveCourseConcurrency() > 1, regardless of whether a given
	// section was actually contended) plus genuine rendering contention
	// from multiple tabs sharing one Chromium process, together outweighing
	// the parallelism gain whenever the course count is the kind of small
	// (5-8 non-trivial courses) a real student's semester load looks like -
	// there simply isn't enough concurrent work to amortize that fixed tax
	// against, with or without one course dominating.
	//
	// RESOLVED 2026-07-21 (queue task fix-concurrency-global-patience-tax):
	// the suspicion recorded above - that the fixed per-section concurrency>1
	// tax, not real contention, was the cost - is confirmed, and the tax is
	// gone. The default is 2.
	//
	// Attribution, measured on the real account before changing anything:
	// at concurrency 2 the section-content stability poll averaged 1.922s
	// per section against 451ms serial (4.26x, exactly the old global
	// 1->4 requiredStableReads multiplier), and the settle wait 553ms
	// against 343ms. Over 284 sections split across 2 workers that is
	// +4m00s of pure waiting, against a total slowdown of +4m12s - i.e.
	// essentially ALL of it. Genuine rendering contention was not a
	// measurable factor.
	//
	// The fix replaced the global gate with per-section evidence: a page
	// whose MutationObserver reported a full debounce window with zero
	// mutations opens the poll impatient, anything still mutating opens on
	// the full streak, and observed growth escalates either way (see
	// waitForStableSectionContent in internal/scraper/crawl.go and
	// candidateStabilityPoll in navigation.go). The show-all expansion poll
	// deliberately keeps the old concurrency gate - it runs ~6 times per
	// full account against ~284 section visits, so its patience is free,
	// and a shorter streak there is live-confirmed to lose files.
	//
	// Result on the real account, full-account discovery runs:
	//   concurrency 1   5m00.7s   342 files
	//   concurrency 2   9m13.3s   342 files  (BEFORE the fix)
	//   concurrency 2   4m27.3s / 4m23.3s    342 files  (after, two runs)
	//   concurrency 3   4m28.3s   342 files
	//   concurrency 4   4m07.7s   333 files  <- LOSES 9 FILES
	//
	// Concurrency 2 is ~12% faster than serial instead of 84% slower, and
	// every run at 1-3 was byte-for-byte identical to the serial ground
	// truth.
	//
	// The default is 2 and should stay there. 3 buys nothing measurable
	// (4m28s vs 4m25s is inside run-to-run noise on a 6-course account -
	// the parallelism is already saturated), while 4 is the classic
	// faster-but-lossy failure: 9 files missing from the largest course,
	// which is a no, not a tradeoff.
	//
	// IMPORTANT, and a correction to earlier notes claiming the concurrent
	// crawl race was fully closed by PR #94: it is not. It is merely not
	// triggered at 2-3. The 2026-07-21 sweep reproduced real file loss at
	// concurrency 4 on current code. Treat "correctness-safe" as meaning
	// "verified at these levels", never as a general property.
	//
	// BACK TO 1 (2026-07-26). Re-measured against the real account, and both
	// halves of the case for 2 failed:
	//
	//   course=1 (serial)  227.9s   345 files
	//   course=2           228.2s   336 files  <- Ubungsblatter 29 -> 20
	//   course=2           230.4s   345 files
	//   course=2           219.6s   345 files
	//   course=2           229.4s   345 files
	//
	//  1. It is no longer faster. Mean 226.9s against a serial 227.9s - inside
	//     noise, where 2026-07-21 measured a 12% gain. The likely reason is
	//     that PR #105's patience fix sped the *serial* path up much more
	//     (5m00s -> 3m48s), leaving concurrency nothing left to win on a
	//     6-course account.
	//  2. It still loses files: one run in five, silently, reporting success.
	//
	// The mechanism is no longer a mystery either. Diffing the visit log put
	// all nine lost files in a single section - "Ubungsblatter", 29 rows
	// collapsing to exactly 20, one OPAL page - i.e. a "show all" expansion
	// that did not happen under concurrent load. expandShowAllInSection now
	// warns when that occurs (see warnShowAllTruncated) instead of returning
	// a truncated section in silence.
	//
	// So 2 costs an intermittent 9 files and buys nothing measurable. Raising
	// it again needs a fresh byte-for-byte parity sweep, not this history:
	// these numbers are a property of one account's course mix and of whatever
	// the serial path currently costs, and both have moved before.
	DefaultCourseConcurrency = 1

	// DefaultSectionConcurrency is how many of a single course's sections are
	// visited at once, *within* that course's BFS crawl.
	//
	// Course-level concurrency cannot help the case that dominates this
	// account: one course (Softwaretechnologie) holds 160 of 284 sections and
	// is 2m48s of a 5m22s discovery phase, and it is a single course, so no
	// amount of course parallelism touches it. Sections within a BFS level are
	// independent and already all queued before any of them is visited, which
	// makes them the one axis the sync-speed campaign never tried.
	//
	// 1 means "off": the crawl behaves exactly as it did before section
	// concurrency existed, on the caller's single tab. Anything above 1 opens
	// additional tabs.
	//
	// STAYS AT 1 — OFF. Measured against the real account on 2026-07-26, and
	// the result was not close:
	//
	//   course=1 section=1 (ground truth):  345 files, 227.9s
	//   course=1 section=4:                 214 files, 110.7s
	//
	// Twice as fast and **131 files gone (38%)**, including one entire course
	// (Analysis, 30 files) that vanished from the results altogether. That is
	// the textbook faster-but-lossy failure this file's course-concurrency note
	// describes, an order of magnitude worse.
	//
	// The machinery is kept, and `--section-concurrency` can still be set
	// explicitly, because the axis is worth re-testing if the underlying cause
	// is ever addressed - losing a whole course points at something structural
	// (OPAL/Wicket is stateful per session, and several tabs walking the *same*
	// course tree at once is a case course-level concurrency never created),
	// not at a wait being slightly too short. But it is off by default and
	// should stay off until someone has a byte-for-byte clean result to show.
	//
	// See docs/sync-speed-campaign.md for the full run log.
	DefaultSectionConcurrency = 1
)

// DefaultSkipEnrollmentSections is the default for App.SkipEnrollmentSections
// when config.yaml doesn't set skip_enrollment_sections explicitly.
//
// CONFIRMED LIVE 2026-07-13 (queue task
// skip-non-file-sections-by-structural-marker) against this account's real
// 8 enrolled TU Dresden OPAL courses: OPAL's OLAT-based course-tree sidebar
// renders every course-node link with a "node-<type>" CSS class using
// OLAT's internal, fixed course-element type code (e.g. "node-bc" for a
// folder, "node-st" for a structure/subfolder, "node-en" for the
// "Enrollment" building block). Dumping every such link across all 8 real
// courses found "node-en" on 10 distinct nodes across 7 of the 8 courses,
// and every single one of their visible labels was an
// enrollment/sign-up-flavored phrase ("Einschreibung", "Einschreibung in
// den Kurs", "Ãœbungseinschreibung", "Einschreibung in die Ãœbungsgruppen",
// ...) - zero cross-contamination with any real content-bearing node type.
// "en" is OLAT's course-element type code for a student
// self-registration/tutorial-group-signup widget, which structurally can
// never render a downloadable file (unlike a folder/structure node, which
// can). This is a DOM class read directly off the element, not derived
// from its title text or from crawl history - see
// scraper.isNonFileSectionType's doc comment for the full node-type
// breakdown and why only "node-en" is included (other node-<type> classes
// seen in the same dump look like plausible further candidates but were
// not live-confirmed file-incapable in this pass).
//
// Defaults to true (skip) given the strength of that live confirmation,
// but --no-skip-enrollment-sections (CLI) or skip_enrollment_sections:
// false (config.yaml) is kept as an easy escape hatch, per this project's
// history of structural-skip assumptions turning out wrong in production
// (PR #36's maxPages 16->500 silent-content-loss fix; the rejected
// history-based skip idea in research-structure-cache-and-priority-crawl.md).
const DefaultSkipEnrollmentSections = true

type Credentials struct {
	URL       string
	StateFile string
}

type App struct {
	DownloadPath          string
	Courses               []string
	Sync                  bool
	DefaultCourseFolder   string
	CourseFolders         map[string]string
	UseSectionSubfolders  bool
	SectionFolderNames    map[string]string
	SubfolderDestinations map[string]string

	// DownloadConcurrency is the maximum number of files downloaded
	// concurrently via the fast HTTP path during sync. The browser-fallback
	// download path is always serialized regardless of this value. Defaults
	// to DefaultDownloadConcurrency when unset/non-positive.
	DownloadConcurrency int

	// CourseConcurrency is the maximum number of courses crawled
	// concurrently during discovery, each on its own browser tab/page
	// sharing the authenticated browser context. Defaults to
	// DefaultCourseConcurrency when unset/non-positive.
	CourseConcurrency int

	// SectionConcurrency is how many sections of a single course are visited
	// at once within that course's crawl. Falls back to
	// DefaultSectionConcurrency when unset/non-positive; 1 disables it.
	SectionConcurrency int

	// SkipEnrollmentSections controls whether the crawler skips queueing
	// OPAL "Einschreibung" (enrollment/sign-up, e.g. "Einschreibung in die
	// Ãœbungsgruppen") course-node sections for a page visit, based on the
	// structural "node-en" CSS class OPAL's course-tree sidebar renders for
	// that node type (see scraper.isNonFileSectionType) - not on title text
	// or crawl history. Defaults to true (skip) when unset in config.yaml;
	// see DefaultSkipEnrollmentSections's doc comment for the live
	// investigation that justified the default and
	// --no-skip-enrollment-sections for the escape hatch.
	SkipEnrollmentSections bool

	// NotifyOnScheduledFailure controls whether `sync --scheduled` shows a
	// native Windows toast notification when a run's outcome is "failure"
	// (never "partial" or "success" - see internal/statuslog.Outcome and
	// cmd/opal-downloader's buildScheduledRunStatus). Deliberately separate
	// from whether scheduled sync itself is enabled (internal/scheduler,
	// queried live from Task Scheduler rather than stored in config.yaml): a
	// user can want scheduled sync without wanting a desktop notification.
	// Defaults to false (off) when unset - opt-in, same as scheduled sync
	// itself. See internal/notify for the toast implementation.
	NotifyOnScheduledFailure bool
}

type Loaded struct {
	App         App
	Credentials Credentials
}

type rawConfig struct {
	DownloadPath             string            `yaml:"download_path"`
	Courses                  []string          `yaml:"courses"`
	Sync                     *bool             `yaml:"sync"`
	DefaultCourseFolder      string            `yaml:"default_course_folder"`
	CourseFolders            map[string]string `yaml:"course_folders"`
	UseSectionSubfolders     bool              `yaml:"use_section_subfolders"`
	SectionFolderNames       map[string]string `yaml:"section_folder_names"`
	SubfolderDestinations    map[string]string `yaml:"subfolder_destinations"`
	OPALURL                  string            `yaml:"opal_url"`
	SessionStateFile         string            `yaml:"session_state_file"`
	DownloadConcurrency      int               `yaml:"download_concurrency"`
	CourseConcurrency        int               `yaml:"course_concurrency"`
	SectionConcurrency       int               `yaml:"section_concurrency"`
	SkipEnrollmentSections   *bool             `yaml:"skip_enrollment_sections"`
	NotifyOnScheduledFailure bool              `yaml:"notify_on_scheduled_failure"`
}

func LoadCredentials(configPath string) (Credentials, error) {
	var cfg rawConfig
	if err := loadYAML(configPath, &cfg); err != nil {
		return Credentials{}, err
	}

	opalURL := strings.TrimSpace(cfg.OPALURL)
	if opalURL == "" {
		opalURL = DefaultOPALURL
	}
	opalURL = strings.TrimRight(opalURL, "/") + "/"

	stateFile := strings.TrimSpace(cfg.SessionStateFile)
	if stateFile == "" {
		stateFile = DefaultStateFile
	}

	return Credentials{
		URL:       opalURL,
		StateFile: expandHome(stateFile),
	}, nil
}

func Load(configPath string) (Loaded, error) {
	var cfg rawConfig
	if err := loadYAML(configPath, &cfg); err != nil {
		return Loaded{}, err
	}

	credentials, err := LoadCredentials(configPath)
	if err != nil {
		return Loaded{}, err
	}

	downloadPath := strings.TrimSpace(cfg.DownloadPath)
	if downloadPath == "" {
		downloadPath = "./downloads"
	}

	courses := cfg.Courses
	if len(courses) == 0 {
		courses = []string{"*"}
	}

	syncEnabled := true
	if cfg.Sync != nil {
		syncEnabled = *cfg.Sync
	}

	downloadConcurrency := cfg.DownloadConcurrency
	if downloadConcurrency <= 0 {
		downloadConcurrency = DefaultDownloadConcurrency
	}

	courseConcurrency := cfg.CourseConcurrency
	if courseConcurrency <= 0 {
		courseConcurrency = DefaultCourseConcurrency
	}

	sectionConcurrency := cfg.SectionConcurrency
	if sectionConcurrency <= 0 {
		sectionConcurrency = DefaultSectionConcurrency
	}

	skipEnrollmentSections := DefaultSkipEnrollmentSections
	if cfg.SkipEnrollmentSections != nil {
		skipEnrollmentSections = *cfg.SkipEnrollmentSections
	}

	courseFolders := map[string]string{}
	for pattern, folder := range cfg.CourseFolders {
		p := strings.TrimSpace(pattern)
		f := strings.TrimSpace(folder)
		if p == "" || f == "" {
			continue
		}
		courseFolders[p] = f
	}

	sectionFolderNames := map[string]string{}
	for sectionName, mapped := range cfg.SectionFolderNames {
		s := strings.TrimSpace(sectionName)
		m := strings.TrimSpace(mapped)
		if s == "" || m == "" {
			continue
		}
		sectionFolderNames[s] = m
	}

	subfolderDestinations := map[string]string{}
	for key, dest := range cfg.SubfolderDestinations {
		k := strings.TrimSpace(key)
		d := strings.TrimSpace(dest)
		if k == "" || d == "" {
			continue
		}
		subfolderDestinations[k] = expandHome(d)
	}

	return Loaded{
		App: App{
			DownloadPath:             expandHome(downloadPath),
			Courses:                  courses,
			Sync:                     syncEnabled,
			DefaultCourseFolder:      strings.TrimSpace(cfg.DefaultCourseFolder),
			CourseFolders:            courseFolders,
			UseSectionSubfolders:     cfg.UseSectionSubfolders,
			SectionFolderNames:       sectionFolderNames,
			SubfolderDestinations:    subfolderDestinations,
			DownloadConcurrency:      downloadConcurrency,
			CourseConcurrency:        courseConcurrency,
			SectionConcurrency:       sectionConcurrency,
			SkipEnrollmentSections:   skipEnrollmentSections,
			NotifyOnScheduledFailure: cfg.NotifyOnScheduledFailure,
		},
		Credentials: credentials,
	}, nil
}

func ResolveCourseFolder(cfg App, courseName string) (folder string, explicit bool) {
	for pattern, mappedFolder := range cfg.CourseFolders {
		if CourseMatches(courseName, []string{pattern}) {
			return mappedFolder, true
		}
	}

	if strings.TrimSpace(cfg.DefaultCourseFolder) != "" {
		return cfg.DefaultCourseFolder, false
	}

	return SanitizePathComponent(courseName), false
}

// ResolveSectionFolderName maps an OPAL section/folder name to the subfolder
// name that should be used on disk. If sectionName matches a configured
// section_folder_names pattern, the mapped name is returned. Otherwise the
// section name itself is sanitized and returned as-is. Matching uses the same
// CourseMatches-style logic (case-insensitive, diacritic-insensitive,
// substring/glob) as course_folders/course patterns elsewhere in this package.
func ResolveSectionFolderName(cfg App, sectionName string) string {
	for pattern, mapped := range cfg.SectionFolderNames {
		if CourseMatches(sectionName, []string{pattern}) {
			return SanitizePathComponent(mapped)
		}
	}
	return SanitizePathComponent(sectionName)
}

// ResolveSubfolderDestination looks up subfolder_destinations for an override
// destination path matching both courseName and sectionName. Entries are keyed
// as "<course pattern>/<subfolder pattern>"; both halves are matched using the
// same pattern-matching rules as course_folders (CourseMatches). It returns the
// configured destination path and true on a match, or ("", false) otherwise.
func ResolveSubfolderDestination(cfg App, courseName, sectionName string) (destination string, ok bool) {
	for key, dest := range cfg.SubfolderDestinations {
		coursePattern, subfolderPattern, valid := splitSubfolderDestinationKey(key)
		if !valid {
			continue
		}
		if CourseMatches(courseName, []string{coursePattern}) && CourseMatches(sectionName, []string{subfolderPattern}) {
			return dest, true
		}
	}
	return "", false
}

// splitSubfolderDestinationKey splits a subfolder_destinations key of the form
// "<course pattern>/<subfolder pattern>" on the last "/" so that course
// patterns themselves may contain "/" (e.g. nested folder-style names) while
// the subfolder pattern remains a single path component.
func splitSubfolderDestinationKey(key string) (coursePattern, subfolderPattern string, ok bool) {
	idx := strings.LastIndex(key, "/")
	if idx <= 0 || idx >= len(key)-1 {
		return "", "", false
	}
	return strings.TrimSpace(key[:idx]), strings.TrimSpace(key[idx+1:]), true
}

func CourseMatches(name string, patterns []string) bool {
	if len(patterns) == 0 || (len(patterns) == 1 && patterns[0] == "*") {
		return true
	}

	normalizedCourse := normalizeMatchText(name)
	for _, pattern := range patterns {
		if patternMatchesCourse(normalizedCourse, pattern) {
			return true
		}
	}
	return false
}

func SanitizePathComponent(value string) string {
	cleaned := strings.TrimSpace(value)
	re := regexp.MustCompile(`[<>:"/\\|?*\x00-\x1F]`)
	cleaned = re.ReplaceAllString(cleaned, "_")
	spaceRe := regexp.MustCompile(`\s+`)
	cleaned = spaceRe.ReplaceAllString(cleaned, " ")
	cleaned = strings.TrimRight(cleaned, ". ")
	if cleaned == "" {
		return "unnamed"
	}

	upper := strings.ToUpper(cleaned)
	reserved := map[string]struct{}{
		"CON": {}, "PRN": {}, "AUX": {}, "NUL": {},
		"COM1": {}, "COM2": {}, "COM3": {}, "COM4": {}, "COM5": {}, "COM6": {}, "COM7": {}, "COM8": {}, "COM9": {},
		"LPT1": {}, "LPT2": {}, "LPT3": {}, "LPT4": {}, "LPT5": {}, "LPT6": {}, "LPT7": {}, "LPT8": {}, "LPT9": {},
	}
	if _, ok := reserved[upper]; ok {
		return "_" + cleaned
	}

	return cleaned
}

// Save validates cfg and writes it to path in the config.yaml on-disk format.
// If a file already exists at path, it is copied to path+".bak" before being
// overwritten. Save does not preserve comments or formatting from any
// existing file - it always performs a plain struct marshal.
func Save(path string, cfg Loaded) error {
	if err := Validate(cfg); err != nil {
		return err
	}

	raw := toRawConfig(cfg)

	data, err := yaml.Marshal(&raw)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := backupExisting(path); err != nil {
		return err
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("failed to write config to %s: %w", path, err)
	}

	return nil
}

// Validate performs minimal sanity checks on cfg before it is persisted.
func Validate(cfg Loaded) error {
	if strings.TrimSpace(cfg.App.DownloadPath) == "" {
		return errors.New("download_path must not be empty")
	}
	if strings.TrimSpace(cfg.Credentials.URL) == "" {
		return errors.New("opal_url must not be empty")
	}
	for pattern, folder := range cfg.App.CourseFolders {
		if strings.TrimSpace(pattern) == "" {
			return errors.New("course_folders contains an empty pattern")
		}
		if strings.TrimSpace(folder) == "" {
			return fmt.Errorf("course_folders[%q] must not be empty", pattern)
		}
		if _, err := filepath.Match(pattern, ""); err != nil {
			return fmt.Errorf("course_folders[%q] is not a valid glob pattern: %w", pattern, err)
		}
	}
	for _, pattern := range cfg.App.Courses {
		trimmed := strings.TrimSpace(pattern)
		if trimmed == "" {
			return errors.New("courses contains an empty pattern")
		}
		if _, err := filepath.Match(trimmed, ""); err != nil {
			return fmt.Errorf("courses[%q] is not a valid glob pattern: %w", trimmed, err)
		}
	}
	for sectionName, mapped := range cfg.App.SectionFolderNames {
		if strings.TrimSpace(sectionName) == "" {
			return errors.New("section_folder_names contains an empty pattern")
		}
		if strings.TrimSpace(mapped) == "" {
			return fmt.Errorf("section_folder_names[%q] must not be empty", sectionName)
		}
	}
	for key, dest := range cfg.App.SubfolderDestinations {
		if _, _, valid := splitSubfolderDestinationKey(key); !valid {
			return fmt.Errorf("subfolder_destinations key %q must be in the form \"<course pattern>/<subfolder pattern>\"", key)
		}
		if strings.TrimSpace(dest) == "" {
			return fmt.Errorf("subfolder_destinations[%q] must not be empty", key)
		}
	}
	return nil
}

// Warnings returns non-fatal configuration warnings for cfg. Unlike Validate,
// these never block Load/Save - they flag settings that parse fine but
// silently do nothing at sync time, so a user notices the misconfiguration
// instead of wondering why section_folder_names/subfolder_destinations had
// no effect. Callers (CLI on config load, GUI Settings page) are expected to
// surface these to the user; config.Load itself does not print anything.
func Warnings(cfg App) []string {
	var warnings []string
	if !cfg.UseSectionSubfolders {
		if len(cfg.SectionFolderNames) > 0 {
			warnings = append(warnings, "section_folder_names is set but use_section_subfolders is false, so it has no effect. Set use_section_subfolders: true to apply these subfolder name overrides.")
		}
		if len(cfg.SubfolderDestinations) > 0 {
			warnings = append(warnings, "subfolder_destinations is set but use_section_subfolders is false, so it has no effect. Set use_section_subfolders: true to apply these destination overrides.")
		}
	}
	return warnings
}

// toRawConfig converts the normalized in-memory config shape back into the
// on-disk rawConfig shape used for YAML marshaling.
func toRawConfig(cfg Loaded) rawConfig {
	sync := cfg.App.Sync
	skipEnrollmentSections := cfg.App.SkipEnrollmentSections
	return rawConfig{
		DownloadPath:             cfg.App.DownloadPath,
		Courses:                  cfg.App.Courses,
		Sync:                     &sync,
		DefaultCourseFolder:      cfg.App.DefaultCourseFolder,
		CourseFolders:            cfg.App.CourseFolders,
		UseSectionSubfolders:     cfg.App.UseSectionSubfolders,
		SectionFolderNames:       cfg.App.SectionFolderNames,
		SubfolderDestinations:    cfg.App.SubfolderDestinations,
		OPALURL:                  cfg.Credentials.URL,
		SessionStateFile:         cfg.Credentials.StateFile,
		DownloadConcurrency:      cfg.App.DownloadConcurrency,
		CourseConcurrency:        cfg.App.CourseConcurrency,
		SectionConcurrency:       cfg.App.SectionConcurrency,
		SkipEnrollmentSections:   &skipEnrollmentSections,
		NotifyOnScheduledFailure: cfg.App.NotifyOnScheduledFailure,
	}
}

// backupExisting copies the file at path to path+".bak" if path exists.
// If path does not exist, this is a no-op.
func backupExisting(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("failed to read existing config for backup: %w", err)
	}

	backupPath := path + ".bak"
	if err := os.WriteFile(backupPath, data, 0o644); err != nil {
		return fmt.Errorf("failed to write backup to %s: %w", backupPath, err)
	}

	return nil
}

func loadYAML(path string, out any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("config file not found: %s", path)
		}
		return err
	}
	if err := yaml.Unmarshal(data, out); err != nil {
		return fmt.Errorf("invalid yaml in %s: %w", path, err)
	}
	return nil
}

func patternMatchesCourse(normalizedCourse, rawPattern string) bool {
	normalizedPattern := normalizeMatchText(rawPattern)
	if normalizedPattern == "" {
		return false
	}

	hasGlob := strings.ContainsAny(normalizedPattern, "*?[")
	if hasGlob {
		matched, err := filepath.Match(normalizedPattern, normalizedCourse)
		return err == nil && matched
	}

	return strings.Contains(normalizedCourse, normalizedPattern)
}

func normalizeMatchText(value string) string {
	lowered := strings.ToLower(strings.TrimSpace(value))
	decomposed := norm.NFD.String(lowered)
	var b strings.Builder
	prevSpace := false
	for _, r := range decomposed {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		if unicode.IsSpace(r) {
			if !prevSpace {
				b.WriteRune(' ')
			}
			prevSpace = true
			continue
		}
		prevSpace = false
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

func expandHome(path string) string {
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, "~\\") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	if path == "~" {
		home, err := os.UserHomeDir()
		if err == nil {
			return home
		}
	}
	return path
}
