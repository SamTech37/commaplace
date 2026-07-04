package handlers

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func paletteQuery(t *testing.T, s *Server, q string) []paletteItem {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/search/palette?q="+q, nil)
	rec := httptest.NewRecorder()
	s.GetPalette(rec, req)
	var items []paletteItem
	if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
		t.Fatalf("bad JSON: %v\n%s", err, rec.Body.String())
	}
	return items
}

func TestPaletteMatchesTitleCaseAndVariant(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	aliceID := mkUser(t, s, "alice")
	if _, err := s.saveNote(ctx, aliceID, "alice", "shulun", "數論筆記", "body", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.saveNote(ctx, aliceID, "alice", "ux", "UX Research", "body", nil); err != nil {
		t.Fatal(err)
	}

	// Simplified query must find the Traditional title.
	if items := paletteQuery(t, s, "%E6%95%B0%E8%AE%BA"); len(items) != 1 || items[0].URL != "/alice/shulun" {
		t.Errorf("simplified query: got %+v", items)
	}
	// Case-insensitive.
	if items := paletteQuery(t, s, "ux"); len(items) != 1 || items[0].Title != "UX Research" {
		t.Errorf("lowercase query: got %+v", items)
	}
	// Empty query returns empty list, not error.
	if items := paletteQuery(t, s, ""); len(items) != 0 {
		t.Errorf("empty query: got %+v", items)
	}
}
