package handlers

import (
	"context"
	"net/http/httptest"
	"net/url"
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

// Queries under 3 characters take a different SQL expression in
// likeAnyVariant than longer ones (trigram index); both must match the same
// note, in either script.
func TestSuggestNotesMatchesShortAndLongCJK(t *testing.T) {
	s := newTestServer(t)
	aliceID := mkUser(t, s, "alice")
	if _, err := s.saveNote(context.Background(), aliceID, "alice", "shulun", "數論筆記", "body", nil); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"數", "数论", "數論筆", "数论笔记"} {
		w := httptest.NewRecorder()
		s.GetWikiSuggest(w, httptest.NewRequest("GET", "/api/wiki/suggest?q="+url.QueryEscape(q), nil))
		if !strings.Contains(w.Body.String(), `data-insert="@alice/shulun"`) {
			t.Errorf("q=%q: got %s", q, w.Body.String())
		}
	}
}
