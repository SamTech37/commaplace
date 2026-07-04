package handlers

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
)

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
