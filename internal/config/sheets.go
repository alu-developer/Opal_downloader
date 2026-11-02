package config

import (
	"fmt"
	"path"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// SheetPlaceholder is what makes per-sheet (Übungsblatt) routing part of the
// existing subfolder_destinations lever instead of a fourth one.
//
// It is used in two places, and means something slightly different in each:
//
//   - As a trailing key component ("<course>/<section>/{blatt}") it marks the
//     entry as sheet-only: it is consulted exclusively for files whose NAME
//     carries a sheet number (SheetNumber), and ignored for every other file
//     in that same section. That is the opt-in. A course that must keep its
//     non-sheet files where they are - AL10's "Material", which holds both
//     Kapitel1.pdf and Blatt1_A10.pdf - simply keeps its plain entry next to
//     the sheet-only one, and each file picks the one that fits it.
//
//   - Inside the destination value it is replaced by the sheet folder name
//     (SheetFolderName, e.g. "Blatt01"), so the destination says where the
//     per-sheet folders live rather than the code assuming it.
//
// Why the opt-in is per destination rather than a global switch: section
// titles are the only thing the other levers can see, and OPAL serves sheet
// and non-sheet material from ONE section (measured 2026-11-02: AL10's
// "Material" holds Kapitel1.pdf, LA.I/III/IV.pdf, Blatt1_A10.pdf and
// Blatt2_AL10.pdf together). A global "split by sheet number" would also hit
// NM10's "Programmierung", whose 01_Intro.ipynb/04_1_InputOutput.ipynb names
// are full of numbers that are not sheet numbers.
const SheetPlaceholder = "{blatt}"

// sheetFolderPattern is the fixed, deliberately non-configurable shape of a
// sheet folder name: "Blatt" plus the zero-padded two-digit sheet number,
// which is what the maintainer's own hand-filed folders already use
// (_3. Semester/NM10/Blaetter/Blatt01, .../Blatt02).
//
// Not a config knob, on purpose: the destination value already decides WHERE
// the per-sheet folders go, and a second knob for what to call them would be
// one more setting nobody ever changes - while a padding mismatch between
// config and the folders already on disk would silently produce a second,
// parallel set of folders (Blatt1 next to Blatt01).
const sheetFolderPattern = "Blatt%02d"

// sheetMarkersExplicit are the name tokens that introduce a sheet number
// unambiguously, the first of the two tiers SheetNumber applies.
//
// Recognition is a fixed, documented rule rather than a configurable regex:
// the names are not the maintainer's to choose (each lecturer invents their
// own), so a knob would have to be re-tuned per course, which is strictly
// more work than extending this list once - and a user-supplied regex that
// matches too much fails silently, by filing files into folders nobody asked
// for.
//
// The real names this was built against, all live-confirmed 2026-11-02:
// ue01_nm10_wi26-27.pdf, loe01_nm10_wi26-27.pdf, p01.py,
// ue02_nm10_wi26-27.pdf (NM10) and Blatt1_A10.pdf, Blatt2_AL10.pdf (AL10) -
// zero-padded and unpadded, prefix and infix, German and abbreviated.
var sheetMarkersExplicit = []string{
	"blatt", "bl",
	"uebung", "ubung", "ue", "ub",
	"loesung", "losung", "loe", "lsg",
	"serie", "sheet",
}

// sheetMarkersAmbiguous are single letters that mean "sheet number" in
// practice (p01.py, U06.pdf) but also occur in names where the digits after
// them are a year, a module code or a chapter. They are only consulted when
// no explicit marker matched anywhere in the name, which is what keeps
// Blatt1_A10.pdf at sheet 1 instead of reading its "A10" as sheet 10, and
// they require the digits to follow immediately (no separator).
var sheetMarkersAmbiguous = []string{"u", "p", "a", "h"}

// SheetNumber extracts the Übungsblatt number a file name carries, reporting
// false when it carries none - in which case the caller must leave that
// file's destination exactly as it was.
//
// The rule, in full:
//
//   - The name is lower-cased and stripped of diacritics first (the same
//     normalization course/section patterns are matched with), so "Übung"
//     and "Ubung" are one marker, and the file extension is dropped.
//   - A marker only counts at a word boundary: the start of the name, or
//     after a character that is not a letter. This is what stops "nm10" and
//     "Kapitel1.pdf" from matching ("m10" and "a" sit mid-word) while
//     "NM10_Blatt01" still does.
//   - An explicit marker (sheetMarkersExplicit) may be separated from its
//     digits by " ", "_", "-" or "."; an ambiguous one
//     (sheetMarkersAmbiguous) may not, and is only tried when no explicit
//     marker matched.
//   - The digit run must be one or two digits long. That is the cheap guard
//     against years and module codes: "wi26-27" has no marker, but "ue2026"
//     would, and two digits is the honest upper bound for a sheet number.
//   - The first match wins, scanning left to right.
func SheetNumber(fileName string) (int, bool) {
	name := strings.TrimSpace(fileName)
	name = strings.TrimSuffix(name, path.Ext(name))
	name = normalizeMatchText(name)
	if name == "" {
		return 0, false
	}

	if number, ok := scanSheetMarkers(name, sheetMarkersExplicit, true); ok {
		return number, true
	}
	return scanSheetMarkers(name, sheetMarkersAmbiguous, false)
}

// SheetFolderName renders a sheet number as the folder name it is filed
// under ("Blatt01").
func SheetFolderName(number int) string {
	return fmt.Sprintf(sheetFolderPattern, number)
}

// scanSheetMarkers walks name left to right and returns the number behind the
// first word-boundary marker that is actually followed by a sheet-shaped
// digit run. Marker order within markers does not matter: two markers at the
// same position cannot both be followed by digits, since one is then a prefix
// of the other.
func scanSheetMarkers(name string, markers []string, allowSeparator bool) (int, bool) {
	for i := 0; i < len(name); i++ {
		if i > 0 {
			prev, _ := utf8.DecodeLastRuneInString(name[:i])
			if unicode.IsLetter(prev) {
				continue
			}
		}
		for _, marker := range markers {
			if !strings.HasPrefix(name[i:], marker) {
				continue
			}
			if number, ok := leadingSheetNumber(name[i+len(marker):], allowSeparator); ok {
				return number, true
			}
		}
	}
	return 0, false
}

// leadingSheetNumber reads a one- or two-digit sheet number off the front of
// rest, optionally skipping a separator first. A longer digit run is rejected
// rather than truncated: three or more digits is a year or a module code, not
// a sheet.
func leadingSheetNumber(rest string, allowSeparator bool) (int, bool) {
	if allowSeparator {
		rest = strings.TrimLeft(rest, " _-.")
	}

	digits := 0
	for digits < len(rest) && rest[digits] >= '0' && rest[digits] <= '9' {
		digits++
	}
	if digits == 0 || digits > 2 {
		return 0, false
	}

	number, err := strconv.Atoi(rest[:digits])
	if err != nil || number < 0 {
		return 0, false
	}
	return number, true
}

// expandSheetPlaceholder fills SheetPlaceholder in a destination path with
// the sheet folder name. A destination that does not use the placeholder is
// returned unchanged, which is a legitimate (if blunt) way to say "send every
// sheet file of this section to one other folder, flat".
func expandSheetPlaceholder(destination string, number int) string {
	return replaceFoldedPlaceholder(destination, SheetFolderName(number))
}

// replaceFoldedPlaceholder replaces every case-spelling of SheetPlaceholder
// ("{blatt}", "{Blatt}", "{BLATT}") so a config typed with a capital B
// behaves the same as the documented spelling instead of silently creating a
// folder literally named "{Blatt}".
func replaceFoldedPlaceholder(value, replacement string) string {
	var b strings.Builder
	for {
		idx := strings.Index(strings.ToLower(value), SheetPlaceholder)
		if idx < 0 {
			b.WriteString(value)
			return b.String()
		}
		b.WriteString(value[:idx])
		b.WriteString(replacement)
		value = value[idx+len(SheetPlaceholder):]
	}
}

// HasSheetRoutedDestination reports whether any subfolder_destinations entry
// opts into per-sheet routing. Callers use it to skip sheet-routing work
// entirely for the configs that do not use it.
func HasSheetRoutedDestination(cfg App) bool {
	for key := range cfg.SubfolderDestinations {
		if _, _, sheetOnly, _ := splitSubfolderDestinationKey(key); sheetOnly {
			return true
		}
	}
	return false
}

// containsSheetPlaceholder reports whether a destination path uses the
// placeholder, in any case spelling.
func containsSheetPlaceholder(value string) bool {
	return strings.Contains(strings.ToLower(value), SheetPlaceholder)
}
