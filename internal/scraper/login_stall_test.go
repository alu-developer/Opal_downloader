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
// probing must not shorten the overall time before giving up.
func TestLoginBudgetConstantsLeaveRoomForAHuman(t *testing.T) {
	if loginStallProbeMs >= loginTotalBudgetMs {
		t.Fatal("the stall probe must be much shorter than the total budget, or there is no time left to wait for a human")
	}
	spentOnProbes := loginStallProbeMs * loginStallReloadAttempts
	if spentOnProbes >= loginTotalBudgetMs/2 {
		t.Fatalf("reload probes consume %dms of a %dms budget, leaving too little for a human completing 2FA by hand",
			spentOnProbes, loginTotalBudgetMs)
	}
	if loginStallReloadAttempts < 1 || loginStallReloadAttempts > 2 {
		t.Fatalf("reload attempts must stay small (1-2); this is a workaround, not a retry loop, got %d", loginStallReloadAttempts)
	}
}
