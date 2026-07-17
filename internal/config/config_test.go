package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCourseMatches(t *testing.T) {
	tests := []struct {
		name     string
		course   string
		patterns []string
		want     bool
	}{
		{name: "match all", course: "Anything", patterns: []string{"*"}, want: true},
		{name: "glob match", course: "Lineare Algebra 2", patterns: []string{"*Algebra*"}, want: true},
		{name: "case insensitive", course: "Analysis", patterns: []string{"analysis"}, want: true},
		{name: "diacritics normalized", course: "Übungsblätter", patterns: []string{"ubungsblatter"}, want: true},
		{name: "no match", course: "Programmierung", patterns: []string{"*Analysis*"}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CourseMatches(tt.course, tt.patterns)
			if got != tt.want {
				t.Fatalf("CourseMatches(%q, %v) = %v, want %v", tt.course, tt.patterns, got, tt.want)
			}
		})
	}
}

func TestResolveCourseFolder(t *testing.T) {
	cfgExplicit := App{
		DefaultCourseFolder: "default",
		CourseFolders: map[string]string{
			"*Analysis*": "Math/Analysis",
		},
	}
	folder, explicit := ResolveCourseFolder(cfgExplicit, "Analysis I")
	if folder != "Math/Analysis" || !explicit {
		t.Fatalf("explicit rule failed: got (%q, %v)", folder, explicit)
	}

	cfgDefault := App{DefaultCourseFolder: "default"}
	folder, explicit = ResolveCourseFolder(cfgDefault, "Random Course")
	if folder != "default" || explicit {
		t.Fatalf("default rule failed: got (%q, %v)", folder, explicit)
	}

	cfgFallback := App{}
	folder, explicit = ResolveCourseFolder(cfgFallback, "COM1")
	if folder != "_COM1" || explicit {
		t.Fatalf("fallback sanitize failed: got (%q, %v)", folder, explicit)
	}
}

func TestResolveSectionFolderNameDefaultsToSanitizedSectionName(t *testing.T) {
	cfg := App{}
	if got := ResolveSectionFolderName(cfg, "Übungen"); got != "Übungen" {
		t.Fatalf("expected sanitized section name unchanged, got %q", got)
	}
}

func TestResolveSectionFolderNameAppliesMapping(t *testing.T) {
	cfg := App{
		SectionFolderNames: map[string]string{
			"Exercises": "Übungen",
		},
	}
	if got := ResolveSectionFolderName(cfg, "Exercises"); got != "Übungen" {
		t.Fatalf("expected mapped section name, got %q", got)
	}
	// Unmapped sections fall back to their own (sanitized) name.
	if got := ResolveSectionFolderName(cfg, "Vorlesung"); got != "Vorlesung" {
		t.Fatalf("expected unmapped section name unchanged, got %q", got)
	}
}

func TestResolveSubfolderDestinationMatch(t *testing.T) {
	cfg := App{
		SubfolderDestinations: map[string]string{
			"*Analysis*/*Vorlesung*": "D:/Elsewhere/AnalysisSlides",
		},
	}
	dest, ok := ResolveSubfolderDestination(cfg, "Analysis I", "Vorlesung")
	if !ok || dest != "D:/Elsewhere/AnalysisSlides" {
		t.Fatalf("expected override destination match, got (%q, %v)", dest, ok)
	}

	_, ok = ResolveSubfolderDestination(cfg, "Analysis I", "Uebungen")
	if ok {
		t.Fatal("expected no match for non-matching subfolder pattern")
	}

	_, ok = ResolveSubfolderDestination(cfg, "Programmierung", "Vorlesung")
	if ok {
		t.Fatal("expected no match for non-matching course pattern")
	}
}

func TestLoadDefaultsUnchangedWithoutSubfolderConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	content := `download_path: "./downloads"
opal_url: "https://bildungsportal.sachsen.de/opal/"
`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	loaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if loaded.App.UseSectionSubfolders {
		t.Fatal("expected UseSectionSubfolders to default to false")
	}
	if len(loaded.App.SectionFolderNames) != 0 {
		t.Fatalf("expected empty SectionFolderNames, got %v", loaded.App.SectionFolderNames)
	}
	if len(loaded.App.SubfolderDestinations) != 0 {
		t.Fatalf("expected empty SubfolderDestinations, got %v", loaded.App.SubfolderDestinations)
	}
	if !loaded.App.SkipEnrollmentSections {
		t.Fatal("expected SkipEnrollmentSections to default to true when unset in config.yaml")
	}
}

func TestLoadSkipEnrollmentSectionsExplicitFalse(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	content := `download_path: "./downloads"
opal_url: "https://bildungsportal.sachsen.de/opal/"
skip_enrollment_sections: false
`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	loaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.App.SkipEnrollmentSections {
		t.Fatal("expected SkipEnrollmentSections to be false when explicitly set to false in config.yaml")
	}
}

func TestLoadNotifyOnScheduledFailureDefaultsFalse(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	content := `download_path: "./downloads"
opal_url: "https://bildungsportal.sachsen.de/opal/"
`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	loaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.App.NotifyOnScheduledFailure {
		t.Fatal("expected NotifyOnScheduledFailure to default to false when unset in config.yaml")
	}
}

func TestLoadNotifyOnScheduledFailureExplicitTrue(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	content := `download_path: "./downloads"
opal_url: "https://bildungsportal.sachsen.de/opal/"
notify_on_scheduled_failure: true
`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	loaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !loaded.App.NotifyOnScheduledFailure {
		t.Fatal("expected NotifyOnScheduledFailure to be true when explicitly set in config.yaml")
	}
}

func TestSaveRoundTripsNotifyOnScheduledFailure(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	cfg := Loaded{
		App: App{
			DownloadPath:             "./downloads",
			Courses:                  []string{"*"},
			NotifyOnScheduledFailure: true,
		},
		Credentials: Credentials{URL: DefaultOPALURL, StateFile: DefaultStateFile},
	}
	if err := Save(configPath, cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !loaded.App.NotifyOnScheduledFailure {
		t.Fatal("expected NotifyOnScheduledFailure=true to round-trip through Save/Load")
	}
}

func TestLoadParsesSubfolderConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	content := `download_path: "./downloads"
opal_url: "https://bildungsportal.sachsen.de/opal/"
use_section_subfolders: true
section_folder_names:
  "Exercises": "Uebungen"
subfolder_destinations:
  "*Analysis*/*Vorlesung*": "D:/Elsewhere/AnalysisSlides"
`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	loaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !loaded.App.UseSectionSubfolders {
		t.Fatal("expected UseSectionSubfolders to be true")
	}
	if loaded.App.SectionFolderNames["Exercises"] != "Uebungen" {
		t.Fatalf("expected section_folder_names to be parsed, got %v", loaded.App.SectionFolderNames)
	}
	if loaded.App.SubfolderDestinations["*Analysis*/*Vorlesung*"] != "D:/Elsewhere/AnalysisSlides" {
		t.Fatalf("expected subfolder_destinations to be parsed, got %v", loaded.App.SubfolderDestinations)
	}
}

func TestValidateRejectsMalformedSubfolderDestinationKey(t *testing.T) {
	loaded := Loaded{
		App: App{
			DownloadPath:          "./downloads",
			SubfolderDestinations: map[string]string{"NoSlashHere": "D:/dest"},
		},
		Credentials: Credentials{URL: DefaultOPALURL},
	}
	if err := Validate(loaded); err == nil {
		t.Fatal("expected error for malformed subfolder_destinations key, got nil")
	}
}

func TestWarningsFlagsSectionFolderNamesWithoutUseSectionSubfolders(t *testing.T) {
	cfg := App{
		SectionFolderNames: map[string]string{"Exercises": "Uebungen"},
	}
	warnings := Warnings(cfg)
	if len(warnings) != 1 {
		t.Fatalf("expected exactly one warning, got %v", warnings)
	}
	if !strings.Contains(warnings[0], "section_folder_names") || !strings.Contains(warnings[0], "use_section_subfolders") {
		t.Fatalf("expected warning to mention section_folder_names and use_section_subfolders, got %q", warnings[0])
	}
}

func TestWarningsFlagsSubfolderDestinationsWithoutUseSectionSubfolders(t *testing.T) {
	cfg := App{
		SubfolderDestinations: map[string]string{"*Analysis*/*Vorlesung*": "D:/Elsewhere"},
	}
	warnings := Warnings(cfg)
	if len(warnings) != 1 {
		t.Fatalf("expected exactly one warning, got %v", warnings)
	}
	if !strings.Contains(warnings[0], "subfolder_destinations") || !strings.Contains(warnings[0], "use_section_subfolders") {
		t.Fatalf("expected warning to mention subfolder_destinations and use_section_subfolders, got %q", warnings[0])
	}
}

func TestWarningsFlagsBothFieldsWithoutUseSectionSubfolders(t *testing.T) {
	cfg := App{
		SectionFolderNames:    map[string]string{"Exercises": "Uebungen"},
		SubfolderDestinations: map[string]string{"*Analysis*/*Vorlesung*": "D:/Elsewhere"},
	}
	warnings := Warnings(cfg)
	if len(warnings) != 2 {
		t.Fatalf("expected exactly two warnings, got %v", warnings)
	}
}

func TestWarningsEmptyWhenUseSectionSubfoldersTrue(t *testing.T) {
	cfg := App{
		UseSectionSubfolders:  true,
		SectionFolderNames:    map[string]string{"Exercises": "Uebungen"},
		SubfolderDestinations: map[string]string{"*Analysis*/*Vorlesung*": "D:/Elsewhere"},
	}
	if warnings := Warnings(cfg); len(warnings) != 0 {
		t.Fatalf("expected no warnings when use_section_subfolders is true, got %v", warnings)
	}
}

func TestWarningsEmptyWhenNothingConfigured(t *testing.T) {
	if warnings := Warnings(App{}); len(warnings) != 0 {
		t.Fatalf("expected no warnings for zero-value App, got %v", warnings)
	}
}

func TestSanitizePathComponent(t *testing.T) {
	if got := SanitizePathComponent("  folder<>name  "); got != "folder__name" {
		t.Fatalf("sanitize invalid chars failed: got %q", got)
	}
	if got := SanitizePathComponent("   "); got != "unnamed" {
		t.Fatalf("sanitize empty failed: got %q", got)
	}
	if got := SanitizePathComponent("LPT1"); got != "_LPT1" {
		t.Fatalf("sanitize reserved failed: got %q", got)
	}
}

// TestLoadCredentialsIgnoresRemovedBrowserFields is a regression test for the
// chromium-only-login task: config.yaml no longer has a Credentials-level
// concept of browser_executable/browser_user_data_dir/browser_profile_directory
// at all (opal-downloader always launches Playwright's bundled Chromium
// against the single hardcoded ~/.opal-downloader/login-profile - see
// scraper.LoginProfileDir). A config.yaml still carrying these keys from
// before this change (e.g. the maintainer's own local file) must load
// without error, with the unknown keys silently ignored by the YAML parser
// rather than erroring or resurrecting removed fields.
func TestLoadCredentialsIgnoresRemovedBrowserFields(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	content := `opal_url: "https://bildungsportal.sachsen.de/opal/"
session_state_file: "~/.opal_storage_state.json"
browser_executable: "C:/Program Files/BraveSoftware/Brave-Browser/Application/brave.exe"
browser_user_data_dir: "C:/Users/test/AppData/Local/BraveSoftware/Brave-Browser/User Data"
browser_profile_directory: "Profile 1"
`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	credentials, err := LoadCredentials(configPath)
	if err != nil {
		t.Fatalf("LoadCredentials() error = %v", err)
	}
	if credentials.URL != "https://bildungsportal.sachsen.de/opal/" {
		t.Fatalf("URL = %q, unaffected fields should still load normally", credentials.URL)
	}
}

func TestSaveRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	content := `download_path: "D:/Uni/OPAL"
default_course_folder: "default"
course_folders:
  "*Programmierung*": "Informatik/Programmierung"
  "*Analysis*": "Mathematik/Analysis"
courses:
  - "*"
sync: true
opal_url: "https://bildungsportal.sachsen.de/opal/"
session_state_file: "~/.opal_storage_state.json"
`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	loaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if err := Save(configPath, loaded); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	reloaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() after Save() error = %v", err)
	}

	if reloaded.App.DownloadPath != loaded.App.DownloadPath {
		t.Fatalf("DownloadPath mismatch: got %q, want %q", reloaded.App.DownloadPath, loaded.App.DownloadPath)
	}
	if len(reloaded.App.Courses) != len(loaded.App.Courses) || reloaded.App.Courses[0] != loaded.App.Courses[0] {
		t.Fatalf("Courses mismatch: got %v, want %v", reloaded.App.Courses, loaded.App.Courses)
	}
	if reloaded.App.Sync != loaded.App.Sync {
		t.Fatalf("Sync mismatch: got %v, want %v", reloaded.App.Sync, loaded.App.Sync)
	}
	if reloaded.App.DefaultCourseFolder != loaded.App.DefaultCourseFolder {
		t.Fatalf("DefaultCourseFolder mismatch: got %q, want %q", reloaded.App.DefaultCourseFolder, loaded.App.DefaultCourseFolder)
	}
	if len(reloaded.App.CourseFolders) != len(loaded.App.CourseFolders) {
		t.Fatalf("CourseFolders length mismatch: got %v, want %v", reloaded.App.CourseFolders, loaded.App.CourseFolders)
	}
	for k, v := range loaded.App.CourseFolders {
		if reloaded.App.CourseFolders[k] != v {
			t.Fatalf("CourseFolders[%q] mismatch: got %q, want %q", k, reloaded.App.CourseFolders[k], v)
		}
	}
	if reloaded.Credentials.URL != loaded.Credentials.URL {
		t.Fatalf("URL mismatch: got %q, want %q", reloaded.Credentials.URL, loaded.Credentials.URL)
	}
	if reloaded.Credentials.StateFile != loaded.Credentials.StateFile {
		t.Fatalf("StateFile mismatch: got %q, want %q", reloaded.Credentials.StateFile, loaded.Credentials.StateFile)
	}
}

func TestSaveCreatesBackupOnOverwrite(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	originalContent := `download_path: "./original"
opal_url: "https://bildungsportal.sachsen.de/opal/"
`
	if err := os.WriteFile(configPath, []byte(originalContent), 0o644); err != nil {
		t.Fatalf("failed to write initial config: %v", err)
	}

	loaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	loaded.App.DownloadPath = "./changed"

	if err := Save(configPath, loaded); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	backupPath := configPath + ".bak"
	backupData, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("expected backup file at %s, got error: %v", backupPath, err)
	}
	if string(backupData) != originalContent {
		t.Fatalf("backup content mismatch: got %q, want %q", string(backupData), originalContent)
	}

	reloaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() after Save() error = %v", err)
	}
	if got := filepath.Base(reloaded.App.DownloadPath); got != "changed" {
		t.Fatalf("DownloadPath not updated: got %q", reloaded.App.DownloadPath)
	}
}

func TestSaveNoBackupWhenFileDoesNotExist(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	loaded := Loaded{
		App: App{
			DownloadPath: "./downloads",
			Courses:      []string{"*"},
			Sync:         true,
		},
		Credentials: Credentials{
			URL: DefaultOPALURL,
		},
	}

	if err := Save(configPath, loaded); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	backupPath := configPath + ".bak"
	if _, err := os.Stat(backupPath); !os.IsNotExist(err) {
		t.Fatalf("expected no backup file at %s, but stat returned err=%v", backupPath, err)
	}

	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("expected config file to be written at %s, got error: %v", configPath, err)
	}
}

func TestValidateRejectsEmptyDownloadPath(t *testing.T) {
	loaded := Loaded{
		App:         App{DownloadPath: "  "},
		Credentials: Credentials{URL: DefaultOPALURL},
	}
	if err := Validate(loaded); err == nil {
		t.Fatal("expected error for empty download_path, got nil")
	}
}

func TestValidateRejectsEmptyURL(t *testing.T) {
	loaded := Loaded{
		App:         App{DownloadPath: "./downloads"},
		Credentials: Credentials{URL: "  "},
	}
	if err := Validate(loaded); err == nil {
		t.Fatal("expected error for empty opal_url, got nil")
	}
}

func TestValidateRejectsEmptyCourseFolderValue(t *testing.T) {
	loaded := Loaded{
		App: App{
			DownloadPath:  "./downloads",
			CourseFolders: map[string]string{"*Foo*": "  "},
		},
		Credentials: Credentials{URL: DefaultOPALURL},
	}
	if err := Validate(loaded); err == nil {
		t.Fatal("expected error for empty course_folders value, got nil")
	}
}
