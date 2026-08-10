package scraper

import "testing"

// The reload workaround must only fire while the browser is still somewhere
// in the login/IdP flow. Reloading after login has progressed could discard
// a half-finished 2FA exchange - turning a rare annoyance into a real
// failure - so this predicate is the whole safety boundary.
func TestLooksLikeLoginPageURL(t *testing.T) {
	stillLoggingIn := []string{
		"https://bildungsportal.sachsen.de/opal/shiblogin",
		"https://idp.tu-dresden.de/idp/profile/SAML2/Redirect/SSO",
		"https://bildungsportal.sachsen.de/opal/login",
		"https://SOMEHOST/IDP/Profile", // case-insensitive
		"https://example.test/Shibboleth.sso/Login",
	}
	for _, u := range stillLoggingIn {
		if !looksLikeLoginPageURL(u) {
			t.Errorf("expected %q to be treated as a login-flow URL", u)
		}
	}

	alreadyPastLogin := []string{
		"https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/50696421377",
		"https://bildungsportal.sachsen.de/opal/auth/home",
		"https://bildungsportal.sachsen.de/opal/",
	}
	for _, u := range alreadyPastLogin {
		if looksLikeLoginPageURL(u) {
			t.Errorf("expected %q NOT to be treated as a login-flow URL - reloading here could discard an in-progress 2FA exchange", u)
		}
	}
}

// A nil page must not panic the login path.
func TestReloadStalledLoginPageToleratesNilPage(t *testing.T) {
	s := New("", "")
	if s.reloadStalledLoginPage(nil, 1) {
		t.Fatal("expected no reload to be reported for a nil page")
	}
}

// The budget arithmetic is what keeps a genuine human 2FA login working:
// watching for a stall must not shorten the overall time before giving up.
func TestLoginBudgetConstantsLeaveRoomForAHuman(t *testing.T) {
	if loginQuietMs >= loginTotalBudgetMs {
		t.Fatal("the quiet threshold must be much shorter than the total budget, or there is no time left to wait for a human")
	}
	if loginPollMs >= loginQuietMs {
		t.Fatal("the poll interval must be shorter than the quiet threshold, or a stall can never be observed")
	}
	spentBeforeGivingUp := loginQuietMs * loginStallReloadAttempts
	if spentBeforeGivingUp >= loginTotalBudgetMs/2 {
		t.Fatalf("stall handling consumes %dms of a %dms budget, leaving too little for a human completing 2FA by hand",
			spentBeforeGivingUp, loginTotalBudgetMs)
	}
	if loginStallReloadAttempts < 1 || loginStallReloadAttempts > 2 {
		t.Fatalf("reload attempts must stay small (1-2); this is a workaround, not a retry loop, got %d", loginStallReloadAttempts)
	}
	// The whole point of the rewrite: a stalled login is noticed in seconds,
	// not after a fixed three-quarters of a minute.
	if loginQuietMs > 15000 {
		t.Fatalf("waiting %dms before noticing a dead login page is the fixed-timer behaviour this replaced", loginQuietMs)
	}
}

// stalled() is the safety boundary. It decides whether a page gets reloaded,
// and a reload at the wrong moment throws away whatever was on it.
func TestStalledOnlyMatchesAnUntouchedLoginPage(t *testing.T) {
	const loginURL = "https://bildungsportal.sachsen.de/opal/shiblogin"
	const opalURL = "https://bildungsportal.sachsen.de/opal/auth/home"

	if !(loginSignals{URL: loginURL, FieldCount: 2, FilledFields: 0}).stalled() {
		t.Error("an untouched login form is exactly the case this must catch")
	}

	// This is the one the old fixed timer got wrong: a human typing their
	// password stays on a login URL, so a timer-driven reload wiped it.
	if (loginSignals{URL: loginURL, FieldCount: 2, FilledFields: 1}).stalled() {
		t.Error("a login page with something typed into it must never be reloaded")
	}
	if (loginSignals{URL: opalURL, FieldCount: 2, FilledFields: 0}).stalled() {
		t.Error("a page past the login flow must never be reloaded")
	}
	// REVERSED 2026-08-10. This used to assert the opposite - that a
	// login-flow page with no fields is "a redirect or a 2FA wait, not a
	// stall" - and that assumption is what let all three unexplained 300s
	// login timeouts run their full budget with no reload. The third one
	// recorded where it was stuck: ".../opal/shiblogin;jsessionid=...", the
	// Shibboleth IdP's processing screen, which has no fields, so it could
	// never be called stalled and was never nudged.
	//
	// The redirect-in-progress case the old assertion was protecting is
	// already covered a level up: stalled() is only consulted after
	// loginQuietMs with no observable movement, and changedFrom treats a URL
	// change, a field-count change and an unreadable page all as movement. A
	// real redirect moves; this one sat still for five minutes.
	if !(loginSignals{URL: loginURL, FieldCount: 0, FilledFields: 0}).stalled() {
		t.Error("a login-flow page sitting still with nothing to fill in is the Shibboleth stall this must now catch")
	}
	if (loginSignals{Unknown: true}).stalled() {
		t.Error("a page that could not be read must never be treated as stalled")
	}
}

// changedFrom decides whether the page is showing any sign of life, which is
// what stops the quiet timer from running while a login is actually working.
func TestChangedFromNoticesEverySignOfLife(t *testing.T) {
	base := loginSignals{URL: "https://idp.tu-dresden.de/idp/profile/SAML2/Redirect/SSO", FieldCount: 2, FilledFields: 0}

	if base.changedFrom(base) {
		t.Error("an unchanged page must not read as movement, or a stall is never detected")
	}

	moved := []struct {
		what string
		to   loginSignals
	}{
		{"TU-Fast filled the fields", loginSignals{URL: base.URL, FieldCount: 2, FilledFields: 2}},
		{"the flow navigated", loginSignals{URL: "https://bildungsportal.sachsen.de/opal/auth/home", FieldCount: 2}},
		{"a 2FA prompt replaced the password form", loginSignals{URL: base.URL, FieldCount: 1}},
		{"the page could not be read", loginSignals{Unknown: true}},
	}
	for _, m := range moved {
		if !m.to.changedFrom(base) {
			t.Errorf("%s must count as the page doing something", m.what)
		}
	}
}

// An unreadable page must never start the reload clock. A page mid-navigation
// is unreadable and is also exactly what a working login looks like.
func TestAnUnreadablePageIsTreatedAsActivity(t *testing.T) {
	unknown := loginSignals{Unknown: true}
	if !unknown.changedFrom(loginSignals{URL: "https://example.test/login", FieldCount: 1}) {
		t.Error("an unreadable reading must count as movement")
	}
	if !(loginSignals{URL: "https://example.test/login", FieldCount: 1}).changedFrom(unknown) {
		t.Error("a reading taken after an unreadable one must count as movement")
	}
}

// The signal reader must tolerate a nil page rather than panicking a login.
func TestReadLoginSignalsToleratesNilPage(t *testing.T) {
	s := New("", "")
	if got := s.readLoginSignals(nil); !got.Unknown {
		t.Fatalf("expected a nil page to read as Unknown, got %+v", got)
	}
}

// The specific page that produced all three 300s login timeouts. Kept as its
// own test with the real URL shape, because the mechanism was only identified
// after the third occurrence and the URL is the evidence that identified it.
func TestShibbolethProcessingScreenCountsAsStalled(t *testing.T) {
	const shibURL = "https://bildungsportal.sachsen.de/opal/shiblogin;jsessionid=ABC123?0"

	// No fields: it is an interstitial, not a form.
	if !(loginSignals{URL: shibURL, FieldCount: 0, FilledFields: 0}).stalled() {
		t.Fatal("the Shibboleth processing screen must be reloadable, or a stall there costs the full 300s budget")
	}
	// And it is still a login-flow URL by the reload guard's own test, so
	// reloadStalledLoginPage will act on it rather than skip it.
	if !looksLikeLoginPageURL(shibURL) {
		t.Fatal("reloadStalledLoginPage would refuse to reload this URL, making stalled() moot")
	}
}
