// Package handlers contains all HTTP handlers, the templated response
// pipeline, and the embedded HTML/CSS/JS assets.
package handlers

import (
	"bytes"
	"database/sql"
	"embed"
	"fmt"
	"hash/fnv"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"time"
	"unicode"

	"commonplace/internal/auth"
	"commonplace/internal/config"
)

var (
	siteCfg  = config.DefaultSite()
	emailCfg = config.DefaultEmail(siteCfg.Title)
)

//go:embed all:templates all:static
var assetsFS embed.FS

// StaticFS exposes the embedded static/ subtree, mounted at /assets/ by Routes.
func StaticFS() fs.FS {
	sub, err := fs.Sub(assetsFS, "static")
	if err != nil {
		panic(err)
	}
	return sub
}

// Funcs available inside templates.
var funcs = template.FuncMap{
	"avatarColor":   avatarColor,
	"avatarInitial": avatarInitial,
	"avatarURL":     func(handle string) string { return "/u/" + handle + "/avatar.png" },
	"sub":           func(a, b int) int { return a - b },
	"add":           func(a, b int) int { return a + b },
}

// Pages is a name -> parsed template cache. Each page template is parsed
// alongside _base.html so that {{block "content"}} / {{block "title"}}
// overrides apply.
type Pages struct {
	cache    map[string]*template.Template
	partials map[string]*template.Template
}

func LoadPages() (*Pages, error) {
	pageNames := []string{
		"login", "write", "note", "note_stub", "profile", "feed", "error",
		"tag", "saved", "search", "onboarding", "admin_dashboard", "admin_reports", "graph",
		"avatar_builder",
	}
	// Partials are standalone fragments (no _base.html wrapper) used for
	// HTMX swap responses — e.g. infinite-scroll feed batches.
	partialNames := []string{"feed_partial"}

	p := &Pages{
		cache:    make(map[string]*template.Template, len(pageNames)),
		partials: make(map[string]*template.Template, len(partialNames)),
	}
	for _, name := range pageNames {
		t, err := template.New("").Funcs(funcs).ParseFS(assetsFS,
			"templates/_base.html",
			"templates/"+name+".html",
		)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", name, err)
		}
		p.cache[name] = t
	}
	// Partials get the feed page parsed alongside so they can reuse the
	// {{template "card" .}} definition.
	for _, name := range partialNames {
		t, err := template.New("").Funcs(funcs).ParseFS(assetsFS,
			"templates/feed.html",
			"templates/"+name+".html",
		)
		if err != nil {
			return nil, fmt.Errorf("parse partial %s: %w", name, err)
		}
		p.partials[name] = t
	}
	return p, nil
}

func (p *Pages) Render(w http.ResponseWriter, name string, data map[string]any) {
	t, ok := p.cache[name]
	if !ok {
		http.Error(w, "template not found: "+name, http.StatusInternalServerError)
		return
	}
	if data == nil {
		data = map[string]any{}
	}
	if _, has := data["User"]; !has {
		data["User"] = nil
	}
	if _, has := data["SearchQuery"]; !has {
		data["SearchQuery"] = ""
	}
	if _, has := data["Theme"]; !has {
		data["Theme"] = ""
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "base", data); err != nil {
		log.Printf("render %s: %v", name, err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	// Same URL serves full page or HTMX fragment depending on HX-Request; keep them in separate cache slots.
	w.Header().Set("Vary", "HX-Request")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	buf.WriteTo(w)
}

// RenderPartial executes a fragment template (no _base.html wrapper).
// Used for HTMX swap responses.
func (p *Pages) RenderPartial(w http.ResponseWriter, name, defined string, data map[string]any) {
	t, ok := p.partials[name]
	if !ok {
		http.Error(w, "partial not found: "+name, http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, defined, data); err != nil {
		log.Printf("render partial %s: %v", name, err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	// Same URL serves full page or HTMX fragment depending on HX-Request; keep them in separate cache slots.
	w.Header().Set("Vary", "HX-Request")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	buf.WriteTo(w)
}

// ---------- Server ----------

type Server struct {
	DB          *sql.DB
	Auth        *auth.Auth
	Pages       *Pages
	Debug       bool
	PlaytestKey string            // non-empty enables /_dev/login?key=... outside Debug mode
	AdminHandle string            // empty disables admin entirely
	OAuthCfg    *auth.OAuthConfig // nil means Google OAuth is disabled
}

// render is a small wrapper that injects the current user and site config automatically.
func (s *Server) render(w http.ResponseWriter, r *http.Request, name string, data map[string]any) {
	if data == nil {
		data = map[string]any{}
	}
	u, _ := s.Auth.CurrentUser(r)
	data["User"] = u
	data["Site"] = siteCfg
	// Theme: when a logged-in user explicitly chose light/dark, render it
	// server-side to avoid FOUC. "auto" (or visitor) defers to inline JS
	// reading localStorage / prefers-color-scheme.
	if u != nil && (u.Theme == "light" || u.Theme == "dark") {
		data["Theme"] = u.Theme
	} else {
		data["Theme"] = ""
	}
	s.Pages.Render(w, name, data)
}

// renderError writes a templated error page with the given status code.
func (s *Server) renderError(w http.ResponseWriter, r *http.Request, code int, msg string) {
	w.WriteHeader(code)
	s.render(w, r, "error", map[string]any{
		"Code":    fmt.Sprintf("%d %s", code, http.StatusText(code)),
		"Message": msg,
	})
}

// requireUser redirects to /login if no user is logged in.
// Returns nil in that case so the handler should `if u == nil { return }`.
func (s *Server) requireUser(w http.ResponseWriter, r *http.Request) *auth.User {
	u, err := s.Auth.CurrentUser(r)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return nil
	}
	if u == nil {
		next := r.URL.Path
		if r.URL.RawQuery != "" {
			next += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, "/login?next="+template.URLQueryEscaper(next), http.StatusSeeOther)
		return nil
	}
	return u
}

// ---------- helpers ----------

func relativeTime(unix int64) string {
	if unix == 0 {
		return ""
	}
	d := time.Since(time.Unix(unix, 0))
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dmo ago", int(d.Hours()/(24*30)))
	default:
		return fmt.Sprintf("%dy ago", int(d.Hours()/(24*365)))
	}
}

// kebabSlug normalises a free-text title into a URL slug.
// Non-letter/digit runes become "-"; runs collapse; ends are trimmed.
func kebabSlug(title string) string {
	var b strings.Builder
	prevDash := true
	for _, r := range strings.ToLower(title) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			prevDash = false
		} else {
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// avatarPalette is a small fixed palette so dot colors stay consistent
// across renders for the same handle.
var avatarPalette = []string{
	"#fb7185", "#fbbf24", "#a78bfa", "#34d399",
	"#60a5fa", "#f472b6", "#94a3b8", "#fb923c",
}

func avatarColor(handle string) string {
	if handle == "" {
		return avatarPalette[0]
	}
	h := fnv.New32a()
	h.Write([]byte(handle))
	return avatarPalette[int(h.Sum32())%len(avatarPalette)]
}

func avatarInitial(handle string) string {
	if handle == "" {
		return "?"
	}
	for _, r := range handle {
		return strings.ToUpper(string(r))
	}
	return "?"
}

