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
	got := filepath.ToSlash(resolveRemoteTargetPath(cfgExplicit, file).ManifestKey)
	if got != "Math/Analysis/sheet.pdf" {
		t.Fatalf("explicit target path mismatch: %s", got)
	}

	cfgDefault := config.App{DefaultCourseFolder: "default"}
	got = filepath.ToSlash(resolveRemoteTargetPath(cfgDefault, file).ManifestKey)
	if got != "default/Analysis I/sheet.pdf" {
		t.Fatalf("default target path mismatch: %s", got)
	}

	cfgFallback := config.App{}
	got = filepath.ToSlash(resolveRemoteTargetPath(cfgFallback, file).ManifestKey)
	if got != "Analysis I/sheet.pdf" {
		t.Fatalf("fallback target path mismatch: %s", got)
	}
}

func TestResolveRemoteTargetPathDefaultUnchangedWithoutSubfolders(t *testing.T) {
	// Regression guard: with none of the new config keys set, behavior must be
	// byte-identical to the pre-subfolder-support implementation.
	file := scraper.RemoteFile{Name: "sheet.pdf", Course: "Analysis I", SectionTitle: "Uebungen"}

	cfgFallback := config.App{}
	resolved := resolveRemoteTargetPath(cfgFallback, file)
	if filepath.ToSlash(resolved.ManifestKey) != "Analysis I/sheet.pdf" {
		t.Fatalf("expected unchanged flat path, got %s", filepath.ToSlash(resolved.ManifestKey))
	}
	if resolved.LocalPath != filepath.Join(cfgFallback.DownloadPath, resolved.ManifestKey) {
		t.Fatalf("expected LocalPath to be DownloadPath+ManifestKey, got %s", resolved.LocalPath)
	}
}

func TestResolveRemoteTargetPathSectionSubfoldersEnabled(t *testing.T) {
	file := scraper.RemoteFile{Name: "sheet.pdf", Course: "Analysis I", SectionTitle: "Uebungen"}

	cfg := config.App{UseSectionSubfolders: true}
	got := filepath.ToSlash(resolveRemoteTargetPath(cfg, file).ManifestKey)
	if got != "Analysis I/Uebungen/sheet.pdf" {
		t.Fatalf("expected section subfolder path, got %s", got)
	}
}

func TestResolveRemoteTargetPathSectionSubfoldersWithNameMapping(t *testing.T) {
	file := scraper.RemoteFile{Name: "sheet.pdf", Course: "Analysis I", SectionTitle: "Exercises"}

	cfg := config.App{
		UseSectionSubfolders: true,
		SectionFolderNames: map[string]string{
			"Exercises": "Uebungen",
		},
	}
	got := filepath.ToSlash(resolveRemoteTargetPath(cfg, file).ManifestKey)
	if got != "Analysis I/Uebungen/sheet.pdf" {
		t.Fatalf("expected mapped section subfolder path, got %s", got)
	}
}

func TestResolveRemoteTargetPathSubfolderDestinationOverride(t *testing.T) {
	file := scraper.RemoteFile{Name: "slides.pdf", Course: "Analysis I", SectionTitle: "Vorlesung"}

	cfg := config.App{
		DownloadPath:         `C:\downloads`,
		UseSectionSubfolders: true,
		SubfolderDestinations: map[string]string{
			"*Analysis*/*Vorlesung*": `D:\Elsewhere\AnalysisSlides`,
		},
	}
	resolved := resolveRemoteTargetPath(cfg, file)
	wantLocal := filepath.Join(`D:\Elsewhere\AnalysisSlides`, "slides.pdf")
	if resolved.LocalPath != wantLocal {
		t.Fatalf("expected override local path %s, got %s", wantLocal, resolved.LocalPath)
	}
	// Manifest key still tracks the file under its normal course/subfolder path,
	// independent of the override destination.
	if filepath.ToSlash(resolved.ManifestKey) != "Analysis I/Vorlesung/slides.pdf" {
		t.Fatalf("expected manifest key to reflect normal course path, got %s", filepath.ToSlash(resolved.ManifestKey))
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
