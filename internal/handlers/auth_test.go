package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"commonplace/internal/auth"
)

func TestMagicLinkSetsLastLoginCookie(t *testing.T) {
	s := newTestServer(t)
	if _, err := s.DB.Exec(
		`INSERT INTO auth_tokens(token, email, expires_at) VALUES('tok', 'a@example.com', $1)`,
		time.Now().Add(time.Hour).Unix(),
	); err != nil {
		t.Fatalf("insert token: %v", err)
	}
	req := httptest.NewRequest("GET", "/auth/tok", nil)
	req.SetPathValue("token", "tok")
	w := httptest.NewRecorder()
	s.GetAuthCallback(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", w.Code)
	}
	var got string
	for _, c := range w.Result().Cookies() {
		if c.Name == lastLoginCookie {
			got = c.Value
		}
	}
	if got != "email" {
		t.Fatalf("last_login cookie = %q, want email", got)
	}
}

func TestLoginMarksLastUsedMethod(t *testing.T) {
	s := newTestServer(t)
	oauth := &auth.OAuthConfig{ClientID: "id", ClientSecret: "secret"}
	cases := []struct {
		name   string
		oauth  *auth.OAuthConfig
		cookie string
		want   bool
	}{
		{"google", oauth, "google", true},
		{"email", oauth, "email", true},
		{"no cookie", oauth, "", false},
		{"unknown value", oauth, "<b>x</b>", false},
		// Without OAuth there is only one way in; nothing to choose between.
		{"email without oauth", nil, "email", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s.OAuthCfg = tc.oauth
			req := httptest.NewRequest("GET", "/login", nil)
			if tc.cookie != "" {
				req.AddCookie(&http.Cookie{Name: lastLoginCookie, Value: tc.cookie})
			}
			w := httptest.NewRecorder()
			s.GetLogin(w, req)
			if got := strings.Contains(w.Body.String(), `class="login-last"`); got != tc.want {
				t.Fatalf("marker shown = %v, want %v", got, tc.want)
			}
		})
	}
}

// The email pill sits inside the <label>; the input must still be named just 信箱.
func TestLoginEmailNameExcludesLastUsedPill(t *testing.T) {
	s := newTestServer(t)
	s.OAuthCfg = &auth.OAuthConfig{ClientID: "id", ClientSecret: "secret"}
	req := httptest.NewRequest("GET", "/login", nil)
	req.AddCookie(&http.Cookie{Name: lastLoginCookie, Value: "email"})
	w := httptest.NewRecorder()
	s.GetLogin(w, req)
	body := w.Body.String()
	for _, want := range []string{`<span id="login-email-label">信箱</span>`, `aria-labelledby="login-email-label"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s", want)
		}
	}
}

// /_dev/login mints users from ?as= on playtest deploys; a handle outside
// handleRE must not be created (it would flow into suggest fragments).
func TestDevLoginRejectsInvalidHandle(t *testing.T) {
	s := newTestServer(t)
	s.PlaytestKey = "k"
	mkUser(t, s, "shawn_demo")
	cases := []struct {
		as   string
		want int
	}{
		{`x" onmouseover="alert(1)`, http.StatusBadRequest},
		{"Alice", http.StatusBadRequest},
		{"alice", http.StatusSeeOther},
		{"shawn_demo", http.StatusSeeOther}, // existing seed handle still logs in
	}
	for _, tc := range cases {
		req := httptest.NewRequest("GET", "/_dev/login?key=k&as="+url.QueryEscape(tc.as), nil)
		w := httptest.NewRecorder()
		s.GetDevLogin(w, req)
		if w.Code != tc.want {
			t.Fatalf("as=%q: status = %d, want %d", tc.as, w.Code, tc.want)
		}
		if gotCookie := len(w.Result().Cookies()) > 0; gotCookie != (tc.want == http.StatusSeeOther) {
			t.Fatalf("as=%q: session cookie set = %v", tc.as, gotCookie)
		}
	}
	var n int
	if err := s.DB.QueryRow(`SELECT count(*) FROM users`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("users = %d, want 2 (shawn_demo + alice)", n)
	}
}
