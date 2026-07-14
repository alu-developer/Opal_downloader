package scraper

import "strings"

// nonFileSectionTypeClasses lists OPAL/OLAT course-tree "node-<type>" CSS
// classes (the form OPAL's OLAT-based course-tree sidebar renders on a
// course-node link, e.g. <a class="jstree-anchor node-en">) that are
// confirmed, structurally, to never render a downloadable file - as
// opposed to a title-text heuristic, which risks a false match against a
// real content folder that happens to share a keyword (the exact class of
// risk already rejected for history-based skipping - see
// research-structure-cache-and-priority-crawl.md in .claude/queue/done/).
//
// CONFIRMED LIVE 2026-07-13 (queue task
// skip-non-file-sections-by-structural-marker): dumped every jstree-anchor
// course-node class/text pair across this account's real 8 enrolled OPAL
// courses (dump-links against each course's root page, cross-checked
// against extractSectionContentCandidates - the exact function the real
// crawl uses - to confirm these links actually reach the crawl's candidate
// pipeline, not just a whole-document scan). "node-en" appeared on 10
// distinct nodes across 7 of the 8 courses, and every single one of their
// visible labels was an enrollment/sign-up-flavored phrase
// ("Einschreibung", "Einschreibung in den Kurs", "Übungseinschreibung",
// "Einschreibung in die Übungsgruppen", "Einschreibung Kurs", ...) - zero
// cross-contamination with any real content-bearing node type ("node-bc"/
// "node-st", which held the actual downloadable material in the same
// dump). "en" is OLAT's internal course-element type code for its
// "Enrollment" building block (a student self-registration widget used to
// pick a tutorial/exercise-group slot) - structurally just a signup
// form/list, never a container that can hold a file. This is a genuine DOM
// class read directly off the element, not derived from its title text.
//
// Only "node-en" is included here. Other node-<type> classes seen in the
// same live dump (node-fo forum, node-cal calendar, node-co contact-mail,
// node-info announcements/news, node-ll linklist, node-bib bibliography,
// node-ms upload/task) look like plausible further candidates but were NOT
// live-confirmed file-incapable in this pass (in particular node-ms's
// "Upload: Modulbegeleitende Aufgaben" label suggests it may carry
// downloadable task materials, and node-ll linklists can technically point
// at OPAL-hosted files as well as external URLs) - do not add them here
// without the same kind of live cross-check this comment documents.
var nonFileSectionTypeClasses = map[string]struct{}{
	"node-en": {},
}

// isNonFileSectionType reports whether candidate - one of the maps produced
// by extractSectionContentCandidates - is a link into an OPAL course-node
// whose type is structurally incapable of holding downloadable files (see
// nonFileSectionTypeClasses's doc comment). This is deliberately NOT based
// on the node's title/text (looksLikeSectionFolderLink's existing
// German-keyword denylist in files.go is the separate, text-based
// mechanism that already excludes some administrative sections) and NOT
// based on crawl/visit history (that idea was explicitly rejected - see
// research-structure-cache-and-priority-crawl.md in .claude/queue/done/) -
// only the candidate element's own CSS class, read directly from the DOM.
func isNonFileSectionType(candidate map[string]string) bool {
	for _, cls := range strings.Fields(candidate["className"]) {
		if _, skip := nonFileSectionTypeClasses[cls]; skip {
			return true
		}
	}
	return false
}
