package handlers

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func deskRequest(t *testing.T, s *Server, user uuid.UUID, method, path string, value any) *httptest.ResponseRecorder {
	t.Helper()
	var body string
	if value != nil {
		b, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		body = string(b)
	}
	req := authedRequest(s, user, method, path, body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)
	return rec
}
func readDesk(t *testing.T, s *Server, user uuid.UUID) deskEnvelope {
	t.Helper()
	r := deskRequest(t, s, user, "GET", "/api/desk/state", nil)
	if r.Code != 200 {
		t.Fatalf("read desk: %d %s", r.Code, r.Body)
	}
	var e deskEnvelope
	if err := json.Unmarshal(r.Body.Bytes(), &e); err != nil {
		t.Fatal(err)
	}
	return e
}

func TestDeskStateIsolationConflictAndNoCollection(t *testing.T) {
	s := newTestServer(t)
	a := mkUser(t, s, "alice")
	b := mkUser(t, s, "bob")
	id, err := s.saveNote(context.Background(), b, "bob", "article", "Article", "original content", nil)
	if err != nil {
		t.Fatal(err)
	}
	initial := readDesk(t, s, a)
	if initial.Revision != 0 || len(initial.State.Windows) != 0 || initial.State.Active != "" {
		t.Fatalf("initial: %+v", initial)
	}
	initial.State.Windows = append(initial.State.Windows, newDeskWindow("feed", "", ""))
	note := newDeskWindow("note", id.String(), "")
	note.Scroll = 123
	note.Width = 512
	initial.State.Windows = append(initial.State.Windows, note)
	initial.State.Active = note.Key
	initial.State.ScrollLeft = 350
	r := deskRequest(t, s, a, "PUT", "/api/desk/state", initial)
	if r.Code != 200 {
		t.Fatalf("save: %d %s", r.Code, r.Body)
	}
	restored := readDesk(t, s, a)
	if restored.Revision != 1 || len(restored.State.Windows) != 2 || restored.State.Windows[1].Title != "Article" || restored.State.Windows[1].Scroll != 123 || restored.State.Windows[1].Width != 512 || restored.State.Active != note.Key {
		t.Fatalf("restore: %+v", restored)
	}
	if r := deskRequest(t, s, a, "PUT", "/api/desk/state", initial); r.Code != 409 {
		t.Fatalf("stale initial update: %d", r.Code)
	}
	if got := readDesk(t, s, b); len(got.State.Windows) != 0 {
		t.Fatalf("other user leaked: %+v", got)
	}
	if savedCount(t, s, a) != 0 {
		t.Fatal("opening a desk saved content")
	}
	if r := deskRequest(t, s, a, "GET", "/api/desk/bookmarks?ids="+id.String(), nil); r.Code != 200 || !strings.Contains(r.Body.String(), `false`) {
		t.Fatalf("opening unexpectedly bookmarked note: %d %s", r.Code, r.Body)
	}
	var count int
	s.DB.QueryRow(`SELECT count(*) FROM links`).Scan(&count)
	if count != 0 {
		t.Fatal("opening a desk inserted links")
	}
	restored.State.Windows[1].Width = 480
	if r := deskRequest(t, s, a, "PUT", "/api/desk/state", restored); r.Code != 200 {
		t.Fatalf("subsequent save: %d %s", r.Code, r.Body)
	}
	if r := deskRequest(t, s, a, "PUT", "/api/desk/state", restored); r.Code != 409 {
		t.Fatalf("stale subsequent save: %d", r.Code)
	}
}

func TestDeskRejectsPrivateAndRemovedNotes(t *testing.T) {
	s := newTestServer(t)
	a := mkUser(t, s, "alice")
	b := mkUser(t, s, "bob")
	id, err := s.createDraft(context.Background(), b)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"note", "edit"} {
		r := deskRequest(t, s, a, "GET", "/me/desk/pane?kind="+kind+"&ref="+id.String(), nil)
		if r.Code != 404 {
			t.Fatalf("private %s: %d", kind, r.Code)
		}
	}
	env := deskEnvelope{State: deskState{Windows: []deskWindow{newDeskWindow("note", id.String(), "")}}}
	if r := deskRequest(t, s, a, "PUT", "/api/desk/state", env); r.Code != 200 {
		t.Fatalf("prune: %d %s", r.Code, r.Body)
	}
	if len(readDesk(t, s, a).State.Windows) != 0 {
		t.Fatal("private draft survived restore")
	}
	if r := postToggle(t, s, "/api/save", a, id); r.Code != 404 {
		t.Fatalf("bookmarked private draft: %d", r.Code)
	}
	if _, err := s.DB.Exec(`UPDATE notes SET deleted_at=1 WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if r := deskRequest(t, s, b, "GET", "/me/desk/pane?kind=edit&ref="+id.String(), nil); r.Code != 404 {
		t.Fatalf("deleted editor: %d", r.Code)
	}
}

func TestDeskResolveAndValidation(t *testing.T) {
	s := newTestServer(t)
	a := mkUser(t, s, "alice")
	u, _ := s.Auth.CurrentUser(authedRequest(s, a, "GET", "/", ""))
	for _, raw := range []string{"https://evil.example/x", "//evil.example/x", "/\\evil.example", "/api/notes/anything/raw", "/write", "/me/desk"} {
		if _, err := s.deskResolve(context.Background(), u, raw); err == nil {
			t.Errorf("accepted unsafe URL %q", raw)
		}
	}
	for _, raw := range []string{"/feed", "/feed?tab=following&tag=音樂", "/me/saved", "/search?q=台北", "/tag/田野", "/alice", "/graph"} {
		if _, err := s.deskResolve(context.Background(), u, raw); err != nil {
			t.Errorf("rejected %q: %v", raw, err)
		}
	}
	env := readDesk(t, s, a)
	env.State.Windows = []deskWindow{newDeskWindow("feed", "", "")}
	env.State.Windows[0].Width = 10
	if r := deskRequest(t, s, a, "PUT", "/api/desk/state", env); r.Code != 400 {
		t.Errorf("invalid width: %d", r.Code)
	}
	env.State.Windows = make([]deskWindow, 65)
	if r := deskRequest(t, s, a, "PUT", "/api/desk/state", env); r.Code != 400 {
		t.Errorf("unbounded pane count: %d", r.Code)
	}
	req := authedRequest(s, a, "PUT", "http://example.com/api/desk/state", `{}`)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("cross origin state: %d", rec.Code)
	}
}

func TestDeskDraftPaneAndHome(t *testing.T) {
	s := newTestServer(t)
	a := mkUser(t, s, "alice")
	r := deskRequest(t, s, a, "POST", "/api/desk/drafts", nil)
	if r.Code != 201 {
		t.Fatalf("draft: %d %s", r.Code, r.Body)
	}
	var p deskWindow
	json.Unmarshal(r.Body.Bytes(), &p)
	if p.Kind != "edit" || p.Ref == "" {
		t.Fatalf("draft descriptor: %+v", p)
	}
	view := deskRequest(t, s, a, "GET", "/me/desk/pane?kind=edit&ref="+p.Ref, nil)
	if view.Code != 200 || !strings.Contains(view.Body.String(), `desk-embedded`) || !strings.Contains(view.Body.String(), `data-note-id="`+p.Ref+`"`) {
		t.Fatalf("embedded editor: %d %s", view.Code, view.Body)
	}
	if _, ok := publishedAt(t, s, uuid.MustParse(p.Ref)); ok {
		t.Fatal("new desk note published automatically")
	}
	home := deskRequest(t, s, a, "GET", "/", nil)
	if home.Header().Get("Location") != "/feed" {
		t.Fatal("signed in home must open public reading")
	}
	out := httptest.NewRecorder()
	s.Routes().ServeHTTP(out, httptest.NewRequest("GET", "/", nil))
	if out.Header().Get("Location") != "/feed" {
		t.Fatal("public home changed")
	}
	page := deskRequest(t, s, a, "GET", "/me/desk", nil)
	if page.Code != 200 || !strings.Contains(page.Body.String(), `id="private-desk"`) || strings.Contains(page.Body.String(), `class="desk-tabs"`) {
		t.Fatalf("desk page: %d", page.Code)
	}
	for _, unnecessary := range []string{"正在打開工作桌", "在桌上打開", `src="/assets/desk.js`} {
		if strings.Contains(page.Body.String(), unnecessary) {
			t.Errorf("empty shell includes %q", unnecessary)
		}
	}
	if !strings.Contains(view.Body.String(), "desk-rich-content.js") || strings.Contains(view.Body.String(), "mermaid.min.js") {
		t.Fatal("editor pane must defer rich-content dependencies")
	}
}

func TestDeskWikiMaterialPriorityAndPrivacy(t *testing.T) {
	s := newTestServer(t)
	a := mkUser(t, s, "alice")
	b := mkUser(t, s, "bob")
	pub, _ := s.saveNote(context.Background(), b, "bob", "source", "Source", "source", nil)
	draft, _ := s.createDraft(context.Background(), b)
	if err := s.autosaveNote(context.Background(), draft, "bob", "private-secret", "Private Secret", "secret", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.saveNote(context.Background(), a, "alice", "own-note", "Own Note", "own", nil); err != nil {
		t.Fatal(err)
	}
	r := deskRequest(t, s, a, "GET", "/api/wiki/suggest?desk=1&notes="+pub.String()+","+draft.String(), nil)
	if r.Code != 200 || !strings.Contains(r.Body.String(), "@bob/source") || strings.Contains(r.Body.String(), "Private Secret") {
		t.Fatalf("suggestions: %d %s", r.Code, r.Body)
	}
	if strings.Index(r.Body.String(), "Source") > strings.Index(r.Body.String(), "Own Note") {
		t.Fatal("open material must precede own notes")
	}
	if _, err := s.DB.Exec(`UPDATE notes SET hidden_at=1 WHERE id=$1`, pub); err != nil {
		t.Fatal(err)
	}
	if r := deskRequest(t, s, a, "GET", "/me/desk/pane?kind=note&ref="+pub.String(), nil); r.Code != 404 {
		t.Fatalf("hidden note pane: %d", r.Code)
	}
}

func TestEditorRejectsConcurrentDeskSave(t *testing.T) {
	s := newTestServer(t)
	a := mkUser(t, s, "alice")
	id, _ := s.createDraft(context.Background(), a)
	save := func(doc, rev string) *httptest.ResponseRecorder {
		r := authedRequest(s, a, "PATCH", "/api/notes/"+id.String(), "document="+urlenc(doc)+"&revision="+rev)
		r.SetPathValue("id", id.String())
		w := httptest.NewRecorder()
		s.PatchNote(w, r)
		return w
	}
	if r := save("Title\nfirst device", "0"); r.Code != 200 {
		t.Fatalf("first: %d %s", r.Code, r.Body)
	}
	if r := save("Title\nstale second device", "0"); r.Code != 409 {
		t.Fatalf("stale: %d %s", r.Code, r.Body)
	}
	var body string
	s.DB.QueryRow(`SELECT body_md FROM notes WHERE id=$1`, id).Scan(&body)
	if body != "first device" {
		t.Fatalf("lost content: %q", body)
	}
	if r := save("Title\nnext save", "1"); r.Code != 200 {
		t.Fatalf("next: %d %s", r.Code, r.Body)
	}
}

func TestDeskStartsEmptyWithoutCreatingNotes(t *testing.T) {
	s := newTestServer(t)
	a := mkUser(t, s, "alice")
	for range 3 {
		env := readDesk(t, s, a)
		if env.State.Windows == nil || len(env.State.Windows) != 0 || env.State.Active != "" {
			t.Fatalf("new desk must be an empty array: %+v", env)
		}
	}
	var count int
	if err := s.DB.QueryRow(`SELECT count(*) FROM notes WHERE author_id=$1`, a).Scan(&count); err != nil || count != 0 {
		t.Fatalf("visiting desk created notes: count=%d err=%v", count, err)
	}
	env := deskEnvelope{State: deskState{Windows: []deskWindow{}}}
	if r := deskRequest(t, s, a, "PUT", "/api/desk/state", env); r.Code != 200 {
		t.Fatalf("save empty: %d %s", r.Code, r.Body)
	}
	if got := readDesk(t, s, a); len(got.State.Windows) != 0 || got.State.Active != "" || got.Revision != 1 {
		t.Fatalf("closing everything must stay empty after reload: %+v", got)
	}
}

func TestDeskLibraryIncludesOwnDraftsAndFilters(t *testing.T) {
	s := newTestServer(t)
	a := mkUser(t, s, "alice")
	b := mkUser(t, s, "bob")
	pub, err := s.saveNote(context.Background(), a, "alice", "published", "Published", "body", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.createDraft(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	draft, err := s.createDraft(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.autosaveNote(context.Background(), draft, "alice", "draft", "Named Draft", "private", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.saveNote(context.Background(), b, "bob", "other", "Other", "body", nil); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"hidden_at", "deleted_at"} {
		id, err := s.saveNote(context.Background(), a, "alice", field, field, "body", nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.DB.Exec(`UPDATE notes SET `+field+`=1 WHERE id=$1`, id); err != nil {
			t.Fatal(err)
		}
	}
	for _, query := range []string{"", "?q=Published", "?q=Draft", "?filter=public"} {
		r := deskRequest(t, s, a, "GET", "/api/desk/notes"+query, nil)
		var result struct {
			Notes []struct {
				ID        string
				Published bool
			}
			More bool
		}
		if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &result) != nil {
			t.Fatalf("library: %d %s", r.Code, r.Body)
		}
		want := 1
		if query == "" {
			want = 3
		}
		if len(result.Notes) != want || result.More {
			t.Fatalf("unexpected library: %s", r.Body)
		}
		if (query == "?q=Published" || query == "?filter=public") && (result.Notes[0].ID != pub.String() || !result.Notes[0].Published) {
			t.Fatalf("wrong note: %s", r.Body)
		}
	}
	// Filtering the library must never discard existing drafts or editor access.
	if r := deskRequest(t, s, a, "GET", "/me/desk/pane?kind=edit&ref="+draft.String(), nil); r.Code != 200 {
		t.Fatalf("draft no longer editable: %d", r.Code)
	}
}
