package scraper

import (
	"errors"
	"fmt"
)

func (s *OpalScraper) scrapeCoursesBrowser(courseFilter []string) ([]RemoteFile, error) {
	if s.page == nil {
		return nil, errors.New("no page available")
	}

	courses, err := s.discoverCourseLinks(courseFilter)
	if err != nil {
		return nil, err
	}
	fmt.Printf("Found %d course links\n", len(courses))

	remoteFiles := make([]RemoteFile, 0)
	remoteFileSeen := map[string]struct{}{}

	for _, course := range courses {
		fmt.Printf("  Processing: %s\n", course.Title)
		courseFiles, fileErr := s.collectCourseFiles(course)
		if fileErr != nil {
			fmt.Printf("  Course crawl error: %v\n", fileErr)
			continue
		}
		remoteFiles = appendUniqueRemoteFiles(remoteFiles, remoteFileSeen, convertFileRefsToRemoteFiles(courseFiles))
	}

	fmt.Printf("Discovered %d remote files\n", len(remoteFiles))
	return remoteFiles, nil
}

func convertFileRefsToRemoteFiles(items []FileRef) []RemoteFile {
	remoteFiles := make([]RemoteFile, 0, len(items))
	for _, item := range items {
		remoteFiles = append(remoteFiles, RemoteFile{
			Name:         item.Name,
			URL:          item.URL,
			Course:       item.CourseTitle,
			SectionTitle: item.SectionTitle,
			Path:         item.Path,
		})
	}
	return remoteFiles
}

func appendUniqueRemoteFiles(existing []RemoteFile, seen map[string]struct{}, candidates []RemoteFile) []RemoteFile {
	files := append([]RemoteFile(nil), existing...)
	for _, candidate := range candidates {
		key := candidate.Path + "|" + candidate.URL
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		files = append(files, candidate)
	}
	return files
}
