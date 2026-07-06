package scraper

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/mxschmitt/playwright-go"
)

type LinkDumpEntry struct {
	Index               int    `json:"index"`
	PageURL             string `json:"pageUrl"`
	DOMPath             string `json:"domPath"`
	Area                string `json:"area"`
	TagName             string `json:"tagName"`
	Text                string `json:"text"`
	Title               string `json:"title"`
	AriaLabel           string `json:"ariaLabel"`
	Href                string `json:"href"`
	OnClick             string `json:"onclick"`
	DataHref            string `json:"dataHref"`
	DataURL             string `json:"dataUrl"`
	ResolvedTarget      string `json:"resolvedTarget"`
	Visible             bool   `json:"visible"`
	ExistingFileRule    bool   `json:"existingFileRule"`
	ExistingFolderRule  bool   `json:"existingFolderRule"`
	ExistingSectionRule bool   `json:"existingSectionRule"`
	AllowedForCourse    bool   `json:"allowedForCourse"`
	NameGuess           string `json:"nameGuess"`
	TitleGuess          string `json:"titleGuess"`
	ClassName           string `json:"className"`
	Role                string `json:"role"`
	RootText            string `json:"rootText"`
	RootClass           string `json:"rootClass"`
}

type LinkDumpReport struct {
	RequestedURL string          `json:"requestedUrl"`
	FinalURL     string          `json:"finalUrl"`
	RepoID       string          `json:"repoId"`
	EntryCount   int             `json:"entryCount"`
	Entries      []LinkDumpEntry `json:"entries"`
}

func (s *OpalScraper) DumpPageLinks(targetURL, outputPath string) error {
	if strings.TrimSpace(targetURL) == "" {
		return errors.New("target url is required")
	}
	if strings.TrimSpace(outputPath) == "" {
		return errors.New("output path is required")
	}
	if err := s.ensureSession(false); err != nil {
		return err
	}
	if s.page == nil {
		return errors.New("no page available")
	}

	if _, err := s.page.Goto(targetURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(contentGotoTimeoutMs)}); err != nil {
		return err
	}
	s.waitForInteractiveLinks(contentSelectorTimeoutMs, contentFallbackWaitMs)

	value, err := s.page.Evaluate(`() => {
			const selectors = 'a[href], [onclick], [data-href], [data-url]';
			const normalize = (value) => (value || '').replace(/\s+/g, ' ').trim();
			const isVisible = (el) => {
				const rect = el.getBoundingClientRect();
				const style = window.getComputedStyle(el);
				return rect.width > 0 && rect.height > 0 && style.visibility !== 'hidden' && style.display !== 'none';
			};
			const cssPath = (el) => {
				const parts = [];
				let node = el;
				while (node && node.nodeType === Node.ELEMENT_NODE && parts.length < 8) {
					let part = node.tagName.toLowerCase();
					if (node.id) {
						part += '#' + node.id;
						parts.unshift(part);
						break;
					}
					const className = normalize(node.className).split(' ').filter(Boolean).slice(0, 2).join('.');
					if (className) {
						part += '.' + className.replace(/\s+/g, '.');
					}
					let index = 1;
					let sibling = node;
					while ((sibling = sibling.previousElementSibling) != null) {
						if (sibling.tagName === node.tagName) {
							index += 1;
						}
					}
					part += ':nth-of-type(' + index + ')';
					parts.unshift(part);
					node = node.parentElement;
				}
				return parts.join(' > ');
			};
			const findArea = (el) => {
				const area = el.closest('main, nav, aside, header, footer, [role], #il_left_col, #left_col, #il_center_col, #center_col, .sidebar, .o_main_content, .o_page_sidebar, .o_page_body, .ilContainerSideBlock, .ilc_page_cont_page');
				if (!area) {
					return { label: 'document-body', rootText: normalize(document.body ? document.body.textContent : ''), rootClass: normalize(document.body ? document.body.className : '') };
				}
				let label = area.tagName.toLowerCase();
				if (area.id) {
					label += '#' + area.id;
				}
				const areaClass = normalize(area.className).split(' ').filter(Boolean).slice(0, 3).join('.');
				if (areaClass) {
					label += '.' + areaClass.replace(/\s+/g, '.');
				}
				const role = normalize(area.getAttribute('role'));
				if (role) {
					label += '[role=' + role + ']';
				}
				return {
					label,
					rootText: normalize(area.textContent),
					rootClass: normalize(area.className),
				};
			};
			return Array.from(document.querySelectorAll(selectors)).map((el, index) => {
				const area = findArea(el);
				return {
					index,
					pageUrl: window.location.href,
					domPath: cssPath(el),
					area: area.label,
					tagName: (el.tagName || '').toLowerCase(),
					text: normalize(el.textContent),
					title: normalize(el.getAttribute('title')),
					ariaLabel: normalize(el.getAttribute('aria-label')),
					href: normalize(el.getAttribute('href')),
					onclick: normalize(el.getAttribute('onclick')),
					dataHref: normalize(el.getAttribute('data-href')),
					dataUrl: normalize(el.getAttribute('data-url')),
					visible: isVisible(el),
					className: normalize(el.className),
					role: normalize(el.getAttribute('role')),
					rootText: area.rootText,
					rootClass: area.rootClass,
				};
			});
		}`)
	if err != nil {
		return err
	}

	rawEntries := toStringMapSlice(value)
	finalURL := defaultString(s.page.URL(), targetURL)
	repoID := extractRepositoryEntryID(finalURL)
	entries := make([]LinkDumpEntry, 0, len(rawEntries))
	for _, raw := range rawEntries {
		linkTarget := extractLinkTarget(raw["href"], raw["onclick"], raw["dataHref"], raw["dataUrl"])
		resolvedTarget := ""
		allowedForCourse := false
		nameGuess := ""
		titleGuess := deriveSectionTitle(raw["title"], raw["text"], raw["rootText"])
		if linkTarget != "" {
			resolvedTarget = resolveURL(s.opalURL, linkTarget)
			nameGuess = deriveFileName(raw["title"], raw["text"], linkTarget)
			allowedForCourse = isSectionURLAllowedForCourse(resolvedTarget, repoID) || isFileURLAllowedForCourse(resolvedTarget, repoID)
		}

		entries = append(entries, LinkDumpEntry{
			Index:               parseIntOrZero(raw["index"]),
			PageURL:             raw["pageUrl"],
			DOMPath:             raw["domPath"],
			Area:                raw["area"],
			TagName:             raw["tagName"],
			Text:                raw["text"],
			Title:               raw["title"],
			AriaLabel:           raw["ariaLabel"],
			Href:                raw["href"],
			OnClick:             raw["onclick"],
			DataHref:            raw["dataHref"],
			DataURL:             raw["dataUrl"],
			ResolvedTarget:      resolvedTarget,
			Visible:             strings.EqualFold(raw["visible"], "true"),
			ExistingFileRule:    looksLikeFileLink(linkTarget, nameGuess),
			ExistingFolderRule:  looksLikeSectionFolderLink(linkTarget, titleGuess),
			ExistingSectionRule: looksLikeSectionLink(linkTarget, titleGuess),
			AllowedForCourse:    allowedForCourse,
			NameGuess:           nameGuess,
			TitleGuess:          titleGuess,
			ClassName:           raw["className"],
			Role:                raw["role"],
			RootText:            raw["rootText"],
			RootClass:           raw["rootClass"],
		})
	}

	report := LinkDumpReport{
		RequestedURL: targetURL,
		FinalURL:     finalURL,
		RepoID:       repoID,
		EntryCount:   len(entries),
		Entries:      entries,
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(outputPath, data, 0o644)
}
