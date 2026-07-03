package scraper

import (
	"errors"
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/mxschmitt/playwright-go"
)

func (s *OpalScraper) collectCourseFilesV3(course CourseRefV2) ([]FileRefV2, error) {
	if s.page == nil {
		return nil, errors.New("no page available")
	}
	if course.RepoID == "" {
		return nil, errors.New("course repo id is required")
	}

	files := make([]FileRefV2, 0)
	fileSeen := map[string]struct{}{}
	visited := map[string]struct{}{}
	rootKey := sectionKeyV3(course.URL, course.RepoID)
	queue := []string{course.URL}
	queued := map[string]struct{}{rootKey: {}}
	maxPages := 16

	for len(queue) > 0 && len(visited) < maxPages {
		currentURL := queue[0]
		queue = queue[1:]
		currentKey := sectionKeyV3(currentURL, course.RepoID)
		delete(queued, currentKey)
		if _, ok := visited[currentKey]; ok {
			continue
		}
		visited[currentKey] = struct{}{}

		if _, err := s.page.Goto(currentURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(contentGotoTimeoutMsV2)}); err != nil {
			continue
		}
		s.waitForInteractiveLinksV2(contentSelectorTimeoutMsV2, contentFallbackWaitMsV2)

		candidates, err := s.extractSectionContentCandidatesV2()
		if err != nil {
			continue
		}
		if len(candidates) == 0 {
			s.page.WaitForTimeout(contentFallbackWaitMsV2)
			retryCandidates, retryErr := s.extractSectionContentCandidatesV2()
			if retryErr == nil && len(retryCandidates) > 0 {
				candidates = retryCandidates
			}
		}

		sectionTitle := deriveSectionTitleFromURLV3(course.Title, currentURL)
		section := SectionRefV2{CourseRepoID: course.RepoID, Title: sectionTitle, URL: currentURL}
		files = appendSectionFilesV2(files, fileSeen, candidates, course, section, currentURL, s.opalURL, s.downloadCandidates)
		queue = appendSectionFolderTargetsV3(queue, queued, visited, candidates, s.opalURL, course.RepoID, currentURL, course.URL)
	}

	return files, nil
}

func appendSectionFolderTargetsV3(queue []string, queued, visited map[string]struct{}, candidates []map[string]string, opalURL, repoID, currentURL, courseRootURL string) []string {
	currentKey := sectionKeyV3(currentURL, repoID)
	rootKey := sectionKeyV3(courseRootURL, repoID)

	for _, candidate := range candidates {
		linkTarget := extractLinkTarget(candidate["href"], candidate["onclick"], candidate["dataHref"], candidate["dataUrl"])
		if linkTarget == "" {
			continue
		}
		title := deriveSectionTitleV2(candidate["title"], candidate["text"], candidate["rootText"])
		if !looksLikeSectionFolderLinkV2(linkTarget, title) {
			continue
		}

		absURL := resolveURL(opalURL, linkTarget)
		if !isSectionURLAllowedForCourseV2(absURL, repoID) {
			continue
		}
		if !isAllowedFolderNavigationTargetV3(candidate, absURL) {
			continue
		}

		key := sectionKeyV3(absURL, repoID)
		if key == currentKey || key == rootKey {
			continue
		}
		if _, ok := visited[key]; ok {
			continue
		}
		if _, ok := queued[key]; ok {
			continue
		}

		queued[key] = struct{}{}
		queue = append(queue, absURL)
	}

	return queue
}

func isAllowedFolderNavigationTargetV3(candidate map[string]string, absURL string) bool {
	allowlist := map[string]struct{}{
		"materialien":     {},
		"probeklausur":    {},
		"ubungsblatter":   {},
		"uebungsblaetter": {},
		"uebungsblatter":  {},
		"vorlesung":       {},
	}

	for _, raw := range []string{
		deriveSectionTitleV2(candidate["title"], candidate["text"], candidate["rootText"]),
		candidate["title"],
		candidate["text"],
		decodePathBaseV3(absURL),
	} {
		token := normalizeFolderTokenV3(raw)
		if token == "" {
			continue
		}
		if _, ok := allowlist[token]; ok {
			return true
		}
	}

	return false
}

func decodePathBaseV3(absURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(absURL))
	if err != nil {
		return ""
	}
	base := strings.TrimSpace(path.Base(parsed.Path))
	if base == "" || strings.EqualFold(base, "coursenode") || regexp.MustCompile(`^\d+$`).MatchString(base) {
		return ""
	}
	decoded, decodeErr := url.PathUnescape(base)
	if decodeErr != nil {
		return base
	}
	return decoded
}

func normalizeFolderTokenV3(raw string) string {
	cleaned := strings.ToLower(strings.TrimSpace(raw))
	if cleaned == "" {
		return ""
	}
	replacements := strings.NewReplacer(
		"ä", "ae",
		"ö", "oe",
		"ü", "ue",
		"ß", "ss",
	)
	cleaned = replacements.Replace(cleaned)
	cleaned = regexp.MustCompile(`[^\p{L}\p{N}]+`).ReplaceAllString(cleaned, "")
	return strings.TrimSpace(cleaned)
}

func deriveSectionTitleFromURLV3(courseTitle, currentURL string) string {
	if strings.TrimSpace(currentURL) == "" {
		return sanitizeFilename(defaultString(courseTitle, "Kursinhalt"))
	}
	if strings.Contains(strings.ToLower(currentURL), "/coursenode/") {
		return sanitizeFilename("CourseNode")
	}
	return sanitizeFilename(defaultString(courseTitle, "Kursinhalt"))
}

func sectionKeyV3(rawURL, repoID string) string {
	cleaned := strings.TrimSpace(rawURL)
	if cleaned == "" {
		return sectionKeyV2(rawURL, repoID)
	}
	lower := strings.ToLower(cleaned)
	if strings.Contains(lower, "/coursenode/") {
		re := regexp.MustCompile(`(?i)/coursenode/(\d+)(/[^?#]*)?`)
		if match := re.FindStringSubmatch(cleaned); len(match) > 1 {
			suffix := ""
			if len(match) > 2 {
				suffix = strings.TrimSpace(match[2])
			}
			suffix = strings.Trim(suffix, "/")
			normalizedSuffix := ""
			if suffix != "" {
				decoded, err := url.PathUnescape(suffix)
				if err != nil {
					decoded = suffix
				}
				normalizedSuffix = strings.ToLower(strings.TrimSpace(decoded))
			}
			if repoID != "" {
				if normalizedSuffix != "" {
					return "repo|" + repoID + "|coursenode|" + match[1] + "|path|" + normalizedSuffix
				}
				return "repo|" + repoID + "|coursenode|" + match[1]
			}
			if normalizedSuffix != "" {
				return "coursenode|" + match[1] + "|path|" + normalizedSuffix
			}
			return "coursenode|" + match[1]
		}
	}
	return sectionKeyV2(rawURL, repoID)
}
