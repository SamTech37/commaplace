package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	"commonplace/internal/auth"
	"commonplace/internal/markdown"
	"github.com/google/uuid"
)

func vaultRequest(t *testing.T, files map[string]string, batch uuid.UUID) *http.Request {
	t.Helper()
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	for p, body := range files {
		part, err := mw.CreateFormFile(p, "note.md")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = part.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/import/vault?batch="+batch.String(), &b)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	return r
}

func TestVaultReadLimitsAndFrontmatter(t *testing.T) {
	for _, p := range []string{"../a.md", "/a.md", ".obsidian/a.md", "a/.trash/n.md", "a.png", "a/../b.md"} {
		t.Run(p, func(t *testing.T) {
			_, cleanup, err := readVault(vaultRequest(t, map[string]string{p: "hello"}, uuid.New()))
			defer cleanup()
			if err == nil {
				t.Fatal("accepted invalid path")
			}
		})
	}
	for _, body := range []string{strings.Repeat("a", maxVaultFileBytes+1), "\xff", "a\x00b", "---\ntitle: [broken\n---\nbody"} {
		_, cleanup, err := readVault(vaultRequest(t, map[string]string{"note.md": body}, uuid.New()))
		cleanup()
		if err == nil {
			t.Fatal("accepted bad body")
		}
	}
	long := strings.Repeat("x", 70000)
	raw := "---\ntitle: '測試：標題'\ntags:\n  - 閱讀\n  - beta\ndate: 2023-01-01\ncustom: retained\n---\n" + long + "\n![[photo.png]]\n![[Other]]"
	raw = strings.ReplaceAll(raw, "\n", "\r\n")
	notes, cleanup, err := readVault(vaultRequest(t, map[string]string{"note.MD": raw}, uuid.New()))
	defer cleanup()
	if err != nil {
		t.Fatal(err)
	}
	n := notes[0]
	b, err := os.ReadFile(n.temp)
	if err != nil {
		t.Fatal(err)
	}
	if n.Title != "測試：標題" || len(n.tags) != 2 || n.Media != 1 || !strings.Contains(string(b), long) || !strings.Contains(string(b), "custom: retained") || !strings.Contains(string(b), "![[Other]]") {
		t.Fatalf("metadata or contents lost: %+v", n)
	}
	h, err := markdown.Render(string(b), "alice", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(h), "custom: retained") {
		t.Fatal("frontmatter leaked into rendered body")
	}
	variant, excerpt, _, _, _ := analyzeCardBody(string(b))
	if variant != "text" || !strings.HasPrefix(excerpt, "xxx") {
		t.Fatalf("frontmatter polluted card: %s %s", variant, excerpt)
	}
}

func TestVaultMoreThanMultipartDefaultLimit(t *testing.T) {
	files := map[string]string{}
	for i := 0; i < 1001; i++ {
		files[fmt.Sprintf("n%d.md", i)] = "text"
	}
	notes, cleanup, err := readVault(vaultRequest(t, files, uuid.New()))
	defer cleanup()
	if err != nil || len(notes) != 1001 {
		t.Fatalf("notes=%d err=%v", len(notes), err)
	}
}

func TestVaultRewritePathsAndSyntax(t *testing.T) {
	a := &vaultNote{Path: "a/同名.md", Slug: "same", ID: uuid.New()}
	b := &vaultNote{Path: "b/同名.md", Slug: "same-2", ID: uuid.New()}
	source := &vaultNote{Path: "home.md", ID: uuid.New()}
	body := "[[a/同名#標題|別名]] ![[b/同名.md]] [[同名]] [閱讀](a/%E5%90%8C%E5%90%8D.md#標題)\n`[[a/同名]]`\n```md\n[[a/同名]]\n```\n%% [[a/同名]] %%\n[[@other/note]]"
	got := rewriteVaultLinks(body, source, newVaultIndex([]*vaultNote{a, b, source}))
	for _, want := range []string{"[[same#標題|別名]]", "![[same-2|b/同名.md]]", "[[same#標題|閱讀]]", "`[[a/同名]]`", "```md\n[[a/同名]]\n```", "%% [[a/同名]] %%", "[[@other/note]]"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %s", want, got)
		}
	}
	if len(source.Warnings) != 1 || !strings.Contains(got, "[[missing-") {
		t.Fatal("ambiguous filename silently resolved")
	}
}

func TestVault800PublishesResolvesAndRetries(t *testing.T) {
	s := newTestServer(t)
	id := mkUser(t, s, "alice")
	batch := uuid.New()
	files := map[string]string{}
	for i := 0; i < 800; i++ {
		files[fmt.Sprintf("folder/n%03d.md", i)] = fmt.Sprintf("---\ntitle: Note %d\ndate: %d-01-01\ntags: [beta]\n---\n[[n%03d]]\n> [!note] Callout\n- [x] Task\n![[photo.png]]", i, 2023+i%3, (i+1)%800)
	}
	request := func() *httptest.ResponseRecorder {
		r := vaultRequest(t, files, batch)
		r.AddCookie(sessionCookie(t, s, id))
		r.Header.Set("Accept-Encoding", "gzip")
		w := httptest.NewRecorder()
		gzipMiddleware(http.HandlerFunc(s.PostVaultImport)).ServeHTTP(w, r)
		return w
	}
	w := request()
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	var report vaultReport
	lines := strings.Split(strings.TrimSpace(w.Body.String()), "\n")
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &report); err != nil {
		t.Fatal(err)
	}
	if report.Type != "complete" || report.Total != 800 || report.Media != 800 || report.Unresolved != 0 {
		t.Fatalf("bad report: %s", lines[len(lines)-1])
	}
	if len(lines) < 40 || !w.Flushed {
		t.Fatal("progress not streamed")
	}
	var published, links int
	if err := s.DB.QueryRow(`SELECT count(*) FROM notes WHERE author_id=$1 AND published_at IS NOT NULL`, id).Scan(&published); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.QueryRow(`SELECT count(*) FROM links WHERE resolved_target_id IS NOT NULL`).Scan(&links); err != nil {
		t.Fatal(err)
	}
	if published != 800 || links != 800 {
		t.Fatalf("published=%d links=%d", published, links)
	}
	seen := map[uuid.UUID]bool{}
	cursor := feedCursor{}
	for page := 0; page < 42; page++ {
		cards, next, err := loadRecentNotes(httptest.NewRequest("GET", "/alice", nil), s.DB, id, "alice", id, "", cursor)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range cards {
			if seen[c.NoteID] {
				t.Fatal("duplicate profile card")
			}
			seen[c.NoteID] = true
		}
		if !next.set() {
			break
		}
		cursor = next
	}
	if len(seen) != 800 {
		t.Fatalf("only %d/800 reachable", len(seen))
	}
	w = request()
	if !strings.Contains(w.Body.String(), `"type":"complete"`) {
		t.Fatal(w.Body.String())
	}
	if err := s.DB.QueryRow(`SELECT count(*) FROM notes WHERE author_id=$1`, id).Scan(&published); err != nil {
		t.Fatal(err)
	}
	if published != 800 {
		t.Fatalf("retry duplicated notes: %d", published)
	}
	t.Logf("800 notes, 800 resolved links, 40 profile pages; import %d ms", report.ElapsedMS)
}

func TestVaultRollbackAndCollision(t *testing.T) {
	s := newTestServer(t)
	id := mkUser(t, s, "alice")
	mkPublishedAt(t, s, id, "target", "Existing", 1)
	files := map[string]string{"target.md": "# New\nbody", "source.md": "[[target]]"}
	for i := 0; i < 20; i++ {
		files[fmt.Sprintf("dummy%d.md", i)] = "body"
	}
	notes, cleanup, err := readVault(vaultRequest(t, files, uuid.New()))
	defer cleanup()
	if err != nil {
		t.Fatal(err)
	}
	u := &auth.User{ID: id, Handle: "alice"}
	ctx, cancel := context.WithCancel(context.Background())
	_, err = s.importVault(ctx, u, uuid.New(), notes, func(done int) error {
		if done >= 20 {
			cancel()
		}
		return ctx.Err()
	})
	if err == nil {
		t.Fatal("canceled import succeeded")
	}
	var count int
	if err = s.DB.QueryRow(`SELECT count(*) FROM notes WHERE author_id=$1`, id).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("rollback left %d notes", count)
	}
	report, err := s.importVault(context.Background(), u, uuid.New(), notes, func(int) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	var target uuid.UUID
	for _, n := range report.Notes {
		if n.Path == "target.md" {
			if n.Slug != "target-2" {
				t.Fatal(n.Slug)
			}
			target = n.ID
		}
	}
	var linked uuid.UUID
	if err = s.DB.QueryRow(`SELECT resolved_target_id FROM links`).Scan(&linked); err != nil {
		t.Fatal(err)
	}
	if linked != target {
		t.Fatal("import linked to old note instead of new file")
	}
}

func TestVaultAuthentication(t *testing.T) {
	s := newTestServer(t)
	w := httptest.NewRecorder()
	s.PostVaultImport(w, vaultRequest(t, map[string]string{"note.md": "body"}, uuid.New()))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status %d", w.Code)
	}
}

func TestAllListsPaginateSameSecond(t *testing.T) {
	s := newTestServer(t)
	alice := mkUser(t, s, "alice")
	bob := mkUser(t, s, "bob")
	if _, err := s.DB.Exec(`INSERT INTO follows(follower_id,followed_id,created_at) VALUES($1,$2,1)`, bob, alice); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 65; i++ {
		id := mkPublishedAt(t, s, alice, fmt.Sprintf("n%d", i), fmt.Sprintf("N%d", i), 5000)
		if _, err := s.DB.Exec(`INSERT INTO note_tags(note_id,tag,created_at) VALUES($1,'beta',5000)`, id); err != nil {
			t.Fatal(err)
		}
		if _, err := s.DB.Exec(`INSERT INTO saves(user_id,note_id,created_at) VALUES($1,$2,5000)`, bob, id); err != nil {
			t.Fatal(err)
		}
	}
	cardRE := regexp.MustCompile(`href="(/alice/n[0-9]+)"`)
	olderRE := regexp.MustCompile(`hx-get="([^"]+)"`)
	for _, base := range []string{"/alice", "/tag/beta", "/me/saved", "/feed?tab=following"} {
		t.Run(base, func(t *testing.T) {
			next := base
			seen := map[string]bool{}
			for i := 0; i < 15 && next != ""; i++ {
				r := httptest.NewRequest("GET", next, nil)
				r.AddCookie(sessionCookie(t, s, bob))
				r.Header.Set("HX-Request", "true")
				w := httptest.NewRecorder()
				s.Routes().ServeHTTP(w, r)
				if w.Code != 200 {
					t.Fatalf("%s: %d %s", next, w.Code, w.Body.String())
				}
				page := map[string]bool{}
				for _, m := range cardRE.FindAllStringSubmatch(w.Body.String(), -1) {
					page[m[1]] = true
				}
				for link := range page {
					if seen[link] {
						t.Fatalf("duplicate %s", link)
					}
					seen[link] = true
				}
				next = ""
				if m := olderRE.FindStringSubmatch(w.Body.String()); m != nil {
					next = strings.ReplaceAll(m[1], "&amp;", "&")
				}
			}
			if len(seen) != 65 {
				t.Fatalf("%d of 65 notes reachable", len(seen))
			}
		})
	}
}
