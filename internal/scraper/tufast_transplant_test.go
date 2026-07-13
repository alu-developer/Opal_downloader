package scraper

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeFakeProfile creates <dir>/<profile>/Preferences so requireRealProfileDir
// treats it as a real Chromium profile.
func makeFakeProfile(t *testing.T, userDataDir, profile string) string {
	t.Helper()
	profileDir := filepath.Join(userDataDir, profile)
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profileDir, "Preferences"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("WriteFile Preferences: %v", err)
	}
	return profileDir
}

// installFakeTUFastExtension marks the extension as "installed" in
// profileDir (only what requireRealProfileDir/TransplantTUFastLoginData's
// target check actually looks at - the Extensions/<id> directory - not a
// real CRX).
func installFakeTUFastExtension(t *testing.T, profileDir string) {
	t.Helper()
	extDir := filepath.Join(profileDir, "Extensions", TUFastExtensionID, "8.3.0.0_0")
	if err := os.MkdirAll(extDir, 0o755); err != nil {
		t.Fatalf("MkdirAll extension dir: %v", err)
	}
}

// writeFakeTUFastData writes a small file into
// <profileDir>/Local Extension Settings/<id>/ standing in for TU-Fast's real
// leveldb storage - this test only cares that TransplantTUFastLoginData
// copies file *content* correctly, not that it's a valid leveldb.
func writeFakeTUFastData(t *testing.T, profileDir, content string) {
	t.Helper()
	dataDir := filepath.Join(profileDir, "Local Extension Settings", TUFastExtensionID)
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("MkdirAll data dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "000003.log"), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile fake data: %v", err)
	}
}

func TestTransplantTUFastLoginData_Success(t *testing.T) {
	sourceRoot := t.TempDir()
	targetRoot := t.TempDir()

	sourceProfile := makeFakeProfile(t, sourceRoot, "Default")
	installFakeTUFastExtension(t, sourceProfile)
	writeFakeTUFastData(t, sourceProfile, "source-login-data")

	targetProfile := makeFakeProfile(t, targetRoot, "Default")
	installFakeTUFastExtension(t, targetProfile)

	result, err := TransplantTUFastLoginData(sourceRoot, "Default", targetRoot, "Default")
	if err != nil {
		t.Fatalf("TransplantTUFastLoginData: %v", err)
	}
	if len(result.CopiedDirs) == 0 {
		t.Fatalf("expected at least one copied dir, got none")
	}
	if len(result.BackedUpDirs) != 0 {
		t.Fatalf("expected no backups (target had no prior data), got %v", result.BackedUpDirs)
	}

	copied, err := os.ReadFile(filepath.Join(targetProfile, "Local Extension Settings", TUFastExtensionID, "000003.log"))
	if err != nil {
		t.Fatalf("reading copied file: %v", err)
	}
	if string(copied) != "source-login-data" {
		t.Fatalf("copied content = %q, want %q", copied, "source-login-data")
	}

	// The target's own Preferences (its self-consistent, genuinely-installed
	// install record) must be untouched - the whole point of this narrower
	// copy vs. the known-broken whole-profile copy.
	prefs, err := os.ReadFile(filepath.Join(targetProfile, "Preferences"))
	if err != nil {
		t.Fatalf("reading target Preferences: %v", err)
	}
	if string(prefs) != "{}" {
		t.Fatalf("target Preferences was modified: %q", prefs)
	}
}

func TestTransplantTUFastLoginData_BacksUpExistingTargetData(t *testing.T) {
	sourceRoot := t.TempDir()
	targetRoot := t.TempDir()

	sourceProfile := makeFakeProfile(t, sourceRoot, "Default")
	installFakeTUFastExtension(t, sourceProfile)
	writeFakeTUFastData(t, sourceProfile, "new-login-data")

	targetProfile := makeFakeProfile(t, targetRoot, "Default")
	installFakeTUFastExtension(t, targetProfile)
	writeFakeTUFastData(t, targetProfile, "old-login-data")

	result, err := TransplantTUFastLoginData(sourceRoot, "Default", targetRoot, "Default")
	if err != nil {
		t.Fatalf("TransplantTUFastLoginData: %v", err)
	}
	if len(result.BackedUpDirs) != 1 {
		t.Fatalf("expected exactly one backup, got %v", result.BackedUpDirs)
	}

	backed, err := os.ReadFile(filepath.Join(result.BackedUpDirs[0], "000003.log"))
	if err != nil {
		t.Fatalf("reading backed-up file: %v", err)
	}
	if string(backed) != "old-login-data" {
		t.Fatalf("backed-up content = %q, want %q", backed, "old-login-data")
	}

	current, err := os.ReadFile(filepath.Join(targetProfile, "Local Extension Settings", TUFastExtensionID, "000003.log"))
	if err != nil {
		t.Fatalf("reading current target file: %v", err)
	}
	if string(current) != "new-login-data" {
		t.Fatalf("current content = %q, want %q", current, "new-login-data")
	}
}

func TestTransplantTUFastLoginData_TargetMissingExtension(t *testing.T) {
	sourceRoot := t.TempDir()
	targetRoot := t.TempDir()

	sourceProfile := makeFakeProfile(t, sourceRoot, "Default")
	installFakeTUFastExtension(t, sourceProfile)
	writeFakeTUFastData(t, sourceProfile, "source-login-data")

	makeFakeProfile(t, targetRoot, "Default") // no TU-Fast installed

	_, err := TransplantTUFastLoginData(sourceRoot, "Default", targetRoot, "Default")
	if err == nil {
		t.Fatal("expected error when target has no TU-Fast install, got nil")
	}
	if !strings.Contains(err.Error(), "isn't installed") {
		t.Fatalf("error = %q, want mention of TU-Fast not being installed", err)
	}
}

func TestTransplantTUFastLoginData_SourceHasNoData(t *testing.T) {
	sourceRoot := t.TempDir()
	targetRoot := t.TempDir()

	makeFakeProfile(t, sourceRoot, "Default") // no TU-Fast data at all

	targetProfile := makeFakeProfile(t, targetRoot, "Default")
	installFakeTUFastExtension(t, targetProfile)

	_, err := TransplantTUFastLoginData(sourceRoot, "Default", targetRoot, "Default")
	if err == nil {
		t.Fatal("expected error when source has no TU-Fast data, got nil")
	}
	if !strings.Contains(err.Error(), "no TU-Fast login data found") {
		t.Fatalf("error = %q, want mention of no TU-Fast data found", err)
	}
}

func TestTransplantTUFastLoginData_SameProfileRejected(t *testing.T) {
	root := t.TempDir()
	profile := makeFakeProfile(t, root, "Default")
	installFakeTUFastExtension(t, profile)
	writeFakeTUFastData(t, profile, "data")

	_, err := TransplantTUFastLoginData(root, "Default", root, "Default")
	if err == nil {
		t.Fatal("expected error when source and target are the same profile, got nil")
	}
	if !strings.Contains(err.Error(), "same browser profile") {
		t.Fatalf("error = %q, want mention of same browser profile", err)
	}
}

func TestTransplantTUFastLoginData_MissingTargetConfig(t *testing.T) {
	sourceRoot := t.TempDir()
	sourceProfile := makeFakeProfile(t, sourceRoot, "Default")
	installFakeTUFastExtension(t, sourceProfile)
	writeFakeTUFastData(t, sourceProfile, "data")

	_, err := TransplantTUFastLoginData(sourceRoot, "Default", "", "Default")
	if err == nil {
		t.Fatal("expected error when target user data dir is empty, got nil")
	}
}
