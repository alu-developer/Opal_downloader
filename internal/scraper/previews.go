package scraper

import (
	"fmt"
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
//   - Only subframes (frameId != the page's own root frame ID). A main-frame
//     navigation to a FolderResource URL is the download path doing its job,
//     and aborting that would break downloading rather than speed up discovery.
//
// ON BY DEFAULT since 2026-08-07 (Question 26, docs/sync-speed-model.md).
// Set OPAL_BLOCK_FILE_PREVIEWS=0 to disable and fall back to fetching
// previews.
//
// The safety question came back clean twice. First on the original
// ctx.Route implementation (2026-07-27, internal/scraper/filelist_probe_test.go):
// a paired full-account A/B produced byte-for-byte identical file lists, 345
// files each, matching the known ground truth. Then again after the rewrite
// below to raw CDP (Question 26, 2026-08-07): another full-account A/B, 349
// files each (account grew since), zero-line diff. Nothing is lost by
// blocking previews.
//
// The speed question came back the wrong way at first, and a second pair
// confirmed it rather than clearing it - this was all measured against the
// ORIGINAL ctx.Route implementation, superseded below:
//
//	pair                 previews kept   previews blocked   delta
//	1 (2026-07-27 am)          248.3s             324.3s   +30.6%
//	2 (2026-07-27 pm)          210.3s             265.0s   +26.0%
//
// THE SLOWDOWN WAS NEVER THE BLOCKING. It was ctx.Route itself. Question 3
// (docs/sync-speed-model.md) traced it to two CDP mechanisms ctx.Route
// installs together and unconditionally, regardless of pattern:
// Network.setCacheDisabled(true) for the whole session, plus a per-request
// Fetch pause/resume round trip. Question 8 (2026-08-04, local synthetic
// probe, no OPAL account, internal/scraper/ctxroutecost_probe_test.go) then
// separated the two: cache-off is 60.7% of the gap, the Fetch pause/resume
// round trip only 3.1% - and, critically, raw CDP calls DO decouple them even
// though ctx.Route always couples them. That is Playwright's own
// driver-side implementation choice, not a CDP/Chrome requirement.
//
// So this now drives the CDP Fetch domain directly through a raw CDPSession
// per page (attachInlinePreviewBlocker, called at every page-creation site -
// see its own doc comment for why it cannot be installed once on the
// context the way ctx.Route was), instead of through ctx.Route. That keeps
// the ~30 MB/course preview-blocking saving while paying close to Question
// 8's ~3% fetch-only tax instead of the ~30% ctx.Route tax. Question 26
// re-measured it end to end against the live account: 172.6s vs 185.2s
// total test time on this run (baseline had also picked up a fresh-session
// login, so treat that gap as directional, not the load-bearing number -
// the byte-diff is), with 80 previews skipped this pass. The load-bearing
// result is the empty diff; Question 23's shelved rejection was ctx.Route's
// tax, never the blocking itself, and that tax is gone.
//
// Do not re-litigate Abort vs Fulfill vs pattern width against ctx.Route.
// Those are answered and irrelevant now that ctx.Route is gone from this
// path entirely.
const blockPreviewsEnv = "OPAL_BLOCK_FILE_PREVIEWS"

const folderResourceMarker = "/FolderResource/"

// attachInlinePreviewBlocker installs a raw-CDP Fetch interceptor on page.
// Failing to install it is not fatal: the crawl still works, it is just
// paying for previews again.
//
// This is per-page, not per-context, unlike the ctx.Route version it
// replaces. A CDPSession (ctx.NewCDPSession) is bound to one page/frame at
// creation - there is no context-wide equivalent that reaches pages opened
// later, which is exactly why ctx.Route was convenient (it silently covered
// every future page in ctx) and exactly why that convenience cost the
// Network.setCacheDisabled(true) Question 8 traced. The trade this function
// makes explicit: call it at every place a crawl-participating page is
// created (session.go's initial page, orchestrator.go's
// newCourseFileCollector, navigation.go's crash-recovery replacement page,
// section_pool.go's openPage) instead of once on ctx.
func (s *OpalScraper) attachInlinePreviewBlocker(ctx playwright.BrowserContext, page playwright.Page) {
	if os.Getenv(blockPreviewsEnv) == "0" {
		return
	}
	logging.Detail("Blocking inline file previews on new page (default on; %s=0 to disable)", blockPreviewsEnv)

	session, err := ctx.NewCDPSession(page)
	if err != nil {
		logging.Detail("Could not open CDP session for preview blocking, continuing without: %v", err)
		return
	}

	// The root frame ID is what distinguishes "a download navigating in the
	// main frame" (must never be blocked, see isDiscardablePreview) from "an
	// inline preview in a subframe" (the whole point). ctx.Route could ask
	// Playwright's own Frame.ParentFrame() for this; a raw CDPSession has no
	// such object, only frameId strings on each Fetch.requestPaused event, so
	// this captures the page's own frame ID once, up front, via a direct CDP
	// query - it does not need Page.enable first, and the top frame's ID does
	// not change across same-document or cross-document navigations of that
	// frame in Chrome.
	rootFrameID, err := pageRootFrameID(session)
	if err != nil {
		// Deliberately bail out entirely rather than enabling Fetch with an
		// empty/unknown root frame ID: every requestPaused event's frameId
		// would then fail to match "", making every FolderResource request
		// look like a subframe - including a main-frame download navigation,
		// which would silently break downloading. Fail open (no blocking) is
		// always safer than fail blocked-in-the-wrong-place here.
		logging.Detail("Could not determine root frame for preview blocking, continuing without: %v", err)
		return
	}

	session.On("Fetch.requestPaused", func(params map[string]any) {
		requestID, _ := params["requestId"].(string)
		if requestID == "" {
			return
		}
		frameID, _ := params["frameId"].(string)
		resourceType, _ := params["resourceType"].(string)
		reqURL := ""
		reqHeaders := map[string]any{}
		if req, ok := params["request"].(map[string]any); ok {
			reqURL, _ = req["url"].(string)
			if h, ok := req["headers"].(map[string]any); ok {
				reqHeaders = h
			}
		}

		inSubframe := frameID != "" && frameID != rootFrameID
		block := isDiscardablePreview(strings.ToLower(resourceType), reqURL, inSubframe)

		if block {
			atomic.AddInt64(&s.previewsBlocked, 1)
			atomic.AddInt64(&s.previewBytesSaved, contentLengthFromHeaders(reqHeaders))
		}

		// Must not call session.Send synchronously from inside this handler:
		// it runs on the connection's own dispatch loop, and Send blocks
		// waiting for a reply by re-entering that same loop. Learned the hard
		// way building Question 8's probe (internal/scraper/ctxroutecost_probe_test.go) -
		// several requests pausing near-simultaneously recursed one dispatch
		// frame per pending request and hung outright. A goroutine keeps the
		// reply off the dispatch loop's own stack.
		go func() {
			if block {
				// Mirrors ctx.Route's route.Abort("blockedbyclient") from the
				// previous implementation - same CDP-level error reason,
				// different call path.
				_, _ = session.Send("Fetch.failRequest", map[string]any{
					"requestId":   requestID,
					"errorReason": "BlockedByClient",
				})
				return
			}
			_, _ = session.Send("Fetch.continueRequest", map[string]any{"requestId": requestID})
		}()
	})

	// Only FolderResource document requests need to pause at all - unlike
	// ctx.Route, which Playwright always installs at the CDP level with
	// pattern "*" regardless of the caller's pattern (Question 3), a raw
	// Fetch.enable call passes the real pattern straight to Chrome, so every
	// other request on this page skips the pause/resume round trip entirely.
	_, err = session.Send("Fetch.enable", map[string]any{
		"patterns": []map[string]any{
			{"urlPattern": "*" + folderResourceMarker + "*", "requestStage": "Request"},
		},
	})
	if err != nil {
		logging.Detail("Could not enable preview blocking, continuing without: %v", err)
	}
}

// pageRootFrameID queries the page's own top frame ID directly, without
// requiring Page.enable first (Page.getFrameTree is a query, not an event
// subscription).
func pageRootFrameID(session playwright.CDPSession) (string, error) {
	result, err := session.Send("Page.getFrameTree", nil)
	if err != nil {
		return "", err
	}
	tree, ok := result.(map[string]any)
	if !ok {
		return "", fmt.Errorf("unexpected Page.getFrameTree result shape: %T", result)
	}
	frameTree, ok := tree["frameTree"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("Page.getFrameTree result has no frameTree: %v", tree)
	}
	frame, ok := frameTree["frame"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("Page.getFrameTree result has no frame: %v", frameTree)
	}
	id, ok := frame["id"].(string)
	if !ok || id == "" {
		return "", fmt.Errorf("Page.getFrameTree result has no frame id: %v", frame)
	}
	return id, nil
}

// isDiscardablePreview is the whole safety argument in one function. It takes
// plain values rather than a playwright.Request so the argument can be tested
// exhaustively without a browser - which matters more than usual here, because
// the expensive live A/B can only ever sample the cases a real account happens
// to contain. resourceType is expected lower-cased (CDP's own values are
// capitalized, e.g. "Document"; the call site normalizes before calling in).
func isDiscardablePreview(resourceType, url string, inSubframe bool) bool {
	// A subframe navigation is a preview nothing reads. A main-frame one is
	// the download path doing its job, and aborting it would break downloading
	// rather than speed up discovery.
	return resourceType == "document" &&
		inSubframe &&
		strings.Contains(url, folderResourceMarker)
}

// contentLengthFromHeaders reports what the request said it was about to
// send, for the saved-bytes figure - a best-effort number, same as the
// ctx.Route version this replaces (a GET preview request rarely carries a
// body, so this is usually 0; the bytes actually saved are the response the
// browser never fetches, which this vantage point cannot see). Looked up
// case-insensitively: CDP's raw request.headers preserves whatever casing the
// page's own script/markup used.
func contentLengthFromHeaders(headers map[string]any) int64 {
	for k, v := range headers {
		if !strings.EqualFold(k, "content-length") {
			continue
		}
		s, ok := v.(string)
		if !ok {
			return 0
		}
		var n int64
		for _, c := range strings.TrimSpace(s) {
			if c < '0' || c > '9' {
				return 0
			}
			n = n*10 + int64(c-'0')
		}
		return n
	}
	return 0
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
