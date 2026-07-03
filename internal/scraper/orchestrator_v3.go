package scraper

import (
	"errors"
	"fmt"
)

func (s *OpalScraper) ScrapeWithSavedSessionV3(courseFilter []string) ([]RemoteFile, error) {
	if len(courseFilter) == 0 {
		courseFilter = []string{"*"}
	}
	if err := s.ensureSession(false); err != nil {
		return nil, err
	}
	return s.scrapeCoursesBrowserV3(courseFilter)
}

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
