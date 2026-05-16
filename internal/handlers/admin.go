package handlers

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"strconv"
	"time"

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
	ID           int64
	Reason       string
	CreatedRel   string
	Status       string
	NoteID       int64
	NoteTitle    string
	NoteURL      string
	NoteHidden   bool
	AuthorHandle string
	ReporterID   int64
	ReporterHandle string
}

func (s *Server) GetAdminReports(w http.ResponseWriter, r *http.Request) {
	if a := s.requireAdmin(w, r); a == nil {
		return
	}
	rows, err := s.DB.QueryContext(r.Context(), `
		SELECT r.id, r.reason, r.created_at, r.status,
		       n.id, n.title, n.folder_path, n.slug, (n.hidden_at IS NOT NULL),
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
			folder  string
			slug    string
		)
		if err := rows.Scan(&rep.ID, &rep.Reason, &created, &rep.Status,
			&rep.NoteID, &rep.NoteTitle, &folder, &slug, &rep.NoteHidden,
			&rep.AuthorHandle, &rep.ReporterID, &rep.ReporterHandle); err != nil {
			s.renderError(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		rep.CreatedRel = relativeTime(created)
		rep.NoteURL = noteURL(rep.AuthorHandle, folder, slug)
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
	noteID, err := strconv.ParseInt(r.PostFormValue("note_id"), 10, 64)
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
			`UPDATE reports SET status = 'resolved' WHERE note_id = ?`, noteID); err != nil {
			log.Printf("admin hide: resolve reports for note %d: %v", noteID, err)
		}
	}
	http.Redirect(w, r, "/admin/reports", http.StatusSeeOther)
}

func setHidden(ctx context.Context, db *sql.DB, noteID int64, hide bool) error {
	if hide {
		_, err := db.ExecContext(ctx,
			`UPDATE notes SET hidden_at = ? WHERE id = ?`, time.Now().Unix(), noteID)
		return err
	}
	_, err := db.ExecContext(ctx,
		`UPDATE notes SET hidden_at = NULL WHERE id = ?`, noteID)
	return err
}
