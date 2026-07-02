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
