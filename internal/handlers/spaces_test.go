package handlers

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublishedDraftRemainsPrivateUntilExplicitUpdate(t *testing.T) {
	s := newTestServer(t)
	a := mkUser(t, s, "alice")
	ctx := context.Background()
	id, err := s.saveNote(ctx, a, "alice", "story", "Published title", "Published body #old", []string{"old"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.autosaveNoteDocument(ctx, id, "alice", "story", "Unfinished title", "Secret body #secret", []string{"secret"}, "semi", 0); err != nil {
		t.Fatal(err)
	}
	var title, body string
	s.DB.QueryRow(`SELECT title,body_md FROM notes WHERE id=$1`, id).Scan(&title, &body)
	if title != "Published title" || body != "Published body #old" {
		t.Fatal("autosave changed public snapshot")
	}
	view := httptest.NewRecorder()
	s.Routes().ServeHTTP(view, httptest.NewRequest("GET", "/alice/story", nil))
	if view.Code != 200 || strings.Contains(view.Body.String(), "Secret body") || strings.Contains(view.Body.String(), "Unfinished title") {
		t.Fatal("public reader leaked draft")
	}
	edit := deskRequest(t, s, a, "GET", "/edit/"+id.String(), nil)
	if edit.Code != 200 || !strings.Contains(edit.Body.String(), "Secret body") {
		t.Fatal("editor did not restore private draft")
	}
	if strings.Contains(edit.Body.String(), "#old") {
		t.Fatal("reopening restored a removed public tag")
	}
	stale := authedRequest(s, a, "POST", "/api/notes/"+id.String()+"/publish", "revision=0")
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, stale)
	if w.Code != 409 {
		t.Fatalf("stale publish: %d", w.Code)
	}
	w = httptest.NewRecorder()
	s.Routes().ServeHTTP(w, authedRequest(s, a, "POST", "/api/notes/"+id.String()+"/publish", "revision=1"))
	if w.Code != 200 {
		t.Fatalf("publish: %d %s", w.Code, w.Body)
	}
	if feedHasSlug(t, s, "story") {
		t.Fatal("semi-public article entered recommendations")
	}
	view = httptest.NewRecorder()
	s.Routes().ServeHTTP(view, httptest.NewRequest("GET", "/alice/story", nil))
	if view.Code != 200 || !strings.Contains(view.Body.String(), "Secret body") {
		t.Fatal("semi-public article must remain readable")
	}
	tags, err := loadTagsForNote(ctx, s.DB, id)
	if err != nil || len(tags) != 1 || tags[0] != "secret" {
		t.Fatalf("publish failed to update tag index: %v %v", tags, err)
	}
}

func TestSpacePublishOwnershipIsolationAndReach(t *testing.T) {
	s := newTestServer(t)
	a := mkUser(t, s, "alice")
	b := mkUser(t, s, "bob")
	ctx := context.Background()
	one, _ := s.saveNote(ctx, a, "alice", "chapter", "Chapter", "Public content", nil)
	other, _ := s.saveNote(ctx, b, "bob", "foreign", "Foreign", "foreign", nil)
	draft, _ := s.createDraft(ctx, a)
	if err := s.autosaveNote(ctx, draft, "alice", "draft-chapter", "Private chapter", "Unpublished content", nil); err != nil {
		t.Fatal(err)
	}
	env := spaceEnvelope{Document: spaceDocument{Title: "Book", Body: "Introduction", Blocks: []spaceBlock{{Type: "note", ID: other.String()}}}}
	if w := deskRequest(t, s, a, "PUT", "/api/space", env); w.Code != 422 {
		t.Fatalf("foreign reference accepted: %d", w.Code)
	}
	env.Document.Blocks = []spaceBlock{{Type: "note", ID: one.String(), Distribution: "semi"}, {Type: "text", Text: "Between chapters"}, {Type: "note", ID: draft.String()}}
	w := deskRequest(t, s, a, "PUT", "/api/space", env)
	if w.Code != 200 {
		t.Fatalf("save: %d %s", w.Code, w.Body)
	}
	if w = deskRequest(t, s, a, "PUT", "/api/space", env); w.Code != 409 {
		t.Fatal("stale space save overwrote content")
	}
	if err := s.autosaveNote(ctx, one, "alice", "chapter", "Unfinished chapter", "Private edits", nil); err != nil {
		t.Fatal(err)
	}
	w = deskRequest(t, s, a, "POST", "/api/space/publish", map[string]any{"revision": 1, "publishIDs": []string{}})
	if w.Code != 200 {
		t.Fatalf("publish book: %d %s", w.Code, w.Body)
	}
	if feedHasSlug(t, s, "chapter") {
		t.Fatal("semi-public child remains in feed")
	}
	if _, ok := publishedAt(t, s, draft); ok {
		t.Fatal("book silently published an unchecked draft")
	}
	view := httptest.NewRecorder()
	s.Routes().ServeHTTP(view, httptest.NewRequest("GET", "/alice", nil))
	if view.Code != 200 || !strings.Contains(view.Body.String(), "Between chapters") || !strings.Contains(view.Body.String(), "Chapter") || strings.Contains(view.Body.String(), "Private chapter") || strings.Contains(view.Body.String(), "Unfinished chapter") {
		t.Fatalf("incorrect public book: %d %s", view.Code, view.Body)
	}
	var body string
	s.DB.QueryRow(`SELECT body_md FROM notes WHERE id=$1`, one).Scan(&body)
	if body != "Public content" {
		t.Fatal("reach change published unfinished content")
	}
	w = deskRequest(t, s, b, "GET", "/api/space", nil)
	if strings.Contains(w.Body.String(), "Introduction") {
		t.Fatal("private space crossed accounts")
	}
	w = deskRequest(t, s, a, "GET", "/api/space", nil)
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Revision != 2 {
		t.Fatalf("wrong revision: %d", env.Revision)
	}
	w = deskRequest(t, s, a, "POST", "/api/space/publish", map[string]any{"revision": 2, "publishIDs": []string{draft.String()}})
	if w.Code != 200 {
		t.Fatalf("selected draft publish: %d %s", w.Code, w.Body)
	}
	if _, ok := publishedAt(t, s, draft); !ok {
		t.Fatal("explicitly selected draft not published")
	}
	view = httptest.NewRecorder()
	s.Routes().ServeHTTP(view, httptest.NewRequest("GET", "/alice", nil))
	if !strings.Contains(view.Body.String(), "Private chapter") {
		t.Fatal("selected chapter missing from public book")
	}
	view = httptest.NewRecorder()
	s.Routes().ServeHTTP(view, httptest.NewRequest("GET", "/alice/chapter?space=alice", nil))
	if !strings.Contains(view.Body.String(), `href="/alice/draft-chapter?space=alice"`) || !strings.Contains(view.Body.String(), "← Book") {
		t.Fatal("reader cannot return to the book or continue to the next chapter")
	}
}
