package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const oauthStateCookie = "oauth_state"

// Vars, not literals, so tests can point the flow at a fake Google.
var (
	googleEndpoint    = google.Endpoint
	googleUserinfoURL = "https://openidconnect.googleapis.com/v1/userinfo"
)

// OAuthConfig holds Google OAuth credentials. Nil means OAuth is disabled.
type OAuthConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
}

func (a *Auth) googleOAuthConfig(cfg OAuthConfig) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURL,
		Scopes:       []string{"openid", "email"},
		Endpoint:     googleEndpoint,
	}
}

// StartGoogleOAuth redirects the user to Google's consent page.
func (a *Auth) StartGoogleOAuth(w http.ResponseWriter, r *http.Request, cfg OAuthConfig) {
	state, _ := randomHex(16)
	http.SetCookie(w, &http.Cookie{
		Name:     oauthStateCookie,
		Value:    state,
		Path:     "/",
		MaxAge:   300,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	url := a.googleOAuthConfig(cfg).AuthCodeURL(state, oauth2.AccessTypeOnline)
	http.Redirect(w, r, url, http.StatusFound)
}

// HandleGoogleCallback exchanges the code, fetches the user's email,
// and returns the matched-or-created User. The caller sets the session cookie.
func (a *Auth) HandleGoogleCallback(w http.ResponseWriter, r *http.Request, cfg OAuthConfig) (*User, error) {
	stateCookie, err := r.Cookie(oauthStateCookie)
	if err != nil || stateCookie.Value != r.URL.Query().Get("state") {
		return nil, fmt.Errorf("invalid OAuth state")
	}
	// Clear the state cookie
	http.SetCookie(w, &http.Cookie{Name: oauthStateCookie, Path: "/", MaxAge: -1})

	token, err := a.googleOAuthConfig(cfg).Exchange(r.Context(), r.URL.Query().Get("code"))
	if err != nil {
		return nil, fmt.Errorf("token exchange: %w", err)
	}

	email, err := fetchGoogleEmail(r.Context(), token)
	if err != nil {
		return nil, fmt.Errorf("fetch email: %w", err)
	}

	tx, err := a.DB.BeginTx(r.Context(), nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	u, err := findOrCreateUser(r.Context(), tx, email)
	if err != nil {
		return nil, err
	}
	return u, tx.Commit()
}

func fetchGoogleEmail(ctx context.Context, token *oauth2.Token) (string, error) {
	client := oauth2.NewClient(ctx, oauth2.StaticTokenSource(token))
	resp, err := client.Get(googleUserinfoURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var info struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
	}
	if err := json.Unmarshal(body, &info); err != nil || info.Email == "" {
		return "", fmt.Errorf("no email in Google userinfo response")
	}
	// Same email = same account, so an unverified address must not sign in.
	if !info.EmailVerified {
		return "", fmt.Errorf("Google email not verified")
	}
	return info.Email, nil
}

