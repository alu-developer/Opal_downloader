package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// postSuggest runs the handler against a JSON body and returns the decoded
// response, failing the test if the transport contract is broken.
func postSuggest(t *testing.T, configPath string, body any) map[string]any {
	t.Helper()

	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encode request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/settings/suggest-folders", strings.NewReader(string(encoded)))
	rec := httptest.NewRecorder()
	handleSuggestFolders(configPath)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 so the page can render the message, got %d", rec.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("response was not valid JSON: %v (body %q)", err, rec.Body.String())
	}
	return payload
}

func suggestionsOf(t *testing.T, payload map[string]any) map[string]string {
	t.Helper()

	if msg, ok := payload["error"].(string); ok && msg != "" {
		t.Fatalf("unexpected error response: %s", msg)
	}
	raw, ok := payload["suggestions"].(map[string]any)
	if !ok {
		t.Fatalf("expected a suggestions object, got %v", payload)
	}
	out := map[string]string{}
	for k, v := range raw {
		s, ok := v.(string)
		if !ok {
			t.Fatalf("suggestion for %q was not a string: %v", k, v)
		}
		out[k] = s
	}
	return out
}

func TestSuggestFoldersRejectsGet(t *testing.T) {
	rec := httptest.NewRecorder()
	handleSuggestFolders("whatever.yaml")(rec, httptest.NewRequest(http.MethodGet, "/settings/suggest-folders", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for GET, got %d", rec.Code)
	}
}

// With no download path anywhere - neither typed into the form nor saved -
// there is nothing to search. That has to come back as a readable JSON
// message with HTTP 200, since it is an ordinary mid-setup state.
func TestSuggestFoldersWithoutDownloadPathExplainsItself(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.yaml")
	payload := postSuggest(t, missing, suggestFoldersRequest{
		Courses: []suggestCourseRow{{Name: "Analysis I"}},
	})

	msg, ok := payload["error"].(string)
	if !ok || msg == "" {
		t.Fatalf("expected a non-empty error field, got %v", payload)
	}
	if _, has := payload["suggestions"]; has {
		t.Fatal("an error response must not also carry suggestions")
	}
}

// The end-to-end case this feature exists for: abbreviated folder names, a
// "…/Downloads" leaf, and paths handed back relative to the download root so
// they mean the same thing config.yaml means by them.
func TestSuggestFoldersMatchesAbbreviatedFolders(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{
		filepath.Join("SoSe26", "AlgData", "Downloads"),
		filepath.Join("SoSe26", "NuMa"),
		filepath.Join("SoSe26", "Chemie"),
	} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	payload := postSuggest(t, filepath.Join(root, "config.yaml"), suggestFoldersRequest{
		DownloadPath: root,
		Courses: []suggestCourseRow{
			{Name: "Algorithmen und Datenstrukturen"},
			{Name: "Numerische Mathematik"},
		},
	})
	got := suggestionsOf(t, payload)

	want := map[string]string{
		"Algorithmen und Datenstrukturen": "SoSe26/AlgData/Downloads",
		"Numerische Mathematik":           "SoSe26/NuMa",
	}
	for course, wantPath := range want {
		if got[course] != wantPath {
			t.Errorf("suggestion for %q = %q, want %q", course, got[course], wantPath)
		}
	}
}

// A folder the user already typed is their answer, not a candidate: it must
// not be handed to a second course, and the course that has it must not be
// overwritten.
func TestSuggestFoldersLeavesFilledRowsAloneAndDoesNotReuseTheirFolders(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "AlgData"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	payload := postSuggest(t, filepath.Join(root, "config.yaml"), suggestFoldersRequest{
		DownloadPath: root,
		Courses: []suggestCourseRow{
			{Name: "Algorithmen und Datenstrukturen", Folder: "Woanders"},
			{Name: "Algorithmen und Datenstrukturen II"},
		},
	})
	got := suggestionsOf(t, payload)

	if _, has := got["Algorithmen und Datenstrukturen"]; has {
		t.Error("a course whose folder is already filled in must not be suggested for")
	}
	if p := got["Algorithmen und Datenstrukturen II"]; strings.Contains(p, "Woanders") {
		t.Errorf("suggested a folder that is already spoken for: %q", p)
	}
}

// Nothing plausible in the tree means no suggestion at all - the whole point
// of the confidence threshold. An empty field costs seconds; a wrong one
// silently files a course's downloads somewhere the user never looks.
func TestSuggestFoldersStaysSilentWhenNothingMatches(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Urlaubsfotos"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	payload := postSuggest(t, filepath.Join(root, "config.yaml"), suggestFoldersRequest{
		DownloadPath: root,
		Courses:      []suggestCourseRow{{Name: "Numerische Mathematik"}},
	})

	if got := suggestionsOf(t, payload); len(got) != 0 {
		t.Fatalf("expected no suggestions, got %v", got)
	}
}

func TestSettingsPageOffersFolderSuggestion(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("download_path: ./downloads\ncourses:\n  - Analysis\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	rec := httptest.NewRecorder()
	handleSettings(configPath)(rec, httptest.NewRequest(http.MethodGet, "/settings", nil))
	body := rec.Body.String()

	for _, want := range []string{
		`id="suggest-folders-btn"`,
		`id="suggest-folders-status"`,
		`/settings/suggest-folders`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("settings page is missing %q", want)
		}
	}
}
