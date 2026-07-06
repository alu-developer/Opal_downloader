package scraper

import "testing"

func TestConvertFileRefsToRemoteFilesV2(t *testing.T) {
	items := []FileRefV2{
		{
			CourseRepoID: "123",
			CourseTitle:  "Programmierung 1",
			SectionTitle: "Materialien",
			Name:         "Folien.pdf",
			URL:          "https://bildungsportal.sachsen.de/opal/goto.php?target=file_55&cmd=sendfile",
			Path:         "Programmierung 1/Folien.pdf",
		},
	}

	remoteFiles := convertFileRefsToRemoteFilesV2(items)
	if len(remoteFiles) != 1 {
		t.Fatalf("expected one remote file, got %d: %#v", len(remoteFiles), remoteFiles)
	}
	if remoteFiles[0].Name != "Folien.pdf" || remoteFiles[0].Course != "Programmierung 1" {
		t.Fatalf("unexpected remote file conversion: %#v", remoteFiles[0])
	}
	if remoteFiles[0].Path != "Programmierung 1/Folien.pdf" {
		t.Fatalf("unexpected remote file path: %#v", remoteFiles[0])
	}
}

func TestAppendUniqueRemoteFilesV2DeduplicatesByPathAndURL(t *testing.T) {
	existing := []RemoteFile{{
		Name:   "Folien.pdf",
		URL:    "https://bildungsportal.sachsen.de/opal/goto.php?target=file_55&cmd=sendfile",
		Course: "Programmierung 1",
		Path:   "Programmierung 1/Folien.pdf",
	}}
	seen := map[string]struct{}{
		"Programmierung 1/Folien.pdf|https://bildungsportal.sachsen.de/opal/goto.php?target=file_55&cmd=sendfile": {},
	}
	candidates := []RemoteFile{
		{
			Name:   "Folien.pdf",
			URL:    "https://bildungsportal.sachsen.de/opal/goto.php?target=file_55&cmd=sendfile",
			Course: "Programmierung 1",
			Path:   "Programmierung 1/Folien.pdf",
		},
		{
			Name:   "Uebung.pdf",
			URL:    "https://bildungsportal.sachsen.de/opal/goto.php?target=file_56&cmd=sendfile",
			Course: "Programmierung 1",
			Path:   "Programmierung 1/Uebung.pdf",
		},
	}

	files := appendUniqueRemoteFilesV2(existing, seen, candidates)
	if len(files) != 2 {
		t.Fatalf("expected one new file after dedupe, got %d: %#v", len(files), files)
	}
	if files[1].Name != "Uebung.pdf" {
		t.Fatalf("unexpected appended file: %#v", files[1])
	}
}

func TestOrchestratorSectionKeyDeduplicationByURLVariants(t *testing.T) {
	course := CourseRefV2{RepoID: "123", Title: "Algorithmen und Datenstrukturen"}
	sections := []SectionRefV2{
		{CourseRepoID: "123", Title: "Algorithmen und Datenstrukturen", URL: "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/123"},
		{CourseRepoID: "123", Title: "Algorithmen und Datenstrukturen", URL: "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/123?cmd=view&ref_id=123"},
		{CourseRepoID: "123", Title: "Materialien", URL: "https://bildungsportal.sachsen.de/opal/goto.php?target=fold_1234"},
	}

	seen := map[string]struct{}{}
	unique := make([]SectionRefV2, 0, len(sections))
	for _, section := range sections {
		key := sectionKeyV2(section.URL, course.RepoID)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, section)
	}

	if len(unique) != 2 {
		t.Fatalf("expected two unique sections after key-based dedupe, got %d: %#v", len(unique), unique)
	}
}
