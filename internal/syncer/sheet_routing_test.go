package syncer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alu-developer/opal-downloader/internal/config"
	"github.com/alu-developer/opal-downloader/internal/scraper"
)

// maintainerSheetConfig is the real config.yaml shape (2026-11-02) with
// per-sheet entries added next to the plain ones, under a scratch
// download_path.
func maintainerSheetConfig(root string) config.App {
	return config.App{
		DownloadPath:         root,
		DefaultCourseFolder:  filepath.Join(root, "Default_downloads"),
		UseSectionSubfolders: true,
		CourseFolders: map[string]string{
			"TUDMATH WS2026/27 Modul Math-Ba-AL10: Algebra - Grundlegende Konzepte":                filepath.Join(root, "AL10", "Downloads"),
			"TUDMATH WS2026/27 Modul Math-Ba-NM10: Numerische Mathematik - Grundlegende Verfahren": filepath.Join(root, "NM10", "Downloads"),
		},
		SubfolderDestinations: map[string]string{
			"Math-Ba-AL10/Material":                                     filepath.Join(root, "AL10", "Material"),
			"Math-Ba-AL10/Material/{blatt}":                             filepath.Join(root, "AL10", "Blaetter", "{blatt}"),
			"Math-Ba-NM10/Programmierung":                               filepath.Join(root, "NM10", "Programmierung"),
			"Math-Ba-NM10/Übung mit modulbegleitenden Aufgaben":         filepath.Join(root, "NM10", "Blaetter"),
			"Math-Ba-NM10/Übung mit modulbegleitenden Aufgaben/{blatt}": filepath.Join(root, "NM10", "Blaetter", "{blatt}"),
		},
	}
}

const (
	al10Course = "TUDMATH WS2026/27 Modul Math-Ba-AL10: Algebra - Grundlegende Konzepte"
	nm10Course = "TUDMATH WS2026/27 Modul Math-Ba-NM10: Numerische Mathematik - Grundlegende Verfahren"
)

// TestResolveRemoteTargetPathSheetRouting is the whole target behaviour in
// one table: the maintainer's real files, the real sections they share, and
// the folders each one has to land in.
func TestResolveRemoteTargetPathSheetRouting(t *testing.T) {
	root := t.TempDir()
	cfg := maintainerSheetConfig(root)

	tests := []struct {
		name      string
		course    string
		section   string
		wantLocal string
	}{
		{"ue01_nm10_wi26-27.pdf", nm10Course, "Übung mit modulbegleitenden Aufgaben", filepath.Join(root, "NM10", "Blaetter", "Blatt01", "ue01_nm10_wi26-27.pdf")},
		{"loe01_nm10_wi26-27.pdf", nm10Course, "Übung mit modulbegleitenden Aufgaben", filepath.Join(root, "NM10", "Blaetter", "Blatt01", "loe01_nm10_wi26-27.pdf")},
		{"p01.py", nm10Course, "Übung mit modulbegleitenden Aufgaben", filepath.Join(root, "NM10", "Blaetter", "Blatt01", "p01.py")},
		{"ue02_nm10_wi26-27.pdf", nm10Course, "Übung mit modulbegleitenden Aufgaben", filepath.Join(root, "NM10", "Blaetter", "Blatt02", "ue02_nm10_wi26-27.pdf")},
		{"Blatt1_A10.pdf", al10Course, "Material", filepath.Join(root, "AL10", "Blaetter", "Blatt01", "Blatt1_A10.pdf")},
		{"Blatt2_AL10.pdf", al10Course, "Material", filepath.Join(root, "AL10", "Blaetter", "Blatt02", "Blatt2_AL10.pdf")},

		// Unchanged destinations: the non-sheet files of the very same
		// AL10 section, and a section with no sheet-only entry at all.
		{"Kapitel1.pdf", al10Course, "Material", filepath.Join(root, "AL10", "Material", "Kapitel1.pdf")},
		{"LA.I.pdf", al10Course, "Material", filepath.Join(root, "AL10", "Material", "LA.I.pdf")},
		{"LA.III.pdf", al10Course, "Material", filepath.Join(root, "AL10", "Material", "LA.III.pdf")},
		{"LA.IV.pdf", al10Course, "Material", filepath.Join(root, "AL10", "Material", "LA.IV.pdf")},
		{"01_Intro.ipynb", nm10Course, "Programmierung", filepath.Join(root, "NM10", "Programmierung", "01_Intro.ipynb")},
		{"04_1_InputOutput.ipynb", nm10Course, "Programmierung", filepath.Join(root, "NM10", "Programmierung", "04_1_InputOutput.ipynb")},
		{"nm10_Programmierstil.pdf", nm10Course, "Programmierung", filepath.Join(root, "NM10", "Programmierung", "nm10_Programmierstil.pdf")},
	}

	for _, tt := range tests {
		file := scraper.RemoteFile{Name: tt.name, Course: tt.course, SectionTitle: tt.section}
		resolved := resolveRemoteTargetPath(cfg, file)
		if resolved.LocalPath != tt.wantLocal {
			t.Errorf("%s: local path = %s, want %s", tt.name, resolved.LocalPath, tt.wantLocal)
		}

		// The manifest key is deliberately untouched by sheet routing: it
		// stays the course/section-relative path it has always been, so
		// turning this on re-keys nothing and re-downloads nothing.
		wantKey := filepath.Join(config.SanitizePathComponent(tt.course), config.SanitizePathComponent(tt.section), tt.name)
		if resolved.ManifestKey != wantKey {
			t.Errorf("%s: manifest key = %s, want %s", tt.name, resolved.ManifestKey, wantKey)
		}
	}
}

// A config without any "/{blatt}" entry must resolve exactly as it did
// before per-sheet routing existed, even for files whose names are full of
// sheet numbers.
func TestResolveRemoteTargetPathUnchangedWithoutSheetEntries(t *testing.T) {
	root := t.TempDir()
	cfg := config.App{
		DownloadPath:         root,
		UseSectionSubfolders: true,
		SubfolderDestinations: map[string]string{
			"Math-Ba-NM10/Übung mit modulbegleitenden Aufgaben": filepath.Join(root, "NM10", "Blaetter"),
		},
	}

	file := scraper.RemoteFile{Name: "ue01_nm10_wi26-27.pdf", Course: nm10Course, SectionTitle: "Übung mit modulbegleitenden Aufgaben"}
	want := filepath.Join(root, "NM10", "Blaetter", "ue01_nm10_wi26-27.pdf")
	if got := resolveRemoteTargetPath(cfg, file).LocalPath; got != want {
		t.Fatalf("local path = %s, want %s", got, want)
	}
}

// The leftover sweep: an already-downloaded file sitting in the flat location
// is carried into its sheet folder (no re-download, no duplicate), while a
// copy the maintainer had already hand-filed there is never written over -
// the flat duplicate is reported instead.
func TestRelocateSheetRoutedFiles(t *testing.T) {
	root := t.TempDir()
	cfg := maintainerSheetConfig(root)

	flatDir := filepath.Join(root, "NM10", "Blaetter")
	if err := os.MkdirAll(filepath.Join(flatDir, "Blatt02"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(flatDir, "ue01_nm10_wi26-27.pdf"), "flat sheet 1")
	writeFile(t, filepath.Join(flatDir, "ue02_nm10_wi26-27.pdf"), "flat sheet 2")
	writeFile(t, filepath.Join(flatDir, "Blatt02", "ue02_nm10_wi26-27.pdf"), "hand-filed sheet 2")

	// A non-sheet file of a section that has no sheet-only entry must not be
	// touched by the sweep at all.
	materialDir := filepath.Join(root, "AL10", "Material")
	if err := os.MkdirAll(materialDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(materialDir, "Kapitel1.pdf"), "chapter 1")

	remoteFiles := []scraper.RemoteFile{
		{Name: "ue01_nm10_wi26-27.pdf", Course: nm10Course, SectionTitle: "Übung mit modulbegleitenden Aufgaben"},
		{Name: "ue02_nm10_wi26-27.pdf", Course: nm10Course, SectionTitle: "Übung mit modulbegleitenden Aufgaben"},
		{Name: "Kapitel1.pdf", Course: al10Course, SectionTitle: "Material"},
	}

	// The manifest describes the two downloads that went to the FLAT path.
	manifest := &Manifest{Files: map[string]FileRecord{}}
	for _, file := range remoteFiles {
		manifest.Files[filepath.ToSlash(resolveRemoteTargetPath(cfg, file).ManifestKey)] = FileRecord{
			Size: sizePtr(12), Modified: strPtr("22.10.2026 um 16:51 Uhr"),
		}
	}

	report := relocateSheetRoutedFiles(manifest, cfg, remoteFiles)
	if !report.Changed() {
		t.Fatal("expected the sweep to report something")
	}

	if len(report.Moved) != 1 {
		t.Fatalf("expected exactly one move, got %v", report.Moved)
	}
	movedTo := filepath.Join(flatDir, "Blatt01", "ue01_nm10_wi26-27.pdf")
	if got := readFile(t, movedTo); got != "flat sheet 1" {
		t.Fatalf("sheet 1 did not land in Blatt01, got %q", got)
	}
	if _, err := os.Stat(filepath.Join(flatDir, "ue01_nm10_wi26-27.pdf")); !os.IsNotExist(err) {
		t.Fatal("expected the flat copy of sheet 1 to be gone after the move")
	}

	// Sheet 2: the hand-filed copy survives untouched and the flat
	// duplicate is reported, not deleted.
	if got := readFile(t, filepath.Join(flatDir, "Blatt02", "ue02_nm10_wi26-27.pdf")); got != "hand-filed sheet 2" {
		t.Fatalf("hand-filed copy was overwritten, got %q", got)
	}
	if got := readFile(t, filepath.Join(flatDir, "ue02_nm10_wi26-27.pdf")); got != "flat sheet 2" {
		t.Fatalf("flat duplicate was touched, got %q", got)
	}
	if len(report.Left) != 1 {
		t.Fatalf("expected exactly one reported duplicate, got %v", report.Left)
	}

	if got := readFile(t, filepath.Join(materialDir, "Kapitel1.pdf")); got != "chapter 1" {
		t.Fatalf("a non-sheet file was moved, got %q", got)
	}

	// Sheet 2's record describes the flat download, not the hand-filed copy
	// now at its sheet path, so it must not be trusted on metadata alone -
	// the same trap as a migrated key pointing at a file the sync never
	// wrote. Sheet 1, which the sweep moved itself, stays trusted.
	sheet2Key := filepath.ToSlash(resolveRemoteTargetPath(cfg, remoteFiles[1]).ManifestKey)
	if !manifest.Files[sheet2Key].Unverified {
		t.Fatalf("expected %s to be flagged unverified", sheet2Key)
	}
	if report.Unverified != 1 {
		t.Fatalf("expected one flagged entry, got %d", report.Unverified)
	}
	sheet1Key := filepath.ToSlash(resolveRemoteTargetPath(cfg, remoteFiles[0]).ManifestKey)
	if manifest.Files[sheet1Key].Unverified {
		t.Fatalf("expected the relocated %s to stay trusted", sheet1Key)
	}
}

// Without a sheet-only entry the sweep does nothing and reports nothing, so
// every existing config is unaffected.
func TestRelocateSheetRoutedFilesNoopWithoutSheetEntries(t *testing.T) {
	root := t.TempDir()
	cfg := config.App{
		DownloadPath:         root,
		UseSectionSubfolders: true,
		SubfolderDestinations: map[string]string{
			"Math-Ba-NM10/Übung mit modulbegleitenden Aufgaben": filepath.Join(root, "NM10", "Blaetter"),
		},
	}
	remoteFiles := []scraper.RemoteFile{
		{Name: "ue01_nm10_wi26-27.pdf", Course: nm10Course, SectionTitle: "Übung mit modulbegleitenden Aufgaben"},
	}
	if report := relocateSheetRoutedFiles(nil, cfg, remoteFiles); report.Changed() {
		t.Fatalf("expected no sweep without sheet entries, got %v", report.Lines())
	}
}
