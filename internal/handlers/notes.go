package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"commonplace/internal/markdown"
)

// ---------- write ----------

func (s *Server) GetWrite(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	_ = s.sweepOrphanDrafts(r.Context(), u.ID)

	// Reply prefill: title "Re: {original}", body embeds the original + separator.
	doc := ""
	if id := r.URL.Query().Get("reply-to"); id != "" {
		if replyID, err := uuid.Parse(id); err == nil {
			var title, slug, handle string
			if err := s.DB.QueryRowContext(r.Context(),
				`SELECT n.title, n.slug, u.handle FROM notes n JOIN users u ON u.id = n.author_id WHERE n.id = $1`,
				replyID).Scan(&title, &slug, &handle); err == nil {
				var embed string
				if handle == u.Handle {
					embed = "![[" + slug + "]]"
				} else {
					embed = "![[@" + handle + "/" + slug + "]]"
				}
				doc = "Re: " + title + "\n\n" + embed + "\n\n---\n\n"
			}
		}
	}

	draftID, err := s.createDraft(r.Context(), u.ID)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.renderPage(w, r, pageTitle("Write"), "page-editor", nil, writePage(WriteProps{
		NoteID:   draftID.String(),
		Document: doc,
	}))
}

func (s *Server) PostWrite(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	if err := r.ParseMultipartForm(5 << 20); err != nil {
		s.renderError(w, r, http.StatusBadRequest, "bad form")
		return
	}
	title := strings.TrimSpace(r.PostFormValue("title"))
	body := r.PostFormValue("body_md")

	// Note: title/body/tags and the validation error message aren't
	// rendered anywhere in write.templ (the editor is a client-side
	// textarea, not server-templated form fields) — same as the old
	// write.html, which never referenced .Form/.Error either. Preserved
	// as-is rather than fixed here; it's the "publish guard"/lost-draft
	// behavior already tracked in .claude/runs.md's TODO list.
	if title == "" {
		s.renderPage(w, r, pageTitle("Write"), "page-editor", nil, writePage(WriteProps{}))
		return
	}
	slug := kebabSlug(title)
	if slug == "" {
		s.renderPage(w, r, pageTitle("Write"), "page-editor", nil, writePage(WriteProps{}))
		return
	}

	tagsInput := r.PostFormValue("tags")
	if inline := markdown.ExtractInlineTags(body); len(inline) > 0 {
		tagsInput += "," + strings.Join(inline, ",")
	}
	tags := parseTags(tagsInput)
	noteID, err := s.saveNote(r.Context(), u.ID, u.Handle, slug, title, body, tags)
	if err != nil {
		s.renderPage(w, r, pageTitle("Write"), "page-editor", nil, writePage(WriteProps{}))
		return
	}
	if err := s.saveNoteImage(r, noteID); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	http.Redirect(w, r, noteURL(u.Handle, slug), http.StatusSeeOther)
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
	embed := s.buildEmbedResolver(r.Context(), u.Handle, 2)
	html, err := markdown.Render(body, u.Handle, resolver, embed)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
}

// ---------- edit ----------

func (s *Server) GetEdit(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	noteID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "note not found")
		return
	}

	var authorID uuid.UUID
	var title, body, slug string
	var publishedAt sql.NullInt64
	err = s.DB.QueryRowContext(r.Context(), `
		SELECT author_id, title, body_md, published_at, slug
		FROM notes WHERE id = $1`, noteID,
	).Scan(&authorID, &title, &body, &publishedAt, &slug)
	if errors.Is(err, sql.ErrNoRows) {
		s.renderError(w, r, http.StatusNotFound, "note not found")
		return
	}
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if authorID != u.ID {
		s.renderError(w, r, http.StatusForbidden, "you can only edit your own notes")
		return
	}

	// The unified document is title + body. Tags stored on the old note that
	// aren't already inline #hashtags are appended so they survive the round
	// trip (the editor's tag model is inline-only).
	doc := title + "\n" + body
	inline := map[string]bool{}
	for _, t := range markdown.ExtractInlineTags(body) {
		inline[t] = true
	}
	var extra []string
	if tags, _ := loadTagsForNote(r.Context(), s.DB, noteID); len(tags) > 0 {
		for _, t := range tags {
			if !inline[t] {
				extra = append(extra, "#"+t)
			}
		}
	}
	if len(extra) > 0 {
		doc += "\n\n" + strings.Join(extra, " ")
	}

	s.renderPage(w, r, pageTitle("Edit"), "page-editor", nil, writePage(WriteProps{
		NoteID:    noteID.String(),
		Document:  doc,
		IsEdit:    true,
		Published: publishedAt.Valid,
		NoteURL:   noteURL(u.Handle, slug),
	}))
}

// ---------- note view ----------

type noteView struct {
	ID        uuid.UUID
	Title     string
	BodyMD    string
	UpdatedAt int64
	Slug      string
	AuthorID    uuid.UUID
	HiddenAt    sql.NullInt64
	DeletedAt   sql.NullInt64
	PublishedAt sql.NullInt64
}

func (s *Server) GetNote(w http.ResponseWriter, r *http.Request) {
	handle := r.PathValue("user")
	slug := r.PathValue("slug")
	if slug == "" {
		s.renderError(w, r, http.StatusNotFound, "note not found")
		return
	}

	var n noteView
	err := s.DB.QueryRowContext(r.Context(), `
		SELECT n.id, n.title, n.body_md, n.updated_at, n.slug,
		       n.author_id, n.hidden_at, n.deleted_at, n.published_at
		FROM notes n
		JOIN users u ON u.id = n.author_id
		WHERE u.handle = $1 AND n.slug = $2`,
		handle, slug,
	).Scan(&n.ID, &n.Title, &n.BodyMD, &n.UpdatedAt, &n.Slug,
		&n.AuthorID, &n.HiddenAt, &n.DeletedAt, &n.PublishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		var userExists bool
		s.DB.QueryRowContext(r.Context(),
			`SELECT EXISTS(SELECT 1 FROM users WHERE handle = $1)`, handle,
		).Scan(&userExists)
		if userExists {
			s.renderPage(w, r, pageTitle("@"+handle+"/"+slug), "", nil, noteStubContent(NoteStubProps{
				Handle: handle,
				Slug:   slug,
			}))
			return
		}
		s.renderError(w, r, http.StatusNotFound, "note not found")
		return
	}
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	// Hidden notes and unpublished drafts are visible only to their author/admin.
	if n.HiddenAt.Valid || !n.PublishedAt.Valid {
		viewer, _ := s.Auth.CurrentUser(r)
		isAuthor := viewer != nil && viewer.ID == n.AuthorID
		if !isAuthor && !s.IsAdmin(viewer) {
			s.renderError(w, r, http.StatusNotFound, "note not found")
			return
		}
	}

	if n.DeletedAt.Valid {
		s.renderError(w, r, http.StatusNotFound, "note not found")
		return
	}

	resolver := s.buildResolverForNote(r.Context(), handle, n.ID)
	embed := s.buildEmbedResolver(r.Context(), handle, 2)
	bodyHTML, err := markdown.Render(n.BodyMD, handle, resolver, embed)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	tags, _ := loadTagsForNote(r.Context(), s.DB, n.ID)
	sameVaultBL, crossVaultBL, _ := s.loadBacklinksSplit(r.Context(), n.ID, handle)
	outgoingSame, outgoingCross, _ := s.loadOutgoingSplit(r.Context(), n.ID, handle)
	authorStats, err := loadAuthorStats(r.Context(), s.DB, n.AuthorID)
	if err != nil {
		log.Printf("GetNote loadAuthorStats %s: %v", n.ID, err)
	}

	viewer, _ := s.Auth.CurrentUser(r)
	var viewerID uuid.UUID
	if viewer != nil {
		viewerID = viewer.ID
	}
	likeN, err := likeCount(r.Context(), s.DB, n.ID)
	if err != nil {
		log.Printf("GetNote likeCount %s: %v", n.ID, err)
	}
	liked, err := userHasLiked(r.Context(), s.DB, viewerID, n.ID)
	if err != nil {
		log.Printf("GetNote userHasLiked %s: %v", n.ID, err)
	}
	saved, err := userHasSaved(r.Context(), s.DB, viewerID, n.ID)
	if err != nil {
		log.Printf("GetNote userHasSaved %s: %v", n.ID, err)
	}
	viewerFollows, err := userFollows(r.Context(), s.DB, viewerID, n.AuthorID)
	if err != nil {
		log.Printf("GetNote userFollows %s: %v", n.ID, err)
	}

	hasImage := noteHasImage(r, s.DB, n.ID)

	noteProps := NoteViewProps{
		Note:           n,
		AuthorHandle:   handle,
		AuthorID:       n.AuthorID,
		AuthorStats:    authorStats,
		ViewerFollows:  viewerFollows,
		BodyHTML:       bodyHTML,
		Tags:           tags,
		BacklinksSame:  sameVaultBL,
		BacklinksCross: crossVaultBL,
		OutgoingSame:   outgoingSame,
		OutgoingCross:  outgoingCross,
		UpdatedRel:     relativeTime(n.UpdatedAt),
		ReadingMinutes: readingMinutes(n.BodyMD),
		LikeCount:      likeN,
		Liked:          liked,
		Saved:          saved,
		ViewerLoggedIn: viewer != nil,
		IsAuthor:       viewer != nil && viewer.ID == n.AuthorID,
		IsHidden:       n.HiddenAt.Valid,
		OGDescription:  markdown.Excerpt(n.BodyMD, 160),
		OGImage:        s.resolveOGImage(n.ID, hasImage),
		OGURL:          s.absoluteNoteURL(handle, n.Slug),
	}
	s.renderPage(w, r, pageTitle(n.Title+" · @"+handle), "", noteMeta(noteProps), noteContent(noteProps))
}

// ---------- save + link recomputation ----------

func (s *Server) saveNote(ctx context.Context, authorID uuid.UUID, authorHandle, slug, title, body string, tags []string) (uuid.UUID, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return uuid.UUID{}, err
	}
	defer tx.Rollback()

	now := nowUnix()
	var noteID uuid.UUID
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO notes(author_id, slug, slug_ci, title, body_md, created_at, updated_at, published_at)
		VALUES($1, $2, $3, $4, $5, $6, $6, $6) RETURNING id`,
		authorID, slug, strings.ToLower(slug), title, body, now,
	).Scan(&noteID); err != nil {
		return uuid.UUID{}, err
	}

	for _, t := range tags {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO note_tags(note_id, tag, created_at) VALUES($1, $2, $3)`,
			noteID, t, now,
		); err != nil {
			return uuid.UUID{}, err
		}
	}

	if err := recomputeLinks(ctx, tx, noteID, authorHandle, body); err != nil {
		return uuid.UUID{}, err
	}

	if err := backfillStubLinks(ctx, tx, noteID, authorID, slug); err != nil {
		return uuid.UUID{}, err
	}

	if err := tx.Commit(); err != nil {
		return uuid.UUID{}, err
	}
	return noteID, nil
}

// ---------- draft + autosave ----------

const orphanDraftMaxAgeDays = 7

// createDraft inserts an empty, unpublished note and returns its id. The editor
// binds to this id so autosave + image upload have a target before publish.
func (s *Server) createDraft(ctx context.Context, authorID uuid.UUID) (uuid.UUID, error) {
	now := nowUnix()
	slug := "draft-" + uuid.NewString()[:8]
	var id uuid.UUID
	err := s.DB.QueryRowContext(ctx, `
		INSERT INTO notes(author_id, slug, slug_ci, title, body_md, created_at, updated_at)
		VALUES($1, $2, $2, '', '', $3, $3) RETURNING id`,
		authorID, slug, now,
	).Scan(&id)
	return id, err
}

// sweepOrphanDrafts deletes this author's empty drafts older than the cutoff —
// notes opened at /write that were never written to or published.
func (s *Server) sweepOrphanDrafts(ctx context.Context, authorID uuid.UUID) error {
	cutoff := nowUnix() - orphanDraftMaxAgeDays*86400
	_, err := s.DB.ExecContext(ctx, `
		DELETE FROM notes
		WHERE author_id = $1 AND published_at IS NULL
		  AND title = '' AND body_md = '' AND created_at < $2`,
		authorID, cutoff)
	return err
}

// splitTitleBody splits a unified editor document into its title (first line,
// leading "#" stripped) and body (everything after the first newline).
func splitTitleBody(doc string) (title, body string) {
	if i := strings.IndexByte(doc, '\n'); i >= 0 {
		title, body = doc[:i], doc[i+1:]
	} else {
		title = doc
	}
	return strings.TrimSpace(strings.TrimLeft(title, "#")), body
}

// PatchNote autosaves the editor document to a note (draft or published):
// first line -> title, recompute slug/tags/links. Does not touch published_at.
func (s *Server) PatchNote(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	noteID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.Error(w, "bad id", http.StatusNotFound)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	var authorID uuid.UUID
	var curSlug string
	var publishedAt sql.NullInt64
	err = s.DB.QueryRowContext(r.Context(),
		`SELECT author_id, slug, published_at FROM notes WHERE id = $1`, noteID,
	).Scan(&authorID, &curSlug, &publishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if authorID != u.ID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	title, body := splitTitleBody(r.PostFormValue("document"))
	slug := curSlug
	// Only regenerate slug for drafts; a published note's slug is its permanent URL.
	if title != "" && !publishedAt.Valid {
		if sl := kebabSlug(title); sl != "" {
			slug = sl
		} else if strings.HasPrefix(curSlug, "draft-") {
			// Title has no letters/digits (emoji/punctuation-only) so kebabSlug
			// can't build a slug from it. Give it a non-"draft-" slug anyway so
			// PublishNote's "still using the auto slug" check doesn't block a
			// note that clearly does have a title.
			slug = "note-" + noteID.String()[:8]
		}
	}
	tags := parseTags(strings.Join(markdown.ExtractInlineTags(body), ","))

	if err := s.autosaveNote(r.Context(), noteID, u.Handle, slug, title, body, tags); err != nil {
		if isUniqueViolation(err) {
			http.Error(w, "A note with this title already exists.", http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	fmt.Fprintf(w, `{"savedAt":%d}`, nowUnix())
}

func (s *Server) autosaveNote(ctx context.Context, noteID uuid.UUID, authorHandle, slug, title, body string, tags []string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := nowUnix()
	if _, err := tx.ExecContext(ctx, `
		UPDATE notes SET title=$1, body_md=$2, slug=$3, slug_ci=$4, updated_at=$5
		WHERE id=$6`, title, body, slug, strings.ToLower(slug), now, noteID,
	); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM note_tags WHERE note_id = $1`, noteID); err != nil {
		return err
	}
	for _, t := range tags {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO note_tags(note_id, tag, created_at) VALUES($1, $2, $3)`,
			noteID, t, now); err != nil {
			return err
		}
	}
	if err := recomputeLinks(ctx, tx, noteID, authorHandle, body); err != nil {
		return err
	}
	// Stubs are matched by the author's uuid, and this path only carries the
	// handle; the note itself is the cheapest place to get the id.
	var authorID uuid.UUID
	if err := tx.QueryRowContext(ctx,
		`SELECT author_id FROM notes WHERE id = $1`, noteID,
	).Scan(&authorID); err != nil {
		return err
	}
	if err := backfillStubLinks(ctx, tx, noteID, authorID, slug); err != nil {
		return err
	}
	return tx.Commit()
}

// PublishNote marks a note published (idempotent) and returns its URL.
func (s *Server) PublishNote(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	noteID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.Error(w, "bad id", http.StatusNotFound)
		return
	}
	var slug, title string
	err = s.DB.QueryRowContext(r.Context(), `
		SELECT slug, title FROM notes WHERE id = $1 AND author_id = $2`,
		noteID, u.ID,
	).Scan(&slug, &title)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if strings.TrimSpace(title) == "" {
		http.Error(w, "Title is required before publishing.", http.StatusUnprocessableEntity)
		return
	}
	if strings.HasPrefix(slug, "draft-") {
		http.Error(w, "Save a title before publishing.", http.StatusUnprocessableEntity)
		return
	}
	_, err = s.DB.ExecContext(r.Context(), `
		UPDATE notes SET published_at = COALESCE(published_at, $1)
		WHERE id = $2 AND author_id = $3`,
		nowUnix(), noteID, u.ID,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	fmt.Fprintf(w, `{"url":%q}`, noteURL(u.Handle, slug))
}

// ---------- helpers ----------

func noteURL(handle, slug string) string {
	return "/" + handle + "/" + slug
}

// absoluteNoteURL is noteURL prefixed with BaseURL, for og:url.
func (s *Server) absoluteNoteURL(handle, slug string) string {
	return strings.TrimRight(s.BaseURL, "/") + noteURL(handle, slug)
}

func nowUnix() int64 { return timeNow().Unix() }

var timeNow = func() time.Time { return time.Now() }

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// ---------- delete ----------

func (s *Server) PostDeleteNote(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		s.renderError(w, r, http.StatusBadRequest, "invalid note id")
		return
	}
	res, err := s.DB.ExecContext(r.Context(),
		`UPDATE notes SET deleted_at = $1 WHERE id = $2 AND author_id = $3`,
		nowUnix(), id, u.ID)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		s.renderError(w, r, http.StatusForbidden, "not your note")
		return
	}
	http.Redirect(w, r, "/"+u.Handle, http.StatusSeeOther)
}

// PostBulkDeleteDrafts soft-deletes multiple of the caller's own draft notes at
// once (checkbox multi-select on the profile drafts tab). Scoped to
// published_at IS NULL so this can never touch a published note even if a
// stale/tampered id sneaks into the form.
func (s *Server) PostBulkDeleteDrafts(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderError(w, r, http.StatusBadRequest, "bad form")
		return
	}
	raw := r.Form["ids"]
	ids := make([]string, 0, len(raw))
	for _, idStr := range raw {
		if id, err := uuid.Parse(idStr); err == nil {
			ids = append(ids, id.String())
		}
	}
	if len(ids) > 0 {
		_, err := s.DB.ExecContext(r.Context(), `
			UPDATE notes SET deleted_at = $1
			WHERE id = ANY($2::uuid[]) AND author_id = $3 AND published_at IS NULL`,
			nowUnix(), ids, u.ID)
		if err != nil {
			s.renderError(w, r, http.StatusInternalServerError, err.Error())
			return
		}
	}
	http.Redirect(w, r, "/"+u.Handle+"?tab=drafts", http.StatusSeeOther)
}
