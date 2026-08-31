package handlers

import "net/http"

// GetTerms and GetPrivacy serve the static legal pages. Content lives in the
// terms.html / privacy.html templates; both are world-readable.
func (s *Server) GetTerms(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, r, pageTitle("服務條款"), "", nil, termsPage())
}

func (s *Server) GetPrivacy(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, r, pageTitle("隱私政策"), "", nil, privacyPage())
}
