package scraper

import (
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

func extractLinkTarget(href, onclick, dataHref, dataURL string) string {
	for _, candidate := range []string{href, dataHref, dataURL} {
		c := strings.TrimSpace(candidate)
		if c != "" && c != "#" && !strings.HasPrefix(strings.ToLower(c), "javascript:") {
			return c
		}
	}
	if onclick != "" {
		re1 := regexp.MustCompile(`(?:location\.href|window\.open)\s*\(\s*['\"]([^'\"]+)['\"]`)
		if match := re1.FindStringSubmatch(onclick); len(match) > 1 {
			return match[1]
		}
		re2 := regexp.MustCompile(`location\.href\s*=\s*['\"]([^'\"]+)['\"]`)
		if match := re2.FindStringSubmatch(onclick); len(match) > 1 {
			return match[1]
		}
		if strings.Contains(onclick, "goto.php") {
			re3 := regexp.MustCompile(`(/[^'\"]*goto\.php[^'\"]*)`)
			if match := re3.FindStringSubmatch(onclick); len(match) > 1 {
				return match[1]
			}
		}
	}
	return ""
}

func sanitizeFilename(name string) string {
	cleaned := strings.TrimSpace(name)
	invalid := regexp.MustCompile(`[<>:"/\\|?*\x00-\x1F]`)
	cleaned = invalid.ReplaceAllString(cleaned, "_")
	cleaned = regexp.MustCompile(`\s+`).ReplaceAllString(cleaned, " ")
	cleaned = strings.TrimRight(cleaned, ". ")
	if cleaned == "" {
		cleaned = "unnamed"
	}
	upper := strings.ToUpper(cleaned)
	reserved := map[string]struct{}{"CON": {}, "PRN": {}, "AUX": {}, "NUL": {}, "COM1": {}, "COM2": {}, "COM3": {}, "COM4": {}, "COM5": {}, "COM6": {}, "COM7": {}, "COM8": {}, "COM9": {}, "LPT1": {}, "LPT2": {}, "LPT3": {}, "LPT4": {}, "LPT5": {}, "LPT6": {}, "LPT7": {}, "LPT8": {}, "LPT9": {}}
	if _, ok := reserved[upper]; ok {
		cleaned = "_" + cleaned
	}
	return cleaned
}

func extractRepositoryEntryID(rawURL string) string {
	re := regexp.MustCompile(`(?i)/RepositoryEntry/(\d+)`)
	if match := re.FindStringSubmatch(rawURL); len(match) > 1 {
		return match[1]
	}
	return ""
}

func normalizeURLForCrawl(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	query := u.Query()
	keep := map[string]struct{}{"target": {}, "ref_id": {}, "cmd": {}, "cmdClass": {}, "baseClass": {}, "cmdNode": {}, "node_id": {}, "crs_next_sess": {}}
	filtered := url.Values{}
	for key, values := range query {
		if _, ok := keep[key]; !ok {
			continue
		}
		for _, value := range values {
			filtered.Add(key, value)
		}
	}
	u.RawQuery = filtered.Encode()
	u.Fragment = ""
	return u.String()
}

func sectionKeyV2(rawURL, repoID string) string {
	normalized := normalizeURLForCrawl(rawURL)
	u, err := url.Parse(normalized)
	if err != nil {
		return normalized
	}

	targetRepoID := extractRepositoryEntryID(u.String())
	if repoID != "" && targetRepoID != "" && targetRepoID != repoID {
		return "foreign-repo|" + targetRepoID
	}

	query := u.Query()
	target := strings.ToLower(strings.TrimSpace(query.Get("target")))
	if repoID != "" && (targetRepoID == repoID || targetRepoID == "") {
		if target != "" && (strings.HasPrefix(target, "fold_") || strings.HasPrefix(target, "grp_") || strings.HasPrefix(target, "crs_")) {
			return "repo|" + repoID + "|target|" + target
		}

		nodeID := strings.TrimSpace(query.Get("node_id"))
		if nodeID != "" {
			return "repo|" + repoID + "|node|" + nodeID
		}
		cmdNode := strings.TrimSpace(query.Get("cmdNode"))
		if cmdNode != "" {
			return "repo|" + repoID + "|cmdnode|" + cmdNode
		}

		cmd := strings.ToLower(strings.TrimSpace(query.Get("cmd")))
		if cmd == "" || cmd == "view" {
			return "repo|" + repoID + "|root"
		}
		return "repo|" + repoID + "|cmd|" + cmd
	}

	return normalized
}

func resolveURL(baseURL, href string) string {
	base, err := url.Parse(baseURL)
	if err != nil {
		return href
	}
	rel, err := url.Parse(href)
	if err != nil {
		return href
	}
	return base.ResolveReference(rel).String()
}

func toStringSlice(value interface{}) []string {
	items, ok := value.([]interface{})
	if !ok {
		return nil
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok {
			result = append(result, text)
		}
	}
	return result
}

func toStringMapSlice(value interface{}) []map[string]string {
	items, ok := value.([]interface{})
	if !ok {
		return nil
	}
	result := make([]map[string]string, 0, len(items))
	for _, item := range items {
		mapped, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		entry := map[string]string{}
		for key, raw := range mapped {
			entry[key] = fmt.Sprintf("%v", raw)
			if entry[key] == "<nil>" {
				entry[key] = ""
			}
		}
		result = append(result, entry)
	}
	return result
}

func mapValues[K comparable, V any](m map[K]V) []V {
	result := make([]V, 0, len(m))
	for _, value := range m {
		result = append(result, value)
	}
	return result
}

func defaultString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func parseIntOrZero(value string) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0
	}
	return parsed
}

func containsAny(text string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func normalizePersistentProfileSettings(userDataDir, profileDir string) (string, string) {
	trimmedUserDataDir := strings.TrimSpace(userDataDir)
	trimmedProfileDir := strings.TrimSpace(profileDir)
	if trimmedUserDataDir == "" {
		return "", trimmedProfileDir
	}
	if trimmedProfileDir != "" {
		return trimmedUserDataDir, trimmedProfileDir
	}

	cleaned := filepath.Clean(trimmedUserDataDir)
	base := filepath.Base(cleaned)
	parent := filepath.Dir(cleaned)
	if strings.EqualFold(filepath.Base(parent), "User Data") {
		if strings.EqualFold(base, "Default") || strings.HasPrefix(base, "Profile ") {
			return parent, base
		}
	}

	return trimmedUserDataDir, ""
}
