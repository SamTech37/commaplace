package handlers

import (
	"context"
	"html"
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

// Queries without 3 consecutive word characters take a different SQL
// expression in likeAnyVariant than longer ones (trigram index); both must
// match the same note, in either script.
func TestSuggestNotesMatchesShortAndLongCJK(t *testing.T) {
	trgmUnicode = true // as ProbeTrgm sets it on a UTF-8 LC_CTYPE
	t.Cleanup(func() { trgmUnicode = false })
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

// hostile holds every character special in HTML or in a Go %q literal, plus
// CJK, which the editor must receive verbatim. App paths normalize handles,
// slugs and tags to letters, digits and "-", so tests write these rows
// directly, as a legacy row or a looser future normalizer could.
const hostile = `evil"<>&'\中`

// between returns each substring of body between open and the next close,
// HTML-unescaped the way a browser decodes it. For open = `attr="` and
// close = `"`, that is exactly the attribute value a browser would read.
func between(body, open, close string) []string {
	var out []string
	for {
		i := strings.Index(body, open)
		if i < 0 {
			return out
		}
		body = body[i+len(open):]
		j := strings.Index(body, close)
		if j < 0 {
			return append(out, "<unterminated>")
		}
		out = append(out, html.UnescapeString(body[:j]))
		body = body[j+len(close):]
	}
}

func TestWikiSuggestEscapesDataInsert(t *testing.T) {
	s := newTestServer(t)
	id := mkUser(t, s, hostile)
	title := "Title " + hostile
	if _, err := s.saveNote(context.Background(), id, hostile, hostile, title, "body", nil); err != nil {
		t.Fatalf("saveNote: %v", err)
	}
	for _, tc := range []struct{ q, insert, primary string }{
		{"@evil", "@" + hostile + "/", "@" + hostile},               // suggestUsers
		{"@" + hostile + "/", "@" + hostile + "/" + hostile, title}, // suggestNotesForUser
		{"evil", "@" + hostile + "/" + hostile, title},              // suggestNotes
	} {
		w := httptest.NewRecorder()
		s.GetWikiSuggest(w, httptest.NewRequest("GET", "/api/wiki/suggest?q="+url.QueryEscape(tc.q), nil))
		body := w.Body.String()
		if got := between(body, `data-insert="`, `"`); len(got) != 1 || got[0] != tc.insert {
			t.Errorf("q=%q: data-insert = %q, want [%q]\n%s", tc.q, got, tc.insert, body)
		}
		if got := between(body, `<span class="ac-primary">`, `</span>`); len(got) != 1 || got[0] != tc.primary {
			t.Errorf("q=%q: ac-primary = %q, want [%q]\n%s", tc.q, got, tc.primary, body)
		}
	}
}
