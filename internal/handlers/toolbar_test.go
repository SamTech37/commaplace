package handlers

import (
	"context"
	"strings"
	"testing"

	"commonplace/internal/auth"
	"github.com/a-h/templ"
)

// renderChrome renders the shared page chrome so the toolbar can be asserted
// on without standing up a database or a real request.
func renderChrome(t *testing.T, u *auth.User) string {
	t.Helper()
	var sb strings.Builder
	c := ChromeProps{User: u, Site: siteCfg, Nav: navCfg}
	if err := Layout(c, "test", "", nil, templ.NopComponent).Render(context.Background(), &sb); err != nil {
		t.Fatalf("render layout: %v", err)
	}
	return sb.String()
}

// navLinks returns the .nav-links group — the run of links that collapses into
// the ≡ menu on narrow screens.
func navLinks(t *testing.T, html string) string {
	t.Helper()
	const open = `<div class="nav-links">`
	i := strings.Index(html, open)
	if i < 0 {
		t.Fatal(`no <div class="nav-links"> in rendered chrome`)
	}
	rest := html[i+len(open):]
	j := strings.Index(rest, "</div>")
	if j < 0 {
		t.Fatal("unterminated nav-links block")
	}
	return rest[:j]
}

// TestNavLinksAreNavigationOnly is the point of the whole rework: the row of
// links should be destinations and nothing else. Logout is destructive and the
// display preferences are set-once settings; neither belongs at the same weight
// and spacing as 書寫 or 閱覽.
func TestNavLinksAreNavigationOnly(t *testing.T) {
	nav := navLinks(t, renderChrome(t, &auth.User{Handle: "alice", Theme: "auto"}))

	for _, want := range []string{navCfg.Write, navCfg.Feed, navCfg.Graph, "漫遊"} {
		if !strings.Contains(nav, want) {
			t.Errorf("navigation link %q missing from nav-links", want)
		}
	}
	if strings.Contains(nav, navCfg.Logout) {
		t.Errorf("%q is still among the navigation links", navCfg.Logout)
	}
	if strings.Contains(nav, "data-pref-set") {
		t.Error("a preference control is still among the navigation links")
	}
}

// TestOldToggleButtonsAreGone guards against the single-glyph cycling buttons
// creeping back: 繁 was ambiguous (current state, or what clicking does?) and
// the theme one could never return to "follow the system".
func TestOldToggleButtonsAreGone(t *testing.T) {
	html := renderChrome(t, &auth.User{Handle: "alice"})
	for _, id := range []string{`id="theme-toggle"`, `id="motion-toggle"`, `id="script-toggle"`} {
		if strings.Contains(html, id) {
			t.Errorf("%s is back in the toolbar; preferences belong in the menu", id)
		}
	}
}

// TestLogoutLivesInExactlyOneMenu covers the duplication the rework removed:
// logout used to render in the nav row and again in the ≡ menu, with the avatar
// beside them as a third account entry point.
func TestLogoutLivesInExactlyOneMenu(t *testing.T) {
	html := renderChrome(t, &auth.User{Handle: "alice"})

	if n := strings.Count(html, `action="/logout"`); n != 1 {
		t.Errorf("found %d logout forms, want exactly 1", n)
	}
	if !strings.Contains(html, `aria-haspopup="menu"`) {
		t.Error("account menu trigger is missing aria-haspopup")
	}
	if !strings.Contains(html, `href="/alice"`) {
		t.Error("profile link missing — the avatar used to be that link")
	}
}

// TestVisitorsCanReachPreferences is the gap the original spec left open: the
// menu hangs off the avatar, and a signed-out visitor has no avatar. They still
// need to be able to switch to dark mode.
func TestVisitorsCanReachPreferences(t *testing.T) {
	html := renderChrome(t, nil)

	if strings.Contains(html, `action="/logout"`) {
		t.Error("logout rendered for a signed-out visitor")
	}
	if !strings.Contains(html, `class="action-menu prefs-menu"`) {
		t.Error("signed-out visitor has no settings menu")
	}
	if !strings.Contains(html, `href="/login"`) {
		t.Error("signed-out visitor has no way to log in")
	}
	assertPrefSegments(t, html)
}

// TestPreferencesRenderForSignedInUsers pairs with the visitor case: both menus
// share one component, so both must carry the full set of controls.
func TestPreferencesRenderForSignedInUsers(t *testing.T) {
	assertPrefSegments(t, renderChrome(t, &auth.User{Handle: "alice"}))
}

// assertPrefSegments checks every segment of every control is rendered. The
// first value of each is the "don't override, follow the system" option, which
// is what the old cycling buttons could not express.
func assertPrefSegments(t *testing.T, html string) {
	t.Helper()
	want := map[string][]string{
		"theme":  {"auto", "light", "dark"},
		"motion": {"system", "full", "reduced"},
		"script": {"orig", "cn", "tw"},
	}
	for kind, vals := range want {
		for _, v := range vals {
			attr := `data-pref-set="` + kind + `" data-val="` + v + `"`
			if !strings.Contains(html, attr) {
				t.Errorf("missing segment: %s", attr)
			}
		}
	}
}
