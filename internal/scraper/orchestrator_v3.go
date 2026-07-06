package scraper

import (
	"errors"
	"fmt"
)

func (s *OpalScraper) scrapeCoursesBrowserV3(courseFilter []string) ([]RemoteFile, error) {
	if s.page == nil {
		return nil, errors.New("no page available")
	}

	courses, err := s.discoverCourseLinksV3(courseFilter)
	if err != nil {
		return nil, err
	}
	fmt.Printf("Found %d course links (v3)\n", len(courses))

	remoteFiles := make([]RemoteFile, 0)
	remoteFileSeen := map[string]struct{}{}

	for _, course := range courses {
		fmt.Printf("  Processing (v3): %s\n", course.Title)
		courseFiles, fileErr := s.collectCourseFilesV3(course)
		if fileErr != nil {
			fmt.Printf("  Course crawl error: %v\n", fileErr)
			continue
		}
		remoteFiles = appendUniqueRemoteFilesV2(remoteFiles, remoteFileSeen, convertFileRefsToRemoteFilesV2(courseFiles))
	}

	fmt.Printf("Discovered %d remote files (v3)\n", len(remoteFiles))
	return remoteFiles, nil
}

func convertFileRefsToRemoteFilesV2(items []FileRefV2) []RemoteFile {
	remoteFiles := make([]RemoteFile, 0, len(items))
	for _, item := range items {
		remoteFiles = append(remoteFiles, RemoteFile{
			Name:   item.Name,
			URL:    item.URL,
			Course: item.CourseTitle,
			Path:   item.Path,
		})
	}
	return remoteFiles
}

func appendUniqueRemoteFilesV2(existing []RemoteFile, seen map[string]struct{}, candidates []RemoteFile) []RemoteFile {
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
