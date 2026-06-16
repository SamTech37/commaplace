package handlers

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

// PostReport accepts a report from any signed-in user. The admin sees it
// on /admin/reports. Returns a small "thanks" fragment for HTMX.
func (s *Server) PostReport(w http.ResponseWriter, r *http.Request) {
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
	reason := strings.TrimSpace(r.PostFormValue("reason"))
	if reason == "" {
		reason = "(no reason given)"
	}
	if len(reason) > 500 {
		reason = reason[:500]
	}
	if _, err := s.DB.ExecContext(r.Context(), `
		INSERT INTO reports(note_id, reporter_id, reason, created_at, status)
		VALUES($1, $2, $3, $4, 'open')`,
		noteID, u.ID, reason, nowUnix(),
	); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Best-effort email to admin. Skip silently in dev mode.
	if s.AdminHandle != "" && s.Auth.Mailer != nil {
		var adminEmail string
		_ = s.DB.QueryRowContext(r.Context(),
			`SELECT email FROM users WHERE handle = $1`, s.AdminHandle).Scan(&adminEmail)
		if adminEmail != "" {
			_ = s.Auth.Mailer.Send(adminEmail, emailCfg.ReportSubj,
				fmt.Sprintf("Note id %s reported by @%s.\nReason: %s\n",
					noteID, u.Handle, reason))
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(`<span class="report-thanks">Thanks — admin will review.</span>`))
}
