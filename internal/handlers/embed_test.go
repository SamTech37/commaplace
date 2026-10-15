package handlers

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"commonplace/internal/markdown"
)

// Nested embeds and the links inside them are typed relative to the note that
// contains them, not the outermost note being viewed.
func TestEmbedResolverNestedVaults(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	aliceID := mkUser(t, s, "alice")
	bobID := mkUser(t, s, "bob")

	ids := map[string]uuid.UUID{"alice": aliceID, "bob": bobID}
	mustSave := func(handle, slug, title, body string) {
		t.Helper()
		if _, err := s.saveNote(ctx, ids[handle], handle, slug, title, body, nil); err != nil {
			t.Fatalf("save %s/%s: %v", handle, slug, err)
		}
	}
	// Both vaults own a "leaf", with different bodies.
	mustSave("alice", "leaf", "Alice leaf", "ALICE-LEAF")
	mustSave("bob", "leaf", "Bob leaf", "BOB-LEAF [[peer]]")
	mustSave("bob", "peer", "Bob peer", "x")
	mustSave("bob", "mid", "Bob mid", "![[leaf]]")
	mustSave("alice", "solo", "Alice solo", "SOLO")

	body := "![[@bob/mid]]\n\n![[solo]]\n\n![[missing]]"
	embed := s.buildEmbedResolver(ctx, "alice", body, 2)
	html, err := markdown.Render(body, "alice", s.buildResolver(ctx, "alice", markdown.Extract(body)), embed)
	if err != nil {
		t.Fatal(err)
	}
	out := string(html)
	for _, want := range []string{"BOB-LEAF", "SOLO", `href="/bob/peer"`, "找不到"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "ALICE-LEAF") {
		t.Errorf("bob/mid's ![[leaf]] resolved against alice's vault:\n%s", out)
	}
}

// Past maxDepth an embed renders as a placeholder, never recursing.
func TestEmbedResolverDepthCap(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	id := mkUser(t, s, "alice")
	if _, err := s.saveNote(ctx, id, "alice", "loop", "Loop", "LOOP ![[loop]]", nil); err != nil {
		t.Fatal(err)
	}
	body := "![[loop]]"
	html, err := markdown.Render(body, "alice", nil, s.buildEmbedResolver(ctx, "alice", body, 2))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(html), "LOOP"); n != 2 {
		t.Errorf("rendered %d levels, want 2:\n%s", n, html)
	}
	if !strings.Contains(string(html), "嵌入過深") {
		t.Errorf("no too-deep placeholder:\n%s", html)
	}
}

// ![[note#Heading]] embeds only that section; an unknown heading shows the
// missing-section line inside the embed instead of the whole note.
func TestEmbedResolverSection(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	id := mkUser(t, s, "alice")
	src := "# A\nIN-A\n## A1\nIN-A1\n# B\nIN-B\n"
	if _, err := s.saveNote(ctx, id, "alice", "src", "Src", src, nil); err != nil {
		t.Fatal(err)
	}
	body := "![[src#A]]\n\n![[src#Nope]]"
	html, err := markdown.Render(body, "alice", nil, s.buildEmbedResolver(ctx, "alice", body, maxEmbedDepth))
	if err != nil {
		t.Fatal(err)
	}
	out := string(html)
	for _, want := range []string{"IN-A", "IN-A1", "找不到段落 <code>#Nope</code>", `href="/alice/src#a"`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "IN-B") {
		t.Errorf("section embed leaked the next section:\n%s", out)
	}
}
