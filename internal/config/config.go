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
	// all") sections.
	//
	// FOLLOW-UP (queue task
	// investigate-size-aware-course-scheduling-for-concurrency, 2026-07-14):
	// implemented the size/duration-aware scheduler this doc comment
	// previously flagged as worth trying - see
	// internal/scraper/course_scheduling.go. When course_concurrency>1, it
	// keeps a small per-machine cache (~/.opal-course-size-hints.json) of
	// each course's file count from its most recent successful crawl, keyed
	// by RepoID; if one course's cached count is both large in absolute
	// terms (>=60 files) and dominant relative to the next-largest hinted
	// course (>=2x), that course gets a dedicated, non-concurrent crawl
	// pass by itself before the rest run concurrently as before (see
	// selectDominantCourse's doc comment for the exact bar, and
	// splitOutCourse). A course with no history yet (first-ever run) is
	// never treated as dominant, so this only ever helps after an initial
	// run has populated the cache - never requires a separate upfront probe
	// pass, and a stale/missing hint degrades to the pre-existing flat
	// concurrent scheduling with no correctness cost (every course is still
	// crawled in full every run either way).
	//
	// RESULT: live-verified against the real TU Dresden account (8 courses,
	// now 343 real files - grew from the 341 above between this task and
	// the prior one, real account content drift) at course_concurrency 2,
	// 3, and 5, four full-account runs total after fixing a real bug found
	// along the way (an earlier version of the hint-recording code matched
	// courses by raw title; RemoteFile.Course is actually
	// sanitizeFilename(course.Title), so any course whose title contains a
	// character sanitizeFilename rewrites - e.g. the ":" in two of this
	// account's real course titles - silently never got its hint recorded;
	// fixed in courseFileCountsByRepoID, course_scheduling.go). With that
	// fixed:
	//   - The account's dominant course (Softwaretechnologie (SoSe 26), 198
	//     files) was correctly selected for a dedicated pass and came back
	//     byte-for-byte complete (198/198) in all 4 runs (concurrency 2x2,
	//     3x1, 5x1) - the specific "one huge course wrecks everything"
	//     contention pattern this task set out to fix is, as far as this
	//     testing could tell, fully closed.
	//   - The account's full file count was NOT byte-for-byte safe across
	//     those same runs: 2 of the 4 runs (one at concurrency=2, one at
	//     concurrency=5) intermittently lost files - always in the same two
	//     courses (Analysis, Algorithmen und Datenstrukturen), both of
	//     which have paginated "show all" sections and are the exact same
	//     courses named in sectionContentRequiredStableReads's doc comment
	//     (crawl.go) as the ones that already showed this race pre-task.
	//     Since the dedicated pass removes the dominant course from the
	//     "rest" group entirely, this loss happens among the *smaller*
	//     courses contending with each other - a residual instance of the
	//     same pre-existing AJAX-render race documented above, unrelated to
	//     which course is largest, and not something a scheduler scoped to
	//     "isolate the one dominant course" can address. Consistent with
	//     the pre-existing finding above that "course_concurrency=2 showed
	//     the same residual loss as =3": this task's own data shows
	//     concurrency 2 and 5 both hit it while concurrency 3 (one run)
	//     didn't, i.e. not proportional to raw worker count either.
	//
	// STAYS AT 1: the scheduler is a real, unconditionally-safe improvement
	// (only activates when course_concurrency>1 is already explicitly
	// opted into; degrades to the pre-existing behavior with no downside
	// when no dominant course is found) and is shipped anyway since it
	// fully closes one real failure mode with no observed regression across
	// 4 live runs - but the overall "raise the default once verified safe"
	// bar still isn't met, because the *other* residual race (among
	// non-dominant courses) is untouched by this task's scope. A true fix
	// for that would need to revisit per-section wait-tuning again (already
	// shown to have diminishing returns above) or extend size/duration
	// awareness to *all* courses, not just the single most dominant one
	// (e.g. a small pool of "big enough to isolate" courses rather than
	// just one) - worth trying if this is revisited again, but not
	// attempted here given the time this investigation already took and
	// the account's course mix (exactly one course anywhere near dominant
	// today) not exercising that generalization.
	DefaultCourseConcurrency = 1
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
// den Kurs", "Übungseinschreibung", "Einschreibung in die Übungsgruppen",
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
	URL                string
	StateFile          string
	BrowserExecutable  string
	BrowserUserDataDir string
	BrowserProfileDir  string
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

	// SkipEnrollmentSections controls whether the crawler skips queueing
	// OPAL "Einschreibung" (enrollment/sign-up, e.g. "Einschreibung in die
	// Übungsgruppen") course-node sections for a page visit, based on the
	// structural "node-en" CSS class OPAL's course-tree sidebar renders for
	// that node type (see scraper.isNonFileSectionType) - not on title text
	// or crawl history. Defaults to true (skip) when unset in config.yaml;
	// see DefaultSkipEnrollmentSections's doc comment for the live
	// investigation that justified the default and
	// --no-skip-enrollment-sections for the escape hatch.
	SkipEnrollmentSections bool
}

type Loaded struct {
	App         App
	Credentials Credentials
}

type rawConfig struct {
	DownloadPath           string            `yaml:"download_path"`
	Courses                []string          `yaml:"courses"`
	Sync                   *bool             `yaml:"sync"`
	DefaultCourseFolder    string            `yaml:"default_course_folder"`
	CourseFolders          map[string]string `yaml:"course_folders"`
	UseSectionSubfolders   bool              `yaml:"use_section_subfolders"`
	SectionFolderNames     map[string]string `yaml:"section_folder_names"`
	SubfolderDestinations  map[string]string `yaml:"subfolder_destinations"`
	OPALURL                string            `yaml:"opal_url"`
	SessionStateFile       string            `yaml:"session_state_file"`
	BrowserExecutable      string            `yaml:"browser_executable"`
	BrowserUserDataDir     string            `yaml:"browser_user_data_dir"`
	BrowserProfileDir      string            `yaml:"browser_profile_directory"`
	DownloadConcurrency    int               `yaml:"download_concurrency"`
	CourseConcurrency      int               `yaml:"course_concurrency"`
	SkipEnrollmentSections *bool             `yaml:"skip_enrollment_sections"`
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
		URL:                opalURL,
		StateFile:          expandHome(stateFile),
		BrowserExecutable:  expandHome(strings.TrimSpace(cfg.BrowserExecutable)),
		BrowserUserDataDir: expandHome(strings.TrimSpace(cfg.BrowserUserDataDir)),
		BrowserProfileDir:  strings.TrimSpace(cfg.BrowserProfileDir),
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
			DownloadPath:           expandHome(downloadPath),
			Courses:                courses,
			Sync:                   syncEnabled,
			DefaultCourseFolder:    strings.TrimSpace(cfg.DefaultCourseFolder),
			CourseFolders:          courseFolders,
			UseSectionSubfolders:   cfg.UseSectionSubfolders,
			SectionFolderNames:     sectionFolderNames,
			SubfolderDestinations:  subfolderDestinations,
			DownloadConcurrency:    downloadConcurrency,
			CourseConcurrency:      courseConcurrency,
			SkipEnrollmentSections: skipEnrollmentSections,
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
		DownloadPath:           cfg.App.DownloadPath,
		Courses:                cfg.App.Courses,
		Sync:                   &sync,
		DefaultCourseFolder:    cfg.App.DefaultCourseFolder,
		CourseFolders:          cfg.App.CourseFolders,
		UseSectionSubfolders:   cfg.App.UseSectionSubfolders,
		SectionFolderNames:     cfg.App.SectionFolderNames,
		SubfolderDestinations:  cfg.App.SubfolderDestinations,
		OPALURL:                cfg.Credentials.URL,
		SessionStateFile:       cfg.Credentials.StateFile,
		BrowserExecutable:      cfg.Credentials.BrowserExecutable,
		BrowserUserDataDir:     cfg.Credentials.BrowserUserDataDir,
		BrowserProfileDir:      cfg.Credentials.BrowserProfileDir,
		DownloadConcurrency:    cfg.App.DownloadConcurrency,
		CourseConcurrency:      cfg.App.CourseConcurrency,
		SkipEnrollmentSections: &skipEnrollmentSections,
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
