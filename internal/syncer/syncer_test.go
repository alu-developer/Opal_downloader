package syncer

import (
	"path/filepath"
	"testing"

	"github.com/alu-developer/opal-downloader/internal/config"
	"github.com/alu-developer/opal-downloader/internal/scraper"
)

func TestResolveRemoteTargetPath(t *testing.T) {
	file := scraper.RemoteFile{Name: "sheet.pdf", Course: "Analysis I"}

	cfgExplicit := config.App{
		DefaultCourseFolder: "default",
		CourseFolders: map[string]string{
			"*Analysis*": "Math/Analysis",
		},
	}
	got := filepath.ToSlash(resolveRemoteTargetPath(cfgExplicit, file))
	if got != "Math/Analysis/sheet.pdf" {
		t.Fatalf("explicit target path mismatch: %s", got)
	}

	cfgDefault := config.App{DefaultCourseFolder: "default"}
	got = filepath.ToSlash(resolveRemoteTargetPath(cfgDefault, file))
	if got != "default/Analysis I/sheet.pdf" {
		t.Fatalf("default target path mismatch: %s", got)
	}

	cfgFallback := config.App{}
	got = filepath.ToSlash(resolveRemoteTargetPath(cfgFallback, file))
	if got != "Analysis I/sheet.pdf" {
		t.Fatalf("fallback target path mismatch: %s", got)
	}
}

func TestFileChanged(t *testing.T) {
	size10 := int64(10)
	size11 := int64(11)
	modA := "2026-01-01"
	modB := "2026-01-02"

	remote := scraper.RemoteFile{Size: &size10, Modified: &modA}
	if !fileChanged(remote, false, FileRecord{}) {
		t.Fatal("expected changed when no previous file record")
	}

	prev := FileRecord{Size: &size10, Modified: &modA}
	if fileChanged(remote, true, prev) {
		t.Fatal("expected unchanged when metadata matches")
	}

	prev = FileRecord{Size: &size11, Modified: &modA}
	if !fileChanged(remote, true, prev) {
		t.Fatal("expected changed when size differs")
	}

	prev = FileRecord{Size: &size10, Modified: &modB}
	if !fileChanged(remote, true, prev) {
		t.Fatal("expected changed when modified timestamp differs")
	}
}
