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

	// Graph view
	mux.HandleFunc("GET /graph", s.GetGraph)
	mux.HandleFunc("GET /api/graph", s.GetGraphData)
	mux.HandleFunc("GET /api/graph/local", s.GetGraphLocal)

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

	// External (indexed Obsidian Publish vaults)
	mux.HandleFunc("GET /admin/external", s.GetAdminExternal)
	mux.HandleFunc("POST /admin/external/add", s.PostAdminExternalAdd)
	mux.HandleFunc("POST /admin/external/recrawl", s.PostAdminExternalRecrawl)
	mux.HandleFunc("POST /admin/external/delete", s.PostAdminExternalDelete)
	mux.HandleFunc("GET /x/{vault}", s.GetExternalIndex)
	mux.HandleFunc("GET /x/{vault}/{path...}", s.GetExternalNote)
	mux.HandleFunc("GET /api/x/notes/{id}/raw", s.GetExternalNoteRaw)

	// Markdown export
	mux.HandleFunc("GET /api/notes/{id}/raw", s.GetNoteRaw)

	// Profile + note view (last; the /{user} pattern would otherwise shadow
	// non-reserved top-level paths if registered first).
	mux.HandleFunc("GET /{user}", s.GetProfile)
	mux.HandleFunc("GET /{user}/{path...}", s.GetNote)

	return mux
}

// GetHome sends visitors straight to the feed — the landing page IS the feed,
// so logged-out visitors land on content instead of a welcome/login prompt.
func (s *Server) GetHome(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/feed", http.StatusSeeOther)
}
