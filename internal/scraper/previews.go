package scraper

import (
	"os"
	"strings"
	"sync/atomic"

	"github.com/alu-developer/opal-downloader/internal/logging"
	"github.com/mxschmitt/playwright-go"
)

// OPAL course nodes that display their file inline make the browser fetch the
// whole file to render a preview in a subframe. A network trace of one course
// (2026-07-27, docs/sync-speed-campaign.md) recorded 72 such fetches totalling
// 30,617,106 bytes during a pass whose only job is to list filenames - and this
// package has no iframe handling at all, so nothing ever reads them.
//
// Blocking them asks OPAL for less rather than for the same things faster,
// which is the distinction docs/server-load.md draws.
//
// Two conditions, and both matter:
//
//   - Only /FolderResource/ URLs. That is where OPAL serves a course's actual
//     files.
//   - Only subframes (ParentFrame() != nil). A main-frame navigation to a
//     FolderResource URL is the download path doing its job, and aborting that
//     would break downloading rather than speed up discovery.
//
// The kill switch is deliberate. Every past attempt to make this crawl faster
// that changed what the page renders lost files silently, so the old behaviour
// has to stay one environment variable away for an A/B comparison to be
// possible at all.
const keepPreviewsEnv = "OPAL_KEEP_FILE_PREVIEWS"

const folderResourceMarker = "/FolderResource/"

// blockInlineFilePreviews installs the route on ctx. Failing to install it is
// not fatal: the crawl still works, it is just paying for previews again.
func (s *OpalScraper) blockInlineFilePreviews(ctx playwright.BrowserContext) {
	if os.Getenv(keepPreviewsEnv) != "" {
		logging.Detail("Inline file previews left enabled (%s is set)", keepPreviewsEnv)
		return
	}

	err := ctx.Route("**"+folderResourceMarker+"**", func(route playwright.Route) {
		req := route.Request()
		frame := req.Frame()
		// A nil frame means no way to tell a preview from a download, so the
		// inSubframe argument is false and the request is let through: a slow
		// crawl beats a broken one.
		inSubframe := frame != nil && frame.ParentFrame() != nil
		if !isDiscardablePreview(req.ResourceType(), req.URL(), inSubframe) {
			_ = route.Continue()
			return
		}
		atomic.AddInt64(&s.previewsBlocked, 1)
		atomic.AddInt64(&s.previewBytesSaved, contentLengthOf(req))
		// Abort rather than fulfil with an empty body: an empty 200 would make
		// the frame render an error state, which is more DOM churn than a
		// failed load, and the settle wait is watching exactly that.
		_ = route.Abort("blockedbyclient")
	})
	if err != nil {
		logging.Detail("Could not block inline file previews, continuing without: %v", err)
	}
}

// isDiscardablePreview is the whole safety argument in one function. It takes
// plain values rather than a playwright.Request so the argument can be tested
// exhaustively without a browser - which matters more than usual here, because
// the expensive live A/B can only ever sample the cases a real account happens
// to contain.
func isDiscardablePreview(resourceType, url string, inSubframe bool) bool {
	// A subframe navigation is a preview nothing reads. A main-frame one is
	// the download path doing its job, and aborting it would break downloading
	// rather than speed up discovery.
	return resourceType == "document" &&
		inSubframe &&
		strings.Contains(url, folderResourceMarker)
}

// contentLengthOf reports what the request said it was about to fetch, for the
// saved-bytes figure. Headers() reads what the event already carried;
// HeaderValue() would be a round trip into the browser from inside a handler
// the browser is waiting on, which deadlocks (see the network trace probe).
func contentLengthOf(req playwright.Request) int64 {
	var n int64
	if v, ok := req.Headers()["content-length"]; ok {
		for _, c := range strings.TrimSpace(v) {
			if c < '0' || c > '9' {
				return 0
			}
			n = n*10 + int64(c-'0')
		}
	}
	return n
}

// reportBlockedPreviews logs what the route actually did. Without this the
// change is unfalsifiable in the field: a run that blocked nothing and a run
// that blocked everything look identical from the outside.
func (s *OpalScraper) reportBlockedPreviews() {
	blocked := atomic.LoadInt64(&s.previewsBlocked)
	if blocked == 0 {
		return
	}
	logging.Detail("Skipped %d inline file preview(s) that discovery never reads", blocked)
}
