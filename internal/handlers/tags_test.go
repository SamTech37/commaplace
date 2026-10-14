package handlers

import (
	"context"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// GetTagPage now shares feed's noteCardColumns + scanCards path instead of
// its own narrower query — this guards that switch actually returns the
// right note (title, URL, list layout) rather than silently querying an
// empty result set or panicking on a column-count mismatch.
func TestGetTagPageListsTaggedNote(t *testing.T) {
	s := newTestServer(t)
	aliceID := mkUser(t, s, "alice")
	if _, err := s.saveNote(context.Background(), aliceID, "alice", "n1", "Project X Kickoff", "Working on it.", []string{"project-x"}); err != nil {
		t.Fatalf("saveNote: %v", err)
	}

	req := httptest.NewRequest("GET", "/tag/project-x", nil)
	req.SetPathValue("tag", "project-x")
	w := httptest.NewRecorder()
	s.GetTagPage(w, req)

	if w.Code != 200 {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body)
	}
	body := w.Body.String()
	for _, want := range []string{"Project X Kickoff", `href="/alice/n1"`} {
		if !strings.Contains(body, want) {
			t.Errorf("tag page missing %q:\n%s", want, body)
		}
	}
}

// A deleted or hidden note must not keep a tag alive: chips and suggestions
// advertised a count, and clicking through showed nothing.
func TestTagCountsSkipUnreadableNotes(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	aliceID := mkUser(t, s, "alice")
	for _, slug := range []string{"kept", "deleted", "hidden"} {
		if _, err := s.saveNote(ctx, aliceID, "alice", slug, slug, "body", []string{"ghost"}); err != nil {
			t.Fatalf("saveNote %s: %v", slug, err)
		}
	}
	for _, q := range []string{
		`UPDATE notes SET deleted_at = 1 WHERE slug = 'deleted'`,
		`UPDATE notes SET hidden_at = 1 WHERE slug = 'hidden'`,
	} {
		if _, err := s.DB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}

	chips, err := loadTopTagChips(ctx, s.DB, 8)
	if err != nil {
		t.Fatal(err)
	}
	if len(chips) != 1 || chips[0].Count != 1 {
		t.Errorf("chips = %+v, want one #ghost with count 1", chips)
	}

	w := httptest.NewRecorder()
	s.GetTagSuggest(w, httptest.NewRequest("GET", "/api/tags/suggest?q=gho", nil))
	if !strings.Contains(w.Body.String(), `<span class="ac-secondary">1</span>`) {
		t.Errorf("suggest count, want 1: %s", w.Body.String())
	}
}

func TestGetTagSuggestPrefixMatch(t *testing.T) {
	s := newTestServer(t)
	aliceID := mkUser(t, s, "alice")
	if _, err := s.saveNote(context.Background(), aliceID, "alice", "n1", "N1", "Working on it.", []string{"project-x"}); err != nil {
		t.Fatalf("saveNote: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/tags/suggest?q=proj", nil)
	w := httptest.NewRecorder()
	s.GetTagSuggest(w, req)

	if !strings.Contains(w.Body.String(), `data-insert="project-x"`) {
		t.Fatalf("expected project-x suggestion, got %s", w.Body.String())
	}
}

// data-exact marks the item q already names once normalized (or in the other
// script), so the editor doesn't offer to create a tag that exists.
func TestGetTagSuggestMarksExact(t *testing.T) {
	s := newTestServer(t)
	aliceID := mkUser(t, s, "alice")
	if _, err := s.saveNote(context.Background(), aliceID, "alice", "n1", "N1", "body", []string{"sports-science", "sports", "历史"}); err != nil {
		t.Fatalf("saveNote: %v", err)
	}
	for _, tc := range []struct{ q, exact, notExact string }{
		{"sports_science", `data-insert="sports-science" data-exact`, ""},
		{"Sports", `data-insert="sports" data-exact`, `data-insert="sports-science" data-exact`},
		{"歷史", `data-insert="历史" data-exact`, ""},
		{"sports-sci", "", "data-exact"},
	} {
		w := httptest.NewRecorder()
		s.GetTagSuggest(w, httptest.NewRequest("GET", "/api/tags/suggest?q="+url.QueryEscape(tc.q), nil))
		body := w.Body.String()
		if tc.exact != "" && !strings.Contains(body, tc.exact) {
			t.Errorf("q=%q: want %s in %s", tc.q, tc.exact, body)
		}
		if tc.notExact != "" && strings.Contains(body, tc.notExact) {
			t.Errorf("q=%q: unexpected %s in %s", tc.q, tc.notExact, body)
		}
	}
}
