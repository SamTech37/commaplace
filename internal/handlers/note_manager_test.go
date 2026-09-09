package handlers

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestManageNotes(t *testing.T) {
	s := newTestServer(t)
	alice, bob := mkUser(t, s, "alice"), mkUser(t, s, "bob")
	makeNote := func(owner uuid.UUID, handle, slug string) uuid.UUID {
		id, err := s.saveNote(context.Background(), owner, handle, slug, slug, "body", nil)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	a := makeNote(alice, "alice", "first")
	b := makeNote(bob, "bob", "other")
	post := func(body string, want int) {
		w := httptest.NewRecorder()
		s.PostManageNotes(w, authedRequest(s, alice, http.MethodPost, "/api/notes/manage", body))
		if w.Code != want {
			t.Fatalf("status %d, want %d: %s", w.Code, want, w.Body)
		}
	}
	post("action=hide&ids="+a.String()+"&ids="+b.String(), 303)
	if _, valid := publishedAt(t, s, a); valid {
		t.Fatal("own note was not hidden")
	}
	if _, valid := publishedAt(t, s, b); !valid {
		t.Fatal("changed another author's note")
	}
	if getNoteCode(s, bob, "alice", "first", true) != 404 {
		t.Fatal("hidden note leaked")
	}
	post("action=publish&ids="+a.String(), 303)
	if _, valid := publishedAt(t, s, a); !valid {
		t.Fatal("note not republished")
	}
	// Single-row action must ignore both bulk IDs and entire-result selection.
	c := makeNote(alice, "alice", "second")
	post("single=hide:"+a.String()+"&scope=all&tab=all&ids="+c.String(), 303)
	if _, valid := publishedAt(t, s, c); !valid {
		t.Fatal("single action changed checked note")
	}
	// A malformed ID or action must not partially change valid targets.
	post("action=delete&ids="+c.String()+"&ids=bad", 400)
	post("action=unknown&scope=all&tab=all", 400)
	post("action=delete", 400)
	// An invalid draft blocks the entire publish operation.
	draft, err := s.createDraft(context.Background(), alice)
	if err != nil {
		t.Fatal(err)
	}
	post("action=publish&ids="+a.String()+"&ids="+draft.String(), 422)
	if _, valid := publishedAt(t, s, a); valid {
		t.Fatal("invalid batch partially published")
	}
	// More than one scroll page, with a hidden note and another user's note spared.
	for i := 0; i < 25; i++ {
		makeNote(alice, "alice", fmt.Sprintf("extra-%d", i))
	}
	post("action=delete&scope=all", 303)
	var remaining int
	if err := s.DB.QueryRow(`SELECT count(*) FROM notes WHERE author_id=$1 AND deleted_at IS NULL AND published_at IS NOT NULL`, alice).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("%d published notes remain", remaining)
	}
	if getNoteCode(s, alice, "alice", "first", true) != 200 {
		t.Fatal("public-filter delete touched hidden note")
	}
	if getNoteCode(s, bob, "bob", "other", true) != 200 {
		t.Fatal("bulk delete touched other owner")
	}
	post("action=delete&scope=all&tab=all", 303)
	if err := s.DB.QueryRow(`SELECT count(*) FROM notes WHERE author_id=$1 AND deleted_at IS NULL`, alice).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("%d notes remain after entire-vault delete", remaining)
	}
}

func TestProfileManagementRendering(t *testing.T) {
	card := feedCard{NoteID: uuid.New(), Title: "article", URL: "/alice/article"}
	for _, owner := range []bool{false, true} {
		var output bytes.Buffer
		view := NoteListView{Cards: []feedCard{card}, Layout: "list", Manage: owner}
		if err := notesFragment(view).Render(context.Background(), &output); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(output.String(), `name="ids"`) != owner {
			t.Fatal("scroll fragment controls have wrong ownership")
		}
		if strings.Contains(output.String(), `/edit/`) != owner {
			t.Fatal("edit link has wrong ownership")
		}
	}
}
