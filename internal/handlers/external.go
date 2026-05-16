package handlers

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"

	"commonplace/internal/external"
	"commonplace/internal/markdown"
)

// ExternalCrawler is the interface main.go injects for admin-triggered crawls.
type ExternalCrawler interface {
	Trigger(vaultID int64)
}

// ---------- admin: add / list ----------

type externalVaultRow struct {
	ID           int64
	SourceURL    string
	DisplayName  string
	Slug         string
	Status       string
	StatusBadge  string
	ErrorMessage string
	NoteCount    int
	AddedRel     string
	LastCrawlRel string
	BrowseURL    string
}

func (s *Server) GetAdminExternal(w http.ResponseWriter, r *http.Request) {
	if a := s.requireAdmin(w, r); a == nil {
		return
	}
	rows, err := s.externalStore().ListVaults(r.Context())
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	items := make([]externalVaultRow, 0, len(rows))
	for _, v := range rows {
		items = append(items, toAdminRow(v))
	}
	s.render(w, r, "admin_external", map[string]any{
		"Vaults": items,
		"Flash":  r.URL.Query().Get("flash"),
		"Err":    r.URL.Query().Get("err"),
	})
}

func (s *Server) PostAdminExternalAdd(w http.ResponseWriter, r *http.Request) {
	if a := s.requireAdmin(w, r); a == nil {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin/external?err="+url.QueryEscape("bad form"), http.StatusSeeOther)
		return
	}
	raw := r.PostFormValue("url")
	v, inserted, err := s.externalStore().AddVault(r.Context(), raw)
	if err != nil {
		http.Redirect(w, r, "/admin/external?err="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	if s.Crawler != nil {
		s.Crawler.Trigger(v.ID)
	}
	flash := "queued for crawl: " + v.SourceURL
	if !inserted {
		flash = "already registered, re-queued: " + v.SourceURL
	}
	http.Redirect(w, r, "/admin/external?flash="+url.QueryEscape(flash), http.StatusSeeOther)
}

func (s *Server) PostAdminExternalRecrawl(w http.ResponseWriter, r *http.Request) {
	if a := s.requireAdmin(w, r); a == nil {
		return
	}
	id, err := strconv.ParseInt(r.PostFormValue("vault_id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	if _, err := s.DB.ExecContext(r.Context(),
		`UPDATE external_vaults SET status='pending' WHERE id = ?`, id); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if s.Crawler != nil {
		s.Crawler.Trigger(id)
	}
	http.Redirect(w, r, "/admin/external?flash="+url.QueryEscape("re-queued"), http.StatusSeeOther)
}

func (s *Server) PostAdminExternalDelete(w http.ResponseWriter, r *http.Request) {
	if a := s.requireAdmin(w, r); a == nil {
		return
	}
	id, err := strconv.ParseInt(r.PostFormValue("vault_id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	if _, err := s.DB.ExecContext(r.Context(),
		`DELETE FROM external_vaults WHERE id = ?`, id); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	http.Redirect(w, r, "/admin/external?flash="+url.QueryEscape("deleted"), http.StatusSeeOther)
}

// ---------- public: vault index + note view ----------

func (s *Server) GetExternalIndex(w http.ResponseWriter, r *http.Request) {
	slug := strings.ToLower(r.PathValue("vault"))
	v, err := s.externalStore().VaultBySlug(r.Context(), slug)
	if errors.Is(err, sql.ErrNoRows) {
		s.renderError(w, r, http.StatusNotFound, "vault not found")
		return
	}
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	notes, err := s.listExternalNotesByVault(r.Context(), v.ID, v.Slug, 200)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.render(w, r, "external_vault", map[string]any{
		"Vault":    v,
		"AddedRel": relativeTime(v.AddedAt.Unix()),
		"CrawlRel": relativeTime(v.LastCrawledAt.Unix()),
		"Notes":    notes,
	})
}

func (s *Server) GetExternalNote(w http.ResponseWriter, r *http.Request) {
	slug := strings.ToLower(r.PathValue("vault"))
	notePath := r.PathValue("path")
	v, err := s.externalStore().VaultBySlug(r.Context(), slug)
	if errors.Is(err, sql.ErrNoRows) {
		s.renderError(w, r, http.StatusNotFound, "vault not found")
		return
	}
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	noteSlug := strings.ToLower(strings.TrimSuffix(notePath, "/"))

	row := s.DB.QueryRowContext(r.Context(), `
		SELECT id, path, slug, title, body_md, excerpt, original_url, updated_at
		FROM external_notes
		WHERE vault_id = ? AND slug = ?`, v.ID, noteSlug)
	var (
		id        int64
		filePath  string
		slug2     string
		title     string
		bodyMD    string
		excerpt   string
		original  string
		updatedAt int64
	)
	if err := row.Scan(&id, &filePath, &slug2, &title, &bodyMD, &excerpt, &original, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.renderError(w, r, http.StatusNotFound, "note not found")
			return
		}
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	html, err := markdown.RenderExternal(bodyMD, func(raw string) markdown.ExternalLink {
		label, urlStr, cls := resolveExternalLink(r.Context(), s.DB, v.ID, v.Slug, raw)
		return markdown.ExternalLink{URL: urlStr, Class: cls, Label: label}
	})
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	s.render(w, r, "external_note", map[string]any{
		"Vault":       v,
		"NoteID":      id,
		"Title":       title,
		"BodyHTML":    html,
		"OriginalURL": original,
		"UpdatedRel":  relativeTime(updatedAt),
	})
}

func (s *Server) GetExternalNoteRaw(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	var (
		body string
		slug string
	)
	err = s.DB.QueryRowContext(r.Context(),
		`SELECT body_md, slug FROM external_notes WHERE id = ?`, id).Scan(&body, &slug)
	if errors.Is(err, sql.ErrNoRows) {
		s.renderError(w, r, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	fileName := path.Base(slug) + ".md"
	w.Header().Set("Content-Disposition", `attachment; filename="`+fileName+`"`)
	w.Write([]byte(body))
}

// ---------- helpers ----------

func (s *Server) externalStore() *external.Store {
	if s.extStore == nil {
		s.extStore = &external.Store{DB: s.DB}
	}
	return s.extStore
}

func toAdminRow(v external.Vault) externalVaultRow {
	row := externalVaultRow{
		ID:           v.ID,
		SourceURL:    v.SourceURL,
		DisplayName:  v.DisplayName,
		Slug:         v.Slug,
		Status:       v.Status,
		ErrorMessage: v.ErrorMessage,
		NoteCount:    v.NoteCount,
		AddedRel:     relativeTime(v.AddedAt.Unix()),
		BrowseURL:    "/x/" + v.Slug,
	}
	if !v.LastCrawledAt.IsZero() {
		row.LastCrawlRel = relativeTime(v.LastCrawledAt.Unix())
	}
	switch v.Status {
	case "pending":
		row.StatusBadge = "等待中"
	case "crawling":
		row.StatusBadge = "爬取中"
	case "active":
		row.StatusBadge = "已收錄"
	case "error":
		row.StatusBadge = "錯誤"
	default:
		row.StatusBadge = v.Status
	}
	return row
}

type externalNoteRow struct {
	URL        string
	Title      string
	Excerpt    string
	UpdatedRel string
}

func (s *Server) listExternalNotesByVault(ctx context.Context, vaultID int64, vaultSlug string, limit int) ([]externalNoteRow, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT slug, title, excerpt, updated_at
		FROM external_notes
		WHERE vault_id = ?
		ORDER BY updated_at DESC
		LIMIT ?`, vaultID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []externalNoteRow
	for rows.Next() {
		var (
			slug, title, excerpt string
			updated              int64
		)
		if err := rows.Scan(&slug, &title, &excerpt, &updated); err != nil {
			return nil, err
		}
		out = append(out, externalNoteRow{
			URL:        "/x/" + vaultSlug + "/" + slug,
			Title:      title,
			Excerpt:    excerpt,
			UpdatedRel: relativeTime(updated),
		})
	}
	return out, rows.Err()
}

// ---------- wiki-link resolution ----------

func resolveExternalLink(ctx context.Context, db *sql.DB, vaultID int64, vaultSlug, raw string) (label, urlStr, cls string) {
	token := strings.TrimSpace(raw)
	label = token
	if i := strings.Index(token, "|"); i >= 0 {
		label = strings.TrimSpace(token[i+1:])
		token = strings.TrimSpace(token[:i])
	}
	if i := strings.Index(token, "#"); i >= 0 {
		token = strings.TrimSpace(token[:i])
	}
	if token == "" {
		return label, "#", "wiki wiki-unresolved"
	}
	if strings.HasPrefix(token, "@") {
		rest := strings.TrimPrefix(token, "@")
		parts := strings.SplitN(rest, "/", 2)
		handle := strings.ToLower(strings.TrimSpace(parts[0]))
		slug := ""
		if len(parts) == 2 {
			slug = strings.TrimSpace(parts[1])
		}
		if (&external.Store{DB: db}).UserExists(ctx, handle) {
			u := "/" + handle
			if slug != "" {
				u += "/" + slug
			}
			return label, u, "wiki wiki-cross-resolved"
		}
		var otherID int64
		err := db.QueryRowContext(ctx,
			`SELECT id FROM external_vaults WHERE slug = ?`, handle).Scan(&otherID)
		if err == nil {
			u := "/x/" + handle
			if slug != "" {
				ns := strings.ToLower(strings.ReplaceAll(slug, " ", "-"))
				u += "/" + ns
			}
			return label, u, "wiki wiki-cross-resolved"
		}
		return label, "#", "wiki wiki-cross-unresolved"
	}
	want := strings.ToLower(token)
	var noteSlug string
	err := db.QueryRowContext(ctx, `
		SELECT slug FROM external_notes
		 WHERE vault_id = ?
		   AND (LOWER(title) = ? OR LOWER(REPLACE(REPLACE(path,'.md',''),'/','-')) = ?)
		 LIMIT 1`,
		vaultID, want, strings.ReplaceAll(want, " ", "-")).Scan(&noteSlug)
	if err == nil {
		return label, "/x/" + vaultSlug + "/" + noteSlug, "wiki wiki-resolved"
	}
	return label, "#", "wiki wiki-unresolved"
}


