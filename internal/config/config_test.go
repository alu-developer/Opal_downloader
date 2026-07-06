package config

import (
	"os"
	"path/filepath"
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

func TestLoadCredentialsBrowserProfileDirectory(t *testing.T) {
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

	if credentials.BrowserProfileDir != "Profile 1" {
		t.Fatalf("BrowserProfileDir = %q, want %q", credentials.BrowserProfileDir, "Profile 1")
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
browser_executable: ""
browser_user_data_dir: ""
browser_profile_directory: ""
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
	if reloaded.Credentials.BrowserExecutable != loaded.Credentials.BrowserExecutable {
		t.Fatalf("BrowserExecutable mismatch: got %q, want %q", reloaded.Credentials.BrowserExecutable, loaded.Credentials.BrowserExecutable)
	}
	if reloaded.Credentials.BrowserUserDataDir != loaded.Credentials.BrowserUserDataDir {
		t.Fatalf("BrowserUserDataDir mismatch: got %q, want %q", reloaded.Credentials.BrowserUserDataDir, loaded.Credentials.BrowserUserDataDir)
	}
	if reloaded.Credentials.BrowserProfileDir != loaded.Credentials.BrowserProfileDir {
		t.Fatalf("BrowserProfileDir mismatch: got %q, want %q", reloaded.Credentials.BrowserProfileDir, loaded.Credentials.BrowserProfileDir)
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
