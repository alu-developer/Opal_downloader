package scraper

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/mxschmitt/playwright-go"
)

func (s *OpalScraper) DownloadFile(fileURL, localPath string) error {
	if s.context == nil {
		return errors.New("no authenticated browser context available")
	}

	response, err := s.context.Request().Get(fileURL)
	if err == nil && response != nil && response.Status() == 200 {
		headers := response.Headers()
		contentType := strings.ToLower(headers["content-type"])
		body, bodyErr := response.Body()
		if bodyErr == nil && !strings.Contains(contentType, "text/html") {
			if strings.HasSuffix(strings.ToLower(localPath), ".pdf") && !strings.HasPrefix(string(body), "%PDF") {
				return fmt.Errorf("downloaded payload is not a valid PDF")
			}
			return os.WriteFile(localPath, body, 0o644)
		}
	}

	return s.downloadFileViaBrowser(fileURL, localPath)
}

func (s *OpalScraper) downloadFileViaBrowser(fileURL, localPath string) error {
	if s.page == nil {
		return errors.New("no browser page available for download fallback")
	}

	download, err := s.page.ExpectDownload(func() error {
		_, navErr := s.page.Goto(fileURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(20000)})
		return navErr
	}, playwright.PageExpectDownloadOptions{Timeout: playwright.Float(15000)})
	if err == nil {
		return download.SaveAs(localPath)
	}

	candidate, ok := s.downloadCandidates[fileURL]
	if !ok {
		return errors.New("response is HTML, not a direct file download")
	}

	// Try the page where the candidate was originally recorded first. For files only
	// revealed by a section's "show all"/"Alle anzeigen" expansion, that recorded
	// SourceURL is the section's plain (unexpanded) page, which won't render the link -
	// so if the click search comes up empty there, retry on candidate.ShowAllURL, the
	// expanded page where the link actually renders.
	if err := s.clickCandidateLinkOnPage(candidate.SourceURL, candidate, localPath); err == nil {
		return nil
	}

	if strings.TrimSpace(candidate.ShowAllURL) != "" && !strings.EqualFold(strings.TrimSpace(candidate.ShowAllURL), strings.TrimSpace(candidate.SourceURL)) {
		if err := s.clickCandidateLinkOnPage(candidate.ShowAllURL, candidate, localPath); err == nil {
			return nil
		}
	}

	return errors.New("response is HTML, browser fallback click did not find downloadable link")
}

// clickCandidateLinkOnPage navigates to pageURL and attempts to locate and click the
// download candidate's link there, saving the resulting download to localPath. It
// returns an error (without wrapping context) whenever the link could not be found or
// clicked on that page, so callers can try an alternate page as a fallback.
func (s *OpalScraper) clickCandidateLinkOnPage(pageURL string, candidate downloadCandidate, localPath string) error {
	if strings.TrimSpace(pageURL) == "" {
		return errors.New("no page URL to search for downloadable link")
	}

	if _, err := s.page.Goto(pageURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(20000)}); err != nil {
		return err
	}

	targetFragment := strings.TrimSpace(candidate.LinkTarget)
	if strings.HasPrefix(targetFragment, "http") {
		parsed, parseErr := url.Parse(targetFragment)
		if parseErr == nil {
			targetFragment = parsed.Path
		}
	}

	if targetFragment != "" {
		selector := fmt.Sprintf("a[href*='%s']", strings.ReplaceAll(targetFragment, "'", "\\'"))
		download, clickErr := s.page.ExpectDownload(func() error {
			return s.page.Locator(selector).First().Click(playwright.LocatorClickOptions{Timeout: playwright.Float(5000)})
		}, playwright.PageExpectDownloadOptions{Timeout: playwright.Float(15000)})
		if clickErr == nil {
			return download.SaveAs(localPath)
		}
	}

	if candidate.LinkText != "" {
		download, clickErr := s.page.ExpectDownload(func() error {
			return s.page.GetByText(candidate.LinkText, playwright.PageGetByTextOptions{Exact: playwright.Bool(false)}).First().Click(playwright.LocatorClickOptions{Timeout: playwright.Float(5000)})
		}, playwright.PageExpectDownloadOptions{Timeout: playwright.Float(15000)})
		if clickErr == nil {
			return download.SaveAs(localPath)
		}
	}

	return errors.New("downloadable link not found on page")
}
