package handlers

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
)

func (s *Server) GetNoteRaw(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		s.renderError(w, r, http.StatusBadRequest, "bad id")
		return
	}
	var (
		body string
		slug string
	)
	err = s.DB.QueryRowContext(r.Context(),
		`SELECT body_md, slug FROM notes WHERE id = ?`, id,
	).Scan(&body, &slug)
	if errors.Is(err, sql.ErrNoRows) {
		s.renderError(w, r, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+slug+`.md"`)
	w.Write([]byte(body))
}
