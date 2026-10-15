package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// GetTagPage now shares feed's noteCardColumns + scanCards path instead of
// its own narrower query — this guards that switch actually returns the
// right note (title, URL, list layout) rather than silently querying an
// empty result set or panicking on a column-count mismatch.
func TestGetTagPageListsTaggedNote(t *testing.T) {
	s := newTestServer(t)
	aliceID := mkUser(t, s, "alice")
	if _, err := s.saveNote(context.Background(), aliceID, "alice", "n1", "Project X Kickoff", "Working on it.", []string{"project-x"}); err != nil {
		t.Fatalf("saveNote: %v", err)
	}

	req := httptest.NewRequest("GET", "/tag/project-x", nil)
	req.SetPathValue("tag", "project-x")
	w := httptest.NewRecorder()
	s.GetTagPage(w, req)

	if w.Code != 200 {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body)
	}
	body := w.Body.String()
	for _, want := range []string{"Project X Kickoff", `href="/alice/n1"`} {
		if !strings.Contains(body, want) {
			t.Errorf("tag page missing %q:\n%s", want, body)
		}
	}
}

// A deleted or hidden note must not keep a tag alive: chips and suggestions
// advertised a count, and clicking through showed nothing.
func TestTagCountsSkipUnreadableNotes(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	aliceID := mkUser(t, s, "alice")
	for _, slug := range []string{"kept", "deleted", "hidden"} {
		if _, err := s.saveNote(ctx, aliceID, "alice", slug, slug, "body", []string{"ghost"}); err != nil {
			t.Fatalf("saveNote %s: %v", slug, err)
		}
	}
	for _, q := range []string{
		`UPDATE notes SET deleted_at = 1 WHERE slug = 'deleted'`,
		`UPDATE notes SET hidden_at = 1 WHERE slug = 'hidden'`,
	} {
		if _, err := s.DB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}

	chips, err := loadTopTagChips(ctx, s.DB, 8)
	if err != nil {
		t.Fatal(err)
	}
	if len(chips) != 1 || chips[0].Count != 1 {
		t.Errorf("chips = %+v, want one #ghost with count 1", chips)
	}

	w := httptest.NewRecorder()
	s.GetTagSuggest(w, httptest.NewRequest("GET", "/api/tags/suggest?q=gho", nil))
	if !strings.Contains(w.Body.String(), `<span class="ac-secondary">1</span>`) {
		t.Errorf("suggest count, want 1: %s", w.Body.String())
	}
}

func TestGetTagSuggestPrefixMatch(t *testing.T) {
	s := newTestServer(t)
	aliceID := mkUser(t, s, "alice")
	if _, err := s.saveNote(context.Background(), aliceID, "alice", "n1", "N1", "Working on it.", []string{"project-x"}); err != nil {
		t.Fatalf("saveNote: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/tags/suggest?q=proj", nil)
	w := httptest.NewRecorder()
	s.GetTagSuggest(w, req)

	if !strings.Contains(w.Body.String(), `data-insert="project-x"`) {
		t.Fatalf("expected project-x suggestion, got %s", w.Body.String())
	}
}

// data-exact marks the item q already names once normalized (or in the other
// script), so the editor doesn't offer to create a tag that exists.
func TestGetTagSuggestMarksExact(t *testing.T) {
	s := newTestServer(t)
	aliceID := mkUser(t, s, "alice")
	if _, err := s.saveNote(context.Background(), aliceID, "alice", "n1", "N1", "body", []string{"sports-science", "sports", "历史"}); err != nil {
		t.Fatalf("saveNote: %v", err)
	}
	for _, tc := range []struct{ q, exact, notExact string }{
		{"sports_science", `data-insert="sports-science" data-exact`, ""},
		{"Sports", `data-insert="sports" data-exact`, `data-insert="sports-science" data-exact`},
		{"歷史", `data-insert="历史" data-exact`, ""},
		{"sports-sci", "", "data-exact"},
	} {
		w := httptest.NewRecorder()
		s.GetTagSuggest(w, httptest.NewRequest("GET", "/api/tags/suggest?q="+url.QueryEscape(tc.q), nil))
		body := w.Body.String()
		if tc.exact != "" && !strings.Contains(body, tc.exact) {
			t.Errorf("q=%q: want %s in %s", tc.q, tc.exact, body)
		}
		if tc.notExact != "" && strings.Contains(body, tc.notExact) {
			t.Errorf("q=%q: unexpected %s in %s", tc.q, tc.notExact, body)
		}
	}
}

// The related-tag cloud is served from a 5-min cache: a second read doesn't
// rerun the query (a co-occurring tag written behind the cache's back stays
// invisible), and it refills once the entry expires or saveNote expires it.
func TestRelatedTagsCached(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	aliceID := mkUser(t, s, "alice")
	cloud := func() []string {
		var out []string
		for _, ct := range s.relatedTags(ctx, "a", []string{"a"}, []any{"a"}) {
			out = append(out, ct.Tag)
		}
		return out
	}
	save := func(slug string, tags ...string) {
		if _, err := s.saveNote(ctx, aliceID, "alice", slug, slug, "body", tags); err != nil {
			t.Fatalf("saveNote %s: %v", slug, err)
		}
	}

	save("n1", "a", "b")
	if got := cloud(); !slices.Equal(got, []string{"b"}) {
		t.Fatalf("first read = %v, want [b]", got)
	}
	if _, err := s.DB.Exec(`INSERT INTO note_tags(note_id, tag, created_at)
		SELECT id, 'c', 0 FROM notes WHERE slug = 'n1'`); err != nil {
		t.Fatalf("insert tag: %v", err)
	}
	if got := cloud(); !slices.Equal(got, []string{"b"}) {
		t.Errorf("cached read = %v, want [b] (query rerun?)", got)
	}

	// Expiry: backdate the entry past its TTL.
	s.tagClouds.mu.Lock()
	e := s.tagClouds.m["a"]
	e.until = time.Now().Add(-time.Second)
	s.tagClouds.m["a"] = e
	s.tagClouds.mu.Unlock()
	if got := cloud(); !slices.Equal(got, []string{"b", "c"}) {
		t.Errorf("after expiry = %v, want [b c]", got)
	}

	save("n3", "a", "d")
	if got := cloud(); !slices.Equal(got, []string{"b", "c", "d"}) {
		t.Errorf("after saveNote = %v, want [b c d]", got)
	}
}

// relatedCloud reads the cloud for tag through the cache, as GetTagPage does.
func relatedCloud(s *Server, tag string) []string {
	var out []string
	for _, ct := range s.relatedTags(context.Background(), tag, []string{tag}, []any{tag}) {
		out = append(out, ct.Tag)
	}
	return out
}

// Moderation must take effect on a cached cloud at once: hiding a note drops
// its co-tags, unhiding brings them back.
func TestAdminHideExpiresTagCloud(t *testing.T) {
	s := newTestServer(t)
	s.AdminHandle, s.AdminEmail = "alice", "alice@example.com"
	ctx := context.Background()
	alice := mkUser(t, s, "alice")
	if _, err := s.saveNote(ctx, alice, "alice", "n1", "N1", "body", []string{"a", "b"}); err != nil {
		t.Fatalf("saveNote n1: %v", err)
	}
	n2, err := s.saveNote(ctx, alice, "alice", "n2", "N2", "body", []string{"a", "c"})
	if err != nil {
		t.Fatalf("saveNote n2: %v", err)
	}
	if got := relatedCloud(s, "a"); !slices.Equal(got, []string{"b", "c"}) {
		t.Fatalf("before hide = %v, want [b c]", got)
	}
	for _, c := range []struct {
		action string
		want   []string
	}{{"hide", []string{"b"}}, {"unhide", []string{"b", "c"}}} {
		w := httptest.NewRecorder()
		s.PostAdminHide(w, authedRequest(s, alice, http.MethodPost, "/admin/hide",
			"note_id="+n2.String()+"&action="+c.action))
		if w.Code != http.StatusSeeOther {
			t.Fatalf("%s: status %d body=%s", c.action, w.Code, w.Body)
		}
		if got := relatedCloud(s, "a"); !slices.Equal(got, c.want) {
			t.Errorf("after %s = %v, want %v", c.action, got, c.want)
		}
	}
}

// Publishing a draft rewrites its note_tags; a new co-tag shows in a cached
// cloud right away, as with bulk publish.
func TestPublishNoteExpiresTagCloud(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	alice := mkUser(t, s, "alice")
	if _, err := s.saveNote(ctx, alice, "alice", "n1", "N1", "body", []string{"a", "b"}); err != nil {
		t.Fatalf("saveNote: %v", err)
	}
	if got := relatedCloud(s, "a"); !slices.Equal(got, []string{"b"}) {
		t.Fatalf("before publish = %v, want [b]", got)
	}
	draft, err := s.createDraft(ctx, alice)
	if err != nil {
		t.Fatalf("createDraft: %v", err)
	}
	if err := s.autosaveNote(ctx, draft, "alice", "n2", "N2", "#a #c", nil); err != nil {
		t.Fatalf("autosaveNote: %v", err)
	}
	w := httptest.NewRecorder()
	req := authedRequest(s, alice, http.MethodPost, "/api/notes/"+draft.String()+"/publish", "")
	req.SetPathValue("id", draft.String())
	s.PublishNote(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PublishNote: status %d body=%s", w.Code, w.Body)
	}
	if got := relatedCloud(s, "a"); !slices.Equal(got, []string{"b", "c"}) {
		t.Errorf("after publish = %v, want [b c]", got)
	}
}

// Any string can be a tag, so the cache must stay bounded: expired entries go
// first, and a full cache of live entries still evicts rather than grows.
func TestRelatedTagCacheBounded(t *testing.T) {
	var c relatedTagCache
	for i := range relatedTagCacheMax + 50 {
		c.put(strconv.Itoa(i), nil)
	}
	if n := len(c.m); n > relatedTagCacheMax {
		t.Fatalf("cache holds %d entries, max %d", n, relatedTagCacheMax)
	}
	if _, ok := c.get(strconv.Itoa(relatedTagCacheMax + 49)); !ok {
		t.Error("newest entry missing")
	}

	// Expire everything but one key; the next put sweeps the rest.
	c.mu.Lock()
	for k, e := range c.m {
		if k != "keep" {
			e.until = time.Now().Add(-time.Second)
			c.m[k] = e
		}
	}
	c.m["keep"] = relatedTagEntry{until: time.Now().Add(time.Minute)}
	c.mu.Unlock()
	c.put("new", nil)
	if n := len(c.m); n != 2 {
		t.Errorf("after sweep %d entries, want 2 (keep + new)", n)
	}
	if _, ok := c.get("keep"); !ok {
		t.Error("live entry evicted while expired ones were available")
	}
}
