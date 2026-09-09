package handlers

import (
	"net/http"
	"strings"

	"github.com/google/uuid"
)

// PostManageNotes applies one atomic operation to owned, non-moderated notes.
// Entire-result selection is explicit and always constrained by the active tab.
func (s *Server) PostManageNotes(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "無法讀取選取項目。", http.StatusBadRequest)
		return
	}
	tab := r.PostForm.Get("tab")
	if tab != "" && tab != "all" && tab != "drafts" {
		http.Error(w, "無效的篩選條件。", http.StatusBadRequest)
		return
	}
	action := r.PostForm.Get("action")
	raw := r.PostForm["ids"]
	all := r.PostForm.Get("scope") == "all"
	if single := r.PostForm.Get("single"); single != "" {
		var id string
		action, id, _ = strings.Cut(single, ":")
		raw, all = []string{id}, false
	}
	if action != "publish" && action != "hide" && action != "delete" {
		http.Error(w, "請選擇操作。", http.StatusBadRequest)
		return
	}
	ids := make([]string, 0, len(raw))
	for _, value := range raw {
		id, err := uuid.Parse(value)
		if err != nil {
			http.Error(w, "無效的文章。", http.StatusBadRequest)
			return
		}
		ids = append(ids, id.String())
	}
	if !all && len(ids) == 0 {
		http.Error(w, "請先選取文章。", http.StatusBadRequest)
		return
	}
	where := `author_id = $1 AND deleted_at IS NULL AND hidden_at IS NULL`
	args := []any{u.ID}
	if all {
		if tab == "drafts" {
			where += ` AND published_at IS NULL`
		}
		if tab == "" {
			where += ` AND published_at IS NOT NULL`
		}
	} else {
		where += ` AND id = ANY($2::uuid[])`
		args = append(args, ids)
	}
	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, "操作失敗，請重試。", 500)
		return
	}
	defer tx.Rollback()
	// Lock the concrete target set before validating and updating it, so a
	// concurrent edit cannot invalidate the publish checks mid-operation.
	rows, err := tx.QueryContext(r.Context(), `SELECT id, title, slug FROM notes WHERE `+where+` FOR UPDATE`, args...)
	if err != nil {
		http.Error(w, "操作失敗，請重試。", 500)
		return
	}
	targets := []string{}
	invalid := false
	for rows.Next() {
		var id, title, slug string
		if err := rows.Scan(&id, &title, &slug); err != nil {
			rows.Close()
			http.Error(w, "操作失敗，請重試。", 500)
			return
		}
		targets = append(targets, id)
		invalid = invalid || strings.TrimSpace(title) == "" || strings.HasPrefix(slug, "draft-")
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		http.Error(w, "操作失敗，請重試。", 500)
		return
	}
	if action == "publish" && invalid {
		s.renderError(w, r, http.StatusUnprocessableEntity, "所選文章包含尚未儲存標題的草稿，請先編輯標題再公開。此次沒有變更任何文章。")
		return
	}
	set := `deleted_at = $1`
	if action == "hide" {
		set = `published_at = NULL`
	}
	if action == "publish" {
		set = `published_at = COALESCE(published_at, $1)`
	}
	// Keep $1 typed even when hiding (the SET clause has no timestamp).
	_, err = tx.ExecContext(r.Context(), `UPDATE notes SET `+set+` WHERE id = ANY($2::uuid[]) AND author_id = $3 AND $1::bigint IS NOT NULL`, nowUnix(), targets, u.ID)
	if err != nil {
		http.Error(w, "操作失敗，請重試。", 500)
		return
	}
	if err := tx.Commit(); err != nil {
		http.Error(w, "操作失敗，請重試。", 500)
		return
	}
	location := "/" + u.Handle
	if tab != "" {
		location += "?tab=" + tab
	}
	http.Redirect(w, r, location, http.StatusSeeOther)
}
