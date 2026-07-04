package handlers

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// Locks the API contract insertActive() in cmeditor.js depends on: a bare
// "@handle" match must render a trailing slash (partial reference — user
// keeps typing to search that user's notes), not a closed link.
func TestSuggestUsersInsertKeepsTrailingSlash(t *testing.T) {
	s := newTestServer(t)
	mkUser(t, s, "bob")

	req := httptest.NewRequest("GET", "/api/wiki/suggest?q=@bo", nil)
	w := httptest.NewRecorder()
	s.GetWikiSuggest(w, req)

	if !strings.Contains(w.Body.String(), `data-insert="@bob/"`) {
		t.Fatalf("expected data-insert to end with /, got %s", w.Body.String())
	}
}
