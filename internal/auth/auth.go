// Package auth handles signed session cookies, magic-link issuance,
// and current-user lookup. HTTP handlers live in internal/handlers/auth.go;
// this package is HTTP-agnostic apart from the cookie helpers.
package auth

import (
	"context"
	crand "crypto/rand"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	SessionCookie = "commonplace_session"
	sessionMaxAge = 60 * 60 * 24 * 30 // 30 days
	tokenTTL      = 15 * time.Minute
)

type User struct {
	ID        int64
	Handle    string
	Email     string
	CreatedAt int64
}

// Mailer sends a plain-text email. nil Mailer means dev mode (log to stdout).
type Mailer interface {
	Send(to, subject, body string) error
}

type Auth struct {
	DB      *sql.DB
	Secret  []byte
	BaseURL string // e.g. "http://localhost:8080" — used to build magic links
	Mailer  Mailer
}

// ---------- session signing ----------

// Sign returns "<uid>.<hmac_hex>".
func (a *Auth) Sign(userID int64) string {
	payload := strconv.FormatInt(userID, 10)
	mac := hmac.New(sha256.New, a.Secret)
	mac.Write([]byte(payload))
	return payload + "." + hex.EncodeToString(mac.Sum(nil))
}

func (a *Auth) Verify(cookie string) (int64, error) {
	i := strings.LastIndexByte(cookie, '.')
	if i <= 0 || i == len(cookie)-1 {
		return 0, errors.New("malformed cookie")
	}
	payload, sig := cookie[:i], cookie[i+1:]
	want, err := hex.DecodeString(sig)
	if err != nil {
		return 0, err
	}
	mac := hmac.New(sha256.New, a.Secret)
	mac.Write([]byte(payload))
	if !hmac.Equal(mac.Sum(nil), want) {
		return 0, errors.New("bad signature")
	}
	return strconv.ParseInt(payload, 10, 64)
}

func (a *Auth) SetSession(w http.ResponseWriter, userID int64) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    a.Sign(userID),
		Path:     "/",
		MaxAge:   sessionMaxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func (a *Auth) ClearSession(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})
}

// CurrentUser reads the session cookie and looks up the user. Returns
// (nil, nil) for the unauthenticated case (no/bad cookie or user gone).
func (a *Auth) CurrentUser(r *http.Request) (*User, error) {
	c, err := r.Cookie(SessionCookie)
	if err != nil {
		return nil, nil
	}
	uid, err := a.Verify(c.Value)
	if err != nil {
		return nil, nil
	}
	var u User
	err = a.DB.QueryRowContext(r.Context(),
		`SELECT id, handle, email, created_at FROM users WHERE id = ?`, uid,
	).Scan(&u.ID, &u.Handle, &u.Email, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// ---------- magic link ----------

// IssueToken creates an auth_tokens row and emails the magic link. In dev
// (a.Mailer == nil) the link is logged to stdout instead.
func (a *Auth) IssueToken(ctx context.Context, email string) error {
	email = strings.TrimSpace(strings.ToLower(email))
	if !looksLikeEmail(email) {
		return errors.New("invalid email")
	}
	token, err := randomHex(32)
	if err != nil {
		return err
	}
	expires := time.Now().Add(tokenTTL).Unix()
	if _, err := a.DB.ExecContext(ctx,
		`INSERT INTO auth_tokens(token, email, expires_at) VALUES(?, ?, ?)`,
		token, email, expires,
	); err != nil {
		return err
	}
	link := strings.TrimRight(a.BaseURL, "/") + "/auth/" + token
	body := fmt.Sprintf("Sign in to commonplace:\n\n%s\n\nThis link expires in %d minutes.\n",
		link, int(tokenTTL.Minutes()))
	if a.Mailer == nil {
		log.Printf("[magic-link] %s -> %s", email, link)
		return nil
	}
	return a.Mailer.Send(email, "Your commonplace sign-in link", body)
}

// ConsumeToken validates the token and returns the corresponding user
// (creating one on first sign-in).
func (a *Auth) ConsumeToken(ctx context.Context, token string) (*User, error) {
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var (
		email     string
		expiresAt int64
		usedAt    sql.NullInt64
	)
	err = tx.QueryRowContext(ctx,
		`SELECT email, expires_at, used_at FROM auth_tokens WHERE token = ?`, token,
	).Scan(&email, &expiresAt, &usedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("invalid link")
	}
	if err != nil {
		return nil, err
	}
	if usedAt.Valid {
		return nil, errors.New("link already used")
	}
	if time.Now().Unix() > expiresAt {
		return nil, errors.New("link expired")
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE auth_tokens SET used_at = ? WHERE token = ?`,
		time.Now().Unix(), token,
	); err != nil {
		return nil, err
	}
	u, err := findOrCreateUser(ctx, tx, email)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return u, nil
}

// ---------- helpers ----------

var nonHandleRE = regexp.MustCompile(`[^a-z0-9-]+`)
var emailRE = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

func looksLikeEmail(s string) bool { return emailRE.MatchString(s) }

func handleFromEmail(email string) string {
	local := email
	if i := strings.IndexByte(email, '@'); i > 0 {
		local = email[:i]
	}
	local = strings.ToLower(local)
	local = nonHandleRE.ReplaceAllString(local, "-")
	local = strings.Trim(local, "-")
	if local == "" || IsReservedHandle(local) {
		local = "user"
	}
	return local
}

// ReservedHandles are top-level path segments that the application owns
// and therefore cannot be claimed as user handles. Mirrors the routes
// registered in cmd/server.
// reservedHandles are names a user cannot claim, because they collide with
// top-level feature paths to a degree that's user-visible. (Routing never
// gets confused — Go 1.22 mux gives literal segments precedence — but a user
// named "feed" would never see their public profile via /feed.) Note that
// "admin" is intentionally NOT reserved so the configured ADMIN_HANDLE can
// sign in normally; their public profile becomes /<handle> as usual.
var reservedHandles = map[string]bool{
	"login": true, "logout": true, "auth": true,
	"write": true, "me": true, "feed": true,
	"api": true, "static": true, "_test": true,
	"settings": true, "about": true,
	"favicon.ico": true, "robots.txt": true,
	"tag": true, "search": true, "onboarding": true,
}

func IsReservedHandle(h string) bool { return reservedHandles[h] }

func findOrCreateUser(ctx context.Context, tx *sql.Tx, email string) (*User, error) {
	var u User
	err := tx.QueryRowContext(ctx,
		`SELECT id, handle, email, created_at FROM users WHERE email = ?`, email,
	).Scan(&u.ID, &u.Handle, &u.Email, &u.CreatedAt)
	if err == nil {
		return &u, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	handle, err := uniqueHandle(ctx, tx, handleFromEmail(email))
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	res, err := tx.ExecContext(ctx,
		`INSERT INTO users(handle, email, created_at) VALUES(?, ?, ?)`,
		handle, email, now,
	)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return &User{ID: id, Handle: handle, Email: email, CreatedAt: now}, nil
}

func uniqueHandle(ctx context.Context, tx *sql.Tx, base string) (string, error) {
	candidate := base
	for i := 2; i < 10000; i++ {
		var one int
		err := tx.QueryRowContext(ctx,
			`SELECT 1 FROM users WHERE handle = ?`, candidate,
		).Scan(&one)
		if errors.Is(err, sql.ErrNoRows) {
			return candidate, nil
		}
		if err != nil {
			return "", err
		}
		candidate = fmt.Sprintf("%s-%d", base, i)
	}
	return "", errors.New("could not find a unique handle")
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := crand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
