package gui

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/alu-developer/opal-downloader/internal/config"
	"github.com/alu-developer/opal-downloader/internal/foldersuggest"
)

// suggestFoldersRequest is what the settings page posts: the download root as
// currently typed in the form (not necessarily the saved one - the user may
// be setting both up in the same visit) plus the course table as it stands.
type suggestFoldersRequest struct {
	DownloadPath string             `json:"download_path"`
	Courses      []suggestCourseRow `json:"courses"`
}

type suggestCourseRow struct {
	Name   string `json:"name"`
	Folder string `json:"folder"`
}

// handleSuggestFolders backs the settings page's "Suggest folders" button. It
// scans the download root for folders that already look like coursework
// destinations and proposes one per course whose folder field is still empty.
//
// Nothing is saved and nothing on disk is touched: the response only fills in
// form fields the user still has to review and submit. Courses with no
// confident match are simply absent from the response - see foldersuggest,
// where a wrong guess is treated as strictly worse than no guess.
//
// Suggested paths are relative to the download root wherever possible.
// course_folders entries are interpreted relative to download_path, and an
// absolute one changes how manifest keys are derived (see
// syncer.resolveCourseSubfolderBase), so a suggestion should not casually
// hand out absolute paths for folders that sit under the root anyway.
//
// Always responds 200 with either "suggestions" or "error", matching
// handleDiscoverCourses: the common failures here (no download path yet, path
// does not exist) are ordinary setup states the page shows inline, not
// transport faults.
func handleSuggestFolders(configPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		writeErr := func(msg string) {
			_ = json.NewEncoder(w).Encode(map[string]any{"error": msg})
		}

		var req suggestFoldersRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr("Could not read the form: " + err.Error())
			return
		}

		root := strings.TrimSpace(req.DownloadPath)
		if root == "" {
			// Fall back to the saved config so the button also works on a
			// page the user has not edited in this visit.
			if loaded, err := config.Load(configPath); err == nil {
				root = strings.TrimSpace(loaded.App.DownloadPath)
			}
		}
		if root == "" {
			writeErr("Set a download path first - that is the folder tree this searches.")
			return
		}

		candidates, err := foldersuggest.Scan(root)
		if err != nil {
			writeErr("Could not search your download folder: " + err.Error())
			return
		}

		var wanted []string
		var taken []string
		for _, row := range req.Courses {
			name := strings.TrimSpace(row.Name)
			if name == "" {
				continue
			}
			if folder := strings.TrimSpace(row.Folder); folder != "" {
				// Already answered by hand: keep the folder off the table for
				// everyone else rather than proposing it twice.
				taken = append(taken, absoluteUnder(root, folder))
				continue
			}
			wanted = append(wanted, name)
		}
		if len(wanted) == 0 {
			_ = json.NewEncoder(w).Encode(map[string]any{"suggestions": map[string]string{}})
			return
		}

		suggestions := foldersuggest.Suggest(wanted, candidates, taken)

		relative := make(map[string]string, len(suggestions))
		for course, path := range suggestions {
			relative[course] = relativeTo(root, path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"suggestions": relative})
	}
}

// absoluteUnder resolves a configured course folder the way the syncer does:
// relative to the download root unless it is already absolute.
func absoluteUnder(root, folder string) string {
	if filepath.IsAbs(folder) {
		return folder
	}
	return filepath.Join(root, filepath.FromSlash(folder))
}

// relativeTo expresses path relative to root, falling back to the absolute
// path when it cannot (a different drive, say). Forward slashes are used
// because that is how course_folders is written in config.example.yaml and
// how the existing rows render.
func relativeTo(root, path string) string {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return path
	}
	rel, err := filepath.Rel(absRoot, path)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return path
	}
	return filepath.ToSlash(rel)
}
