package handlers

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/google/uuid"
)

var allowedImageTypes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/webp": true,
}

// GetNoteImage serves the stored image blob for a note. 404 if none, or if
// the note is deleted, or hidden from this viewer — mirrors GetNote's gate.
func (s *Server) GetNoteImage(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var (
		blob        []byte
		contentType string
		authorID    uuid.UUID
		hiddenAt    sql.NullInt64
		deletedAt   sql.NullInt64
		publishedAt sql.NullInt64
	)
	err = s.DB.QueryRowContext(r.Context(), `
		SELECT i.image, i.content_type, n.author_id, n.hidden_at, n.deleted_at, n.published_at
		FROM note_images i
		JOIN notes n ON n.id = i.note_id
		WHERE i.note_id = $1`, id,
	).Scan(&blob, &contentType, &authorID, &hiddenAt, &deletedAt, &publishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if deletedAt.Valid {
		http.NotFound(w, r)
		return
	}
	if hiddenAt.Valid || !publishedAt.Valid {
		viewer, _ := s.Auth.CurrentUser(r)
		isAuthor := viewer != nil && viewer.ID == authorID
		if !isAuthor && !s.IsAdmin(viewer) {
			http.NotFound(w, r)
			return
		}
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(blob)
}

// PostNoteImage is the editor's async image upload: store/replace the note's
// single image and return its URL as JSON. Author-only.
func (s *Server) PostNoteImage(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	noteID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.Error(w, "bad id", http.StatusNotFound)
		return
	}
	var authorID uuid.UUID
	err = s.DB.QueryRowContext(r.Context(), `SELECT author_id FROM notes WHERE id = $1`, noteID).Scan(&authorID)
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
	if err := s.saveNoteImage(r, noteID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// saveNoteImage silently skips unrecognised types; treat "nothing stored" as
	// a client error so the editor can surface it.
	if !noteHasImage(r, s.DB, noteID) {
		http.Error(w, "unsupported or missing image", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	fmt.Fprintf(w, `{"url":%q}`, "/api/notes/"+noteID.String()+"/image")
}

// saveNoteImage reads the optional "image" multipart field and, if present
// and its actual bytes sniff as a recognised image type, stores/replaces it
// for noteID. Sniffing (not the client-supplied Content-Type header) decides
// what gets stored, since the header is attacker-controlled.
func (s *Server) saveNoteImage(r *http.Request, noteID uuid.UUID) error {
	file, _, err := r.FormFile("image")
	if err != nil {
		return nil // no file submitted — not an error
	}
	defer file.Close()

	blob, err := io.ReadAll(file)
	if err != nil {
		return err
	}
	if len(blob) == 0 {
		return nil
	}
	contentType := http.DetectContentType(blob)
	if !allowedImageTypes[contentType] {
		return nil
	}
	_, err = s.DB.ExecContext(r.Context(), `
		INSERT INTO note_images(note_id, image, content_type, created_at)
		VALUES($1, $2, $3, $4)
		ON CONFLICT (note_id) DO UPDATE SET image = $2, content_type = $3, created_at = $4`,
		noteID, blob, contentType, nowUnix(),
	)
	return err
}

func noteHasImage(r *http.Request, db *sql.DB, noteID uuid.UUID) bool {
	var exists bool
	db.QueryRowContext(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM note_images WHERE note_id = $1)`, noteID,
	).Scan(&exists)
	return exists
}
