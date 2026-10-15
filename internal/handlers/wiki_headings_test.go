package handlers

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func headingSuggest(t *testing.T, s *Server, viewer uuid.UUID, q string) string {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/wiki/suggest?q="+url.QueryEscape(q), nil)
	if viewer != uuid.Nil {
		req.AddCookie(sessionCookie(t, s, viewer))
	}
	w := httptest.NewRecorder()
	s.GetWikiSuggest(w, req)
	return w.Body.String()
}

func TestWikiSuggestHeadings(t *testing.T) {
	s := newTestServer(t)
	alice := mkUser(t, s, "alice")
	bob := mkUser(t, s, "bob")
	mkPreviewNote(t, s, alice, "doc", "Doc", "# Intro\n## Setup: Linux\n```\n# not one\n```\n## Usage\n", "published")
	mkPreviewNote(t, s, alice, "draft", "Draft", "# Secret plan\n", "draft")

	got := headingSuggest(t, s, alice, "doc#")
	for _, want := range []string{`data-insert="doc#Intro"`, `data-insert="doc#Setup Linux"`, `data-insert="doc#Usage"`, ">H2<"} {
		if !strings.Contains(got, want) {
			t.Errorf("own vault: missing %s in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "not one") {
		t.Errorf("fenced line offered as a heading:\n%s", got)
	}

	if got := headingSuggest(t, s, alice, "doc#set"); !strings.Contains(got, "Setup") || strings.Contains(got, "Usage") {
		t.Errorf("filter by query failed:\n%s", got)
	}
	if got := headingSuggest(t, s, alice, "doc#Intro#us"); !strings.Contains(got, `data-insert="doc#Intro#Usage"`) {
		t.Errorf("nested path not kept:\n%s", got)
	}
	if got := headingSuggest(t, s, bob, "@alice/doc#us"); !strings.Contains(got, `data-insert="@alice/doc#Usage"`) {
		t.Errorf("cross-vault headings missing:\n%s", got)
	}

	// Drafts: their author sees the headings, nobody else does.
	if got := headingSuggest(t, s, alice, "draft#"); !strings.Contains(got, "Secret plan") {
		t.Errorf("author can't see own draft headings:\n%s", got)
	}
	if got := headingSuggest(t, s, bob, "@alice/draft#"); got != "" {
		t.Errorf("draft headings leaked to another user:\n%s", got)
	}
	if got := headingSuggest(t, s, uuid.Nil, "@alice/draft#"); got != "" {
		t.Errorf("draft headings leaked to anonymous:\n%s", got)
	}
	if got := headingSuggest(t, s, alice, "#Intro"); got != "" {
		t.Errorf("same-note # should offer nothing:\n%s", got)
	}
}
