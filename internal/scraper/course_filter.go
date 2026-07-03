package scraper

import "strings"

func strictCourseFilterMatches(courseName string, courseFilter []string) bool {
	name := strings.TrimSpace(courseName)
	if name == "" {
		return false
	}
	if len(courseFilter) == 0 {
		return true
	}

	for _, raw := range courseFilter {
		filter := strings.TrimSpace(raw)
		if filter == "" {
			continue
		}
		if filter == "*" {
			return true
		}
		if strings.EqualFold(name, filter) {
			return true
		}
	}

	return false
}
