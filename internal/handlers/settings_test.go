package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"commonplace/internal/auth"
)

func postHandle(t *testing.T, s *Server, uid uuid.UUID, newHandle string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{"handle": {newHandle}}
	req := httptest.NewRequest("POST", "/settings/handle", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: auth.SessionCookie, Value: s.Auth.Sign(uid)})
	rec := httptest.NewRecorder()
	s.PostHandleSetting(rec, req)
	return rec
}

func TestPostHandleSetting(t *testing.T) {
	s := newTestServer(t)
	alice := mkUser(t, s, "alice")
	mkUser(t, s, "bob")

	// happy path: rename succeeds, redirects to the new profile, DB updated.
	rec := postHandle(t, s, alice, "alice2")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("rename: want 303, got %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/alice2" {
		t.Fatalf("rename: want redirect /alice2, got %q", loc)
	}
	var h, hci string
	if err := s.DB.QueryRow(`SELECT handle, handle_ci FROM users WHERE id=$1`, alice).Scan(&h, &hci); err != nil {
		t.Fatal(err)
	}
	if h != "alice2" || hci != "alice2" {
		t.Fatalf("db: want alice2/alice2, got %q/%q", h, hci)
	}

	cases := []struct {
		name, handle string
		want         int
	}{
		{"reserved", "feed", http.StatusBadRequest},
		{"taken", "bob", http.StatusConflict},
		{"bad-format", "Bad_Name!", http.StatusBadRequest},
		{"leading-hyphen", "-nope", http.StatusBadRequest},
		{"uppercase-folds-to-taken", "BOB", http.StatusConflict},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := postHandle(t, s, alice, c.handle)
			if rec.Code != c.want {
				t.Fatalf("%s=%q: want %d, got %d", c.name, c.handle, c.want, rec.Code)
			}
		})
	}
}
