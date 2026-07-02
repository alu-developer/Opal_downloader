package config

import "testing"

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
