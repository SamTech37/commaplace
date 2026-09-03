package handlers

import (
	"context"
	"net/http/httptest"
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
