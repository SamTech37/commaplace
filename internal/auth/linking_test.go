package auth_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"commonplace/internal/auth"
)

// fakeGoogle serves the token exchange and userinfo endpoints, answering
// userinfo with *email.
func fakeGoogle(t *testing.T, email *string) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"access_token": "at", "token_type": "Bearer"})
	})
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer at" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"email": *email})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Cleanup(auth.UseFakeGoogle(srv.URL+"/token", srv.URL+"/userinfo"))
}

func googleSignIn(t *testing.T, a *auth.Auth) *auth.User {
	t.Helper()
	req := httptest.NewRequest("GET", "/auth/google/callback?state=s&code=c", nil)
	req.AddCookie(&http.Cookie{Name: "oauth_state", Value: "s"})
	u, err := a.HandleGoogleCallback(httptest.NewRecorder(), req, auth.OAuthConfig{ClientID: "id", ClientSecret: "secret"})
	if err != nil {
		t.Fatalf("HandleGoogleCallback: %v", err)
	}
	return u
}

func magicSignIn(t *testing.T, a *auth.Auth, email string) *auth.User {
	t.Helper()
	u, err := a.ConsumeToken(context.Background(), mustToken(t, a, email))
	if err != nil {
		t.Fatalf("ConsumeToken: %v", err)
	}
	return u
}

// One email through either channel, in either order, is one account.
func TestMagicLinkAndGoogleShareAccount(t *testing.T) {
	cases := []struct {
		name        string
		typed       string // what the user types into /login
		googleEmail string // what Google's userinfo returns
		googleFirst bool
	}{
		{"magic link first", "alice@example.com", "alice@example.com", false},
		{"google first", "alice@example.com", "alice@example.com", true},
		{"google returns mixed case", "alice@example.com", "Alice@Example.com", false},
		{"mixed case, google first", "alice@example.com", "Alice@Example.com", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := newTestAuth(t)
			fakeGoogle(t, &tc.googleEmail)
			var first, second *auth.User
			if tc.googleFirst {
				first, second = googleSignIn(t, a), magicSignIn(t, a, tc.typed)
			} else {
				first, second = magicSignIn(t, a, tc.typed), googleSignIn(t, a)
			}
			if first.ID != second.ID {
				t.Fatalf("two accounts: %s (%s) and %s (%s)", first.Handle, first.Email, second.Handle, second.Email)
			}
			var n int
			a.DB.QueryRow(`SELECT count(*) FROM users`).Scan(&n)
			if n != 1 {
				t.Fatalf("users = %d, want 1", n)
			}
		})
	}
}

func TestGoogleCallbackRejectsStateMismatch(t *testing.T) {
	a := newTestAuth(t)
	email := "alice@example.com"
	fakeGoogle(t, &email)
	req := httptest.NewRequest("GET", "/auth/google/callback?state=other&code=c", nil)
	req.AddCookie(&http.Cookie{Name: "oauth_state", Value: "s"})
	if _, err := a.HandleGoogleCallback(httptest.NewRecorder(), req, auth.OAuthConfig{}); err == nil {
		t.Fatal("want error for mismatched state")
	}
}
