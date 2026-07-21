package syncer

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"

	"github.com/alu-developer/opal-downloader/internal/config"
	"github.com/alu-developer/opal-downloader/internal/scraper"
)

// keySchemeOverlapDenominator gates when a manifest key-scheme migration is
// attempted at all: the pass only runs when fewer than 1/N of the stored
// manifest entries still match a key the current code produces. See
// suspectKeySchemeChange for why the gate exists.
const keySchemeOverlapDenominator = 4

// KeyRemap records one stored manifest entry that was re-keyed to the key
// today's code produces for the same file.
type KeyRemap struct {
	OldKey string
	NewKey string

	// Relocated is true when the already-downloaded file was physically
	// moved from OldPath to NewPath, so the sync can skip it instead of
	// re-downloading it and leaving the old copy orphaned.
	Relocated bool
	OldPath   string
	NewPath   string
}

// MigrationReport describes what a key-scheme migration pass did (or
// declined to do). It is purely informational: nothing in it is required for
// the sync to proceed, but everything in Ambiguous/Orphans is something the
// user needs to be told about, because it is a file or a history entry that
// would otherwise be silently abandoned.
type MigrationReport struct {
	// LoadedSchemaVersion is the schema_version the manifest was written
	// with (LegacyManifestSchemaVersion for pre-versioning manifests).
	LoadedSchemaVersion int

	// StoredKeys/MatchedKeys are the entry counts the gate was decided on:
	// how many entries the manifest held, and how many of them still match a
	// key the current key scheme produces.
	StoredKeys  int
	MatchedKeys int

	// Attempted is true when a key-scheme change was suspected and the
	// remap pass actually ran.
	Attempted bool

	Remapped []KeyRemap

	// Ambiguous holds stored keys that look stale but could not be mapped to
	// exactly one current key (e.g. the same file name appears more than
	// once on either side). These are deliberately left untouched.
	Ambiguous []string

	// Orphans holds absolute paths of already-downloaded files that are
	// still on disk under the old layout and were NOT relocated. Reported so
	// the user can move or delete them; never deleted automatically.
	Orphans []string
}

// Changed reports whether the pass did anything worth telling the user about.
func (r *MigrationReport) Changed() bool {
	if r == nil {
		return false
	}
	return len(r.Remapped) > 0 || len(r.Ambiguous) > 0 || len(r.Orphans) > 0
}

// Lines renders the report as human-readable log lines (empty when there is
// nothing to say). Used for both the CLI's stdout output and the GUI's live
// log, so the two never drift apart.
func (r *MigrationReport) Lines() []string {
	if !r.Changed() {
		return nil
	}

	relocated := 0
	for _, remap := range r.Remapped {
		if remap.Relocated {
			relocated++
		}
	}

	lines := []string{
		fmt.Sprintf("Sync history key scheme changed (manifest schema_version %d, current %d): %d of %d stored entries no longer match the current folder layout.",
			r.LoadedSchemaVersion, ManifestSchemaVersion, r.StoredKeys-r.MatchedKeys, r.StoredKeys),
	}
	if len(r.Remapped) > 0 {
		lines = append(lines, fmt.Sprintf("  Re-keyed %d entries to the new layout (%d already-downloaded files moved to their new location, so they are not re-downloaded).",
			len(r.Remapped), relocated))
	}
	if len(r.Ambiguous) > 0 {
		lines = append(lines, fmt.Sprintf("  WARNING: %d stored entries could not be matched to exactly one current file and were left alone. Those files will be re-downloaded and the old copies stay where they are:", len(r.Ambiguous)))
		for _, key := range r.Ambiguous {
			lines = append(lines, "    unmatched history entry: "+key)
		}
	}
	if len(r.Orphans) > 0 {
		lines = append(lines, fmt.Sprintf("  WARNING: %d previously-downloaded files are left behind in their old locations. Nothing was deleted - move or delete them yourself if you don't want duplicates:", len(r.Orphans)))
		for _, orphanPath := range r.Orphans {
			lines = append(lines, "    orphaned file: "+orphanPath)
		}
	}
	return lines
}

// suspectKeySchemeChange reports whether the manifest looks like it was
// written under a different key scheme than the one in force now.
//
// The gate matters: the remap pass matches stale history entries to current
// files by file name, which is exactly right for a key-scheme change (the
// same file, re-keyed) but would be wrong for the ordinary case of one
// remote file disappearing while an unrelated new file with the same name
// appears. Requiring that the *overwhelming majority* of stored entries have
// gone stale keeps the pass off during normal syncs, where nearly every
// stored key still matches, and only lets it run in the situation it exists
// for - a wholesale re-keying such as flipping use_section_subfolders or
// PR #69's absolute-course-folder re-basing.
func suspectKeySchemeChange(stored, matched int) bool {
	if stored == 0 {
		return false
	}
	return matched*keySchemeOverlapDenominator < stored
}

// migrateManifestKeys detects a manifest written under an older/different
// key scheme and salvages what it can: stored entries that map unambiguously
// (by file name) onto a file the current scheme expects are re-keyed, and
// the already-downloaded copy is moved to its new location so it is skipped
// rather than re-downloaded. Anything ambiguous is left strictly alone and
// reported instead, and no file is ever deleted.
//
// Returns nil when there is nothing to migrate (the normal case for every
// ordinary sync).
func migrateManifestKeys(manifest *Manifest, cfg config.App, remoteFiles []scraper.RemoteFile) *MigrationReport {
	if manifest == nil || len(manifest.Files) == 0 {
		return nil
	}

	currentTargets := map[string]resolvedTarget{}
	for _, remoteFile := range remoteFiles {
		resolved := resolveRemoteTargetPath(cfg, remoteFile)
		currentTargets[filepath.ToSlash(resolved.ManifestKey)] = resolved
	}

	matched := 0
	for key := range manifest.Files {
		if _, ok := currentTargets[key]; ok {
			matched++
		}
	}

	report := &MigrationReport{
		LoadedSchemaVersion: manifest.SchemaVersion,
		StoredKeys:          len(manifest.Files),
		MatchedKeys:         matched,
	}

	if !suspectKeySchemeChange(report.StoredKeys, matched) {
		return nil
	}
	report.Attempted = true

	// Candidates on both sides: stored keys no current file claims, and
	// current keys with no stored history.
	staleByName := map[string][]string{}
	for key := range manifest.Files {
		if _, ok := currentTargets[key]; ok {
			continue
		}
		staleByName[path.Base(key)] = append(staleByName[path.Base(key)], key)
	}

	freshByName := map[string][]string{}
	for key := range currentTargets {
		if _, ok := manifest.Files[key]; ok {
			continue
		}
		freshByName[path.Base(key)] = append(freshByName[path.Base(key)], key)
	}

	names := make([]string, 0, len(staleByName))
	for name := range staleByName {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		stale := staleByName[name]
		fresh := freshByName[name]

		// Unambiguous only means exactly one stale entry and exactly one
		// current file share this file name. Anything else (a duplicated
		// file name on either side, or a stale entry with no current
		// counterpart at all) is reported, not guessed at.
		if len(stale) != 1 || len(fresh) != 1 {
			sort.Strings(stale)
			report.Ambiguous = append(report.Ambiguous, stale...)
			for _, key := range stale {
				if orphanPath, ok := existingOldPath(cfg, key); ok {
					report.Orphans = append(report.Orphans, displayPath(orphanPath))
				}
			}
			continue
		}

		oldKey, newKey := stale[0], fresh[0]
		record := manifest.Files[oldKey]
		delete(manifest.Files, oldKey)
		manifest.Files[newKey] = record

		remap := KeyRemap{OldKey: oldKey, NewKey: newKey, NewPath: currentTargets[newKey].LocalPath}
		oldPath, exists := existingOldPath(cfg, oldKey)
		remap.OldPath = oldPath
		switch {
		case !exists || oldPath == remap.NewPath:
			// Nothing on disk under the old layout (or it is already in the
			// right place): re-keying the history entry is all there is to
			// do. If the file is genuinely missing, processRemoteFiles' own
			// os.Stat check downloads it as usual.
		case fileExists(remap.NewPath):
			// A file is already sitting at the new location; moving onto it
			// would destroy whichever copy is the real one. Report instead.
			report.Orphans = append(report.Orphans, displayPath(oldPath))
		default:
			if err := relocateFile(oldPath, remap.NewPath); err != nil {
				report.Orphans = append(report.Orphans, displayPath(oldPath))
			} else {
				remap.Relocated = true
			}
		}

		report.Remapped = append(report.Remapped, remap)
	}

	sort.Strings(report.Ambiguous)
	sort.Strings(report.Orphans)
	sort.Slice(report.Remapped, func(i, j int) bool { return report.Remapped[i].OldKey < report.Remapped[j].OldKey })

	return report
}

// existingOldPath maps a stored (relative, slash-separated) manifest key back
// to where the downloaded file most likely sits on disk, reporting whether a
// file is actually there. It is only a best guess: the manifest records
// nothing but the relative key, so a file redirected by a
// subfolder_destinations override, or one written under a since-changed
// absolute course folder, is simply not found - in which case nothing is
// moved and nothing is claimed about it.
func existingOldPath(cfg config.App, key string) (string, bool) {
	candidate := filepath.Join(cfg.DownloadPath, filepath.FromSlash(key))
	return candidate, fileExists(candidate)
}

// displayPath makes a path absolute for user-facing output when it can;
// download_path is usually relative ("./downloads"), and a relative path is
// not much help when the message is "go find this file and delete it".
func displayPath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// relocateFile moves an already-downloaded file to its new location, falling
// back to copy-then-remove when the two sit on different volumes (common
// here: a subfolder_destinations override or an absolute course folder can
// point at another drive entirely, where os.Rename fails).
func relocateFile(oldPath, newPath string) error {
	if err := os.MkdirAll(filepath.Dir(newPath), 0o755); err != nil {
		return err
	}
	if err := os.Rename(oldPath, newPath); err == nil {
		return nil
	}

	data, err := os.ReadFile(oldPath)
	if err != nil {
		return err
	}
	if err := os.WriteFile(newPath, data, 0o644); err != nil {
		return err
	}
	return os.Remove(oldPath)
}
