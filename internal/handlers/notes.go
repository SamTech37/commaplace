package handlers

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"commonplace/internal/markdown"
)

// ---------- write ----------

func (s *Server) GetWrite(w http.ResponseWriter, r *http.Request) {
	if u := s.requireUser(w, r); u == nil {
		return
	}
	s.render(w, r, "write", map[string]any{
		"Form": map[string]string{"Title": "", "FolderPath": "", "BodyMD": "", "Tags": ""},
	})
}

func (s *Server) PostWrite(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderError(w, r, http.StatusBadRequest, "bad form")
		return
	}
	title := strings.TrimSpace(r.PostFormValue("title"))
	folder := normalizeFolder(r.PostFormValue("folder_path"))
	body := r.PostFormValue("body_md")

	form := map[string]string{
		"Title": title, "FolderPath": folder, "BodyMD": body,
		"Tags": r.PostFormValue("tags"),
	}
	if title == "" {
		s.render(w, r, "write", map[string]any{
			"Form":  form,
			"Error": "Title is required.",
		})
		return
	}
	slug := kebabSlug(title)
	if slug == "" {
		s.render(w, r, "write", map[string]any{
			"Form":  form,
			"Error": "Title must contain at least one ASCII letter or digit (it's used as the URL slug).",
		})
		return
	}

	tagsInput := r.PostFormValue("tags")
	if inline := markdown.ExtractInlineTags(body); len(inline) > 0 {
		tagsInput += "," + strings.Join(inline, ",")
	}
	tags := parseTags(tagsInput)
	if _, err := s.saveNote(r.Context(), u.ID, u.Handle, folder, slug, title, body, tags); err != nil {
		msg := err.Error()
		if isUniqueViolation(err) {
			msg = "A note with this title already exists in that folder. Pick a different title."
		}
		s.render(w, r, "write", map[string]any{
			"Form":  form,
			"Error": msg,
		})
		return
	}
	http.Redirect(w, r, noteURL(u.Handle, folder, slug), http.StatusSeeOther)
}

// PostPreview is the HTMX target for live markdown preview while writing.
// It returns just the rendered HTML fragment (no layout).
func (s *Server) PostPreview(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	body := r.PostFormValue("body_md")
	links := markdown.Extract(body)
	resolver := s.buildResolver(r.Context(), u.Handle, links)
	html, err := markdown.Render(body, u.Handle, resolver)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
}

// ---------- note view ----------

type backlink struct {
	Title        string
	AuthorHandle string
	FolderPath   string
	URL          string
	UpdatedRel   string
	SameVault    bool
}

type noteView struct {
	ID         int64
	Title      string
	BodyMD     string
	UpdatedAt  int64
	FolderPath string
	Slug       string
	AuthorID   int64
	HiddenAt   sql.NullInt64
}

func (s *Server) GetNote(w http.ResponseWriter, r *http.Request) {
	handle := r.PathValue("user")
	path := r.PathValue("path")
	folder, slug := splitNotePath(path)
	if slug == "" {
		s.renderError(w, r, http.StatusNotFound, "note not found")
		return
	}

	var n noteView
	err := s.DB.QueryRowContext(r.Context(), `
		SELECT n.id, n.title, n.body_md, n.updated_at, n.folder_path, n.slug,
		       n.author_id, n.hidden_at
		FROM notes n
		JOIN users u ON u.id = n.author_id
		WHERE u.handle = ? AND n.folder_path = ? AND n.slug = ?`,
		handle, folder, slug,
	).Scan(&n.ID, &n.Title, &n.BodyMD, &n.UpdatedAt, &n.FolderPath, &n.Slug,
		&n.AuthorID, &n.HiddenAt)
	if errors.Is(err, sql.ErrNoRows) {
		s.renderError(w, r, http.StatusNotFound, "note not found")
		return
	}
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	// Hidden notes 404 unless the viewer is the author or the admin.
	if n.HiddenAt.Valid {
		viewer, _ := s.Auth.CurrentUser(r)
		isAuthor := viewer != nil && viewer.ID == n.AuthorID
		if !isAuthor && !s.IsAdmin(viewer) {
			s.renderError(w, r, http.StatusNotFound, "note not found")
			return
		}
	}

	links := markdown.Extract(n.BodyMD)
	resolver := s.buildResolver(r.Context(), handle, links)
	bodyHTML, err := markdown.Render(n.BodyMD, handle, resolver)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	tags, _ := loadTagsForNote(r.Context(), s.DB, n.ID)
	sameVaultBL, crossVaultBL, _ := s.loadBacklinksSplit(r.Context(), n.ID, handle)

	// "From" banner: ?from=N points back to the note that brought us here.
	var fromNote *fromBanner
	if fromIDStr := r.URL.Query().Get("from"); fromIDStr != "" {
		if fromID, err := strconv.ParseInt(fromIDStr, 10, 64); err == nil && fromID > 0 && fromID != n.ID {
			fromNote, _ = loadFromBanner(r.Context(), s.DB, fromID)
		}
	}

	viewer, _ := s.Auth.CurrentUser(r)
	var viewerID int64
	if viewer != nil {
		viewerID = viewer.ID
	}
	likeN, _ := likeCount(r.Context(), s.DB, n.ID)
	liked, _ := userHasLiked(r.Context(), s.DB, viewerID, n.ID)

	var crumbs []string
	if folder != "" {
		crumbs = strings.Split(folder, "/")
	}

	s.render(w, r, "note", map[string]any{
		"Note":             n,
		"AuthorHandle":     handle,
		"Crumbs":           crumbs,
		"BodyHTML":         bodyHTML,
		"Tags":             tags,
		"BacklinksSame":    sameVaultBL,
		"BacklinksCross":   crossVaultBL,
		"From":             fromNote,
		"UpdatedRel":       relativeTime(n.UpdatedAt),
		"LikeCount":        likeN,
		"Liked":            liked,
		"ViewerLoggedIn":   viewer != nil,
		"IsAuthor":         viewer != nil && viewer.ID == n.AuthorID,
		"IsHidden":         n.HiddenAt.Valid,
	})
}

type fromBanner struct {
	ID    int64
	Title string
	URL   string
}

func loadFromBanner(ctx context.Context, db *sql.DB, fromID int64) (*fromBanner, error) {
	var (
		title, folder, slug, handle string
	)
	err := db.QueryRowContext(ctx, `
		SELECT n.title, n.folder_path, n.slug, u.handle
		FROM notes n JOIN users u ON u.id = n.author_id
		WHERE n.id = ?`, fromID,
	).Scan(&title, &folder, &slug, &handle)
	if err != nil {
		return nil, err
	}
	return &fromBanner{
		ID:    fromID,
		Title: title,
		URL:   noteURL(handle, folder, slug),
	}, nil
}

func loadTagsForNote(ctx context.Context, db *sql.DB, noteID int64) ([]string, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT tag FROM note_tags WHERE note_id = ? ORDER BY tag`, noteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// loadBacklinksSplit loads backlinks for a note and partitions them into
// same-vault (source author == this note's author) and cross-vault.
// Each backlink's URL carries ?from=<this note's id> so the destination
// can render a "back to" banner.
func (s *Server) loadBacklinksSplit(ctx context.Context, noteID int64, vaultHandle string) (sameVault, crossVault []backlink, err error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT n.title, n.folder_path, n.slug, u.handle, n.updated_at
		FROM links l
		JOIN notes n ON n.id = l.source_note_id
		JOIN users u ON u.id = n.author_id
		WHERE l.resolved_target_id = ?
		ORDER BY n.updated_at DESC`, noteID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	from := "?from=" + strconv.FormatInt(noteID, 10)
	for rows.Next() {
		var (
			title, folderPath, slug, handle string
			updated                          int64
		)
		if err := rows.Scan(&title, &folderPath, &slug, &handle, &updated); err != nil {
			return nil, nil, err
		}
		bl := backlink{
			Title:        title,
			AuthorHandle: handle,
			FolderPath:   folderPath,
			URL:          noteURL(handle, folderPath, slug) + from,
			UpdatedRel:   relativeTime(updated),
			SameVault:    handle == vaultHandle,
		}
		if bl.SameVault {
			sameVault = append(sameVault, bl)
		} else {
			crossVault = append(crossVault, bl)
		}
	}
	return sameVault, crossVault, rows.Err()
}

// ---------- save + link recomputation ----------

// saveNote inserts a new note row and (re)computes its outgoing links plus
// any previously-dangling links that now resolve to this note. Tags are
// inserted in the same transaction.
func (s *Server) saveNote(ctx context.Context, authorID int64, authorHandle, folder, slug, title, body string, tags []string) (int64, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	now := nowUnix()
	res, err := tx.ExecContext(ctx, `
		INSERT INTO notes(author_id, folder_path, slug, title, body_md, created_at, updated_at)
		VALUES(?, ?, ?, ?, ?, ?, ?)`,
		authorID, folder, slug, title, body, now, now,
	)
	if err != nil {
		return 0, err
	}
	noteID, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}

	for _, t := range tags {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO note_tags(note_id, tag, created_at) VALUES(?, ?, ?)`,
			noteID, t, now,
		); err != nil {
			return 0, err
		}
	}

	if err := recomputeLinks(ctx, tx, noteID, authorHandle, body); err != nil {
		return 0, err
	}

	// Re-resolve any previously-unresolved links pointing at *this* new note.
	if _, err := tx.ExecContext(ctx, `
		UPDATE links SET resolved_target_id = ?
		WHERE resolved_target_id IS NULL
		  AND target_user_handle = ?
		  AND target_folder_path = ?
		  AND target_slug = ?`,
		noteID, authorHandle, folder, slug,
	); err != nil {
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return noteID, nil
}

// RecomputeLinks deletes existing links rows for sourceID and inserts one
// row per unique wiki link in body, with resolved_target_id set when the
// target exists. Exported so the seed package can call it; internal
// callers use the unexported alias `recomputeLinks`.
func RecomputeLinks(ctx context.Context, tx *sql.Tx, sourceID int64, sourceAuthorHandle, body string) error {
	return recomputeLinks(ctx, tx, sourceID, sourceAuthorHandle, body)
}

func recomputeLinks(ctx context.Context, tx *sql.Tx, sourceID int64, sourceAuthorHandle, body string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM links WHERE source_note_id = ?`, sourceID); err != nil {
		return err
	}
	for _, l := range markdown.Extract(body) {
		targetHandle := l.User
		if targetHandle == "" {
			targetHandle = sourceAuthorHandle
		}
		var resolved sql.NullInt64
		var found int64
		err := tx.QueryRowContext(ctx, `
			SELECT n.id FROM notes n
			JOIN users u ON u.id = n.author_id
			WHERE u.handle = ? AND n.folder_path = ? AND n.slug = ?`,
			targetHandle, l.Folder, l.Slug,
		).Scan(&found)
		if err == nil {
			resolved = sql.NullInt64{Int64: found, Valid: true}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO links(source_note_id, target_user_handle, target_folder_path, target_slug, resolved_target_id)
			VALUES(?, ?, ?, ?, ?)`,
			sourceID, targetHandle, l.Folder, l.Slug, resolved,
		); err != nil {
			return err
		}
	}
	return nil
}

// ---------- helpers ----------

func splitNotePath(path string) (folder, slug string) {
	if path == "" {
		return "", ""
	}
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[:i], path[i+1:]
	}
	return "", path
}

func noteURL(handle, folder, slug string) string {
	if folder == "" {
		return "/" + handle + "/" + slug
	}
	return "/" + handle + "/" + folder + "/" + slug
}

func nowUnix() int64 { return timeNow().Unix() }

// timeNow is a var so tests can stub. Production code calls real time.Now.
var timeNow = func() time.Time { return time.Now() }

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "constraint failed: UNIQUE")
}

// buildResolver returns a markdown.Resolver that knows which of `links` map
// to existing notes. vaultHandle is the same-vault default for [[slug]]
// links (no @user prefix). One DB lookup per unique link; fine for v0.
func (s *Server) buildResolver(ctx context.Context, vaultHandle string, links []markdown.WikiLink) markdown.Resolver {
	if len(links) == 0 {
		return func(markdown.WikiLink) bool { return false }
	}
	keyFor := func(l markdown.WikiLink) string {
		h := l.User
		if h == "" {
			h = vaultHandle
		}
		return h + "\x00" + l.Folder + "\x00" + l.Slug
	}
	resolved := map[string]bool{}
	for _, l := range links {
		h := l.User
		if h == "" {
			h = vaultHandle
		}
		var id int64
		err := s.DB.QueryRowContext(ctx, `
			SELECT n.id FROM notes n
			JOIN users u ON u.id = n.author_id
			WHERE u.handle = ? AND n.folder_path = ? AND n.slug = ?`,
			h, l.Folder, l.Slug,
		).Scan(&id)
		if err == nil {
			resolved[keyFor(l)] = true
		}
	}
	return func(l markdown.WikiLink) bool { return resolved[keyFor(l)] }
}
