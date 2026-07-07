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
	ctx := s.getContext()
	if ctx == nil {
		return errors.New("no authenticated browser context available")
	}

	response, err := ctx.Request().Get(fileURL)
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
	page := s.getPage()
	if page == nil {
		return errors.New("no browser page available for download fallback")
	}

	download, err := page.ExpectDownload(func() error {
		_, navErr := page.Goto(fileURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(20000)})
		return navErr
	}, playwright.PageExpectDownloadOptions{Timeout: playwright.Float(15000)})
	if err == nil {
		return download.SaveAs(localPath)
	}

	candidate, ok := s.downloadCandidates[fileURL]
	if !ok {
		return errors.New("response is HTML, not a direct file download")
	}

	if _, err := page.Goto(candidate.SourceURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(20000)}); err != nil {
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
		download, clickErr := page.ExpectDownload(func() error {
			return page.Locator(selector).First().Click(playwright.LocatorClickOptions{Timeout: playwright.Float(5000)})
		}, playwright.PageExpectDownloadOptions{Timeout: playwright.Float(15000)})
		if clickErr == nil {
			return download.SaveAs(localPath)
		}
	}

	if candidate.LinkText != "" {
		download, clickErr := page.ExpectDownload(func() error {
			return page.GetByText(candidate.LinkText, playwright.PageGetByTextOptions{Exact: playwright.Bool(false)}).First().Click(playwright.LocatorClickOptions{Timeout: playwright.Float(5000)})
		}, playwright.PageExpectDownloadOptions{Timeout: playwright.Float(15000)})
		if clickErr == nil {
			return download.SaveAs(localPath)
		}
	}

	return errors.New("response is HTML, browser fallback click did not find downloadable link")
}
