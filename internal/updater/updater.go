// Package updater implements the T2 ("prompted, one-click apply") update
// mechanism described in docs/update-mechanism-plan.md Section 2.2: an
// unauthenticated check against the GitHub Releases API, a version
// comparison against the running binary's build version, and a
// download+checksum-verify of the setup.exe release asset.
//
// This package is a self-contained building block. It does not know about
// cmd/opal-downloader's buildVersion or internal/gui's HTTP handlers -
// callers pass the current version in explicitly (avoiding an import cycle
// with cmd/opal-downloader) and wire the results into UI themselves. See
// the Example in updater_test.go for the intended call pattern:
//
//	rel, err := updater.CheckLatest(ctx, buildVersion)
//	if err == nil && rel.IsNewer {
//	    // show "update available" banner using rel.Version / rel.HTMLURL
//	}
//	...
//	err = updater.Download(ctx, rel.AssetURL, destPath)
//	...
//	err = updater.VerifyChecksumFile(destPath, checksumBytes)
package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	// RepoOwner and RepoName identify the GitHub repository whose Releases
	// API is queried.
	RepoOwner = "alu-developer"
	RepoName  = "Opal_downloader"

	// AssetName is the unversioned installer asset name produced by
	// .github/workflows/release.yml. The workflow uploads it unversioned
	// specifically so the updater can always ask for this fixed name via
	// GET .../releases/latest without parsing the version back out of a
	// filename.
	AssetName = "opal-downloader-setup.exe"

	// ChecksumAssetName is the ".sha256" sidecar release.yml uploads
	// alongside AssetName. Its content is one line of plain
	// `Get-FileHash` output: "<lowercase-hex-sha256>  <filename>" (two
	// spaces, no trailing newline).
	ChecksumAssetName = AssetName + ".sha256"

	// defaultAPIBaseURL is the GitHub REST API base. Overridable per
	// Client for tests (see Client.BaseURL).
	defaultAPIBaseURL = "https://api.github.com"
)

// Release describes the latest GitHub release relevant to the update
// check: the parsed version, whether it's newer than the version passed
// to CheckLatest, and the download locations for the installer asset and
// its checksum sidecar (empty if the release doesn't publish an asset
// matching AssetName/ChecksumAssetName).
type Release struct {
	// TagName is the raw tag, e.g. "v0.2.0".
	TagName string
	// Version is TagName with a single leading "v" stripped, e.g. "0.2.0".
	Version string
	// HTMLURL is the release's GitHub web page (changelog link).
	HTMLURL string
	// IsNewer reports whether Version compares greater than the
	// currentVersion passed to CheckLatest.
	IsNewer bool

	// AssetURL is the browser_download_url of the AssetName asset, empty
	// if the release has no such asset.
	AssetURL string
	// AssetSize is the AssetName asset's size in bytes.
	AssetSize int64
	// ChecksumURL is the browser_download_url of the ChecksumAssetName
	// asset, empty if the release has no such asset.
	ChecksumURL string
}

// ghAsset mirrors the fields we need from a GitHub release asset object.
type ghAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// ghRelease mirrors the fields we need from the GitHub "get latest
// release" API response.
type ghRelease struct {
	TagName string    `json:"tag_name"`
	HTMLURL string    `json:"html_url"`
	Assets  []ghAsset `json:"assets"`
}

// Client is an updater API client. The zero value is ready to use and
// talks to the real GitHub API; tests set BaseURL to an httptest.Server
// URL to fake it.
type Client struct {
	// BaseURL overrides the GitHub API base (default
	// "https://api.github.com"). Only ever set in tests.
	BaseURL string

	// HTTPClient is the client used for requests. Defaults to
	// http.DefaultClient (TLS verification on, no custom transport) when
	// nil, per the plan's explicit requirement not to weaken transport
	// security for this check.
	HTTPClient *http.Client
}

func (c *Client) baseURL() string {
	if c.BaseURL != "" {
		return c.BaseURL
	}
	return defaultAPIBaseURL
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

// CheckLatest fetches the latest GitHub release for this repo and compares
// it against currentVersion (typically cmd/opal-downloader's buildVersion,
// e.g. "dev" or "0.2.0" or "v0.2.0" - a leading "v" is stripped from both
// sides before comparing). It performs a single unauthenticated GET to
// /repos/{owner}/{repo}/releases/latest using the stdlib net/http client.
func CheckLatest(ctx context.Context, currentVersion string) (Release, error) {
	return (&Client{}).CheckLatest(ctx, currentVersion)
}

// CheckLatest is the Client method version of the package-level
// CheckLatest function; see its docs.
func (c *Client) CheckLatest(ctx context.Context, currentVersion string) (Release, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/releases/latest", c.baseURL(), RepoOwner, RepoName)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Release{}, fmt.Errorf("updater: building request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("updater: fetching latest release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return Release{}, fmt.Errorf("updater: GET %s: unexpected status %d: %s", url, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var gh ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&gh); err != nil {
		return Release{}, fmt.Errorf("updater: decoding release JSON: %w", err)
	}

	rel := Release{
		TagName: gh.TagName,
		Version: strings.TrimPrefix(gh.TagName, "v"),
		HTMLURL: gh.HTMLURL,
	}

	for _, a := range gh.Assets {
		switch a.Name {
		case AssetName:
			rel.AssetURL = a.BrowserDownloadURL
			rel.AssetSize = a.Size
		case ChecksumAssetName:
			rel.ChecksumURL = a.BrowserDownloadURL
		}
	}

	newer, err := IsNewerVersion(rel.Version, currentVersion)
	if err != nil {
		// A malformed version string on either side shouldn't fail the
		// whole check - just report "not newer" so callers don't crash a
		// GUI landing page over an unparseable tag. The error is still
		// surfaced via the returned err for logging.
		return rel, fmt.Errorf("updater: comparing versions %q vs %q: %w", rel.Version, currentVersion, err)
	}
	rel.IsNewer = newer

	return rel, nil
}

// IsNewerVersion reports whether latest is a greater version than current.
// Both may have an optional leading "v" (stripped before comparing).
// Comparison is a minimal hand-rolled semver-ish scheme: split on ".",
// compare each numeric component left to right, treating a missing
// trailing component as 0 (so "1.2" == "1.2.0"). Non-numeric components
// are treated as 0 rather than erroring, except that a completely
// unparseable current version like "dev" (the default build value before
// -ldflags injects a real one) is reported as "no update available" -
// callers use the returned error to distinguish that from a genuine parse
// failure worth logging, but IsNewer will simply come back false.
func IsNewerVersion(latest, current string) (bool, error) {
	l := parseVersion(latest)
	c := parseVersion(current)

	// A completely unparseable current version (e.g. the "dev" default)
	// means we can't meaningfully compare - treat as "not newer" without
	// erroring the whole CheckLatest call, but let the caller know via a
	// sentinel-ish error string if they care to log it.
	if !isNumericVersion(current) {
		return false, fmt.Errorf("current version %q is not a parseable numeric version", current)
	}

	n := len(l)
	if len(c) > n {
		n = len(c)
	}
	for i := 0; i < n; i++ {
		var lv, cv int
		if i < len(l) {
			lv = l[i]
		}
		if i < len(c) {
			cv = c[i]
		}
		if lv != cv {
			return lv > cv, nil
		}
	}
	return false, nil
}

func isNumericVersion(v string) bool {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if v == "" {
		return false
	}
	for _, part := range strings.Split(v, ".") {
		if part == "" {
			return false
		}
		if _, err := strconv.Atoi(part); err != nil {
			return false
		}
	}
	return true
}

// parseVersion splits a "v1.2.3"-shaped string into its integer
// components, treating any non-numeric or empty component as 0.
func parseVersion(v string) []int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	parts := strings.Split(v, ".")
	out := make([]int, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			n = 0
		}
		out[i] = n
	}
	return out
}

// Download streams the resource at url (typically Release.AssetURL) to
// destPath, creating/truncating destPath and its parent directory as
// needed. On any failure, the partially-written destPath is removed. The
// download uses the Client's HTTP client (default http.DefaultClient).
func Download(ctx context.Context, url, destPath string) error {
	return (&Client{}).Download(ctx, url, destPath)
}

// Download is the Client method version of the package-level Download
// function; see its docs.
func (c *Client) Download(ctx context.Context, url, destPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("updater: building download request: %w", err)
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("updater: downloading %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("updater: GET %s: unexpected status %d: %s", url, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	if dir := filepath.Dir(destPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("updater: creating destination directory: %w", err)
		}
	}

	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("updater: creating %s: %w", destPath, err)
	}

	if _, err := io.Copy(out, resp.Body); err != nil {
		out.Close()
		os.Remove(destPath)
		return fmt.Errorf("updater: writing %s: %w", destPath, err)
	}

	if err := out.Close(); err != nil {
		os.Remove(destPath)
		return fmt.Errorf("updater: closing %s: %w", destPath, err)
	}

	return nil
}

// VerifyChecksum computes path's SHA-256 and compares it (case
// insensitively) against expectedSHA256, a bare lowercase-or-uppercase hex
// digest. It returns an error if the file can't be read or the digests
// don't match.
func VerifyChecksum(path, expectedSHA256 string) error {
	actual, err := sha256Hex(path)
	if err != nil {
		return err
	}

	expected := strings.ToLower(strings.TrimSpace(expectedSHA256))
	if actual != expected {
		return fmt.Errorf("updater: checksum mismatch for %s: expected %s, got %s", path, expected, actual)
	}
	return nil
}

// VerifyChecksumSidecar parses a ".sha256" sidecar file's content in the
// exact format release.yml produces - one line of plain PowerShell
// `Get-FileHash` output, "<lowercase-hex-sha256><space><space><filename>",
// no trailing newline - and verifies path's SHA-256 against it. The
// sidecar's filename field is not required to match filepath.Base(path);
// only the digest is checked, since the asset may have been downloaded to
// a temp file with a different name.
func VerifyChecksumSidecar(path string, sidecar []byte) error {
	digest, err := parseChecksumSidecar(sidecar)
	if err != nil {
		return err
	}
	return VerifyChecksum(path, digest)
}

// parseChecksumSidecar extracts the hex digest from a ".sha256" sidecar's
// raw content: "<hex>  <filename>" (release.yml writes this via
// `Get-FileHash | Out-File -NoNewline`, so there is normally no trailing
// newline, but a trailing newline is tolerated).
func parseChecksumSidecar(sidecar []byte) (string, error) {
	line := strings.TrimSpace(string(sidecar))
	if line == "" {
		return "", fmt.Errorf("updater: empty checksum sidecar")
	}
	// Split on the first run of whitespace; the standard sha256sum-style
	// format is "<hex>  <filename>" (two spaces) but a single space or
	// tab is accepted too.
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return "", fmt.Errorf("updater: could not parse checksum sidecar: %q", line)
	}
	digest := strings.ToLower(fields[0])
	if len(digest) != 64 {
		return "", fmt.Errorf("updater: checksum sidecar digest has unexpected length %d (want 64 hex chars): %q", len(digest), digest)
	}
	for _, r := range digest {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return "", fmt.Errorf("updater: checksum sidecar digest is not hex: %q", digest)
		}
	}
	return digest, nil
}

// sha256Hex computes the lowercase hex SHA-256 digest of the file at path.
func sha256Hex(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("updater: opening %s for checksum: %w", path, err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("updater: hashing %s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
