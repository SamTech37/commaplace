package handlers

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"
)

func (s *Server) GetLogin(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	s.renderPage(w, r, pageTitle("登入"), "", nil, loginPage(loginProps{
		Sent:         q.Get("sent") == "1",
		Error:        q.Get("err"),
		Email:        q.Get("email"),
		Next:         q.Get("next"),
		OAuthEnabled: s.OAuthCfg != nil,
	}))
}

func (s *Server) PostLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.renderError(w, r, http.StatusBadRequest, "could not parse form")
		return
	}
	email := strings.TrimSpace(strings.ToLower(r.PostFormValue("email")))
	next := r.PostFormValue("next")

	if err := s.Auth.IssueToken(r.Context(), email); err != nil {
		v := url.Values{}
		v.Set("err", err.Error())
		v.Set("email", email)
		if next != "" {
			v.Set("next", next)
		}
		http.Redirect(w, r, "/login?"+v.Encode(), http.StatusSeeOther)
		return
	}

	v := url.Values{"sent": []string{"1"}}
	if next != "" {
		v.Set("next", next)
	}
	http.Redirect(w, r, "/login?"+v.Encode(), http.StatusSeeOther)
}

func (s *Server) GetAuthCallback(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if token == "" {
		s.renderError(w, r, http.StatusBadRequest, "missing token")
		return
	}
	u, err := s.Auth.ConsumeToken(r.Context(), token)
	if err != nil {
		// Use 400 for user-facing token errors.
		s.renderError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	s.Auth.SetSession(w, u.ID)

	next := r.URL.Query().Get("next")
	if next == "" || !strings.HasPrefix(next, "/") {
		// First-time users (no onboarded_at) land on the onboarding choice
		// before /me. Returning users skip straight to /me.
		if onb, _ := userOnboarded(r.Context(), s.DB, u.ID); !onb {
			next = "/onboarding"
		} else {
			next = "/me"
		}
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

// GetDevLogin signs you in as ?as=<handle>, creating the user on the fly if
// needed. Allowed when s.Debug is true (local dev), or when s.PlaytestKey is
// set and the request's ?key= matches it (locked-down playtest deploys —
// DEBUG stays off so error pages don't leak, but testers can still log in
// as any handle using the shared key as a stand-in password).
func (s *Server) GetDevLogin(w http.ResponseWriter, r *http.Request) {
	if !s.Debug && (s.PlaytestKey == "" || r.URL.Query().Get("key") != s.PlaytestKey) {
		s.renderError(w, r, http.StatusNotFound, "not found")
		return
	}
	handle := strings.TrimSpace(r.URL.Query().Get("as"))
	if handle == "" {
		handle = "dev"
	}
	var userID uuid.UUID
	err := s.DB.QueryRowContext(r.Context(),
		`SELECT id FROM users WHERE handle = $1`, handle,
	).Scan(&userID)
	if err != nil {
		err := s.DB.QueryRowContext(r.Context(),
			`INSERT INTO users(handle, handle_ci, email, created_at) VALUES($1, $2, $3, $4) RETURNING id`,
			handle, strings.ToLower(handle), handle+"@dev.local", nowUnix(),
		).Scan(&userID)
		if err != nil {
			s.renderError(w, r, http.StatusInternalServerError, err.Error())
			return
		}
	}
	s.Auth.SetSession(w, userID)
	next := r.URL.Query().Get("next")
	if next == "" || !strings.HasPrefix(next, "/") {
		next = "/me"
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (s *Server) GetOAuthGoogleStart(w http.ResponseWriter, r *http.Request) {
	if s.OAuthCfg == nil {
		s.renderError(w, r, http.StatusNotFound, "Google OAuth is not configured")
		return
	}
	s.Auth.StartGoogleOAuth(w, r, *s.OAuthCfg)
}

func (s *Server) GetOAuthGoogleCallback(w http.ResponseWriter, r *http.Request) {
	if s.OAuthCfg == nil {
		s.renderError(w, r, http.StatusNotFound, "Google OAuth is not configured")
		return
	}
	u, err := s.Auth.HandleGoogleCallback(w, r, *s.OAuthCfg)
	if err != nil {
		s.renderError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	s.Auth.SetSession(w, u.ID)

	if onb, _ := userOnboarded(r.Context(), s.DB, u.ID); !onb {
		http.Redirect(w, r, "/onboarding", http.StatusSeeOther)
	} else {
		http.Redirect(w, r, "/me", http.StatusSeeOther)
	}
}

func (s *Server) PostLogout(w http.ResponseWriter, r *http.Request) {
	s.Auth.ClearSession(w)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// GetMe redirects to the user's own public profile page, which doubles as
// their personal "me" view (with owner-only controls when IsSelf).
func (s *Server) GetMe(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	target := "/" + u.Handle
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

