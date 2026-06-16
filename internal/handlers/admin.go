package handlers

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"

	"commonplace/internal/auth"
)

// IsAdmin returns true when u.Handle matches the configured AdminHandle.
// AdminHandle="" disables admin entirely (useful for early dev).
func (s *Server) IsAdmin(u *auth.User) bool {
	if u == nil || s.AdminHandle == "" {
		return false
	}
	return u.Handle == s.AdminHandle
}

func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) *auth.User {
	u := s.requireUser(w, r)
	if u == nil {
		return nil
	}
	if !s.IsAdmin(u) {
		s.renderError(w, r, http.StatusForbidden, "admin only")
		return nil
	}
	return u
}

type adminReportRow struct {
	ID             uuid.UUID
	Reason         string
	CreatedRel     string
	Status         string
	NoteID         uuid.UUID
	NoteTitle      string
	NoteURL        string
	NoteHidden     bool
	AuthorHandle   string
	ReporterID     uuid.UUID
	ReporterHandle string
}

func (s *Server) GetAdminReports(w http.ResponseWriter, r *http.Request) {
	if a := s.requireAdmin(w, r); a == nil {
		return
	}
	rows, err := s.DB.QueryContext(r.Context(), `
		SELECT r.id, r.reason, r.created_at, r.status,
		       n.id, n.title, n.slug, (n.hidden_at IS NOT NULL),
		       au.handle,
		       r.reporter_id, ru.handle
		FROM reports r
		JOIN notes n  ON n.id = r.note_id
		JOIN users au ON au.id = n.author_id
		JOIN users ru ON ru.id = r.reporter_id
		ORDER BY (r.status = 'open') DESC, r.created_at DESC
		LIMIT 200`)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var items []adminReportRow
	for rows.Next() {
		var (
			rep     adminReportRow
			created int64
			slug    string
		)
		if err := rows.Scan(&rep.ID, &rep.Reason, &created, &rep.Status,
			&rep.NoteID, &rep.NoteTitle, &slug, &rep.NoteHidden,
			&rep.AuthorHandle, &rep.ReporterID, &rep.ReporterHandle); err != nil {
			s.renderError(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		rep.CreatedRel = relativeTime(created)
		rep.NoteURL = noteURL(rep.AuthorHandle, slug)
		items = append(items, rep)
	}

	s.render(w, r, "admin_reports", map[string]any{"Reports": items})
}

// PostAdminHide toggles a note's hidden_at and marks every report on it
// as resolved.
func (s *Server) PostAdminHide(w http.ResponseWriter, r *http.Request) {
	if a := s.requireAdmin(w, r); a == nil {
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
	action := r.PostFormValue("action") // "hide" | "unhide"
	if err := setHidden(r.Context(), s.DB, noteID, action == "hide"); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if action == "hide" {
		if _, err := s.DB.ExecContext(r.Context(),
			`UPDATE reports SET status = 'resolved' WHERE note_id = $1`, noteID); err != nil {
			log.Printf("admin hide: resolve reports for note %s: %v", noteID, err)
		}
	}
	http.Redirect(w, r, "/admin/reports", http.StatusSeeOther)
}

type adminStat struct {
	Label string
	Value int64
	Hint  string
}

type adminRecentUser struct {
	Handle     string
	Email      string
	CreatedRel string
	NoteCount  int64
}

type adminRecentNote struct {
	Title       string
	URL         string
	AuthorHandle string
	UpdatedRel  string
	Hidden      bool
}

// GetAdminDashboard is a minimal monitoring view: site totals, recent
// signups and notes, open reports.
func (s *Server) GetAdminDashboard(w http.ResponseWriter, r *http.Request) {
	if a := s.requireAdmin(w, r); a == nil {
		return
	}
	ctx := r.Context()

	now := time.Now().Unix()
	day := now - 24*3600
	week := now - 7*24*3600

	stats := []adminStat{}

	scalar := func(label, hint, query string, args ...any) {
		var n int64
		if err := s.DB.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
			log.Printf("admin dashboard: %s: %v", label, err)
		}
		stats = append(stats, adminStat{Label: label, Value: n, Hint: hint})
	}

	scalar("Users", "total", `SELECT COUNT(*) FROM users`)
	scalar("Users (7d)", "new this week", `SELECT COUNT(*) FROM users WHERE created_at >= $1`, week)
	scalar("Users (24h)", "new today", `SELECT COUNT(*) FROM users WHERE created_at >= $1`, day)
	scalar("Notes", "visible", `SELECT COUNT(*) FROM notes WHERE hidden_at IS NULL AND deleted_at IS NULL`)
	scalar("Notes hidden", "moderated", `SELECT COUNT(*) FROM notes WHERE hidden_at IS NOT NULL`)
	scalar("Notes (7d)", "new this week", `SELECT COUNT(*) FROM notes WHERE created_at >= $1`, week)
	scalar("Notes (24h)", "new today", `SELECT COUNT(*) FROM notes WHERE created_at >= $1`, day)
	scalar("Likes", "total", `SELECT COUNT(*) FROM likes`)
	scalar("Follows", "total", `SELECT COUNT(*) FROM follows`)
	scalar("Reports open", "pending review", `SELECT COUNT(*) FROM reports WHERE status = 'open'`)

	// Recent signups
	var recentUsers []adminRecentUser
	uRows, err := s.DB.QueryContext(ctx, `
		SELECT u.handle, u.email, u.created_at,
		       (SELECT COUNT(*) FROM notes n WHERE n.author_id = u.id)
		FROM users u
		ORDER BY u.created_at DESC
		LIMIT 10`)
	if err == nil {
		defer uRows.Close()
		for uRows.Next() {
			var (
				ru      adminRecentUser
				created int64
			)
			if err := uRows.Scan(&ru.Handle, &ru.Email, &created, &ru.NoteCount); err == nil {
				ru.CreatedRel = relativeTime(created)
				recentUsers = append(recentUsers, ru)
			}
		}
	}

	// Recent notes
	var recentNotes []adminRecentNote
	nRows, err := s.DB.QueryContext(ctx, `
		SELECT n.title, n.slug, u.handle, n.updated_at, (n.hidden_at IS NOT NULL)
		FROM notes n JOIN users u ON u.id = n.author_id
		ORDER BY n.updated_at DESC
		LIMIT 10`)
	if err == nil {
		defer nRows.Close()
		for nRows.Next() {
			var (
				rn      adminRecentNote
				slug    string
				updated int64
			)
			if err := nRows.Scan(&rn.Title, &slug, &rn.AuthorHandle, &updated, &rn.Hidden); err == nil {
				rn.UpdatedRel = relativeTime(updated)
				rn.URL = noteURL(rn.AuthorHandle, slug)
				recentNotes = append(recentNotes, rn)
			}
		}
	}

	s.render(w, r, "admin_dashboard", map[string]any{
		"Stats":       stats,
		"RecentUsers": recentUsers,
		"RecentNotes": recentNotes,
	})
}

func setHidden(ctx context.Context, db *sql.DB, noteID uuid.UUID, hide bool) error {
	if hide {
		_, err := db.ExecContext(ctx,
			`UPDATE notes SET hidden_at = $1 WHERE id = $2`, time.Now().Unix(), noteID)
		return err
	}
	_, err := db.ExecContext(ctx,
		`UPDATE notes SET hidden_at = NULL WHERE id = $1`, noteID)
	return err
}
