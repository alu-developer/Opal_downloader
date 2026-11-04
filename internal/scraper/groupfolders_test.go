package scraper

import "testing"

// liveCourseGroupsTreeHTML is the groups payload the maintainer's "Englisch
// WiSe26/27" course page carried on 2026-11-04, verbatim - including the
// non-breaking spaces OPAL writes into the label and the parent "Gruppen"
// node whose href shares the BusinessGroup path shape but names "Overview"
// instead of a group id. Both are the reason this parser is not a one-line
// regex.
const liveCourseGroupsTreeHTML = `<script>var initial_data=[{"id":"idgroups","text":"<span class=\"jstree-title\">Gruppen</span>","icon":"fonticon icon-group","children":[{"id":"id56199544905","text":"<span class=\"jstree-title\">Englisch&nbsp;C1.1&nbsp;-&nbsp;Professional Mobility: Project Development and Administration&nbsp;Gruppe 1</span>","parent":"idgroups","icon":"fonticon icon-group","children":true,"a_attr":{"href":"https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/56183324688/BusinessGroup/56199544905","title":"Englisch C1.1 - Professional Mobility: Project Development and Administration Gruppe 1"}}],"a_attr":{"href":"https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/56183324688/BusinessGroup/Overview","title":"Gruppen"}}];</script>`

// liveGroupFolderHTML is an excerpt of that group's Ordner, captured the same
// day. Shape-identical to a course section's file table - which is the point:
// the existing parser, the size/date columns and the download path all work
// on it unchanged. The "&amp;" in the fourth file's name is real and is why
// unescapeHTMLText exists.
const liveGroupFolderHTML = `<table>
<tr>
	<td class="table-check"><input type="checkbox" value="check5"/></td>
	<td><span class="fonticon docx" title="Worddatei"></span></td>
	<td>
		<a id="id81e2" href="https://bildungsportal.sachsen.de/opal/auth/BusinessGroup/56199544905/1.+Project+Mod+Version+Presentation+Essentials.docx" data-file-name="1. Project Mod Version Presentation Essentials.docx">
			<span>1. Project Mod Version Presentation Essentials.docx</span>
		</a>
	</td>
	<td><span>19,5KB</span></td>
	<td><span>am 29.10.2026 um 11:24 Uhr</span></td>
</tr>
<tr>
	<td><a id="id81e5" href="https://bildungsportal.sachsen.de/opal/auth/BusinessGroup/56199544905/Proj+Dev+%26+Admin+-+DESCRIPTION+26.docx" data-file-name="Proj Dev &amp; Admin - DESCRIPTION 26.docx"><span>Proj Dev &amp; Admin - DESCRIPTION 26.docx</span></a></td>
	<td><span>17,2KB</span></td>
	<td><span>am 29.10.2026 um 11:24 Uhr</span></td>
</tr>
</table>`

func TestParseCourseGroupNodesReadsGroupsOffCoursePage(t *testing.T) {
	groups := ParseCourseGroupNodes(liveCourseGroupsTreeHTML)
	if len(groups) != 1 {
		t.Fatalf("got %d groups, want 1 (the Overview parent must not count): %+v", len(groups), groups)
	}
	got := groups[0]
	if got.ID != "56199544905" {
		t.Errorf("ID = %q, want %q", got.ID, "56199544905")
	}
	if got.URL != "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/56183324688/BusinessGroup/56199544905" {
		t.Errorf("URL = %q", got.URL)
	}
	// Non-breaking spaces must not survive into a title that becomes a
	// directory name and a config pattern - see groupNodeTitle.
	want := "Englisch C1.1 - Professional Mobility: Project Development and Administration Gruppe 1"
	if got.Title != want {
		t.Errorf("Title = %q, want %q", got.Title, want)
	}
}

func TestParseCourseGroupNodesIgnoresPageWithoutGroups(t *testing.T) {
	if groups := ParseCourseGroupNodes(liveSectionRowHTML); groups != nil {
		t.Fatalf("got %+v, want nil for a page with no groups payload", groups)
	}
}

func TestBusinessGroupIDFromHref(t *testing.T) {
	cases := []struct {
		href   string
		wantID string
		wantOK bool
	}{
		{"https://x/opal/auth/RepositoryEntry/1/BusinessGroup/56199544905", "56199544905", true},
		{"https://x/opal/auth/BusinessGroup/56199544905", "56199544905", true},
		{"https://x/opal/auth/BusinessGroup/56199544905/menu/MENU_FOLDER", "56199544905", true},
		{"https://x/opal/auth/RepositoryEntry/1/BusinessGroup/Overview", "", false},
		{"https://x/opal/auth/RepositoryEntry/1/CourseNode/2", "", false},
		{"https://x/opal/auth/BusinessGroup/", "", false},
	}
	for _, c := range cases {
		gotID, gotOK := businessGroupIDFromHref(c.href)
		if gotID != c.wantID || gotOK != c.wantOK {
			t.Errorf("businessGroupIDFromHref(%q) = (%q, %v), want (%q, %v)", c.href, gotID, gotOK, c.wantID, c.wantOK)
		}
	}
}

func TestGroupFolderURL(t *testing.T) {
	cases := map[string]string{
		"https://x/opal/auth/RepositoryEntry/1/BusinessGroup/2":  "https://x/opal/auth/RepositoryEntry/1/BusinessGroup/2/menu/MENU_FOLDER",
		"https://x/opal/auth/RepositoryEntry/1/BusinessGroup/2/": "https://x/opal/auth/RepositoryEntry/1/BusinessGroup/2/menu/MENU_FOLDER",
	}
	for in, want := range cases {
		if got := groupFolderURL(in); got != want {
			t.Errorf("groupFolderURL(%q) = %q, want %q", in, got, want)
		}
	}
}

// A group Ordner must produce ordinary FileRefs - attributed to the course,
// titled by the group, and carrying the size/date signals that keep
// syncer.fileChanged from re-downloading them on every run.
func TestGroupFolderFilesCarryCourseSectionAndChangeSignals(t *testing.T) {
	folderURL := "https://bildungsportal.sachsen.de/opal/auth/BusinessGroup/56199544905/menu/MENU_FOLDER"
	course := CourseRef{RepoID: "56183324688", Title: "Englisch WiSe26/27"}
	section := SectionRef{CourseRepoID: "56183324688", Title: "Englisch C1.1 - Professional Mobility: Project Development and Administration Gruppe 1", URL: folderURL}

	files := appendSectionFiles(nil, map[string]struct{}{}, parseHTTPSectionCandidates(liveGroupFolderHTML), course, section, folderURL, "", "", false, testOpalURL, nil)
	if len(files) != 2 {
		t.Fatalf("got %d files, want 2: %+v", len(files), fileNamesFromHTTPFiles(files))
	}
	for _, f := range files {
		if f.CourseTitle != "Englisch WiSe26/27" {
			t.Errorf("%s: CourseTitle = %q, want the related course's title", f.Name, f.CourseTitle)
		}
		if f.SectionTitle != section.Title {
			t.Errorf("%s: SectionTitle = %q, want the group's label", f.Name, f.SectionTitle)
		}
		if f.Size == nil || f.Modified == nil {
			t.Errorf("%s: Size=%v Modified=%v, want both parsed from the Ordner's own columns", f.Name, f.Size, f.Modified)
		}
	}
}

// The "&" regression: without unescapeHTMLText this file was discovered as
// "Proj Dev &amp; Admin - DESCRIPTION 26.docx" and saved under that literal
// name, next to any correctly-named copy already on disk.
func TestGroupFolderUnescapesAmpersandInFileName(t *testing.T) {
	folderURL := "https://bildungsportal.sachsen.de/opal/auth/BusinessGroup/56199544905/menu/MENU_FOLDER"
	course := CourseRef{RepoID: "56183324688", Title: "Englisch WiSe26/27"}

	files := appendSectionFiles(nil, map[string]struct{}{}, parseHTTPSectionCandidates(liveGroupFolderHTML), course, SectionRef{CourseRepoID: "56183324688"}, folderURL, "", "", false, testOpalURL, nil)

	want := "Proj Dev & Admin - DESCRIPTION 26.docx"
	for _, f := range files {
		if f.Name == want {
			return
		}
	}
	t.Fatalf("no file named %q among %+v", want, fileNamesFromHTTPFiles(files))
}
