package scraper

import "testing"

// TestIsNonFileSectionType uses fixture candidate maps shaped like the real
// className values captured live from this account's real OPAL courses
// (queue task skip-non-file-sections-by-structural-marker) - see
// nonFileSectionTypeClasses's doc comment for the live investigation.
func TestIsNonFileSectionType(t *testing.T) {
	tests := []struct {
		name      string
		candidate map[string]string
		want      bool
	}{
		{
			name: "true positive: real Einschreibung enrollment node-en class",
			candidate: map[string]string{
				"text":      "Einschreibung in die Übungsgruppen",
				"className": "jstree-anchor  node-en",
			},
			want: true,
		},
		{
			name: "true positive: single-word Einschreibung label, node-en class",
			candidate: map[string]string{
				"text":      "Einschreibung",
				"className": "jstree-anchor node-en",
			},
			want: true,
		},
		{
			name: "true negative: real content folder, node-bc class",
			candidate: map[string]string{
				"text":      "Übungsblätter",
				"className": "jstree-anchor node-bc",
			},
			want: false,
		},
		{
			name: "true negative: real content structure node, node-st class",
			candidate: map[string]string{
				"text":      "Materialien",
				"className": "jstree-anchor node-st",
			},
			want: false,
		},
		{
			name: "no className at all",
			candidate: map[string]string{
				"text": "Einschreibung",
			},
			want: false,
		},
		{
			name: "className present but empty",
			candidate: map[string]string{
				"text":      "Einschreibung",
				"className": "",
			},
			want: false,
		},
		{
			name: "substring collision guard: node-enX must not match as node-en",
			candidate: map[string]string{
				"text":      "Not really enrollment",
				"className": "jstree-anchor node-enX",
			},
			want: false,
		},
		{
			name: "other non-file admin node types are not (yet) classified - see doc comment",
			candidate: map[string]string{
				"text":      "Forum",
				"className": "jstree-anchor node-fo",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isNonFileSectionType(tt.candidate); got != tt.want {
				t.Fatalf("isNonFileSectionType(%#v) = %v, want %v", tt.candidate, got, tt.want)
			}
		})
	}
}

// TestAppendSectionFolderTargetsSkipsEnrollmentSectionsWhenEnabled covers the
// crux of the acceptance criteria: a candidate carrying the real, live-confirmed
// node-en class is left out of the crawl queue and reported via the returned
// []skippedSection (auditable), while a real content folder alongside it is
// still queued normally.
func TestAppendSectionFolderTargetsSkipsEnrollmentSectionsWhenEnabled(t *testing.T) {
	repoID := "50696421377"
	currentURL := "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/50696421377"
	courseRootURL := currentURL
	enrollmentURL := "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/50696421377/CourseNode/1709609352182565009"
	materialURL := "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/50696421377/CourseNode/1775529461522481011"
	candidates := []map[string]string{
		{
			"href":      enrollmentURL,
			"title":     "Einschreibung in die Übungsgruppen",
			"text":      "Einschreibung in die Übungsgruppen",
			"rootText":  "Einschreibung in die Übungsgruppen",
			"className": "jstree-anchor  node-en",
		},
		{
			"href":      materialURL,
			"title":     "Material",
			"text":      "Material",
			"rootText":  "Material",
			"className": "jstree-anchor node-bc",
		},
	}

	queue, skipped := appendSectionFolderTargets(nil, map[string]struct{}{}, map[string]struct{}{}, candidates, "https://bildungsportal.sachsen.de/opal/", repoID, currentURL, courseRootURL, "Some Course", map[string]string{}, true)

	if len(queue) != 1 || queue[0] != materialURL {
		t.Fatalf("expected only the real content folder to be queued, got queue=%#v", queue)
	}
	if len(skipped) != 1 {
		t.Fatalf("expected exactly one skipped section reported, got %d: %#v", len(skipped), skipped)
	}
	if skipped[0].URL != enrollmentURL {
		t.Fatalf("expected the enrollment section to be reported skipped, got %#v", skipped[0])
	}
	if skipped[0].Title != "Einschreibung in die Übungsgruppen" {
		t.Fatalf("expected the skipped section's title to be recorded for the audit log, got %q", skipped[0].Title)
	}
}

// TestAppendSectionFolderTargetsSkipDisabledByFlag is the escape-hatch test:
// with skipNonFileSections=false, the pre-existing behavior (visit every
// discovered section, including Einschreibung) is preserved exactly, and
// nothing is ever reported as skipped.
func TestAppendSectionFolderTargetsSkipDisabledByFlag(t *testing.T) {
	repoID := "50696421377"
	currentURL := "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/50696421377"
	courseRootURL := currentURL
	enrollmentURL := "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/50696421377/CourseNode/1709609352182565009"
	candidates := []map[string]string{
		{
			"href":      enrollmentURL,
			"title":     "Einschreibung in die Übungsgruppen",
			"text":      "Einschreibung in die Übungsgruppen",
			"rootText":  "Einschreibung in die Übungsgruppen",
			"className": "jstree-anchor  node-en",
		},
	}

	queue, skipped := appendSectionFolderTargets(nil, map[string]struct{}{}, map[string]struct{}{}, candidates, "https://bildungsportal.sachsen.de/opal/", repoID, currentURL, courseRootURL, "Some Course", map[string]string{}, false)

	if len(queue) != 1 || queue[0] != enrollmentURL {
		t.Fatalf("expected the enrollment section to still be queued when skipping is disabled, got queue=%#v", queue)
	}
	if len(skipped) != 0 {
		t.Fatalf("expected nothing reported as skipped when skipNonFileSections is false, got %#v", skipped)
	}
}
