package scraper

type CourseRefV2 struct {
	RepoID string
	Title  string
	URL    string
}

type SectionRefV2 struct {
	CourseRepoID string
	Title        string
	URL          string
}

type FileRefV2 struct {
	CourseRepoID string
	CourseTitle  string
	SectionTitle string
	Name         string
	URL          string
	Path         string
}
