package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"commonplace/internal/auth"
	"commonplace/internal/feedrank"
)

// mkDraftNote writes a titled, unpublished note: ready for PublishNote.
func mkDraftNote(t *testing.T, s *Server, author uuid.UUID, slug, body string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := s.DB.QueryRow(`
		INSERT INTO notes(author_id, slug, slug_ci, title, body_md, created_at, updated_at)
		VALUES($1, $2, $2, $2, $3, 0, 0) RETURNING id`, author, slug, body).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// publishAsync runs PublishNote on id and delivers its response.
func publishAsync(s *Server, user, id uuid.UUID) <-chan *httptest.ResponseRecorder {
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		w := httptest.NewRecorder()
		r := authedRequest(s, user, http.MethodPost, "/api/notes/"+id.String()+"/publish", "")
		r.SetPathValue("id", id.String())
		s.PublishNote(w, r)
		done <- w
	}()
	return done
}

// waitAdvisoryWaiters returns once n backends are queued on an advisory lock.
func waitAdvisoryWaiters(t *testing.T, s *Server, n int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var waiting int
		if err := s.DB.QueryRow(`
			SELECT count(*) FROM pg_locks
			WHERE locktype = 'advisory' AND NOT granted
			  AND database = (SELECT oid FROM pg_database WHERE datname = current_database())`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting >= n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d of %d waiters on the author lock", waiting, n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// publishOK fails t unless the publish delivered on done succeeded.
func publishOK(t *testing.T, done <-chan *httptest.ResponseRecorder) {
	t.Helper()
	select {
	case w := <-done:
		if w.Code != http.StatusOK {
			t.Errorf("publish: %d %s", w.Code, w.Body)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("publish never finished")
	}
}

// A publish arriving while its author's vault import holds the ranking lock
// waits for the import instead of deadlocking with it: the import links to
// the note being published, so a publish that row-locked the note before
// queueing on the author lock would block the import's link insert.
func TestPublishDuringVaultImportDoesNotDeadlock(t *testing.T) {
	s := newTestServer(t)
	alice := mkUser(t, s, "alice")
	target := mkDraftNote(t, s, alice, "target", "x")
	notes, cleanup, err := readVault(vaultRequest(t, map[string]string{"source.md": "[[target]]"}, uuid.New()))
	defer cleanup()
	if err != nil {
		t.Fatal(err)
	}
	var published <-chan *httptest.ResponseRecorder
	_, err = s.importVault(context.Background(), &auth.User{ID: alice, Handle: "alice"}, uuid.New(), notes, func(done int) error {
		if done == 0 { // the import holds the author lock and has written nothing yet
			published = publishAsync(s, alice, target)
			waitAdvisoryWaiters(t, s, 1)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("vault import: %v", err)
	}
	publishOK(t, published)
	var linked uuid.UUID
	if err := s.DB.QueryRow(`SELECT resolved_target_id FROM links WHERE source_note_id = $1`, notes[0].ID).Scan(&linked); err != nil || linked != target {
		t.Fatalf("import link resolved to %s (%v), want %s", linked, err, target)
	}
}

// Two publishes by one author whose notes link each other: each needs a key
// share on the other's row for its link, so they must be serialized on the
// author lock before either row-locks its own note.
func TestConcurrentCrossLinkedPublishesDoNotDeadlock(t *testing.T) {
	s := newTestServer(t)
	alice := mkUser(t, s, "alice")
	x := mkDraftNote(t, s, alice, "x", "[[y]]")
	y := mkDraftNote(t, s, alice, "y", "[[x]]")

	// Hold the author lock until both publishes are queued on it.
	gate, err := s.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer gate.Rollback()
	if err := feedrank.LockAuthor(context.Background(), gate, alice); err != nil {
		t.Fatal(err)
	}
	px := publishAsync(s, alice, x)
	py := publishAsync(s, alice, y)
	waitAdvisoryWaiters(t, s, 2)
	if err := gate.Commit(); err != nil {
		t.Fatal(err)
	}
	publishOK(t, px)
	publishOK(t, py)
	var resolved int
	if err := s.DB.QueryRow(`SELECT count(*) FROM links WHERE resolved_target_id IN ($1, $2)`, x, y).Scan(&resolved); err != nil || resolved != 2 {
		t.Fatalf("%d resolved cross links (%v), want 2", resolved, err)
	}
}
