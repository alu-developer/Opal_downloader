package scraper

import (
	"encoding/json"
	"strings"

	"github.com/alu-developer/opal-downloader/internal/logging"
)

// This file reaches the files that live in a learning group's Ordner instead
// of in the course tree.
//
// WHY IT EXISTS (diagnosed live 2026-11-04, the maintainer's "Englisch
// WiSe26/27" course). That course's node tree is six node-st structure nodes
// whose every leaf is a node-en enrollment node, and not one node anywhere in
// it holds a file - confirmed by fetching all of them. The course reported 0
// files and always had. The material is real, though: enrolling in a language
// course puts the student in an OPAL *Lerngruppe*, and that group has its own
// file area, which is not a course node and is therefore structurally
// invisible to a course-tree crawl:
//
//	/opal/auth/BusinessGroup/56199544905/menu/MENU_FOLDER
//	  -> 6 files, including the three the maintainer had hand-downloaded
//
// This also corrects, for this one course, the conclusion recorded in
// section_type.go: node-en is still structurally incapable of holding files,
// so skipping it remains right - but "the enrollment node has no files" does
// NOT mean the course has none. The files are one level sideways, in the
// group the enrollment put the student into.
//
// HOW A GROUP IS FOUND, AND WHY IT COSTS NO EXTRA REQUEST. Every course page
// carries a second jstree payload next to the course tree coursetree.go
// already reads - a groups tree holding exactly this user's groups for that
// course, each with a direct href:
//
//	var initial_data=[{"id":"idgroups","text":"...Gruppen...","children":[
//	  {"id":"id56199544905",
//	   "text":"<span class=\"jstree-title\">Englisch C1.1 - ... Gruppe 1</span>",
//	   "a_attr":{"href":".../RepositoryEntry/56183324688/BusinessGroup/56199544905"}}]}]
//
// So enumeration is free: discoverSectionsHTTP already has the course root
// body in hand when it seeds the section queue. The group is already scoped
// to the course, which is what decides where its files land - no title
// matching, no separate "Meine Gruppen" page, no Wicket AJAX dance to turn a
// preview tile into a group id, and no pagination (the account's own groups
// list paginates at 10 of 18; the per-course tree does not).
//
// Each group then costs exactly one request: the Ordner URL is the group URL
// plus "/menu/MENU_FOLDER", and a group with no Ordner answers 200 with an
// empty file table rather than an error (verified live against three
// folderless groups), so no probe-then-fetch is needed. The response is an
// ordinary OPAL file table - <tr> rows with data-file-name, a size cell and a
// "zuletzt geändert" cell, byte-identical in shape to a course section's - so
// parseHTTPSectionCandidates, appendSectionFiles, parseRowSizeBytes and the
// download path's counter-refresh all work on it unchanged. Verified live:
// the refresh dance returns a downloadering?fibercode= URL for a group file
// exactly as it does for a course file.

// groupMenuFolderSuffix is the Ordner (file area) route of a group page.
// OPAL's group menu is a fixed set of MENU_<ITEM> routes (MENU_OVERVIEW,
// MENU_CONTACTFORM, MENU_FOLDER, ...) named in the group page's own jstree
// payload; MENU_FOLDER is the file area.
const groupMenuFolderSuffix = "/menu/MENU_FOLDER"

// businessGroupHrefNeedle identifies a groups-tree node's href. The parent
// "Gruppen" node uses the same path with the literal "Overview" in place of a
// group id (.../BusinessGroup/Overview) and is skipped by the digit check in
// parseCourseGroupNodes.
const businessGroupHrefNeedle = "/BusinessGroup/"

// CourseGroup is one learning group the course page's groups tree lists for
// this user.
//
// Title is OPAL's verbatim group label with whitespace normalized (see
// groupNodeTitle) - it becomes the file's SectionTitle, so it is what
// config's section_folder_names and subfolder_destinations patterns are
// matched against, the same contract FileRef.SectionTitle already documents.
type CourseGroup struct {
	ID    string
	Title string
	URL   string
}

// ParseCourseGroupNodes returns the learning groups a course page's groups
// tree lists, in document order, deduped by URL.
//
// Like ParseCourseTreeNodes it never fails loudly: a page without the payload
// returns nil, and callers must read that as "this course lists no groups for
// this user", which is the normal case for most courses.
func ParseCourseGroupNodes(html string) []CourseGroup {
	var out []CourseGroup
	seen := map[string]struct{}{}

	for _, loc := range initialDataAssignRe.FindAllStringIndex(html, -1) {
		raw, ok := extractBalancedJSONArray(html, loc[1])
		if !ok {
			continue
		}
		var roots []jstreeNode
		if err := json.Unmarshal([]byte(raw), &roots); err != nil {
			continue
		}
		collectCourseGroupNodes(roots, seen, &out)
	}
	return out
}

// collectCourseGroupNodes walks one initial_data array depth-first, appending
// every node whose href names a concrete BusinessGroup.
func collectCourseGroupNodes(nodes []jstreeNode, seen map[string]struct{}, out *[]CourseGroup) {
	for _, n := range nodes {
		href := strings.TrimSpace(n.AAttr.Href)
		if id, ok := businessGroupIDFromHref(href); ok {
			if _, dup := seen[href]; !dup {
				seen[href] = struct{}{}
				*out = append(*out, CourseGroup{ID: id, Title: groupNodeTitle(n), URL: href})
			}
		}
		var kids []jstreeNode
		if len(n.Children) > 0 && json.Unmarshal(n.Children, &kids) == nil {
			collectCourseGroupNodes(kids, seen, out)
		}
	}
}

// businessGroupIDFromHref extracts the numeric group id that follows
// "/BusinessGroup/" in href. It reports false for the parent "Gruppen" node
// (".../BusinessGroup/Overview") and for any href that is not a group link at
// all - the id must be all digits, which is what separates a real group from
// the menu route that shares its path shape.
func businessGroupIDFromHref(href string) (string, bool) {
	idx := strings.Index(href, businessGroupHrefNeedle)
	if idx < 0 {
		return "", false
	}
	rest := href[idx+len(businessGroupHrefNeedle):]
	if cut := strings.IndexAny(rest, "/?#"); cut >= 0 {
		rest = rest[:cut]
	}
	if rest == "" {
		return "", false
	}
	for i := 0; i < len(rest); i++ {
		if rest[i] < '0' || rest[i] > '9' {
			return "", false
		}
	}
	return rest, true
}

// groupNodeTitle renders a groups-tree node's label as a path-usable title.
//
// Unlike courseTreeNodeTitle it does NOT prefer a_attr.title: OPAL writes the
// group label with non-breaking spaces ("Englisch C1.1 - ..."),
// and a_attr.title carries them verbatim. A U+00A0 inside a folder name is an
// invisible trap - the directory it creates cannot be typed, and a
// section_folder_names pattern written by hand with ordinary spaces would
// never match it. So the label goes through the same entity-unescape and
// whitespace-collapse every other title in this package gets, which turns the
// non-breaking spaces into plain ones (strings.Fields splits on U+00A0).
func groupNodeTitle(n jstreeNode) string {
	stripped := courseTreeTagRe.ReplaceAllString(n.Text, " ")
	title := strings.Join(strings.Fields(unescapeHTMLText(stripped)), " ")
	if title != "" {
		return title
	}
	return strings.Join(strings.Fields(unescapeHTMLText(n.AAttr.Title)), " ")
}

// groupFolderURL is the Ordner route of a group URL.
func groupFolderURL(groupURL string) string {
	return strings.TrimRight(strings.TrimSpace(groupURL), "/") + groupMenuFolderSuffix
}

// appendGroupFolderFiles fetches the Ordner of every group the course page's
// groups tree lists and appends the files it holds to files, returning the
// grown slice and the number of HTTP requests issued.
//
// Each group's files are attributed to this course with the group's label as
// their SectionTitle, so they land under the course's configured folder like
// any other section - which is the point: the maintainer's English course
// folder is already configured, and its files were simply never discovered.
//
// A group whose Ordner fetch fails is reported through onError (may be nil)
// and contributes nothing; the course's own sections are unaffected. A group
// with no Ordner answers 200 with no file rows and likewise contributes
// nothing, which is why no separate "does this group have a folder" probe
// exists.
func appendGroupFolderFiles(files []FileRef, fileSeen map[string]struct{}, fetch httpFetcher, course CourseRef, rootHTML, opalURL string, downloadCandidates map[string]downloadCandidate, onError func(url string, err error)) ([]FileRef, int) {
	groups := ParseCourseGroupNodes(rootHTML)
	if len(groups) == 0 {
		return files, 0
	}

	requests := 0
	for _, group := range groups {
		folderURL := groupFolderURL(group.URL)
		body, err := httpGetText(fetch, folderURL)
		requests++
		if err != nil {
			if onError != nil {
				onError(folderURL, err)
			}
			continue
		}

		section := SectionRef{CourseRepoID: course.RepoID, Title: group.Title, URL: folderURL}
		before := len(files)
		files = appendSectionFiles(files, fileSeen, parseHTTPSectionCandidates(body), course, section, folderURL, "", "", false, opalURL, downloadCandidates)

		// A group Ordner is an ordinary paginated OPAL table, so it caps at
		// ~20 rows exactly like a section's file list does - follow the same
		// show-all endpoint the section path already follows rather than
		// silently keeping the first page.
		if rel := extractShowAllURLFromHTML(body); rel != "" {
			showAllURL := resolveURL(opalURL, rel)
			showBody, showErr := httpGetText(fetch, showAllURL)
			requests++
			if showErr != nil {
				if onError != nil {
					onError(showAllURL, showErr)
				}
			} else {
				files = appendSectionFiles(files, fileSeen, parseHTTPSectionCandidates(showBody), course, section, showAllURL, "", "", false, opalURL, downloadCandidates)
			}
		}

		if found := len(files) - before; found > 0 {
			logging.Detail("Group %q (%s): %d files in its Ordner", group.Title, folderURL, found)
		}
	}
	return files, requests
}
