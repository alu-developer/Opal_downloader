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
	Name         string
	URL          string
	Path         string
}
