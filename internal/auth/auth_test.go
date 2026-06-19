package auth_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"commonplace/internal/auth"
	"commonplace/internal/db"
)

func newTestAuth(t *testing.T) *auth.Auth {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	d, err := db.Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Migrate(d); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := d.Exec(`TRUNCATE users, notes, links, note_tags, likes, follows, reports, auth_tokens, note_images CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	t.Cleanup(func() {
		d.Exec(`TRUNCATE users, notes, links, note_tags, likes, follows, reports, auth_tokens, note_images CASCADE`)
		d.Close()
	})
	return &auth.Auth{DB: d, Secret: []byte("test")}
}

// --- pure unit tests ---

func TestSignVerifyRoundtrip(t *testing.T) {
	a := &auth.Auth{Secret: []byte("secret")}
	id := uuid.New()
	got, err := a.Verify(a.Sign(id))
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got != id {
		t.Fatalf("want %v, got %v", id, got)
	}
}

func TestVerifyMalformed(t *testing.T) {
	a := &auth.Auth{Secret: []byte("secret")}
	if _, err := a.Verify("nodot"); err == nil {
		t.Fatal("expected error for malformed cookie")
	}
}

func TestVerifyBadSignature(t *testing.T) {
	a := &auth.Auth{Secret: []byte("secret")}
	b := &auth.Auth{Secret: []byte("other")}
	id := uuid.New()
	_, err := b.Verify(a.Sign(id))
	if err == nil || err.Error() != "bad signature" {
		t.Fatalf("want 'bad signature', got %v", err)
	}
}

func TestIsReservedHandle(t *testing.T) {
	for _, h := range []string{"login", "me", "api", "feed", "auth", "settings", "tag", "search"} {
		if !auth.IsReservedHandle(h) {
			t.Errorf("%q should be reserved", h)
		}
	}
	for _, h := range []string{"alice", "bob", "admin", "sam"} {
		if auth.IsReservedHandle(h) {
			t.Errorf("%q should not be reserved", h)
		}
	}
}

func TestSetSession(t *testing.T) {
	a := &auth.Auth{Secret: []byte("secret")}
	w := httptest.NewRecorder()
	a.SetSession(w, uuid.New())
	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("no cookie set")
	}
	c := cookies[0]
	if c.Name != auth.SessionCookie {
		t.Errorf("name: want %q, got %q", auth.SessionCookie, c.Name)
	}
	if c.MaxAge != 60*60*24*30 {
		t.Errorf("MaxAge: want %d, got %d", 60*60*24*30, c.MaxAge)
	}
	if !c.HttpOnly {
		t.Error("want HttpOnly=true")
	}
}

func TestClearSession(t *testing.T) {
	a := &auth.Auth{Secret: []byte("secret")}
	w := httptest.NewRecorder()
	a.ClearSession(w)
	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("no cookie set")
	}
	if cookies[0].MaxAge != -1 {
		t.Errorf("want MaxAge=-1, got %d", cookies[0].MaxAge)
	}
}

// mustToken issues a magic-link token for email and returns the raw token string.
func mustToken(t *testing.T, a *auth.Auth, email string) string {
	t.Helper()
	if err := a.IssueToken(context.Background(), email); err != nil {
		t.Fatalf("IssueToken(%s): %v", email, err)
	}
	var tok string
	if err := a.DB.QueryRow(
		`SELECT token FROM auth_tokens WHERE email = $1 AND used_at IS NULL`, email,
	).Scan(&tok); err != nil {
		t.Fatalf("get token for %s: %v", email, err)
	}
	return tok
}

// --- DB tests ---

func TestIssueTokenValid(t *testing.T) {
	a := newTestAuth(t)
	if err := a.IssueToken(context.Background(), "test@example.com"); err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	var count int
	if err := a.DB.QueryRow(`SELECT COUNT(*) FROM auth_tokens WHERE email = 'test@example.com'`).Scan(&count); err != nil {
		t.Fatalf("scan count: %v", err)
	}
	if count != 1 {
		t.Fatalf("want 1 token row, got %d", count)
	}
}

func TestIssueTokenInvalidEmail(t *testing.T) {
	a := newTestAuth(t)
	if err := a.IssueToken(context.Background(), "notanemail"); err == nil {
		t.Fatal("expected error for invalid email")
	}
}

func TestConsumeTokenValid(t *testing.T) {
	a := newTestAuth(t)
	ctx := context.Background()
	tok := mustToken(t, a, "user@example.com")
	u, err := a.ConsumeToken(ctx, tok)
	if err != nil {
		t.Fatalf("ConsumeToken: %v", err)
	}
	if u.Email != "user@example.com" {
		t.Errorf("want email user@example.com, got %s", u.Email)
	}
}

func TestConsumeTokenAlreadyUsed(t *testing.T) {
	a := newTestAuth(t)
	ctx := context.Background()
	tok := mustToken(t, a, "used@example.com")
	if _, err := a.ConsumeToken(ctx, tok); err != nil {
		t.Fatalf("first ConsumeToken: %v", err)
	}
	_, err := a.ConsumeToken(ctx, tok)
	if err == nil || err.Error() != "link already used" {
		t.Fatalf("want 'link already used', got %v", err)
	}
}

func TestConsumeTokenInvalid(t *testing.T) {
	a := newTestAuth(t)
	_, err := a.ConsumeToken(context.Background(), "nonexistent")
	if err == nil || err.Error() != "invalid link" {
		t.Fatalf("want 'invalid link', got %v", err)
	}
}

func TestConsumeTokenExpired(t *testing.T) {
	a := newTestAuth(t)
	ctx := context.Background()
	if _, err := a.DB.ExecContext(ctx,
		`INSERT INTO auth_tokens(token, email, expires_at) VALUES('expiredtok', 'exp@example.com', $1)`,
		time.Now().Add(-time.Minute).Unix(),
	); err != nil {
		t.Fatalf("insert expired token: %v", err)
	}
	_, err := a.ConsumeToken(ctx, "expiredtok")
	if err == nil || err.Error() != "link expired" {
		t.Fatalf("want 'link expired', got %v", err)
	}
}

func TestConsumeTokenSameUserOnRepeat(t *testing.T) {
	a := newTestAuth(t)
	ctx := context.Background()
	tok1 := mustToken(t, a, "repeat@example.com")
	u1, err := a.ConsumeToken(ctx, tok1)
	if err != nil {
		t.Fatalf("first ConsumeToken: %v", err)
	}
	tok2 := mustToken(t, a, "repeat@example.com")
	u2, err := a.ConsumeToken(ctx, tok2)
	if err != nil {
		t.Fatalf("second ConsumeToken: %v", err)
	}
	if u1.ID != u2.ID {
		t.Errorf("second sign-in should return same user: %v vs %v", u1.ID, u2.ID)
	}
}

func TestCurrentUserValid(t *testing.T) {
	a := newTestAuth(t)
	ctx := context.Background()
	tok := mustToken(t, a, "cur@example.com")
	u, err := a.ConsumeToken(ctx, tok)
	if err != nil {
		t.Fatalf("ConsumeToken: %v", err)
	}

	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(&http.Cookie{Name: auth.SessionCookie, Value: a.Sign(u.ID)})
	got, err := a.CurrentUser(r)
	if err != nil {
		t.Fatalf("CurrentUser: %v", err)
	}
	if got == nil || got.ID != u.ID {
		t.Fatalf("want user %v, got %v", u.ID, got)
	}
}

func TestCurrentUserNoCookie(t *testing.T) {
	a := newTestAuth(t)
	r := httptest.NewRequest("GET", "/", nil)
	u, err := a.CurrentUser(r)
	if err != nil || u != nil {
		t.Fatalf("want (nil, nil), got (%v, %v)", u, err)
	}
}

func TestCurrentUserTamperedCookie(t *testing.T) {
	a := newTestAuth(t)
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(&http.Cookie{Name: auth.SessionCookie, Value: "tampered.badvalue"})
	// CurrentUser swallows bad-cookie errors and returns (nil, nil)
	u, err := a.CurrentUser(r)
	if err != nil || u != nil {
		t.Fatalf("want (nil, nil), got (%v, %v)", u, err)
	}
}
