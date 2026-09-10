package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	"commonplace/internal/auth"
)

// handleRE mirrors the URL-safe handle shape: lowercase alphanumerics and
// interior hyphens, 2–30 chars. Matches how handles are minted from emails
// (auth.handleFromEmail) so a changed handle is never something the router
// or profile URLs can't represent.
var handleRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,28}[a-z0-9]$`)

// PostHandleSetting lets a logged-in user rename their own @handle. Links
// survive because edges resolve by UUID (links.resolved_target_id), not by
// handle — see plan.md. Uniqueness is case-folded via handle_ci.
func (s *Server) PostHandleSetting(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	next := strings.ToLower(strings.TrimSpace(r.FormValue("handle")))
	if next == u.Handle {
		http.Redirect(w, r, "/"+u.Handle, http.StatusSeeOther)
		return
	}
	if !handleRE.MatchString(next) {
		s.renderError(w, r, http.StatusBadRequest, "用戶名只能用小寫英數字與連字號（2–30 字），不能以連字號開頭或結尾。")
		return
	}
	if auth.IsReservedHandle(next) {
		s.renderError(w, r, http.StatusBadRequest, "這個用戶名是系統保留字，換一個吧。")
		return
	}

	var taken bool
	if err := s.DB.QueryRowContext(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM users WHERE handle_ci = $1 AND id <> $2)`,
		next, u.ID,
	).Scan(&taken); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if taken {
		s.renderError(w, r, http.StatusConflict, "這個用戶名已經有人用了。")
		return
	}

	if _, err := s.DB.ExecContext(r.Context(),
		`UPDATE users SET handle = $1, handle_ci = $1 WHERE id = $2`, next, u.ID,
	); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	http.Redirect(w, r, "/"+next, http.StatusSeeOther)
}

const (
	profileTitleMax        = 80
	profileHomeMax         = 1200
	profileShowcaseBodyMax = 700
	profileShowcaseRefMax  = 120
)

// PostProfileSetting lets a user arrange their public profile as a doorway:
// a title, a short Markdown note, and ordered showcase slots.
func (s *Server) PostProfileSetting(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "無法讀取布置。", http.StatusBadRequest)
		return
	}
	title := strings.TrimSpace(r.PostFormValue("profile_title"))
	bodyMD := strings.TrimSpace(r.PostFormValue("profile_bio"))
	if utf8.RuneCountInString(title) > profileTitleMax {
		s.renderError(w, r, http.StatusBadRequest, "門牌太長了。")
		return
	}
	if utf8.RuneCountInString(bodyMD) > profileHomeMax {
		s.renderError(w, r, http.StatusBadRequest, "小屋導覽太長了。")
		return
	}
	showcases, err := profileShowcasesFromForm(r)
	if err != nil {
		s.renderError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	showcaseJSON, err := json.Marshal(showcases)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	topicInput := strings.Join(profileTopicTags(r.PostFormValue("profile_topics")), ",")
	if topicInput == "" {
		topicInput = strings.Join(profileShowcaseTags(showcases), ",")
	}

	if _, err := s.DB.ExecContext(r.Context(),
		`UPDATE users SET profile_title = $1, profile_bio = $2, profile_topic_tags = $3, profile_showcases = $4 WHERE id = $5`,
		title, bodyMD, topicInput, string(showcaseJSON), u.ID,
	); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	http.Redirect(w, r, "/"+u.Handle+"#profile-home", http.StatusSeeOther)
}

func profileShowcasesFromForm(r *http.Request) ([]profileShowcase, error) {
	types := r.PostForm["showcase_type"]
	titles := r.PostForm["showcase_title"]
	refs := r.PostForm["showcase_ref"]
	bodies := r.PostForm["showcase_body"]
	raw := make([]profileShowcase, 0, min(len(types), profileShowcaseLimit))
	for i := 0; i < len(types) && len(raw) < profileShowcaseLimit; i++ {
		item := profileShowcase{
			Type:   formValueAt(types, i),
			Title:  formValueAt(titles, i),
			Ref:    formValueAt(refs, i),
			BodyMD: formValueAt(bodies, i),
		}
		raw = append(raw, item)
	}
	showcases := normalizeProfileShowcases(raw)
	for _, item := range showcases {
		if utf8.RuneCountInString(item.Title) > profileTitleMax {
			return nil, errors.New("展示櫃標題太長了。")
		}
		if utf8.RuneCountInString(item.Ref) > profileShowcaseRefMax {
			return nil, errors.New("展示櫃引用太長了。")
		}
		if utf8.RuneCountInString(item.BodyMD) > profileShowcaseBodyMax {
			return nil, errors.New("展示櫃補充文字太長了。")
		}
	}
	return showcases, nil
}

func formValueAt(values []string, index int) string {
	if index < 0 || index >= len(values) {
		return ""
	}
	return values[index]
}

func profileShowcaseTags(showcases []profileShowcase) []string {
	tags := make([]string, 0, len(showcases))
	seen := map[string]bool{}
	for _, item := range showcases {
		if item.Type != profileShowcaseTag || item.Ref == "" || seen[item.Ref] {
			continue
		}
		seen[item.Ref] = true
		tags = append(tags, item.Ref)
	}
	return tags
}

// PostThemeSetting persists a logged-in user's theme preference. Visitors
// get 204 — they store the choice in localStorage client-side.
func (s *Server) PostThemeSetting(w http.ResponseWriter, r *http.Request) {
	v := r.FormValue("theme")
	if v != "auto" && v != "light" && v != "dark" {
		http.Error(w, "bad theme", http.StatusBadRequest)
		return
	}
	u, _ := s.Auth.CurrentUser(r)
	if u == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if _, err := s.DB.ExecContext(r.Context(),
		`UPDATE users SET theme = $1 WHERE id = $2`, v, u.ID,
	); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
