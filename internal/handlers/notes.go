package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
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
	var title, body, slug, distribution string
	var revision int64
	var hasDraft bool
	var publishedAt sql.NullInt64
	err = s.DB.QueryRowContext(r.Context(), `
		SELECT author_id, COALESCE(draft_title,title), COALESCE(draft_body_md,body_md), published_at, slug, edit_version, COALESCE(draft_distribution,distribution), draft_body_md IS NOT NULL
		FROM notes WHERE id = $1 AND deleted_at IS NULL AND hidden_at IS NULL`, noteID,
	).Scan(&authorID, &title, &body, &publishedAt, &slug, &revision, &distribution, &hasDraft)
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
	if tags, _ := loadTagsForNote(r.Context(), s.DB, noteID); !hasDraft && len(tags) > 0 {
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
		NoteID:       noteID.String(),
		Document:     doc,
		IsEdit:       true,
		Published:    publishedAt.Valid,
		NoteURL:      noteURL(u.Handle, slug),
		Revision:     revision,
		Distribution: distribution,
	}))
}

// ---------- note view ----------

type noteView struct {
	ID          uuid.UUID
	Title       string
	BodyMD      string
	UpdatedAt   int64
	Slug        string
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
	if r.URL.Query().Get("space") == handle {
		space, ok, err := s.loadSpaceReading(r.Context(), n.AuthorID, handle)
		if err != nil {
			log.Printf("GetNote space navigation: %v", err)
		} else if ok {
			chapters := []readingBlock{}
			for _, b := range space.Blocks {
				if b.URL != "" {
					chapters = append(chapters, b)
				}
			}
			for i, b := range chapters {
				if b.URL == noteURL(handle, n.Slug)+"?space="+handle {
					noteProps.SpaceTitle, noteProps.SpaceURL = space.Title, "/"+handle
					if i > 0 {
						noteProps.SpacePrevious = chapters[i-1]
					}
					if i+1 < len(chapters) {
						noteProps.SpaceNext = chapters[i+1]
					}
					break
				}
			}
		}
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
		INSERT INTO notes(author_id, slug, slug_ci, title, body_md, created_at, updated_at, distribution)
		VALUES($1, $2, $2, '', '', $3, $3, 'semi') RETURNING id`,
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
		`SELECT author_id, slug, published_at FROM notes WHERE id = $1 AND deleted_at IS NULL AND hidden_at IS NULL`, noteID,
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

	var versions []int64
	if raw := r.PostFormValue("revision"); raw != "" {
		version, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || version < 0 {
			http.Error(w, "bad revision", 400)
			return
		}
		versions = []int64{version}
	}
	distribution := r.PostFormValue("distribution")
	if distribution != "" && !validDistribution(distribution) {
		http.Error(w, "無效的公開設定", http.StatusBadRequest)
		return
	}
	if err := s.autosaveNoteDocument(r.Context(), noteID, u.Handle, slug, title, body, tags, distribution, versions...); err != nil {
		if errors.Is(err, errEditConflict) {
			http.Error(w, "另一個編輯器已更新這篇文章。內容已保留在本機，請重新開啟文章後比對復原。", http.StatusConflict)
			return
		}
		if isUniqueViolation(err) {
			http.Error(w, "A note with this title already exists.", http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if len(versions) > 0 {
		fmt.Fprintf(w, `{"savedAt":%d,"revision":%d}`, nowUnix(), versions[0]+1)
	} else {
		fmt.Fprintf(w, `{"savedAt":%d}`, nowUnix())
	}
}

var errEditConflict = errors.New("note edited concurrently")

func (s *Server) autosaveNote(ctx context.Context, noteID uuid.UUID, authorHandle, slug, title, body string, tags []string, versions ...int64) error {
	return s.autosaveNoteDocument(ctx, noteID, authorHandle, slug, title, body, tags, "", versions...)
}

func (s *Server) autosaveNoteDocument(ctx context.Context, noteID uuid.UUID, authorHandle, slug, title, body string, tags []string, distribution string, versions ...int64) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current int64
	var published sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT edit_version,published_at FROM notes WHERE id=$1 AND deleted_at IS NULL AND hidden_at IS NULL FOR UPDATE`, noteID).Scan(&current, &published); err != nil {
		return err
	}
	if len(versions) > 0 && current != versions[0] {
		return errEditConflict
	}
	if published.Valid {
		_, err = tx.ExecContext(ctx, `UPDATE notes SET draft_title=$2,draft_body_md=$3,draft_distribution=COALESCE(NULLIF($4,''),draft_distribution),edit_version=edit_version+1 WHERE id=$1`, noteID, title, body, distribution)
		if err != nil {
			return err
		}
		return tx.Commit()
	}
	now := nowUnix()
	if _, err := tx.ExecContext(ctx, `
		UPDATE notes SET title=$1, body_md=$2, draft_title=NULL, draft_body_md=NULL, slug=$3, slug_ci=$4, updated_at=$5, edit_version=edit_version+1,
		  draft_distribution=COALESCE(NULLIF($7,''),draft_distribution)
		WHERE id=$6`, title, body, slug, strings.ToLower(slug), now, noteID, distribution,
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

// PublishNote promotes the saved draft and returns its URL and new revision.
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
	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, "無法發布", 500)
		return
	}
	defer tx.Rollback()
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", 400)
		return
	}
	if value := r.PostFormValue("revision"); value != "" {
		expected, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			http.Error(w, "bad revision", 400)
			return
		}
		var current int64
		err = tx.QueryRowContext(r.Context(), `SELECT edit_version FROM notes WHERE id=$1 AND author_id=$2 AND deleted_at IS NULL AND hidden_at IS NULL FOR UPDATE`, noteID, u.ID).Scan(&current)
		if err != nil {
			http.Error(w, "not found", 404)
			return
		}
		if current != expected {
			http.Error(w, "另一個編輯器已更新，請重新載入後比對內容。", 409)
			return
		}
	}
	slug, err := publishDocument(r.Context(), tx, noteID, u.ID, u.Handle)
	if errors.Is(err, errPublishTitle) {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var revision int64
	if err = tx.QueryRowContext(r.Context(), `SELECT edit_version FROM notes WHERE id=$1`, noteID).Scan(&revision); err != nil {
		http.Error(w, "無法發布", 500)
		return
	}
	err = tx.Commit()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	fmt.Fprintf(w, `{"url":%q,"revision":%d}`, noteURL(u.Handle, slug), revision)
}

var errPublishTitle = errors.New("請先儲存標題再發布")

func validDistribution(value string) bool { return value == "public" || value == "semi" }

// publishDocument promotes a complete draft atomically; readers and indexes
// continue using notes.title/body_md until this transaction commits.
func publishDocument(ctx context.Context, tx *sql.Tx, id, author uuid.UUID, handle string) (string, error) {
	var slug, title, body string
	err := tx.QueryRowContext(ctx, `SELECT slug,COALESCE(draft_title,title),COALESCE(draft_body_md,body_md) FROM notes WHERE id=$1 AND author_id=$2 AND hidden_at IS NULL AND deleted_at IS NULL FOR UPDATE`, id, author).Scan(&slug, &title, &body)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(title) == "" {
		return "", errPublishTitle
	}
	_, err = tx.ExecContext(ctx, `UPDATE notes SET title=$2,body_md=$3,draft_title=NULL,draft_body_md=NULL,distribution=COALESCE(draft_distribution,distribution),draft_distribution=NULL,published_at=COALESCE(published_at,$4),updated_at=$4,edit_version=edit_version+1 WHERE id=$1`, id, title, body, nowUnix())
	if err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM note_tags WHERE note_id=$1`, id); err != nil {
		return "", err
	}
	for _, tag := range parseTags(strings.Join(markdown.ExtractInlineTags(body), ",")) {
		if _, err = tx.ExecContext(ctx, `INSERT INTO note_tags(note_id,tag,created_at) VALUES($1,$2,$3)`, id, tag, nowUnix()); err != nil {
			return "", err
		}
	}
	if err = recomputeLinks(ctx, tx, id, handle, body); err != nil {
		return "", err
	}
	if err = backfillStubLinks(ctx, tx, id, author, slug); err != nil {
		return "", err
	}
	return slug, nil
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
