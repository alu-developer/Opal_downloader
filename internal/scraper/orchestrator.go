package scraper

import (
	"errors"
	"fmt"

	"github.com/alu-developer/opal-downloader/internal/timing"
)

func (s *OpalScraper) scrapeCoursesBrowser(courseFilter []string) ([]RemoteFile, error) {
	if s.page == nil {
		return nil, errors.New("no page available")
	}

	discoveryTimer := timing.StartTimer()
	courses, err := s.discoverCourseLinks(courseFilter)
	discoveryElapsed := discoveryTimer.Elapsed()
	if err != nil {
		return nil, err
	}
	fmt.Printf("Found %d course links\n", len(courses))
	timing.PrintDiscoverySummary(discoveryElapsed, len(courses))

	remoteFiles := make([]RemoteFile, 0)
	remoteFileSeen := map[string]struct{}{}

	fileCollectionTimer := timing.StartTimer()
	for _, course := range courses {
		fmt.Printf("  Processing: %s\n", course.Title)
		courseTimer := timing.StartTimer()
		courseFiles, fileErr := s.collectCourseFiles(course)
		timing.PrintProfileLine("course %q: %s (%d files)", course.Title, courseTimer.Elapsed(), len(courseFiles))
		if fileErr != nil {
			fmt.Printf("  Course crawl error: %v\n", fileErr)
			continue
		}
		remoteFiles = appendUniqueRemoteFiles(remoteFiles, remoteFileSeen, convertFileRefsToRemoteFiles(courseFiles))
	}
	fileCollectionElapsed := fileCollectionTimer.Elapsed()
	timing.PrintProfileLine("file collection (aggregate): %s", fileCollectionElapsed)

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
