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
