package handlers

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"

	"commonplace/internal/markdown"
)

// MaxBatchFiles caps the number of .md files acceptable in one upload.
const MaxBatchFiles = 10

func (s *Server) GetImport(w http.ResponseWriter, r *http.Request) {
	if u := s.requireUser(w, r); u == nil {
		return
	}
	s.renderPage(w, r, pageTitle("匯入筆記"), "", nil, importPage(ImportPageProps{}))
}

// batchItem is one pending note in the batch-edit UI.
type batchItem struct {
	Idx   int
	Title string
	Body  string
	Tags  string
	Slug  string
}

func (s *Server) PostImport(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	if err := r.ParseMultipartForm(50 << 20); err != nil {
		s.renderError(w, r, http.StatusBadRequest, "upload too large or bad form")
		return
	}

	// Accept both "files" (multi) and "file" (legacy single).
	headers := r.MultipartForm.File["files"]
	if len(headers) == 0 {
		headers = r.MultipartForm.File["file"]
	}
	if len(headers) == 0 {
		s.renderPage(w, r, pageTitle("匯入筆記"), "", nil, importPage(ImportPageProps{Error: "No file selected."}))
		return
	}
	if len(headers) > MaxBatchFiles {
		s.renderPage(w, r, pageTitle("匯入筆記"), "", nil, importPage(ImportPageProps{
			Error: fmt.Sprintf("Too many files — upload at most %d at a time.", MaxBatchFiles),
		}))
		return
	}

	items := make([]batchItem, 0, len(headers))
	for i, hdr := range headers {
		title, body, tagsInput, err := parseUploadedNote(hdr)
		if err != nil {
			s.renderPage(w, r, pageTitle("匯入筆記"), "", nil, importPage(ImportPageProps{
				Error: fmt.Sprintf("%s: %s", hdr.Filename, err.Error()),
			}))
			return
		}
		items = append(items, batchItem{
			Idx:   i,
			Title: title,
			Body:  body,
			Tags:  tagsInput,
			Slug:  kebabSlug(title),
		})
	}

	// Single file → preserve old behavior: save immediately, redirect to note.
	if len(items) == 1 {
		it := items[0]
		if it.Slug == "" {
			s.renderPage(w, r, pageTitle("匯入筆記"), "", nil, importPage(ImportPageProps{Error: "Title must contain at least one letter or digit."}))
			return
		}
		tags := parseTags(it.Tags)
		if _, err := s.saveNote(r.Context(), u.ID, u.Handle, it.Slug, it.Title, it.Body, tags); err != nil {
			msg := err.Error()
			if isUniqueViolation(err) {
				msg = "A note with this title already exists."
			}
			s.renderPage(w, r, pageTitle("匯入筆記"), "", nil, importPage(ImportPageProps{Error: msg}))
			return
		}
		http.Redirect(w, r, noteURL(u.Handle, it.Slug), http.StatusSeeOther)
		return
	}

	// Multi-file → render batch editor.
	s.renderPage(w, r, pageTitle("批次匯入"), "", nil, importBatchPage(items))
}

// PostImportSaveOne saves a single pending note from the batch editor.
// On success returns the "saved" fragment that HTMX swaps in place of the
// section's form. On failure returns an inline error fragment.
func (s *Server) PostImportSaveOne(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	if err := r.ParseForm(); err != nil {
		writeBatchError(w, "bad form")
		return
	}
	title := strings.TrimSpace(r.PostFormValue("title"))
	body := r.PostFormValue("body_md")
	tagsInput := r.PostFormValue("tags")

	if title == "" {
		writeBatchError(w, "標題不可空白")
		return
	}
	slug := kebabSlug(title)
	if slug == "" {
		writeBatchError(w, "標題至少要含一個字元")
		return
	}
	tags := parseTags(tagsInput)

	if _, err := s.saveNote(r.Context(), u.ID, u.Handle, slug, title, body, tags); err != nil {
		if isUniqueViolation(err) {
			writeBatchError(w, "已有同名筆記")
			return
		}
		writeBatchError(w, err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<section class="batch-item batch-item-saved" data-status="saved">
  <div class="batch-saved-row">
    <span class="batch-saved-mark">✓</span>
    <span class="batch-saved-title">%s</span>
    <a class="batch-saved-link" href="%s" target="_blank">開啟筆記 →</a>
  </div>
</section>`, htmlEscape(title), htmlEscape(noteURL(u.Handle, slug)))
}

func writeBatchError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	fmt.Fprintf(w, `<p class="batch-item-error">%s</p>`, htmlEscape(msg))
}

func htmlEscape(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&#39;",
	)
	return r.Replace(s)
}

func parseUploadedNote(hdr *multipart.FileHeader) (title, body, tagsInput string, err error) {
	f, err := hdr.Open()
	if err != nil {
		return "", "", "", fmt.Errorf("could not open: %w", err)
	}
	defer f.Close()
	raw, err := io.ReadAll(f)
	if err != nil {
		return "", "", "", fmt.Errorf("could not read: %w", err)
	}

	fm, body := parseFrontmatter(raw)

	title = fm["title"]
	if title == "" {
		title = extractH1(body)
	}
	if title == "" {
		title = strings.TrimSuffix(hdr.Filename, filepath.Ext(hdr.Filename))
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return "", "", "", fmt.Errorf("could not determine a title; add a # heading or title: frontmatter")
	}

	if t := fm["tags"]; t != "" {
		tagsInput = t
	}
	if inline := markdown.ExtractInlineTags(body); len(inline) > 0 {
		if tagsInput != "" {
			tagsInput += ","
		}
		tagsInput += strings.Join(inline, ",")
	}
	return title, body, tagsInput, nil
}

// parseFrontmatter splits YAML frontmatter (--- block) from the body.
// Returns a map of simple key: value pairs and the body without frontmatter.
func parseFrontmatter(raw []byte) (map[string]string, string) {
	fm := map[string]string{}
	s := bufio.NewScanner(bytes.NewReader(raw))

	// Must start with ---
	if !s.Scan() || strings.TrimSpace(s.Text()) != "---" {
		return fm, string(raw)
	}

	var fmLines []string
	for s.Scan() {
		line := s.Text()
		if strings.TrimSpace(line) == "---" {
			break
		}
		fmLines = append(fmLines, line)
	}

	// Remaining lines are the body
	var bodyLines []string
	for s.Scan() {
		bodyLines = append(bodyLines, s.Text())
	}
	body := strings.Join(bodyLines, "\n")

	for _, line := range fmLines {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		// Strip inline YAML list brackets: tags: [a, b, c] → "a, b, c"
		v = strings.Trim(v, "[]")
		fm[k] = v
	}
	return fm, body
}

// extractH1 returns the text of the first # heading in the markdown, or "".
func extractH1(body string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimPrefix(line, "# ")
		}
	}
	return ""
}
