package config

import "testing"

// TestSheetNumberRealNames pins the extraction against the file names it was
// built for - every one of them read off the maintainer's real OPAL sections
// and real folders on 2026-11-02 - and, just as importantly, against the
// names in those same sections that must NOT be read as sheets.
func TestSheetNumberRealNames(t *testing.T) {
	tests := []struct {
		name   string
		number int
		want   bool
	}{
		// NM10, section "Übung mit modulbegleitenden Aufgaben".
		{"ue01_nm10_wi26-27.pdf", 1, true},
		{"loe01_nm10_wi26-27.pdf", 1, true},
		{"p01.py", 1, true},
		{"ue02_nm10_wi26-27.pdf", 2, true},

		// AL10, section "Material" - sheets and non-sheets side by side.
		{"Blatt1_A10.pdf", 1, true},
		{"Blatt2_AL10.pdf", 2, true},
		{"Kapitel1.pdf", 0, false},
		{"LA.I.pdf", 0, false},
		{"LA.III.pdf", 0, false},
		{"LA.IV.pdf", 0, false},

		// NM10, section "Programmierung": numbers everywhere, no sheets.
		// These are the names that make a global "split by number" rule
		// unusable, so they are the important half of this table.
		{"01_Intro.ipynb", 0, false},
		{"02_VariablesAndDataTypes.ipynb", 0, false},
		{"04_1_InputOutput.ipynb", 0, false},
		{"04_3_Loops.ipynb", 0, false},
		{"07_Applications_with_Numpy.ipynb", 0, false},
		{"nm10_Programmierstil.pdf", 0, false},
	}

	for _, tt := range tests {
		number, ok := SheetNumber(tt.name)
		if ok != tt.want || (ok && number != tt.number) {
			t.Errorf("SheetNumber(%q) = (%d, %v), want (%d, %v)", tt.name, number, ok, tt.number, tt.want)
		}
	}
}

func TestSheetNumberSpellings(t *testing.T) {
	tests := []struct {
		name   string
		number int
		want   bool
	}{
		// Explicit markers, padded/unpadded, prefix/infix, with and without
		// a separator, German and abbreviated.
		{"Blatt01.pdf", 1, true},
		{"blatt_7.pdf", 7, true},
		{"Blatt 12 Aufgaben.pdf", 12, true},
		{"NM10_Vorübungen_Blatt01_Aufgaben.pdf", 1, true},
		{"Übung03.pdf", 3, true},
		{"uebung-04.pdf", 4, true},
		{"Lösung05.pdf", 5, true},
		{"loesung_6.pdf", 6, true},
		{"lsg08.pdf", 8, true},
		{"Serie09.pdf", 9, true},
		{"sheet10.pdf", 10, true},
		{"bl11.pdf", 11, true},
		{"U00.pdf", 0, true},
		{"U06.pdf", 6, true},

		// An explicit marker wins over an ambiguous one anywhere in the
		// name, which is what keeps "Blatt1_A10" at sheet 1 rather than
		// reading its module code as sheet 10.
		{"Blatt1_A10.pdf", 1, true},
		{"A10_Blatt3.pdf", 3, true},

		// Years and module codes: three or more digits is never a sheet.
		{"ue2026.pdf", 0, false},
		{"skript_ws2026.pdf", 0, false},
		{"p100.py", 0, false},

		// Mid-word digits are not sheet numbers: the marker has to start at
		// a word boundary.
		{"nm10.pdf", 0, false},
		{"Kapitel1.pdf", 0, false},
		{"Zusammenfassung_LA10.pdf", 0, false},

		// No marker at all.
		{"Skript.pdf", 0, false},
		{"", 0, false},
		{"2026-10-30_Mitschrift.pdf", 0, false},
	}

	for _, tt := range tests {
		number, ok := SheetNumber(tt.name)
		if ok != tt.want || (ok && number != tt.number) {
			t.Errorf("SheetNumber(%q) = (%d, %v), want (%d, %v)", tt.name, number, ok, tt.number, tt.want)
		}
	}
}

func TestSheetFolderNameIsZeroPaddedToTwoDigits(t *testing.T) {
	for number, want := range map[int]string{0: "Blatt00", 1: "Blatt01", 9: "Blatt09", 12: "Blatt12"} {
		if got := SheetFolderName(number); got != want {
			t.Errorf("SheetFolderName(%d) = %q, want %q", number, got, want)
		}
	}
}

// TestResolveSubfolderDestinationSheetRouting is the maintainer's real AL10
// case: one OPAL section holding both sheets and chapter material, which has
// to split two ways.
func TestResolveSubfolderDestinationSheetRouting(t *testing.T) {
	cfg := App{
		SubfolderDestinations: map[string]string{
			"Math-Ba-AL10/Material":         `C:\OneDrive\AL10\Material`,
			"Math-Ba-AL10/Material/{blatt}": `C:\OneDrive\AL10\Blaetter\{blatt}`,
			"Math-Ba-NM10/Programmierung":   `C:\OneDrive\NM10\Programmierung`,
		},
	}

	tests := []struct {
		fileName string
		want     string
	}{
		{"Blatt1_A10.pdf", `C:\OneDrive\AL10\Blaetter\Blatt01`},
		{"Blatt2_AL10.pdf", `C:\OneDrive\AL10\Blaetter\Blatt02`},
		// No sheet number: exactly the destination it had before per-sheet
		// routing existed.
		{"Kapitel1.pdf", `C:\OneDrive\AL10\Material`},
		{"LA.I.pdf", `C:\OneDrive\AL10\Material`},
	}
	for _, tt := range tests {
		dest, ok := ResolveSubfolderDestination(cfg, "TUDMATH WS2026/27 Modul Math-Ba-AL10: Algebra", "Material", tt.fileName)
		if !ok || dest != tt.want {
			t.Errorf("ResolveSubfolderDestination(%q) = (%q, %v), want (%q, true)", tt.fileName, dest, ok, tt.want)
		}
	}

	// A section with no sheet-only entry is never split, however
	// number-laden its file names are.
	for _, fileName := range []string{"01_Intro.ipynb", "04_1_InputOutput.ipynb", "nm10_Programmierstil.pdf"} {
		dest, ok := ResolveSubfolderDestination(cfg, "TUDMATH WS2026/27 Modul Math-Ba-NM10: Numerische Mathematik", "Programmierung", fileName)
		if !ok || dest != `C:\OneDrive\NM10\Programmierung` {
			t.Errorf("ResolveSubfolderDestination(%q) = (%q, %v), want the plain destination", fileName, dest, ok)
		}
	}
}

// A sheet-only entry with no plain sibling must leave non-sheet files to the
// rest of the resolution chain (course folder + section subfolder), not claim
// them.
func TestResolveSubfolderDestinationSheetOnlyEntryIgnoresNonSheetFiles(t *testing.T) {
	cfg := App{
		SubfolderDestinations: map[string]string{
			"*NM10*/*Übung*/{blatt}": `C:\OneDrive\NM10\Blaetter\{blatt}`,
		},
	}

	dest, ok := ResolveSubfolderDestination(cfg, "NM10", "Übung mit modulbegleitenden Aufgaben", "ue01_nm10_wi26-27.pdf")
	if !ok || dest != `C:\OneDrive\NM10\Blaetter\Blatt01` {
		t.Fatalf("expected sheet destination, got (%q, %v)", dest, ok)
	}

	if _, ok := ResolveSubfolderDestination(cfg, "NM10", "Übung mit modulbegleitenden Aufgaben", "Hinweise.pdf"); ok {
		t.Fatal("expected a non-sheet file to match no entry at all")
	}
}

// A sheet-only destination without the placeholder is allowed: it means "send
// this section's sheet files somewhere else, flat".
func TestResolveSubfolderDestinationSheetOnlyWithoutPlaceholder(t *testing.T) {
	cfg := App{
		SubfolderDestinations: map[string]string{
			"*NM10*/*Übung*/{blatt}": `C:\OneDrive\NM10\Blaetter`,
		},
	}
	dest, ok := ResolveSubfolderDestination(cfg, "NM10", "Übungen", "ue02.pdf")
	if !ok || dest != `C:\OneDrive\NM10\Blaetter` {
		t.Fatalf("expected flat sheet destination, got (%q, %v)", dest, ok)
	}
}

func TestSplitSubfolderDestinationKeySheetSuffix(t *testing.T) {
	// The suffix has to come off before the last-"/" split, or a course
	// title containing "/" plus "{blatt}" parses as the wrong pair.
	course, section, sheetOnly, ok := splitSubfolderDestinationKey("WS2026/27 Math-Ba-NM10/Übung/{blatt}")
	if !ok || !sheetOnly || course != "WS2026/27 Math-Ba-NM10" || section != "Übung" {
		t.Fatalf("got (%q, %q, %v, %v)", course, section, sheetOnly, ok)
	}

	course, section, sheetOnly, ok = splitSubfolderDestinationKey("Math-Ba-AL10/Material")
	if !ok || sheetOnly || course != "Math-Ba-AL10" || section != "Material" {
		t.Fatalf("got (%q, %q, %v, %v)", course, section, sheetOnly, ok)
	}

	// Capital spelling is accepted the same way the placeholder in a
	// destination is.
	if _, _, sheetOnly, ok := splitSubfolderDestinationKey("A/B/{Blatt}"); !ok || !sheetOnly {
		t.Fatalf("expected capitalized placeholder suffix to be recognized, got (%v, %v)", sheetOnly, ok)
	}

	// Nothing left to split once the suffix is off.
	if _, _, _, ok := splitSubfolderDestinationKey("Material/{blatt}"); ok {
		t.Fatal("expected a key with no course/section pair to be rejected")
	}
}

func TestValidateRejectsPlaceholderOnNonSheetKey(t *testing.T) {
	cfg := Loaded{
		App: App{
			DownloadPath: `C:\downloads`,
			SubfolderDestinations: map[string]string{
				"Math-Ba-AL10/Material": `C:\OneDrive\AL10\Blaetter\{blatt}`,
			},
		},
		Credentials: Credentials{URL: DefaultOPALURL},
	}
	if err := Validate(cfg); err == nil {
		t.Fatal("expected a destination placeholder on a non-sheet key to be rejected")
	}

	cfg.App.SubfolderDestinations = map[string]string{
		"Math-Ba-AL10/Material/{blatt}": `C:\OneDrive\AL10\Blaetter\{blatt}`,
	}
	if err := Validate(cfg); err != nil {
		t.Fatalf("expected a sheet-only key to validate, got %v", err)
	}
}
