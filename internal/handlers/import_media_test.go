package handlers

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"commonplace/internal/markdown"
)

// mdUpload builds a POST /import request carrying one .md file.
func mdUpload(t *testing.T, filename, content string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("files", filename)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := fw.Write([]byte(content)); err != nil {
		t.Fatalf("write: %v", err)
	}
	w.Close()
	r := httptest.NewRequest(http.MethodPost, "/import", &buf)
	r.Header.Set("Content-Type", w.FormDataContentType())
	return r
}

// A vault exported from Obsidian is full of attachments. Nothing here can host
// one, so an imported reference could only point at somebody else's server —
// and the first image in a body becomes a feed card thumbnail, hotlinked for
// every viewer of the feed. Importing must not be a way to plant one.
func TestImportStripsMedia(t *testing.T) {
	s := newTestServer(t)
	author := mkUser(t, s, "importer")

	const body = `# Trip notes

![](https://tracker.example/px.gif)

![[Pasted image 20240101.png]]

![[a-real-note]] stays, and so does [a link](https://example.com).

<video src="https://x/y.mp4"></video>
`
	r := mdUpload(t, "trip.md", body)
	r.AddCookie(sessionCookie(t, s, author))
	w := httptest.NewRecorder()
	s.PostImport(w, r)
	if w.Code != http.StatusOK && w.Code != http.StatusSeeOther {
		t.Fatalf("import: status %d body=%s", w.Code, w.Body)
	}

	var saved string
	if err := s.DB.QueryRow(
		`SELECT body_md FROM notes WHERE author_id = $1`, author,
	).Scan(&saved); err != nil {
		t.Fatalf("read back: %v", err)
	}

	for _, gone := range []string{"tracker.example", "Pasted image", "<video"} {
		if strings.Contains(saved, gone) {
			t.Errorf("media reference %q survived import:\n%s", gone, saved)
		}
	}
	for _, kept := range []string{"![[a-real-note]]", "[a link](https://example.com)"} {
		if !strings.Contains(saved, kept) {
			t.Errorf("%q should have been kept:\n%s", kept, saved)
		}
	}
	// The thumbnail path is the reason this matters most.
	if got := markdown.FirstImageURL(saved); got != "" {
		t.Errorf("an imported note still offers a feed thumbnail: %q", got)
	}
}
