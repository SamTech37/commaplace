package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"commonplace/internal/markdown"
)

// PostLike toggles a like row for the current user on the given note.
// Returns the heart+count fragment so HTMX can swap it inline.
func (s *Server) PostLike(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	noteID, err := uuid.Parse(r.PostFormValue("note_id"))
	if err != nil {
		http.Error(w, "bad note id", http.StatusBadRequest)
		return
	}

	// Toggle: try insert; if already there, delete.
	res, err := s.DB.ExecContext(r.Context(),
		`INSERT INTO likes(user_id, note_id, created_at) VALUES($1, $2, $3) ON CONFLICT (user_id, note_id) DO NOTHING`,
		u.ID, noteID, nowUnix())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	inserted, _ := res.RowsAffected()
	liked := inserted == 1
	if !liked {
		if _, err := s.DB.ExecContext(r.Context(),
			`DELETE FROM likes WHERE user_id = $1 AND note_id = $2`, u.ID, noteID,
		); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	count, _ := likeCount(r.Context(), s.DB, noteID)
	writeHeartFragment(w, noteID, liked, count)
}

// writeHeartFragment renders the like button + count. The button toggles
// itself when clicked. Uses the same .action-btn class as the initial
// server-rendered button in note.html (and its share/save siblings) so the
// button doesn't restyle/jump on the first toggle.
func writeHeartFragment(w http.ResponseWriter, noteID uuid.UUID, liked bool, count int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	cls := "action-btn"
	icon := "♡"
	if liked {
		cls += " liked"
		icon = "♥"
	}
	fmt.Fprintf(w, `<form class="inline-form" hx-post="/api/like" hx-target="this" hx-swap="outerHTML">
  <input type="hidden" name="note_id" value="%s">
  <button type="submit" class="%s" aria-pressed="%t">%s <span class="count">%d</span></button>
</form>`, noteID, cls, liked, icon, count)
}

func likeCount(ctx context.Context, db *sql.DB, noteID uuid.UUID) (int, error) {
	var n int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM likes WHERE note_id = $1`, noteID,
	).Scan(&n)
	return n, err
}

func userHasLiked(ctx context.Context, db *sql.DB, userID, noteID uuid.UUID) (bool, error) {
	if userID == uuid.Nil {
		return false, nil
	}
	var one int
	err := db.QueryRowContext(ctx,
		`SELECT 1 FROM likes WHERE user_id = $1 AND note_id = $2`, userID, noteID,
	).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// GetSaved lists notes the current user has saved, newest save first.
// Pagination cursors on save time (sv.created_at), not note update time —
// those are different columns here, unlike feed's cursor which is the note's
// own updated_at.
func (s *Server) GetSaved(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	var older int64
	if v := r.URL.Query().Get("older"); v != "" {
		older, _ = strconv.ParseInt(v, 10, 64)
	}

	args := []any{u.ID}
	q := strings.Builder{}
	q.WriteString(`
		SELECT n.id, n.title, n.slug, n.body_md, n.updated_at, u2.handle, sv.created_at,
		       (SELECT COUNT(*) FROM likes WHERE note_id = n.id),
		       (SELECT COUNT(*) FROM links WHERE source_note_id = n.id),
		       (SELECT COUNT(*) FROM links WHERE source_note_id = n.id AND target_user_id IS DISTINCT FROM u2.id)
		FROM saves sv
		JOIN notes n  ON n.id = sv.note_id
		JOIN users u2 ON u2.id = n.author_id
		WHERE sv.user_id = $1 AND n.hidden_at IS NULL AND n.deleted_at IS NULL AND n.published_at IS NOT NULL`)
	if older > 0 {
		args = append(args, older)
		fmt.Fprintf(&q, ` AND sv.created_at < $%d`, len(args))
	}
	args = append(args, pageCfg.FeedPageSize)
	fmt.Fprintf(&q, ` ORDER BY sv.created_at DESC LIMIT $%d`, len(args))

	rows, err := s.DB.QueryContext(r.Context(), q.String(), args...)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	var cards []feedCard
	var lastSaveCreatedAt int64
	for rows.Next() {
		var (
			c      feedCard
			slug   string
			body   string
			handle string
			saved  int64
		)
		if err := rows.Scan(&c.NoteID, &c.Title, &slug, &body, &c.UpdatedAt, &handle, &saved,
			&c.LikeCount, &c.LinkCount, &c.CrossCount); err != nil {
			rows.Close()
			s.renderError(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		c.AuthorHandle = handle
		c.URL = noteURL(handle, slug)
		c.UpdatedRel = relativeTime(c.UpdatedAt)
		c.Variant, c.Excerpt, c.ListItems, c.Quote, c.LinkChips = analyzeCardBody(body)
		c.ImageURL = markdown.FirstImageURL(body)
		cards = append(cards, c)
		lastSaveCreatedAt = saved
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	attachTagsToCards(r.Context(), s.DB, cards)

	var olderURL string
	if len(cards) == pageCfg.FeedPageSize {
		v := r.URL.Query()
		v.Set("older", strconv.FormatInt(lastSaveCreatedAt, 10))
		olderURL = "/me/saved?" + v.Encode()
	}

	view := NoteListView{
		Cards:    cards,
		OlderURL: olderURL,
		Empty:    emptyText("還沒有收藏。在筆記頁按「＋ 收藏」即可加入這裡。"),
	}

	if r.Header.Get("HX-Request") == "true" {
		s.renderFragment(w, r, notesFragment(view))
		return
	}

	s.renderPage(w, r, pageTitle("收藏"), "", nil, savedPage(view))
}
