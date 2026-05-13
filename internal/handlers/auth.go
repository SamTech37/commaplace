package handlers

import (
	"net/http"
	"net/url"
	"strings"
)

func (s *Server) GetLogin(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	s.render(w, r, "login", map[string]any{
		"Sent":  q.Get("sent") == "1",
		"Error": q.Get("err"),
		"Email": q.Get("email"),
		"Next":  q.Get("next"),
	})
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

func (s *Server) PostLogout(w http.ResponseWriter, r *http.Request) {
	s.Auth.ClearSession(w)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) GetMe(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	pinned, _ := pinnedNoteForUser(r.Context(), s.DB, u.ID)
	s.render(w, r, "me", map[string]any{"User": u, "Pinned": pinned})
}

