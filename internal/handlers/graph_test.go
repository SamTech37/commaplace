package handlers

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

// mkGraphNote inserts a published note and returns its id.
func mkGraphNote(t *testing.T, s *Server, author uuid.UUID, slug string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := s.DB.QueryRow(
		`INSERT INTO notes(author_id, slug, slug_ci, title, body_md, created_at, updated_at, published_at)
		 VALUES($1, $2, $2, $2, '', 1, 1, 1) RETURNING id`,
		author, slug,
	).Scan(&id); err != nil {
		t.Fatalf("mkGraphNote %s: %v", slug, err)
	}
	return id
}

func mkGraphLink(t *testing.T, s *Server, src, tgt, author uuid.UUID, slug string) {
	t.Helper()
	if _, err := s.DB.Exec(
		`INSERT INTO links(source_note_id, target_user_id, target_slug, resolved_target_id)
		 VALUES($1, $2, $3, $4)`, src, author, slug, tgt,
	); err != nil {
		t.Fatalf("mkGraphLink: %v", err)
	}
}

func localGraphIDs(t *testing.T, s *Server, note uuid.UUID, hops string) map[string]bool {
	t.Helper()
	url := "/api/graph/local?note=" + note.String()
	if hops != "" {
		url += "&hops=" + hops
	}
	w := httptest.NewRecorder()
	s.GetGraphLocal(w, httptest.NewRequest("GET", url, nil))
	if w.Code != 200 {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var payload struct {
		Nodes []graphNode `json:"nodes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	got := map[string]bool{}
	for _, n := range payload.Nodes {
		got[n.Title] = true
	}
	return got
}

// a -> b -> c: one hop sees b only, two hops reach c.
func TestLocalGraphHops(t *testing.T) {
	s := newTestServer(t)
	alice := mkUser(t, s, "alice")
	a := mkGraphNote(t, s, alice, "a")
	b := mkGraphNote(t, s, alice, "b")
	c := mkGraphNote(t, s, alice, "c")
	mkGraphLink(t, s, a, b, alice, "b")
	mkGraphLink(t, s, b, c, alice, "c")

	one := localGraphIDs(t, s, a, "")
	if !one["a"] || !one["b"] {
		t.Errorf("1 hop should hold a and b, got %v", one)
	}
	if one["c"] {
		t.Errorf("1 hop must not reach c, got %v", one)
	}

	two := localGraphIDs(t, s, a, "2")
	for _, want := range []string{"a", "b", "c"} {
		if !two[want] {
			t.Errorf("2 hops missing %q, got %v", want, two)
		}
	}
}

// Hop 2 follows backlinks too: b <- a and b -> c means from c we reach a.
func TestLocalGraphHopsBothDirections(t *testing.T) {
	s := newTestServer(t)
	alice := mkUser(t, s, "alice")
	a := mkGraphNote(t, s, alice, "a")
	b := mkGraphNote(t, s, alice, "b")
	c := mkGraphNote(t, s, alice, "c")
	mkGraphLink(t, s, a, b, alice, "b")
	mkGraphLink(t, s, b, c, alice, "c")

	two := localGraphIDs(t, s, c, "2")
	if !two["a"] {
		t.Errorf("2 hops from c should reach a via b's backlink, got %v", two)
	}
}

// A hub two hops out must not blow past the node cap.
func TestLocalGraphNodeCap(t *testing.T) {
	s := newTestServer(t)
	alice := mkUser(t, s, "alice")
	center := mkGraphNote(t, s, alice, "center")
	hub := mkGraphNote(t, s, alice, "hub")
	mkGraphLink(t, s, center, hub, alice, "hub")
	for i := 0; i < maxLocalGraphNodes+20; i++ {
		leaf := mkGraphNote(t, s, alice, "leaf"+string(rune('a'+i%26))+uuid.NewString()[:8])
		mkGraphLink(t, s, hub, leaf, alice, "leaf")
	}

	w := httptest.NewRecorder()
	s.GetGraphLocal(w, httptest.NewRequest("GET", "/api/graph/local?note="+center.String()+"&hops=2", nil))
	if w.Code != 200 {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var payload struct {
		Nodes []graphNode `json:"nodes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Nodes) > maxLocalGraphNodes {
		t.Errorf("node cap %d exceeded: %d nodes", maxLocalGraphNodes, len(payload.Nodes))
	}
}
