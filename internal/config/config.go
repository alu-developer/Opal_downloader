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
	// Live-tested 2026-07-12 against the real TU Dresden OPAL account
	// (8 courses, 341 real files via a serial course_concurrency=1 ground
	// truth): course_concurrency=3 (the old default) silently returned 0
	// files for 2 whole courses that actually had 38 and 34 files
	// respectively (21% of all files silently lost, no error/warning beyond
	// a generic "found 0 files" log line); course_concurrency=5 lost 76% of
	// files, including a 198-file course dropping to 0 and other courses
	// returning wrong (partial) counts. This is not rate-limiting - it's an
	// AJAX-render race in concurrent course crawling that PR #64 only
	// partially fixed (that PR fixed show-all pagination specifically; this
	// is a broader instance of the same race that can drop a course's
	// content entirely). Only course_concurrency=1 (serial) produced
	// correct, complete file counts across 3 separate runs, so that is now
	// the default. Do not raise this until the underlying race is fixed;
	// raising it trades speed for silently missing files, which is worse
	// than slow.
	DefaultCourseConcurrency = 1
)

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
}

type Loaded struct {
	App         App
	Credentials Credentials
}

type rawConfig struct {
	DownloadPath          string            `yaml:"download_path"`
	Courses               []string          `yaml:"courses"`
	Sync                  *bool             `yaml:"sync"`
	DefaultCourseFolder   string            `yaml:"default_course_folder"`
	CourseFolders         map[string]string `yaml:"course_folders"`
	UseSectionSubfolders  bool              `yaml:"use_section_subfolders"`
	SectionFolderNames    map[string]string `yaml:"section_folder_names"`
	SubfolderDestinations map[string]string `yaml:"subfolder_destinations"`
	OPALURL               string            `yaml:"opal_url"`
	SessionStateFile      string            `yaml:"session_state_file"`
	BrowserExecutable     string            `yaml:"browser_executable"`
	BrowserUserDataDir    string            `yaml:"browser_user_data_dir"`
	BrowserProfileDir     string            `yaml:"browser_profile_directory"`
	DownloadConcurrency   int               `yaml:"download_concurrency"`
	CourseConcurrency     int               `yaml:"course_concurrency"`
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
			DownloadPath:          expandHome(downloadPath),
			Courses:               courses,
			Sync:                  syncEnabled,
			DefaultCourseFolder:   strings.TrimSpace(cfg.DefaultCourseFolder),
			CourseFolders:         courseFolders,
			UseSectionSubfolders:  cfg.UseSectionSubfolders,
			SectionFolderNames:    sectionFolderNames,
			SubfolderDestinations: subfolderDestinations,
			DownloadConcurrency:   downloadConcurrency,
			CourseConcurrency:     courseConcurrency,
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
	return rawConfig{
		DownloadPath:          cfg.App.DownloadPath,
		Courses:               cfg.App.Courses,
		Sync:                  &sync,
		DefaultCourseFolder:   cfg.App.DefaultCourseFolder,
		CourseFolders:         cfg.App.CourseFolders,
		UseSectionSubfolders:  cfg.App.UseSectionSubfolders,
		SectionFolderNames:    cfg.App.SectionFolderNames,
		SubfolderDestinations: cfg.App.SubfolderDestinations,
		OPALURL:               cfg.Credentials.URL,
		SessionStateFile:      cfg.Credentials.StateFile,
		BrowserExecutable:     cfg.Credentials.BrowserExecutable,
		BrowserUserDataDir:    cfg.Credentials.BrowserUserDataDir,
		BrowserProfileDir:     cfg.Credentials.BrowserProfileDir,
		DownloadConcurrency:   cfg.App.DownloadConcurrency,
		CourseConcurrency:     cfg.App.CourseConcurrency,
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
