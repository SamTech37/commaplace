package handlers

import (
	"context"
	"strings"
	"testing"

	"commonplace/internal/markdown"
)

// TestRenameFollowsInboundLink: renaming a note's slug must update the href of
// every inbound [[link]] without rewriting any note body — the resolver follows
// the stored resolved_target_id uuid, not the author-typed slug.
func TestRenameFollowsInboundLink(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	alice := mkUser(t, s, "alice")

	target, err := s.saveNote(ctx, alice, "alice", "target", "Target", "hi", nil)
	if err != nil {
		t.Fatalf("save target: %v", err)
	}
	src, err := s.saveNote(ctx, alice, "alice", "src", "Src", "see [[target]]", nil)
	if err != nil {
		t.Fatalf("save src: %v", err)
	}

	// Sanity: link resolves to the original slug before rename.
	before := s.buildResolverForNote(ctx, "alice", src)
	if rt := before(markdown.WikiLink{Slug: "target"}); rt == nil || rt.Slug != "target" {
		t.Fatalf("pre-rename resolve = %+v, want slug=target", rt)
	}

	// Rename the target's slug. resolved_target_id is untouched (uuid stable).
	if _, err := s.DB.Exec(
		`UPDATE notes SET slug='renamed', slug_ci='renamed' WHERE id=$1`, target,
	); err != nil {
		t.Fatalf("rename: %v", err)
	}

	resolver := s.buildResolverForNote(ctx, "alice", src)
	rt := resolver(markdown.WikiLink{Slug: "target"})
	if rt == nil {
		t.Fatal("link unresolved after rename")
	}
	if rt.Slug != "renamed" {
		t.Fatalf("resolved slug = %q, want renamed", rt.Slug)
	}

	// And the rendered href follows.
	html, err := markdown.Render("see [[target]]", "alice", resolver, nil)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(string(html), `href="/alice/renamed"`) {
		t.Fatalf("href did not follow rename:\n%s", html)
	}
}
