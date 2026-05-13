package handlers

import (
	"net/http"
)

// Routes wires every URL pattern the server handles. Order matters where
// patterns overlap; the Go 1.22 mux resolves "more specific" wins so this
// is mostly informational.
func (s *Server) Routes() *http.ServeMux {
	mux := http.NewServeMux()

	// Static assets
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(StaticFS())))

	// Home
	mux.HandleFunc("GET /{$}", s.GetHome)

	// Auth
	mux.HandleFunc("GET /login", s.GetLogin)
	mux.HandleFunc("POST /login", s.PostLogin)
	mux.HandleFunc("GET /auth/{token}", s.GetAuthCallback)
	mux.HandleFunc("POST /logout", s.PostLogout)
	mux.HandleFunc("GET /me", s.GetMe)

	// Write + preview
	mux.HandleFunc("GET /write", s.GetWrite)
	mux.HandleFunc("POST /write", s.PostWrite)
	mux.HandleFunc("POST /preview", s.PostPreview)

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

	// Onboarding (after first sign-in, no notes yet)
	mux.HandleFunc("GET /onboarding", s.GetOnboarding)
	mux.HandleFunc("POST /onboarding/fork", s.PostOnboardingFork)

	// Moderation
	mux.HandleFunc("POST /api/report", s.PostReport)
	mux.HandleFunc("GET /admin/reports", s.GetAdminReports)
	mux.HandleFunc("POST /admin/hide", s.PostAdminHide)

	// Markdown export
	mux.HandleFunc("GET /api/notes/{id}/raw", s.GetNoteRaw)

	// Profile + note view (last; the /{user} pattern would otherwise shadow
	// non-reserved top-level paths if registered first).
	mux.HandleFunc("GET /{user}", s.GetProfile)
	mux.HandleFunc("GET /{user}/{path...}", s.GetNote)

	return mux
}

// GetHome renders the landing page.
func (s *Server) GetHome(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "home", nil)
}
