package handlers

import "net/http"

// GetTerms and GetPrivacy serve the static legal pages. Content lives in the
// terms.html / privacy.html templates; both are world-readable.
func (s *Server) GetTerms(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "terms", nil)
}

func (s *Server) GetPrivacy(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "privacy", nil)
}
