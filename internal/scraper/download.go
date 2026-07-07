package scraper

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/mxschmitt/playwright-go"
)

// DownloadFile downloads fileURL to localPath. It first tries a plain HTTP
// GET through the shared Playwright APIRequestContext (s.context.Request()),
// which is safe to call concurrently from multiple goroutines - the
// underlying connection dispatches each call with its own atomic message ID
// and a sync.Map of pending callbacks (see playwright-go's connection.go),
// so it is not tied to a single page/tab the way page navigation is.
//
// If the fast path doesn't return a direct, non-HTML 200 response, it falls
// back to downloadFileViaBrowser, which drives the single shared s.page and
// therefore must not run concurrently with itself or with any other
// navigation of s.page. That fallback is serialized behind
// s.browserDownloadMu so callers are free to invoke DownloadFile from a
// worker pool for the common (fast-path) case.
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

	s.browserDownloadMu.Lock()
	defer s.browserDownloadMu.Unlock()
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

	if _, err := s.page.Goto(candidate.SourceURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(20000)}); err != nil {
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

	return errors.New("response is HTML, browser fallback click did not find downloadable link")
}
