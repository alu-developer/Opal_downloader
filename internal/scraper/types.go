package scraper

type CourseRef struct {
	RepoID string
	Title  string
	URL    string
}

type SectionRef struct {
	CourseRepoID string
	Title        string
	URL          string
}

type FileRef struct {
	CourseRepoID string
	CourseTitle  string
	SectionTitle string
	// SectionURL is the page this file was found on. Titles are not usable as
	// an identity - two sections can share one, and they are sanitised for the
	// filesystem - so anything keyed per section (the change-detection cache in
	// internal/sectioncache) needs the URL.
	SectionURL string
	Name       string
	URL        string
	Path       string
	// Size and Modified carry OPAL's own "Größe"/"Zuletzt geändert" file-list
	// column values, when present, so syncer.fileChanged can detect an
	// in-place content edit on OPAL's side (same name/path, different
	// content) without requiring --force. They are best-effort: extracted by
	// regex from the file link's surrounding row text (see
	// parseRowSizeBytes/parseRowModified in files.go), since OPAL exposes
	// this as free-form column text rather than structured
	// data-*/aria-* attributes. Nil when no size/date could be parsed out of
	// the row (e.g. the column is hidden, or the row shape doesn't match).
	Size     *int64
	Modified *string
}
