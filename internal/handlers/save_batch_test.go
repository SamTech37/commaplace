package handlers

import (
	"context"
	"testing"
)

// recomputeLinks and the tag inserts write every row in one statement each;
// each row must still resolve exactly as a per-link lookup would have.
func TestSaveWritesAllLinksAndTags(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	alice := mkUser(t, s, "alice")
	bob := mkUser(t, s, "bob")
	if _, err := s.saveNote(ctx, bob, "bob", "foo", "Foo", "", nil); err != nil {
		t.Fatal(err)
	}
	body := "[[@bob/foo]] [[@BOB/bar]] [[@ghost/x]] [[local]] [[@bob/foo#Part]]"
	id, err := s.saveNote(ctx, alice, "alice", "src", "Src", body, []string{"one", "two", "three"})
	if err != nil {
		t.Fatal(err)
	}

	rows, err := s.DB.Query(`
		SELECT l.raw_target, u.handle, l.resolved_target_id IS NOT NULL
		FROM links l LEFT JOIN users u ON u.id = l.target_user_id
		WHERE l.source_note_id = $1`, id)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type row struct {
		user     string
		resolved bool
	}
	got := map[string]row{}
	for rows.Next() {
		var raw string
		var user *string
		var r row
		if err := rows.Scan(&raw, &user, &r.resolved); err != nil {
			t.Fatal(err)
		}
		if user != nil {
			r.user = *user
		}
		got[raw] = r
	}
	// Extract dedupes by (user, slug), so @bob/foo#Part folds into @bob/foo.
	want := map[string]row{
		"@bob/foo": {"bob", true},
		"@BOB/bar": {"bob", false},
		"@ghost/x": {"", false},
		"local":    {"alice", false},
	}
	if len(got) != len(want) {
		t.Errorf("got %d link rows %v, want %d", len(got), got, len(want))
	}
	for raw, w := range want {
		if got[raw] != w {
			t.Errorf("link %q = %+v, want %+v", raw, got[raw], w)
		}
	}

	var tags int
	if err := s.DB.QueryRow(`SELECT count(*) FROM note_tags WHERE note_id = $1`, id).Scan(&tags); err != nil {
		t.Fatal(err)
	}
	if tags != 3 {
		t.Errorf("got %d tags, want 3", tags)
	}
}
