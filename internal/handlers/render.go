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
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/a-h/templ"

	"commonplace/internal/auth"
	"commonplace/internal/config"
)

var (
	siteCfg  = config.DefaultSite()
	navCfg   = config.DefaultNav()
	emailCfg = config.DefaultEmail(siteCfg.Title)
)

//go:embed all:static
var assetsFS embed.FS

// StaticFS exposes the embedded static/ subtree, mounted at /assets/ by Routes.
func StaticFS() fs.FS {
	sub, err := fs.Sub(assetsFS, "static")
	if err != nil {
		panic(err)
	}
	return sub
}

// obsidianURL builds an obsidian://new deep link. Params are percent-encoded
// with %20 for spaces (not '+') because Obsidian's URI parser decodes per
// RFC 3986, where '+' is a literal plus.
func obsidianURL(name, content string) template.URL {
	enc := func(s string) string { return strings.ReplaceAll(url.QueryEscape(s), "+", "%20") }
	return template.URL("obsidian://new?name=" + enc(name) + "&content=" + enc(content))
}

// ---------- Server ----------

// tagChipCache memoizes the top-tag-chips aggregate (loadTopTagChips), measured
// at ~40ms over note_tags⋈notes at 100k notes — the one hot-path DB cost on a
// full /feed render. The cached list omits the per-request Active flag; callers
// set it after read. ponytail: in-process, 5-min TTL, lazy expiry; move to Render
// Key Value only if/when we run multiple web instances.
type tagChipCache struct {
	mu    sync.RWMutex
	data  []tagChip
	until time.Time
}

type Server struct {
	DB          *sql.DB
	Auth        *auth.Auth
	Debug       bool
	PlaytestKey string            // non-empty enables /_dev/login?key=... outside Debug mode
	AdminHandle string            // empty disables admin entirely
	OAuthCfg    *auth.OAuthConfig // nil means Google OAuth is disabled
	BaseURL     string            // e.g. "http://localhost:8080"; for absolute OG/canonical URLs
	tagChips    tagChipCache      // memoized top-tag chips; see tagChipCache
}

// chrome builds the site-chrome data (nav, current user, theme) every
// templ page needs. SearchQuery is left blank; the one caller that shows a
// live query (search.go) sets it after this returns.
func (s *Server) chrome(r *http.Request) ChromeProps {
	u, _ := s.Auth.CurrentUser(r)
	theme := ""
	if u != nil && (u.Theme == "light" || u.Theme == "dark") {
		theme = u.Theme
	}
	return ChromeProps{User: u, Site: siteCfg, Nav: navCfg, Theme: theme}
}

// renderPage writes a full HTML page (Layout + chrome + body) for a templ
// component. meta may be nil (Layout falls back to the default og: tags).
func (s *Server) renderPage(w http.ResponseWriter, r *http.Request, title, pageClass string, meta, body templ.Component) {
	c := s.chrome(r)
	var buf bytes.Buffer
	if err := Layout(c, title, pageClass, meta, body).Render(r.Context(), &buf); err != nil {
		log.Printf("render page %q: %v", title, err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	// Same URL serves full page or HTMX fragment depending on HX-Request; keep them in separate cache slots.
	w.Header().Add("Vary", "HX-Request") // Add, not Set: gzip middleware also varies on Accept-Encoding
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	buf.WriteTo(w)
}

// renderPageWithQuery is renderPage for the one page (search) that shows the
// live search query in the topbar's search box.
func (s *Server) renderPageWithQuery(w http.ResponseWriter, r *http.Request, title, pageClass, query string, meta, body templ.Component) {
	c := s.chrome(r)
	c.SearchQuery = query
	var buf bytes.Buffer
	if err := Layout(c, title, pageClass, meta, body).Render(r.Context(), &buf); err != nil {
		log.Printf("render page %q: %v", title, err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Add("Vary", "HX-Request")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	buf.WriteTo(w)
}

// renderFragment writes an HTMX fragment: no Layout wrapper, just the component.
func (s *Server) renderFragment(w http.ResponseWriter, r *http.Request, body templ.Component) {
	var buf bytes.Buffer
	if err := body.Render(r.Context(), &buf); err != nil {
		log.Printf("render fragment: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Add("Vary", "HX-Request")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	buf.WriteTo(w)
}

// pageTitle builds a "prefix · Site.Title" title, or just Site.Title when
// prefix is empty.
func pageTitle(prefix string) string {
	if prefix == "" {
		return siteCfg.Title
	}
	return prefix + " · " + siteCfg.Title
}

// renderError writes a templated error page with the given status code.
func (s *Server) renderError(w http.ResponseWriter, r *http.Request, code int, msg string) {
	w.WriteHeader(code)
	statusCode := fmt.Sprintf("%d %s", code, http.StatusText(code))
	s.renderPage(w, r, pageTitle(statusCode), "", nil, errorContent(statusCode, msg))
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
