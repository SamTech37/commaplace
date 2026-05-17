package handlers

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"commonplace/internal/seed"
)

// GetOnboarding shows the "fork the tour / start fresh" choice. Auth
// callback redirects new users here when their onboarded_at is NULL.
func (s *Server) GetOnboarding(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	if onb, _ := userOnboarded(r.Context(), s.DB, u.ID); onb {
		http.Redirect(w, r, "/me", http.StatusSeeOther)
		return
	}
	// Photo upload is not offered — every new account must build an avatar
	// from parts first. Once the avatar exists, fall through to the existing
	// tour-fork prompt.
	if !userHasAvatar(r, s.DB, u.ID) {
		http.Redirect(w, r, "/me/avatar", http.StatusSeeOther)
		return
	}
	s.render(w, r, "onboarding", map[string]any{"User": u})
}

// PostOnboardingFork handles both choices. ?skip=1 marks the user
// onboarded without copying anything; otherwise we fork the tour vault.
func (s *Server) PostOnboardingFork(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	skip := r.URL.Query().Get("skip") == "1"
	now := time.Now().Unix()

	if skip {
		if _, err := s.DB.ExecContext(r.Context(),
			`UPDATE users SET onboarded_at = ? WHERE id = ?`, now, u.ID,
		); err != nil {
			s.renderError(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		http.Redirect(w, r, "/me", http.StatusSeeOther)
		return
	}

	welcomeID, err := s.forkTour(r.Context(), u.ID, u.Handle)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := s.DB.ExecContext(r.Context(),
		`UPDATE users SET pinned_note_id = ?, onboarded_at = ? WHERE id = ?`,
		welcomeID, now, u.ID,
	); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	http.Redirect(w, r, "/me", http.StatusSeeOther)
}

// forkTour copies every TourHandle-authored note into the new user's vault.
// Same-vault wiki links naturally re-resolve to the new copies. Cross-vault
// links (e.g. [[@maker/...]]) keep pointing at the originals because they
// carry an explicit @user prefix.
func (s *Server) forkTour(ctx context.Context, newUserID int64, newUserHandle string) (welcomeID int64, err error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	now := time.Now().Unix()

	for _, n := range seed.TourNotes {
		if n.Author != seed.TourHandle {
			continue
		}
		res, err := tx.ExecContext(ctx, `
			INSERT INTO notes(author_id, folder_path, slug, title, body_md, created_at, updated_at)
			VALUES(?, '', ?, ?, ?, ?, ?)`,
			newUserID, n.Slug, n.Title, n.Body, now, now,
		)
		if err != nil {
			return 0, err
		}
		nid, _ := res.LastInsertId()
		if n.IsWelcome {
			welcomeID = nid
		}
		for _, t := range n.Tags {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO note_tags(note_id, tag, created_at) VALUES(?, ?, ?)`,
				nid, t, now,
			); err != nil {
				return 0, err
			}
		}
		if err := recomputeLinks(ctx, tx, nid, newUserHandle, n.Body); err != nil {
			return 0, err
		}
		// Re-resolve previously-unresolved links pointing at this just-copied note.
		if _, err := tx.ExecContext(ctx, `
			UPDATE links SET resolved_target_id = ?
			WHERE resolved_target_id IS NULL
			  AND target_user_handle = ? AND target_slug = ?`,
			nid, newUserHandle, n.Slug,
		); err != nil {
			return 0, err
		}
	}
	return welcomeID, tx.Commit()
}

func userOnboarded(ctx context.Context, db *sql.DB, userID int64) (bool, error) {
	var t sql.NullInt64
	err := db.QueryRowContext(ctx,
		`SELECT onboarded_at FROM users WHERE id = ?`, userID,
	).Scan(&t)
	if err != nil {
		return false, err
	}
	return t.Valid, nil
}

// pinnedNoteForUser fetches the user's pinned welcome note (or nil).
func pinnedNoteForUser(ctx context.Context, db *sql.DB, userID int64) (*pinnedNote, error) {
	var pinnedID sql.NullInt64
	if err := db.QueryRowContext(ctx,
		`SELECT pinned_note_id FROM users WHERE id = ?`, userID,
	).Scan(&pinnedID); err != nil || !pinnedID.Valid {
		return nil, err
	}
	var (
		title, slug string
	)
	err := db.QueryRowContext(ctx, `
		SELECT n.title, n.slug
		FROM notes n
		WHERE n.id = ? AND n.author_id = ?`,
		pinnedID.Int64, userID,
	).Scan(&title, &slug)
	if err != nil {
		return nil, nil
	}
	return &pinnedNote{
		Title: title,
		URL:   noteURL(authorHandleByID(ctx, db, userID), slug),
	}, nil
}

type pinnedNote struct {
	Title string
	URL   string
}

func authorHandleByID(ctx context.Context, db *sql.DB, userID int64) string {
	var h string
	db.QueryRowContext(ctx, `SELECT handle FROM users WHERE id = ?`, userID).Scan(&h)
	return h
}
