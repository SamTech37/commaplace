package handlers

import (
	"compress/gzip"
	"net/http"
	"strings"

	"commonplace/internal/auth"
)

// Routes wires every URL pattern the server handles. Order matters where
// patterns overlap; the Go 1.22 mux resolves "more specific" wins so this
// is mostly informational.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	// Static assets are served from the catch-all handler to avoid mux
	// conflicts with user routes. cacheControl adds a 1-day max-age (stdlib
	// ETag still revalidates after expiry).
	static := http.StripPrefix("/assets/", cacheControl(http.FileServerFS(StaticFS())))

	// Skip-login cookie issuer. 404s unless Debug is true or a matching
	// PlaytestKey is supplied via ?key=, so registering unconditionally is safe.
	mux.HandleFunc("GET /_dev/login", s.GetDevLogin)

	// Home
	mux.HandleFunc("GET /{$}", s.GetHome)

	// Graph view
	mux.HandleFunc("GET /graph", s.GetGraph)
	mux.HandleFunc("GET /api/graph", s.GetGraphData)
	mux.HandleFunc("GET /api/graph/local", s.GetGraphLocal)
	mux.HandleFunc("GET /tag/{tag}/graph", s.GetTagGraph)

	// Auth
	mux.HandleFunc("GET /login", s.GetLogin)
	mux.HandleFunc("POST /login", s.PostLogin)
	mux.HandleFunc("GET /auth/{token}", s.GetAuthCallback)
	mux.HandleFunc("GET /auth/google", s.GetOAuthGoogleStart)
	mux.HandleFunc("GET /auth/google/callback", s.GetOAuthGoogleCallback)
	mux.HandleFunc("POST /logout", s.PostLogout)
	mux.HandleFunc("GET /me", s.GetMe)

	// Write + preview
	mux.HandleFunc("GET /write", s.GetWrite)
	mux.HandleFunc("POST /write", s.PostWrite)
	mux.HandleFunc("POST /preview", s.PostPreview)

	// Editor autosave + publish (draft model)
	mux.HandleFunc("PATCH /api/notes/{id}", s.PatchNote)
	mux.HandleFunc("POST /api/notes/{id}/publish", s.PublishNote)

	// Edit + delete
	mux.HandleFunc("GET /edit/{id}", s.GetEdit)
	mux.HandleFunc("POST /delete/{id}", s.PostDeleteNote)

	// Import
	mux.HandleFunc("GET /import", s.GetImport)
	mux.HandleFunc("POST /import", s.PostImport)

	// Feed
	mux.HandleFunc("GET /feed", s.GetFeed)

	// Tag pages
	mux.HandleFunc("GET /tag/{tag}", s.GetTagPage)

	// Likes
	mux.HandleFunc("POST /api/like", s.PostLike)
	mux.HandleFunc("GET /me/saved", s.GetSaved)

	// Follows
	mux.HandleFunc("POST /api/follow", s.PostFollow)

	// Wiki autocomplete
	mux.HandleFunc("GET /api/wiki/suggest", s.GetWikiSuggest)

	// Search
	mux.HandleFunc("GET /search", s.GetSearch)

	// Settings
	mux.HandleFunc("POST /settings/theme", s.PostThemeSetting)

	// Avatar builder
	mux.HandleFunc("GET /me/avatar", s.GetAvatarBuilder)
	mux.HandleFunc("POST /me/avatar", s.PostAvatarBuilder)
	mux.HandleFunc("GET /u/{handle}/avatar.png", s.GetAvatarPNG)

	// Onboarding (after first sign-in, no notes yet)
	mux.HandleFunc("GET /onboarding", s.GetOnboarding)
	mux.HandleFunc("POST /onboarding/fork", s.PostOnboardingFork)

	// Moderation
	mux.HandleFunc("POST /api/report", s.PostReport)
	mux.HandleFunc("GET /admin", s.GetAdminDashboard)
	mux.HandleFunc("GET /admin/{$}", s.GetAdminDashboard)
	mux.HandleFunc("GET /admin/reports", s.GetAdminReports)
	mux.HandleFunc("POST /admin/hide", s.PostAdminHide)

	// Markdown export
	mux.HandleFunc("GET /api/notes/{id}/raw", s.GetNoteRaw)

	// Note image
	mux.HandleFunc("GET /api/notes/{id}/image", s.GetNoteImage)
	mux.HandleFunc("POST /api/notes/{id}/image", s.PostNoteImage)

	// Per-user graph.
	mux.HandleFunc("GET /u/{user}/graph", s.GetUserGraph)

	// Profile + note view + assets (last; catch-all for user routes).
	mux.HandleFunc("GET /{path...}", s.GetCatchAll(static))

	return gzipMiddleware(mux)
}

// cacheControl tags responses with a conservative 1-day cache. Not "immutable"/
// far-future: assets are go:embed'd and change on redeploy, so without
// content-hashed URLs an immutable cache would serve stale JS/CSS. The stdlib
// FileServer's ETag handles revalidation after the day expires.
func cacheControl(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=86400")
		next.ServeHTTP(w, r)
	})
}

// gzipMiddleware compresses text responses (HTML/CSS/JS/SVG/JSON) when the
// client accepts gzip. It deliberately skips already-compressed types — woff2
// fonts, PNGs — which gzip cannot shrink (and would only waste CPU). The
// compressible decision is made at WriteHeader time from the Content-Type.
func gzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Always advertise that the response varies on Accept-Encoding, even when
		// this client didn't send it — otherwise a shared cache can store an
		// uncompressed (no-Vary) copy and serve it ambiguously.
		w.Header().Add("Vary", "Accept-Encoding")
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		gw := &gzipResponseWriter{ResponseWriter: w}
		defer gw.Close()
		next.ServeHTTP(gw, r)
	})
}

func gzipCompressible(ct string) bool {
	ct = strings.ToLower(ct)
	for _, p := range []string{"text/", "application/json", "application/javascript", "application/xml", "image/svg+xml"} {
		if strings.HasPrefix(ct, p) {
			return true
		}
	}
	return false
}

type gzipResponseWriter struct {
	http.ResponseWriter
	gz       *gzip.Writer
	decided  bool
	wroteHdr bool
}

func (g *gzipResponseWriter) decide() {
	if g.decided {
		return
	}
	g.decided = true
	if gzipCompressible(g.Header().Get("Content-Type")) {
		g.Header().Del("Content-Length") // length changes once compressed
		g.Header().Set("Content-Encoding", "gzip")
		g.gz = gzip.NewWriter(g.ResponseWriter)
	}
}

func (g *gzipResponseWriter) WriteHeader(code int) {
	g.decide()
	g.wroteHdr = true
	g.ResponseWriter.WriteHeader(code)
}

func (g *gzipResponseWriter) Write(b []byte) (int, error) {
	if !g.wroteHdr {
		if g.Header().Get("Content-Type") == "" {
			g.Header().Set("Content-Type", http.DetectContentType(b))
		}
		g.WriteHeader(http.StatusOK)
	}
	if g.gz != nil {
		return g.gz.Write(b)
	}
	return g.ResponseWriter.Write(b)
}

func (g *gzipResponseWriter) Close() error {
	if g.gz != nil {
		return g.gz.Close()
	}
	return nil
}

// GetCatchAll routes assets and user pages without conflicting mux patterns.
func (s *Server) GetCatchAll(static http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/assets" {
			http.Redirect(w, r, "/assets/", http.StatusMovedPermanently)
			return
		}
		if strings.HasPrefix(path, "/assets/") {
			static.ServeHTTP(w, r)
			return
		}

		trimmed := strings.Trim(path, "/")
		if trimmed == "" {
			s.renderError(w, r, http.StatusNotFound, "not found")
			return
		}
		parts := strings.Split(trimmed, "/")
		switch len(parts) {
		case 1:
			user := parts[0]
			if auth.IsReservedHandle(strings.ToLower(user)) {
				s.renderError(w, r, http.StatusNotFound, "not found")
				return
			}
			r.SetPathValue("user", user)
			s.GetProfile(w, r)
		case 2:
			user := parts[0]
			if auth.IsReservedHandle(strings.ToLower(user)) {
				s.renderError(w, r, http.StatusNotFound, "not found")
				return
			}
			r.SetPathValue("user", user)
			r.SetPathValue("slug", parts[1])
			s.GetNote(w, r)
		default:
			s.renderError(w, r, http.StatusNotFound, "not found")
		}
	}
}

// GetHome sends visitors straight to the feed — the landing page IS the feed,
// so logged-out visitors land on content instead of a welcome/login prompt.
func (s *Server) GetHome(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/feed", http.StatusSeeOther)
}
