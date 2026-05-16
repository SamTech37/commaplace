package handlers

import (
	"context"
	"path/filepath"
	"testing"

	"commonplace/internal/auth"
	"commonplace/internal/db"
	"commonplace/internal/markdown"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Migrate(d); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return &Server{
		DB:   d,
		Auth: &auth.Auth{DB: d, Secret: []byte("test")},
	}
}

func mkUser(t *testing.T, s *Server, handle string) int64 {
	t.Helper()
	res, err := s.DB.Exec(
		`INSERT INTO users(handle, email, created_at) VALUES(?, ?, 0)`,
		handle, handle+"@example.com",
	)
	if err != nil {
		t.Fatalf("mkUser: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

func countResolved(t *testing.T, s *Server) (resolved, unresolved int) {
	t.Helper()
	if err := s.DB.QueryRow(
		`SELECT COUNT(*) FROM links WHERE resolved_target_id IS NOT NULL`,
	).Scan(&resolved); err != nil {
		t.Fatalf("count resolved: %v", err)
	}
	if err := s.DB.QueryRow(
		`SELECT COUNT(*) FROM links WHERE resolved_target_id IS NULL`,
	).Scan(&unresolved); err != nil {
		t.Fatalf("count unresolved: %v", err)
	}
	return
}

func TestSaveResolvesAndBacklinks(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()

	aliceID := mkUser(t, s, "alice")
	bobID := mkUser(t, s, "bob")

	// alice writes "intro" linking to bob's not-yet-existent foo, a missing
	// user, and a same-vault note that doesn't exist yet.
	body := "See [[@bob/foo]] and [[@nobody/x]] and [[localnote]]."
	if _, err := s.saveNote(ctx, aliceID, "alice", "", "intro", "Intro", body, nil); err != nil {
		t.Fatalf("save alice/intro: %v", err)
	}
	if r, u := countResolved(t, s); r != 0 || u != 3 {
		t.Fatalf("after alice/intro: resolved=%d unresolved=%d, want 0/3", r, u)
	}

	// bob creates foo — alice's @bob/foo link should now resolve.
	if _, err := s.saveNote(ctx, bobID, "bob", "", "foo", "Foo", "", nil); err != nil {
		t.Fatalf("save bob/foo: %v", err)
	}
	if r, u := countResolved(t, s); r != 1 || u != 2 {
		t.Fatalf("after bob/foo: resolved=%d unresolved=%d, want 1/2", r, u)
	}

	// alice creates localnote — same-vault link should now resolve.
	if _, err := s.saveNote(ctx, aliceID, "alice", "", "localnote", "Local", "", nil); err != nil {
		t.Fatalf("save alice/localnote: %v", err)
	}
	if r, u := countResolved(t, s); r != 2 || u != 1 {
		t.Fatalf("after alice/localnote: resolved=%d unresolved=%d, want 2/1", r, u)
	}

	// Resolver: when rendering alice's intro note in her vault, the resolver
	// should match the DB state we just verified.
	links := markdown.Extract(body)
	resolver := s.buildResolver(ctx, "alice", links)
	for _, l := range links {
		got := resolver(l)
		want := false
		switch {
		case l.User == "bob" && l.Slug == "foo":
			want = true
		case l.User == "" && l.Slug == "localnote":
			want = true
		}
		if got != want {
			t.Errorf("resolver(%+v) = %v, want %v", l, got, want)
		}
	}

	// Backlinks for bob's foo should include alice's intro.
	var fooID int64
	if err := s.DB.QueryRow(
		`SELECT id FROM notes WHERE author_id = ? AND slug = 'foo'`, bobID,
	).Scan(&fooID); err != nil {
		t.Fatal(err)
	}
	sameVault, crossVault, err := s.loadBacklinksSplit(ctx, fooID, "bob")
	if err != nil {
		t.Fatalf("loadBacklinksSplit: %v", err)
	}
	if len(sameVault) != 0 {
		t.Fatalf("sameVault = %+v, want 0 entries (alice is not bob)", sameVault)
	}
	if len(crossVault) != 1 || crossVault[0].Title != "Intro" || crossVault[0].AuthorHandle != "alice" {
		t.Fatalf("crossVault = %+v, want one entry titled Intro by alice", crossVault)
	}
}

func TestSlugAndFolderNormalization(t *testing.T) {
	cases := []struct {
		title, want string
	}{
		{"Hello World", "hello-world"},
		{"Why I rate by mood", "why-i-rate-by-mood"},
		{"   leading/trailing   ", "leading-trailing"},
		{"non-ASCII 日記 mix", "non-ascii-mix"},
		{"...", ""},
	}
	for _, c := range cases {
		if got := kebabSlug(c.title); got != c.want {
			t.Errorf("kebabSlug(%q) = %q, want %q", c.title, got, c.want)
		}
	}

	folderCases := []struct {
		in, want string
	}{
		{"music/mixing", "music/mixing"},
		{"  /a/b/  ", "a/b"},
		{"Music / Mixing", "music/mixing"},
		{"", ""},
	}
	for _, c := range folderCases {
		if got := normalizeFolder(c.in); got != c.want {
			t.Errorf("normalizeFolder(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
