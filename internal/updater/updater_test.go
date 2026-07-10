package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestIsNewerVersion(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
		wantErr         bool
	}{
		{"0.2.0", "0.1.0", true, false},
		{"v0.2.0", "v0.1.0", true, false},
		{"0.1.0", "0.2.0", false, false},
		{"0.2.0", "0.2.0", false, false},
		{"1.0.0", "0.9.9", true, false},
		{"1.2", "1.2.0", false, false},
		{"1.2.1", "1.2", true, false},
		{"0.2.0", "dev", false, true}, // unparseable current: no update reported, but error surfaced
	}

	for _, tc := range cases {
		got, err := IsNewerVersion(tc.latest, tc.current)
		if (err != nil) != tc.wantErr {
			t.Errorf("IsNewerVersion(%q, %q) error = %v, wantErr %v", tc.latest, tc.current, err, tc.wantErr)
			continue
		}
		if got != tc.want {
			t.Errorf("IsNewerVersion(%q, %q) = %v, want %v", tc.latest, tc.current, got, tc.want)
		}
	}
}

func TestCheckLatest(t *testing.T) {
	const wantExeURL = "https://github.example/releases/download/v0.2.0/opal-downloader-setup.exe"
	const wantChecksumURL = wantExeURL + ".sha256"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/repos/alu-developer/Opal_downloader/releases/latest"; got != want {
			t.Errorf("unexpected path %q, want %q", got, want)
		}
		resp := map[string]interface{}{
			"tag_name": "v0.2.0",
			"html_url": "https://github.example/releases/tag/v0.2.0",
			"assets": []map[string]interface{}{
				{
					"name":                 AssetName,
					"browser_download_url": wantExeURL,
					"size":                 12345,
				},
				{
					"name":                 ChecksumAssetName,
					"browser_download_url": wantChecksumURL,
					"size":                 71,
				},
				{
					"name":                 "some-other-asset.txt",
					"browser_download_url": "https://github.example/other",
					"size":                 1,
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL}
	rel, err := c.CheckLatest(context.Background(), "0.1.0")
	if err != nil {
		t.Fatalf("CheckLatest: %v", err)
	}

	if rel.TagName != "v0.2.0" {
		t.Errorf("TagName = %q, want v0.2.0", rel.TagName)
	}
	if rel.Version != "0.2.0" {
		t.Errorf("Version = %q, want 0.2.0", rel.Version)
	}
	if !rel.IsNewer {
		t.Errorf("IsNewer = false, want true")
	}
	if rel.AssetURL != wantExeURL {
		t.Errorf("AssetURL = %q, want %q", rel.AssetURL, wantExeURL)
	}
	if rel.AssetSize != 12345 {
		t.Errorf("AssetSize = %d, want 12345", rel.AssetSize)
	}
	if rel.ChecksumURL != wantChecksumURL {
		t.Errorf("ChecksumURL = %q, want %q", rel.ChecksumURL, wantChecksumURL)
	}
	if rel.HTMLURL == "" {
		t.Errorf("HTMLURL is empty")
	}
}

func TestCheckLatest_NoUpdate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"tag_name": "v0.1.0",
			"html_url": "https://github.example/releases/tag/v0.1.0",
			"assets":   []map[string]interface{}{},
		})
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL}
	rel, err := c.CheckLatest(context.Background(), "0.1.0")
	if err != nil {
		t.Fatalf("CheckLatest: %v", err)
	}
	if rel.IsNewer {
		t.Errorf("IsNewer = true, want false (same version)")
	}
	if rel.AssetURL != "" || rel.ChecksumURL != "" {
		t.Errorf("expected empty asset URLs when release has no matching assets, got AssetURL=%q ChecksumURL=%q", rel.AssetURL, rel.ChecksumURL)
	}
}

func TestCheckLatest_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL}
	if _, err := c.CheckLatest(context.Background(), "0.1.0"); err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
}

func TestDownload(t *testing.T) {
	const content = "fake installer bytes"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, content)
	}))
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "nested", "opal-downloader-setup.exe")

	c := &Client{}
	if err := c.Download(context.Background(), srv.URL, dest); err != nil {
		t.Fatalf("Download: %v", err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("reading downloaded file: %v", err)
	}
	if string(got) != content {
		t.Errorf("downloaded content = %q, want %q", got, content)
	}
}

func TestDownload_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "out.exe")
	if err := Download(context.Background(), srv.URL, dest); err == nil {
		t.Fatal("expected error for 404 response, got nil")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Errorf("expected dest file not to exist after failed download, stat err = %v", err)
	}
}

func TestVerifyChecksum(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.bin")
	content := []byte("hello world")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}

	sum := sha256.Sum256(content)
	hexSum := hex.EncodeToString(sum[:])

	if err := VerifyChecksum(path, hexSum); err != nil {
		t.Errorf("VerifyChecksum with correct digest: %v", err)
	}

	// Case-insensitivity.
	if err := VerifyChecksum(path, fmt.Sprintf("%X", sum)); err != nil {
		t.Errorf("VerifyChecksum with uppercase digest: %v", err)
	}

	if err := VerifyChecksum(path, "0000000000000000000000000000000000000000000000000000000000000000"); err == nil {
		t.Error("expected error for wrong-length digest, got nil")
	}

	wrongSum := sha256.Sum256([]byte("goodbye world"))
	if err := VerifyChecksum(path, hex.EncodeToString(wrongSum[:])); err == nil {
		t.Error("expected error for mismatched digest, got nil")
	}
}

func TestVerifyChecksumSidecar(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opal-downloader-setup.exe")
	content := []byte("pretend this is the installer")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}

	sum := sha256.Sum256(content)
	hexSum := hex.EncodeToString(sum[:])

	// Exactly release.yml's format: lowercase hex, two spaces, filename,
	// no trailing newline (Out-File -NoNewline).
	sidecar := []byte(hexSum + "  " + AssetName)

	if err := VerifyChecksumSidecar(path, sidecar); err != nil {
		t.Errorf("VerifyChecksumSidecar: %v", err)
	}

	// Tolerate a trailing newline too, in case that ever changes.
	sidecarWithNewline := append(append([]byte{}, sidecar...), '\n')
	if err := VerifyChecksumSidecar(path, sidecarWithNewline); err != nil {
		t.Errorf("VerifyChecksumSidecar with trailing newline: %v", err)
	}

	badSidecar := []byte("not-a-hex-digest  " + AssetName)
	if err := VerifyChecksumSidecar(path, badSidecar); err == nil {
		t.Error("expected error for non-hex digest in sidecar, got nil")
	}

	if err := VerifyChecksumSidecar(path, []byte("")); err == nil {
		t.Error("expected error for empty sidecar, got nil")
	}
}

// Example demonstrates the intended call pattern from a caller like
// internal/gui's update banner (not wired in by this package).
func Example() {
	ctx := context.Background()
	currentVersion := "0.1.0" // would be cmd/opal-downloader's buildVersion

	rel, err := CheckLatest(ctx, currentVersion)
	if err != nil {
		return
	}
	if !rel.IsNewer {
		return
	}

	destPath := filepath.Join(os.TempDir(), AssetName)
	if err := Download(ctx, rel.AssetURL, destPath); err != nil {
		return
	}
	_ = VerifyChecksum(destPath, "" /* fetched from rel.ChecksumURL and parsed with VerifyChecksumSidecar */)
}
