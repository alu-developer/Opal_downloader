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
	// concurrently during sync. Kept conservative (not "max parallelism") to
	// avoid tripping OPAL's rate-limiting/bot-detection.
	DefaultDownloadConcurrency = 3
)

type Credentials struct {
	URL                string
	StateFile          string
	BrowserExecutable  string
	BrowserUserDataDir string
	BrowserProfileDir  string
}

type App struct {
	DownloadPath        string
	Courses             []string
	Sync                bool
	DefaultCourseFolder string
	CourseFolders       map[string]string

	// DownloadConcurrency is the maximum number of files downloaded
	// concurrently via the fast HTTP path during sync. The browser-fallback
	// download path is always serialized regardless of this value. Defaults
	// to DefaultDownloadConcurrency when unset/non-positive.
	DownloadConcurrency int
}

type Loaded struct {
	App         App
	Credentials Credentials
}

type rawConfig struct {
	DownloadPath        string            `yaml:"download_path"`
	Courses             []string          `yaml:"courses"`
	Sync                *bool             `yaml:"sync"`
	DefaultCourseFolder string            `yaml:"default_course_folder"`
	CourseFolders       map[string]string `yaml:"course_folders"`
	OPALURL             string            `yaml:"opal_url"`
	SessionStateFile    string            `yaml:"session_state_file"`
	BrowserExecutable   string            `yaml:"browser_executable"`
	BrowserUserDataDir  string            `yaml:"browser_user_data_dir"`
	BrowserProfileDir   string            `yaml:"browser_profile_directory"`
	DownloadConcurrency int               `yaml:"download_concurrency"`
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

	courseFolders := map[string]string{}
	for pattern, folder := range cfg.CourseFolders {
		p := strings.TrimSpace(pattern)
		f := strings.TrimSpace(folder)
		if p == "" || f == "" {
			continue
		}
		courseFolders[p] = f
	}

	return Loaded{
		App: App{
			DownloadPath:        expandHome(downloadPath),
			Courses:             courses,
			Sync:                syncEnabled,
			DefaultCourseFolder: strings.TrimSpace(cfg.DefaultCourseFolder),
			CourseFolders:       courseFolders,
			DownloadConcurrency: downloadConcurrency,
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
	}
	return nil
}

// toRawConfig converts the normalized in-memory config shape back into the
// on-disk rawConfig shape used for YAML marshaling.
func toRawConfig(cfg Loaded) rawConfig {
	sync := cfg.App.Sync
	return rawConfig{
		DownloadPath:        cfg.App.DownloadPath,
		Courses:             cfg.App.Courses,
		Sync:                &sync,
		DefaultCourseFolder: cfg.App.DefaultCourseFolder,
		CourseFolders:       cfg.App.CourseFolders,
		OPALURL:             cfg.Credentials.URL,
		SessionStateFile:    cfg.Credentials.StateFile,
		BrowserExecutable:   cfg.Credentials.BrowserExecutable,
		BrowserUserDataDir:  cfg.Credentials.BrowserUserDataDir,
		BrowserProfileDir:   cfg.Credentials.BrowserProfileDir,
		DownloadConcurrency: cfg.App.DownloadConcurrency,
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
