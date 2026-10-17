package handlers

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"commonplace/internal/auth"
)

// sessionCookie fabricates a signed session cookie for id, as SetSession would.
func sessionCookie(t *testing.T, s *Server, id uuid.UUID) *http.Cookie {
	t.Helper()
	return &http.Cookie{Name: auth.SessionCookie, Value: s.Auth.Sign(id)}
}

func multipartUpload(t *testing.T, files map[string]string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for name, content := range files {
		fw, err := mw.CreateFormFile("files", name)
		if err != nil {
			t.Fatal(err)
		}
		fw.Write([]byte(content))
	}
	mw.Close()
	return &buf, mw.FormDataContentType()
}

func TestPostImportMultiFileRendersBatchEditor(t *testing.T) {
	s := newTestServer(t)
	aliceID := mkUser(t, s, "alice")

	body, ctype := multipartUpload(t, map[string]string{
		"one.md": "# First Note\n\nbody one",
		"two.md": "---\ntitle: Second Note\ntags: [x, y]\n---\nbody two",
	})
	req := httptest.NewRequest("POST", "/import", body)
	req.Header.Set("Content-Type", ctype)
	req.AddCookie(sessionCookie(t, s, aliceID))
	rec := httptest.NewRecorder()
	s.PostImport(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d\n%s", rec.Code, rec.Body.String())
	}
	html := rec.Body.String()
	for _, want := range []string{"First Note", "Second Note", "batch-item"} {
		if !strings.Contains(html, want) {
			t.Errorf("batch page missing %q", want)
		}
	}
}

func TestPostImportSaveOneCreatesNote(t *testing.T) {
	s := newTestServer(t)
	aliceID := mkUser(t, s, "alice")

	form := url.Values{"title": {"Saved Note"}, "body_md": {"hello"}, "tags": {"a,b"}}
	req := httptest.NewRequest("POST", "/import/save-one", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(sessionCookie(t, s, aliceID))
	rec := httptest.NewRecorder()
	s.PostImportSaveOne(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d\n%s", rec.Code, rec.Body.String())
	}
	var count int
	if err := s.DB.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM notes WHERE author_id = $1 AND slug = 'saved-note'`, aliceID,
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("note count = %d, want 1", count)
	}
}

// importOne posts a single file to /import with title_from (omitted when
// empty) and returns where the import redirected.
func importOne(t *testing.T, s *Server, user uuid.UUID, name, content, titleFrom string) string {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("files", name)
	if err != nil {
		t.Fatal(err)
	}
	fw.Write([]byte(content))
	if titleFrom != "" {
		mw.WriteField("title_from", titleFrom)
	}
	mw.Close()
	req := httptest.NewRequest("POST", "/import", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(sessionCookie(t, s, user))
	rec := httptest.NewRecorder()
	s.PostImport(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("%s: status = %d\n%s", name, rec.Code, rec.Body.String())
	}
	return rec.Header().Get("Location")
}

func TestImportTitleFrom(t *testing.T) {
	s := newTestServer(t)
	alice := mkUser(t, s, "alice")

	// A fenced "# line" is a comment, not a heading.
	all := "---\ntitle: FM Title\n---\n```sh\n# not a heading\n```\n# Real Heading\n"
	noFM := "```sh\n# not a heading\n```\n# Real Heading\n"
	cases := []struct{ file, content, from, want string }{
		{"x1.md", all, "", "/alice/fm-title"}, // default: frontmatter, as before
		{"Guide Two.md", all, "filename", "/alice/guide-two"},
		{"x2.md", noFM, "heading", "/alice/real-heading"},
		// An empty choice falls back: no frontmatter → heading.
		{"x3.md", "# Fallback Heading\n", "frontmatter", "/alice/fallback-heading"},
		// No heading at all → frontmatter, then filename.
		{"Only Name.md", "plain body\n", "heading", "/alice/only-name"},
	}
	for _, c := range cases {
		if got := importOne(t, s, alice, c.file, c.content, c.from); got != c.want {
			t.Errorf("%s title_from=%q: landed on %s, want %s", c.file, c.from, got, c.want)
		}
	}
}
