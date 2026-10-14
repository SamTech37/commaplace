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
// userinfo with *email and email_verified.
func fakeGoogle(t *testing.T, email *string, verified bool) {
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
		json.NewEncoder(w).Encode(map[string]any{"email": *email, "email_verified": verified})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Cleanup(auth.UseFakeGoogle(srv.URL+"/token", srv.URL+"/userinfo"))
}

func googleCallback(a *auth.Auth) (*auth.User, error) {
	req := httptest.NewRequest("GET", "/auth/google/callback?state=s&code=c", nil)
	req.AddCookie(&http.Cookie{Name: "oauth_state", Value: "s"})
	return a.HandleGoogleCallback(httptest.NewRecorder(), req, auth.OAuthConfig{ClientID: "id", ClientSecret: "secret"})
}

func googleSignIn(t *testing.T, a *auth.Auth) *auth.User {
	t.Helper()
	u, err := googleCallback(a)
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
			fakeGoogle(t, &tc.googleEmail, true)
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
	fakeGoogle(t, &email, true)
	req := httptest.NewRequest("GET", "/auth/google/callback?state=other&code=c", nil)
	req.AddCookie(&http.Cookie{Name: "oauth_state", Value: "s"})
	if _, err := a.HandleGoogleCallback(httptest.NewRecorder(), req, auth.OAuthConfig{}); err == nil {
		t.Fatal("want error for mismatched state")
	}
}

// An unverified Google email must not create an account or sign into the
// magic-link account that owns that address.
func TestGoogleRejectsUnverifiedEmail(t *testing.T) {
	a := newTestAuth(t)
	email := "alice@example.com"
	fakeGoogle(t, &email, false)
	if _, err := googleCallback(a); err == nil {
		t.Fatal("want error for unverified email")
	}
	var n int
	a.DB.QueryRow(`SELECT count(*) FROM users`).Scan(&n)
	if n != 0 {
		t.Fatalf("users = %d, want 0", n)
	}
	magicSignIn(t, a, email)
	if u, err := googleCallback(a); err == nil {
		t.Fatalf("unverified Google email signed into %s", u.Handle)
	}
}

// Rows differing only by email case (left by the old case-sensitive match)
// resolve to the oldest account, even after an UPDATE moves its row on disk.
func TestCaseDuplicateEmailsPickOldest(t *testing.T) {
	a := newTestAuth(t)
	var oldest string
	if err := a.DB.QueryRow(
		`INSERT INTO users(handle, handle_ci, email, created_at) VALUES('alice', 'alice', 'Alice@Example.com', 100) RETURNING id`,
	).Scan(&oldest); err != nil {
		t.Fatal(err)
	}
	if _, err := a.DB.Exec(
		`INSERT INTO users(handle, handle_ci, email, created_at) VALUES('alice2', 'alice2', 'alice@example.com', 200)`,
	); err != nil {
		t.Fatal(err)
	}
	if u := magicSignIn(t, a, "alice@example.com"); u.ID.String() != oldest {
		t.Fatalf("before update: got %s, want oldest account", u.Handle)
	}
	if _, err := a.DB.Exec(`UPDATE users SET theme = 'dark' WHERE id = $1`, oldest); err != nil {
		t.Fatal(err)
	}
	if u := magicSignIn(t, a, "alice@example.com"); u.ID.String() != oldest {
		t.Fatalf("after update: got %s, want oldest account", u.Handle)
	}
}
