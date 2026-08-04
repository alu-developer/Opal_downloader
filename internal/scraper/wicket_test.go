package scraper

import (
	"errors"
	"strings"
	"testing"

	"github.com/mxschmitt/playwright-go"
)

// wicketFakePage drives wicket.go's two helpers without a browser. It embeds
// the (nil) playwright.Page interface so it satisfies it for free, and only
// overrides the three methods these helpers actually call.
type wicketFakePage struct {
	playwright.Page

	// evalResults are returned from Evaluate in order; evalErrs likewise.
	evalResults []interface{}
	evalErrs    []error
	evalCalls   int

	waitErr  error
	waitCall int

	url string
}

func (f *wicketFakePage) Evaluate(expression string, arg ...interface{}) (interface{}, error) {
	i := f.evalCalls
	f.evalCalls++
	var err error
	if i < len(f.evalErrs) {
		err = f.evalErrs[i]
	}
	if err != nil {
		return nil, err
	}
	if i < len(f.evalResults) {
		return f.evalResults[i], nil
	}
	return nil, nil
}

func (f *wicketFakePage) WaitForFunction(expression string, arg interface{}, options ...playwright.PageWaitForFunctionOptions) (playwright.JSHandle, error) {
	f.waitCall++
	return nil, f.waitErr
}

func (f *wicketFakePage) URL() string { return f.url }

func TestArmWicketExpansionWatchReportsWhetherWicketIsPresent(t *testing.T) {
	tests := []struct {
		name      string
		result    interface{}
		evalErr   error
		wantArmed bool
		wantErr   bool
	}{
		{name: "wicket present", result: true, wantArmed: true},
		{name: "not a wicket page", result: false, wantArmed: false},
		{name: "non-boolean result is not armed", result: "yes", wantArmed: false},
		{name: "evaluate failed", evalErr: errors.New("execution context was destroyed"), wantArmed: false, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			page := &wicketFakePage{evalResults: []interface{}{tc.result}}
			if tc.evalErr != nil {
				page.evalErrs = []error{tc.evalErr}
			}
			armed, err := armWicketExpansionWatch(page)
			if armed != tc.wantArmed {
				t.Errorf("armed = %v, want %v", armed, tc.wantArmed)
			}
			if (err != nil) != tc.wantErr {
				t.Errorf("err = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestArmWicketExpansionWatchHandlesNilPage(t *testing.T) {
	armed, err := armWicketExpansionWatch(nil)
	if armed || err != nil {
		t.Fatalf("armWicketExpansionWatch(nil) = (%v, %v), want (false, nil)", armed, err)
	}
}

// The critical contract: AJAX_CALL_DONE fires for BOTH a successful and a
// failed expansion, so a caller that only learns "done" would treat a dropped
// expansion as a completed one and silently lose the files past page 1. These
// cases pin down that failed is reported independently of signalled.
func TestAwaitWicketExpansionDoneSeparatesCompletionFromFailure(t *testing.T) {
	tests := []struct {
		name          string
		waitErr       error
		failedResult  interface{}
		failedErr     error
		wantSignalled bool
		wantFailed    bool
	}{
		{
			name:          "completed successfully",
			failedResult:  false,
			wantSignalled: true,
			wantFailed:    false,
		},
		{
			name:          "completed but wicket reported AJAX_CALL_FAILURE",
			failedResult:  true,
			wantSignalled: true,
			wantFailed:    true,
		},
		{
			name:          "event never arrived within the budget",
			waitErr:       errors.New("timeout 4000ms exceeded"),
			wantSignalled: false,
			wantFailed:    false,
		},
		{
			// Cannot vouch for success -> report not-signalled so the caller
			// takes the stability-poll fallback. Safe direction.
			name:          "failure flag unreadable",
			failedErr:     errors.New("execution context was destroyed"),
			wantSignalled: false,
			wantFailed:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			page := &wicketFakePage{
				waitErr:     tc.waitErr,
				evalResults: []interface{}{tc.failedResult},
			}
			if tc.failedErr != nil {
				page.evalErrs = []error{tc.failedErr}
			}

			signalled, failed, _ := awaitWicketExpansionDone(page, 4000)
			if signalled != tc.wantSignalled {
				t.Errorf("signalled = %v, want %v", signalled, tc.wantSignalled)
			}
			if failed != tc.wantFailed {
				t.Errorf("failed = %v, want %v", failed, tc.wantFailed)
			}
		})
	}
}

func TestAwaitWicketExpansionDoneDoesNotReadFailureFlagAfterTimeout(t *testing.T) {
	// On timeout there is no completed call to ask about; evaluating anyway
	// would be a wasted round-trip against a page that may be mid-navigation.
	page := &wicketFakePage{waitErr: errors.New("timeout")}
	if signalled, _, _ := awaitWicketExpansionDone(page, 4000); signalled {
		t.Fatal("signalled = true after a wait timeout")
	}
	if page.evalCalls != 0 {
		t.Errorf("Evaluate called %d times after timeout, want 0", page.evalCalls)
	}
}

func TestAwaitWicketExpansionDoneHandlesNilPage(t *testing.T) {
	signalled, failed, waitErr := awaitWicketExpansionDone(nil, 4000)
	if signalled || failed || waitErr != nil {
		t.Fatalf("awaitWicketExpansionDone(nil) = (%v, %v, %v), want (false, false, nil)", signalled, failed, waitErr)
	}
}

func TestArmedScriptTargetsBothWicketTopics(t *testing.T) {
	// Guards the non-negotiable part of the contract at the source level: if
	// someone drops the FAILURE subscription, DONE alone silently reports a
	// dropped expansion as a good one.
	page := &wicketFakePage{evalResults: []interface{}{true}}
	var seen string
	page.evalResults = []interface{}{true}
	captured := &wicketScriptCapturePage{wicketFakePage: page, script: &seen}

	if _, err := armWicketExpansionWatch(captured); err != nil {
		t.Fatalf("arm: %v", err)
	}
	for _, topic := range []string{"AJAX_CALL_DONE", "AJAX_CALL_FAILURE"} {
		if !strings.Contains(seen, topic) {
			t.Errorf("arming script does not subscribe to %s:\n%s", topic, seen)
		}
	}
	if !strings.Contains(seen, wicketWatchVar) {
		t.Errorf("arming script does not write to %s", wicketWatchVar)
	}
}

type wicketScriptCapturePage struct {
	*wicketFakePage
	script *string
}

func (f *wicketScriptCapturePage) Evaluate(expression string, arg ...interface{}) (interface{}, error) {
	*f.script = expression
	return f.wicketFakePage.Evaluate(expression, arg...)
}

func TestIsWicketWatchUnavailableError(t *testing.T) {
	tests := []struct {
		err  error
		want bool
	}{
		{err: nil, want: false},
		{err: errors.New("Execution context was destroyed"), want: true},
		{err: errors.New("page is navigating and changing the content"), want: true},
		{err: errors.New("target closed"), want: true},
		{err: errors.New("some unrelated failure"), want: false},
	}
	for _, tc := range tests {
		if got := isWicketWatchUnavailableError(tc.err); got != tc.want {
			t.Errorf("isWicketWatchUnavailableError(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}

func TestExpandedPageURLAfterClick(t *testing.T) {
	tests := []struct {
		name       string
		pageURL    string
		currentURL string
		want       string
	}{
		{
			name:       "wicket page-instance counter appended",
			pageURL:    "https://opal.example/auth/RepositoryEntry/1/CourseNode/2/Vorlesung?1032",
			currentURL: "https://opal.example/auth/RepositoryEntry/1/CourseNode/2/Vorlesung",
			want:       "https://opal.example/auth/RepositoryEntry/1/CourseNode/2/Vorlesung?1032",
		},
		{
			name:       "unchanged url records nothing",
			pageURL:    "https://opal.example/auth/RepositoryEntry/1/CourseNode/2",
			currentURL: "https://opal.example/auth/RepositoryEntry/1/CourseNode/2",
			want:       "",
		},
		{
			name:       "comparison ignores case and surrounding space",
			pageURL:    "  https://OPAL.example/Auth/X  ",
			currentURL: "https://opal.example/auth/x",
			want:       "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			page := &wicketFakePage{url: tc.pageURL}
			if got := expandedPageURLAfterClick(page, tc.currentURL); got != tc.want {
				t.Errorf("expandedPageURLAfterClick() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestExpandedPageURLAfterClickHandlesNilPage(t *testing.T) {
	if got := expandedPageURLAfterClick(nil, "https://opal.example/x"); got != "" {
		t.Fatalf("expandedPageURLAfterClick(nil) = %q, want empty", got)
	}
}
