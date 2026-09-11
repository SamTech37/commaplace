package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"commonplace/internal/auth"
	"github.com/google/uuid"
)

type deskPaneContext struct{}

type deskWindow struct {
	Key    string  `json:"key"`
	Kind   string  `json:"kind"`
	Ref    string  `json:"ref,omitempty"`
	Query  string  `json:"query,omitempty"`
	Width  float64 `json:"width"`
	Scroll float64 `json:"scroll"`
	Title  string  `json:"title,omitempty"`
	URL    string  `json:"url,omitempty"`
}
type deskState struct {
	Windows    []deskWindow `json:"windows"`
	Active     string       `json:"active"`
	ScrollLeft float64      `json:"scrollLeft"`
}
type deskEnvelope struct {
	Revision int64     `json:"revision"`
	State    deskState `json:"state"`
}

func deskJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func (s *Server) deskUser(w http.ResponseWriter, r *http.Request) *auth.User {
	u, err := s.Auth.CurrentUser(r)
	if err != nil {
		http.Error(w, "無法讀取登入狀態", 500)
		return nil
	}
	if u == nil {
		http.Error(w, "請重新登入", 401)
	}
	return u
}

func normalizeDeskWindow(p *deskWindow) error {
	p.Title, p.URL = "", ""
	if len(p.Query) > 300 || len(p.Ref) > 200 {
		return errors.New("查詢過長")
	}
	switch p.Kind {
	case "note", "edit":
		id, err := uuid.Parse(p.Ref)
		if err != nil {
			return errors.New("無效文章")
		}
		p.Ref = id.String()
	case "feed", "following", "saved", "search", "graph":
		p.Ref = ""
	case "profile":
		if p.Ref == "" || strings.ContainsAny(p.Ref, "/\\?#\x00") {
			return errors.New("無效作者")
		}
	case "tag":
		if strings.TrimSpace(p.Ref) == "" {
			return errors.New("無效標籤")
		}
	default:
		return errors.New("無效窗格類型")
	}
	if p.Kind != "feed" && p.Kind != "following" && p.Kind != "search" {
		p.Query = ""
	}
	p.Key = p.Kind + ":" + p.Ref + ":" + p.Query
	if math.IsNaN(p.Width) || math.IsInf(p.Width, 0) || p.Width < 260 || p.Width > 1200 {
		return errors.New("無效欄寬")
	}
	if math.IsNaN(p.Scroll) || math.IsInf(p.Scroll, 0) || p.Scroll < 0 || p.Scroll > 10000000 {
		return errors.New("無效閱讀位置")
	}
	return nil
}

func deskDefaultWidth(kind string) float64 {
	if kind == "edit" {
		return 480
	}
	if kind == "note" {
		return 430
	}
	return 340
}
func newDeskWindow(kind, ref, q string) deskWindow {
	p := deskWindow{Kind: kind, Ref: ref, Query: q, Width: deskDefaultWidth(kind)}
	_ = normalizeDeskWindow(&p)
	return p
}

// hydrateDesk performs one permission query for all note panes, including restores.
// Titles and canonical URLs always come from current database rows, never saved input.
func (s *Server) hydrateDesk(ctx context.Context, u *auth.User, state *deskState) error {
	ids := []string{}
	for i := range state.Windows {
		p := &state.Windows[i]
		if err := normalizeDeskWindow(p); err != nil {
			return err
		}
		if p.Kind == "note" || p.Kind == "edit" {
			ids = append(ids, p.Ref)
		}
	}
	type noteMeta struct {
		title, slug, handle string
		own                 bool
	}
	notes := map[string]noteMeta{}
	if len(ids) > 0 {
		rows, err := s.DB.QueryContext(ctx, `SELECT n.id,n.title,n.slug,u.handle,n.author_id=$1 FROM notes n JOIN users u ON u.id=n.author_id WHERE n.id=ANY($2::uuid[]) AND n.deleted_at IS NULL AND n.hidden_at IS NULL AND (n.author_id=$1 OR n.published_at IS NOT NULL)`, u.ID, ids)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			var n noteMeta
			if err := rows.Scan(&id, &n.title, &n.slug, &n.handle, &n.own); err != nil {
				return err
			}
			notes[id] = n
		}
		if err := rows.Err(); err != nil {
			return err
		}
	}
	result := make([]deskWindow, 0, len(state.Windows))
	seen := map[string]bool{}
	for _, p := range state.Windows {
		if seen[p.Key] {
			continue
		}
		seen[p.Key] = true
		switch p.Kind {
		case "note", "edit":
			n, ok := notes[p.Ref]
			if !ok || (p.Kind == "edit" && !n.own) {
				continue
			}
			p.Title = n.title
			if p.Title == "" {
				p.Title = "未命名文章"
			}
			p.URL = noteURL(n.handle, n.slug)
			if p.Kind == "edit" {
				p.URL = "/edit/" + p.Ref
			}
		case "feed", "following":
			p.Title = "探索 Feed"
			v := url.Values{}
			if p.Kind == "following" {
				p.Title = "追蹤中"
				v.Set("tab", "following")
			}
			if p.Query != "" {
				v.Set("tag", p.Query)
			}
			p.URL = "/feed"
			if len(v) > 0 {
				p.URL += "?" + v.Encode()
			}
		case "saved":
			p.Title = "我的收藏"
			p.URL = "/me/saved"
		case "search":
			p.Title = "搜尋：" + p.Query
			p.URL = "/search?q=" + url.QueryEscape(p.Query)
		case "graph":
			p.Title = "圖譜"
			p.URL = "/graph"
		case "profile":
			p.Title = "@" + p.Ref
			p.URL = "/" + url.PathEscape(p.Ref)
		case "tag":
			p.Title = "#" + p.Ref
			p.URL = "/tag/" + url.PathEscape(p.Ref)
		}
		result = append(result, p)
	}
	state.Windows = result
	active := false
	for _, p := range result {
		active = active || p.Key == state.Active
	}
	if !active {
		state.Active = ""
		if len(result) > 0 {
			state.Active = result[0].Key
		}
	}
	return nil
}

func (s *Server) GetDesk(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	s.renderPage(w, r, pageTitle("工作桌"), "page-desk", nil, deskPage(u.ID.String(), u.Handle))
}

func (s *Server) GetDeskState(w http.ResponseWriter, r *http.Request) {
	u := s.deskUser(w, r)
	if u == nil {
		return
	}
	var raw []byte
	env := deskEnvelope{}
	err := s.DB.QueryRowContext(r.Context(), `SELECT revision,state FROM desk_states WHERE user_id=$1`, u.ID).Scan(&env.Revision, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		env.State.Windows = []deskWindow{}
	} else if err != nil {
		http.Error(w, "無法讀取工作桌", 500)
		return
	} else if json.Unmarshal(raw, &env.State) != nil {
		http.Error(w, "無法讀取工作桌", 500)
		return
	}
	if err := s.hydrateDesk(r.Context(), u, &env.State); err != nil {
		http.Error(w, "無法讀取文章", 500)
		return
	}
	deskJSON(w, 200, env)
}

func (s *Server) PutDeskState(w http.ResponseWriter, r *http.Request) {
	u := s.deskUser(w, r)
	if u == nil {
		return
	}
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		http.Error(w, "需要 JSON", 415)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var env deskEnvelope
	if err := dec.Decode(&env); err != nil {
		http.Error(w, "無效工作桌", 400)
		return
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		http.Error(w, "無效工作桌", 400)
		return
	}
	if env.Revision < 0 || len(env.State.Windows) > 64 || env.State.ScrollLeft < 0 || env.State.ScrollLeft > 100000 || math.IsNaN(env.State.ScrollLeft) || math.IsInf(env.State.ScrollLeft, 0) {
		http.Error(w, "工作桌超出範圍", 400)
		return
	}
	for i := range env.State.Windows {
		if err := normalizeDeskWindow(&env.State.Windows[i]); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
	}
	if err := s.hydrateDesk(r.Context(), u, &env.State); err != nil {
		http.Error(w, "無法驗證工作桌", 500)
		return
	}
	// Content and display metadata are not duplicated in presentation storage.
	for i := range env.State.Windows {
		env.State.Windows[i].Title = ""
		env.State.Windows[i].URL = ""
	}
	raw, _ := json.Marshal(env.State)
	var revision int64
	var err error
	if env.Revision == 0 {
		err = s.DB.QueryRowContext(r.Context(), `INSERT INTO desk_states(user_id,revision,state,updated_at) VALUES($1,1,$2,$3) ON CONFLICT(user_id) DO NOTHING RETURNING revision`, u.ID, string(raw), nowUnix()).Scan(&revision)
	} else {
		err = s.DB.QueryRowContext(r.Context(), `UPDATE desk_states SET state=$2,revision=revision+1,updated_at=$3 WHERE user_id=$1 AND revision=$4 RETURNING revision`, u.ID, string(raw), nowUnix(), env.Revision).Scan(&revision)
	}
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "另一個工作桌已更新，請重新載入桌面狀態。", 409)
		return
	}
	if err != nil {
		http.Error(w, "無法儲存工作桌", 500)
		return
	}
	deskJSON(w, 200, map[string]int64{"revision": revision})
}

func (s *Server) deskResolve(ctx context.Context, u *auth.User, raw string) (deskWindow, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") || strings.ContainsAny(raw, "\\\x00") {
		return deskWindow{}, errors.New("無效連結")
	}
	p := newDeskWindow("feed", "", "")
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	switch parsed.Path {
	case "/feed":
		if parsed.Query().Get("tab") == "following" {
			p.Kind = "following"
		}
		p.Query = parsed.Query().Get("tag")
	case "/me/saved":
		p.Kind = "saved"
	case "/graph":
		p.Kind = "graph"
	case "/search":
		p.Kind = "search"
		p.Query = parsed.Query().Get("q")
	default:
		if len(parts) == 2 && parts[0] == "tag" {
			p.Kind = "tag"
			p.Ref = parts[1]
		} else if len(parts) == 2 && parts[0] == "edit" {
			p.Kind = "edit"
			p.Ref = parts[1]
		} else if len(parts) == 1 && !auth.IsReservedHandle(strings.ToLower(parts[0])) {
			p.Kind = "profile"
			p.Ref = parts[0]
		} else if len(parts) == 2 && !auth.IsReservedHandle(strings.ToLower(parts[0])) {
			p.Kind = "note"
			err = s.DB.QueryRowContext(ctx, `SELECT n.id FROM notes n JOIN users u ON u.id=n.author_id WHERE u.handle_ci=$1 AND n.slug_ci=$2 AND n.deleted_at IS NULL AND n.hidden_at IS NULL AND (n.author_id=$3 OR n.published_at IS NOT NULL)`, strings.ToLower(parts[0]), strings.ToLower(parts[1]), u.ID).Scan(&p.Ref)
			if err != nil {
				return p, errors.New("文章不存在或無權閱讀")
			}
		} else {
			return p, errors.New("此連結請在原頁面開啟")
		}
	}
	p.Width = deskDefaultWidth(p.Kind)
	state := deskState{Windows: []deskWindow{p}}
	if err := s.hydrateDesk(ctx, u, &state); err != nil {
		return p, err
	}
	if len(state.Windows) == 0 {
		return p, errors.New("文章不存在或無權閱讀")
	}
	return state.Windows[0], nil
}

func (s *Server) GetDeskResolve(w http.ResponseWriter, r *http.Request) {
	u := s.deskUser(w, r)
	if u == nil {
		return
	}
	p, err := s.deskResolve(r.Context(), u, r.URL.Query().Get("url"))
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	deskJSON(w, 200, p)
}

func (s *Server) PostDeskDraft(w http.ResponseWriter, r *http.Request) {
	u := s.deskUser(w, r)
	if u == nil {
		return
	}
	id, err := s.createDraft(r.Context(), u.ID)
	if err != nil {
		http.Error(w, "無法建立草稿", 500)
		return
	}
	state := deskState{Windows: []deskWindow{newDeskWindow("edit", id.String(), "")}}
	if err := s.hydrateDesk(r.Context(), u, &state); err != nil {
		http.Error(w, "無法讀取草稿", 500)
		return
	}
	deskJSON(w, 201, state.Windows[0])
}

func (s *Server) GetDeskNotes(w http.ResponseWriter, r *http.Request) {
	u := s.deskUser(w, r)
	if u == nil {
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) > 300 {
		http.Error(w, "查詢過長", 400)
		return
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 || offset > 100000 {
		http.Error(w, "無效頁碼", 400)
		return
	}
	rows, err := s.DB.QueryContext(r.Context(), `SELECT id,title,slug,published_at IS NOT NULL FROM notes WHERE author_id=$1 AND published_at IS NOT NULL AND deleted_at IS NULL AND hidden_at IS NULL AND ($2='' OR title ILIKE '%'||$2||'%') ORDER BY updated_at DESC,id DESC LIMIT 51 OFFSET $3`, u.ID, q, offset)
	if err != nil {
		http.Error(w, "無法讀取我的空間", 500)
		return
	}
	defer rows.Close()
	type item struct {
		ID        string `json:"id"`
		Title     string `json:"title"`
		URL       string `json:"url"`
		Published bool   `json:"published"`
	}
	items := []item{}
	for rows.Next() {
		var n item
		var slug string
		if err := rows.Scan(&n.ID, &n.Title, &slug, &n.Published); err != nil {
			http.Error(w, "無法讀取我的空間", 500)
			return
		}
		if n.Title == "" {
			n.Title = "未命名文章"
		}
		n.URL = noteURL(u.Handle, slug)
		items = append(items, n)
	}
	if rows.Err() != nil {
		http.Error(w, "無法讀取我的空間", 500)
		return
	}
	more := len(items) > 50
	if more {
		items = items[:50]
	}
	deskJSON(w, 200, map[string]any{"notes": items, "more": more})
}

func (s *Server) GetDeskPane(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	p := newDeskWindow(r.URL.Query().Get("kind"), r.URL.Query().Get("ref"), r.URL.Query().Get("q"))
	state := deskState{Windows: []deskWindow{p}}
	if err := s.hydrateDesk(r.Context(), u, &state); err != nil || len(state.Windows) != 1 {
		http.Error(w, "這篇文章已移除或無權閱讀。", 404)
		return
	}
	p = state.Windows[0]
	target, _ := url.Parse(p.URL)
	r = r.Clone(context.WithValue(r.Context(), deskPaneContext{}, true))
	r.URL = target
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	switch p.Kind {
	case "feed", "following":
		s.GetFeed(w, r)
	case "saved":
		s.GetSaved(w, r)
	case "search":
		s.GetSearch(w, r)
	case "graph":
		s.GetGraph(w, r)
	case "profile":
		r.SetPathValue("user", p.Ref)
		s.GetProfile(w, r)
	case "tag":
		r.SetPathValue("tag", p.Ref)
		s.GetTagPage(w, r)
	case "edit":
		r.SetPathValue("id", p.Ref)
		s.GetEdit(w, r)
	case "note":
		parts := strings.Split(strings.Trim(target.Path, "/"), "/")
		r.SetPathValue("user", parts[0])
		r.SetPathValue("slug", parts[1])
		s.GetNote(w, r)
	default:
		http.Error(w, fmt.Sprintf("未知窗格 %q", p.Kind), 400)
	}
}

func (s *Server) GetDeskBookmarks(w http.ResponseWriter, r *http.Request) {
	u := s.deskUser(w, r)
	if u == nil {
		return
	}
	ids := strings.Split(r.URL.Query().Get("ids"), ",")
	if len(ids) > 50 {
		http.Error(w, "過多文章", 400)
		return
	}
	for _, id := range ids {
		if _, err := uuid.Parse(id); err != nil {
			http.Error(w, "無效文章", 400)
			return
		}
	}
	rows, err := s.DB.QueryContext(r.Context(), `SELECT n.id,s.user_id IS NOT NULL FROM notes n LEFT JOIN saves s ON s.note_id=n.id AND s.user_id=$1 WHERE n.id=ANY($2::uuid[]) AND n.hidden_at IS NULL AND n.deleted_at IS NULL AND (n.author_id=$1 OR n.published_at IS NOT NULL)`, u.ID, ids)
	if err != nil {
		http.Error(w, "無法讀取收藏", 500)
		return
	}
	defer rows.Close()
	result := map[string]bool{}
	for rows.Next() {
		var id string
		var saved bool
		if err := rows.Scan(&id, &saved); err != nil {
			http.Error(w, "無法讀取收藏", 500)
			return
		}
		result[id] = saved
	}
	if rows.Err() != nil {
		http.Error(w, "無法讀取收藏", 500)
		return
	}
	deskJSON(w, 200, result)
}
