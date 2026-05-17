package handlers

import (
	"bufio"
	"bytes"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"commonplace/internal/markdown"
)

func (s *Server) GetImport(w http.ResponseWriter, r *http.Request) {
	if u := s.requireUser(w, r); u == nil {
		return
	}
	s.render(w, r, "import", nil)
}

func (s *Server) PostImport(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		s.renderError(w, r, http.StatusBadRequest, "file too large or bad form")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		s.render(w, r, "import", map[string]any{"Error": "No file selected."})
		return
	}
	defer file.Close()

	raw, err := io.ReadAll(file)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "could not read file")
		return
	}

	fm, body := parseFrontmatter(raw)

	title := fm["title"]
	if title == "" {
		title = extractH1(body)
	}
	if title == "" {
		title = strings.TrimSuffix(header.Filename, filepath.Ext(header.Filename))
	}
	title = strings.TrimSpace(title)

	if title == "" {
		s.render(w, r, "import", map[string]any{"Error": "Could not determine a title. Add a # heading or title: in frontmatter."})
		return
	}

	slug := kebabSlug(title)
	if slug == "" {
		s.render(w, r, "import", map[string]any{"Error": "Title must contain at least one letter or digit."})
		return
	}

	var tagsInput string
	if t := fm["tags"]; t != "" {
		tagsInput = t
	}
	if inline := markdown.ExtractInlineTags(body); len(inline) > 0 {
		if tagsInput != "" {
			tagsInput += ","
		}
		tagsInput += strings.Join(inline, ",")
	}
	tags := parseTags(tagsInput)

	if _, err := s.saveNote(r.Context(), u.ID, u.Handle, slug, title, body, tags); err != nil {
		msg := err.Error()
		if isUniqueViolation(err) {
			msg = "A note with this title already exists."
		}
		s.render(w, r, "import", map[string]any{"Error": msg})
		return
	}

	http.Redirect(w, r, noteURL(u.Handle, slug), http.StatusSeeOther)
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
