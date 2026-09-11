package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"strings"

	"commonplace/internal/markdown"
	"github.com/google/uuid"
)

type spaceBlock struct {
	Type         string `json:"type"`
	ID           string `json:"id,omitempty"`
	Text         string `json:"text,omitempty"`
	Distribution string `json:"distribution,omitempty"`
}
type spaceDocument struct {
	Title  string       `json:"title"`
	Body   string       `json:"body"`
	Blocks []spaceBlock `json:"blocks"`
}
type spaceEnvelope struct {
	Revision int64         `json:"revision"`
	Document spaceDocument `json:"document"`
	Notes    []spaceNote   `json:"notes,omitempty"`
}
type spaceNote struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	URL          string `json:"url"`
	Published    bool   `json:"published"`
	Distribution string `json:"distribution"`
}

func spaceIDs(doc spaceDocument) ([]string, error) {
	if len(doc.Blocks) > 200 || len(doc.Title) > 500 || len(doc.Body) > 1<<20 {
		return nil, errors.New("內容過長")
	}
	ids := []string{}
	seen := map[string]bool{}
	for _, b := range doc.Blocks {
		switch b.Type {
		case "text":
			if len(b.Text) > 1<<20 {
				return nil, errors.New("內容過長")
			}
		case "note":
			if _, err := uuid.Parse(b.ID); err != nil || seen[b.ID] {
				return nil, errors.New("無效或重複的文章")
			}
			if b.Distribution != "" && !validDistribution(b.Distribution) {
				return nil, errors.New("無效的公開設定")
			}
			seen[b.ID] = true
			ids = append(ids, b.ID)
		default:
			return nil, errors.New("無效的文章項目")
		}
	}
	return ids, nil
}

func (s *Server) GetSpace(w http.ResponseWriter, r *http.Request) {
	u := s.deskUser(w, r)
	if u == nil {
		return
	}
	env := spaceEnvelope{Document: spaceDocument{Title: "我的空間", Blocks: []spaceBlock{}}}
	var raw []byte
	err := s.DB.QueryRowContext(r.Context(), `SELECT revision,document FROM user_spaces WHERE user_id=$1`, u.ID).Scan(&env.Revision, &raw)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "無法讀取我的空間", 500)
		return
	}
	if len(raw) > 0 {
		if err = json.Unmarshal(raw, &env.Document); err != nil {
			http.Error(w, "無法讀取我的空間", 500)
			return
		}
	}
	ids, _ := spaceIDs(env.Document)
	rows, err := s.DB.QueryContext(r.Context(), `SELECT id,COALESCE(draft_title,title),slug,published_at IS NOT NULL,CASE WHEN published_at IS NULL THEN COALESCE(draft_distribution,distribution) ELSE distribution END FROM notes WHERE author_id=$1 AND id=ANY($2::uuid[]) AND deleted_at IS NULL AND hidden_at IS NULL`, u.ID, ids)
	if err != nil {
		http.Error(w, "無法讀取文章", 500)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var n spaceNote
		var slug string
		if err = rows.Scan(&n.ID, &n.Title, &slug, &n.Published, &n.Distribution); err != nil {
			http.Error(w, "無法讀取文章", 500)
			return
		}
		n.URL = noteURL(u.Handle, slug)
		env.Notes = append(env.Notes, n)
	}
	if rows.Err() != nil {
		http.Error(w, "無法讀取文章", 500)
		return
	}
	deskJSON(w, 200, env)
}

func (s *Server) PutSpace(w http.ResponseWriter, r *http.Request) {
	u := s.deskUser(w, r)
	if u == nil {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	var env spaceEnvelope
	if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
		http.Error(w, "無效的內容", 400)
		return
	}
	ids, err := spaceIDs(env.Document)
	if err != nil || env.Revision < 0 {
		http.Error(w, "無效的內容", 400)
		return
	}
	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, "無法儲存", 500)
		return
	}
	defer tx.Rollback()
	var count int
	err = tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM notes WHERE id=ANY($1::uuid[]) AND author_id=$2 AND deleted_at IS NULL AND hidden_at IS NULL`, ids, u.ID).Scan(&count)
	if err != nil {
		http.Error(w, "無法儲存", 500)
		return
	}
	if count != len(ids) {
		http.Error(w, "文章已移除或無權編輯", 422)
		return
	}
	raw, _ := json.Marshal(env.Document)
	var revision int64
	if env.Revision == 0 {
		err = tx.QueryRowContext(r.Context(), `INSERT INTO user_spaces(user_id,revision,document,updated_at) VALUES($1,1,$2,$3) ON CONFLICT DO NOTHING RETURNING revision`, u.ID, string(raw), nowUnix()).Scan(&revision)
	} else {
		err = tx.QueryRowContext(r.Context(), `UPDATE user_spaces SET document=$2,revision=revision+1,updated_at=$3 WHERE user_id=$1 AND revision=$4 RETURNING revision`, u.ID, string(raw), nowUnix(), env.Revision).Scan(&revision)
	}
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "另一個編輯器已更新，請重新載入後比對內容。", 409)
		return
	}
	if err != nil {
		http.Error(w, "無法儲存", 500)
		return
	}
	if err = tx.Commit(); err != nil {
		http.Error(w, "無法儲存", 500)
		return
	}
	deskJSON(w, 200, map[string]int64{"revision": revision})
}

func (s *Server) PublishSpace(w http.ResponseWriter, r *http.Request) {
	u := s.deskUser(w, r)
	if u == nil {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req struct {
		Revision   int64    `json:"revision"`
		PublishIDs []string `json:"publishIDs"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		http.Error(w, "無效的內容", 400)
		return
	}
	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, "無法發布", 500)
		return
	}
	defer tx.Rollback()
	var raw []byte
	var revision int64
	err = tx.QueryRowContext(r.Context(), `SELECT document,revision FROM user_spaces WHERE user_id=$1 FOR UPDATE`, u.ID).Scan(&raw, &revision)
	if err != nil {
		http.Error(w, "請先儲存", 422)
		return
	}
	if req.Revision != revision {
		http.Error(w, "另一個編輯器已更新，請重新載入。", 409)
		return
	}
	var doc spaceDocument
	if json.Unmarshal(raw, &doc) != nil || strings.TrimSpace(doc.Title) == "" {
		http.Error(w, "請先填寫標題", 422)
		return
	}
	ids, err := spaceIDs(doc)
	if err != nil {
		http.Error(w, "無效的內容", 422)
		return
	}
	rows, err := tx.QueryContext(r.Context(), `SELECT id,published_at IS NOT NULL FROM notes WHERE id=ANY($1::uuid[]) AND author_id=$2 AND deleted_at IS NULL AND hidden_at IS NULL ORDER BY id FOR UPDATE`, ids, u.ID)
	if err != nil {
		http.Error(w, "無法發布", 500)
		return
	}
	available := map[string]bool{}
	for rows.Next() {
		var id string
		var published bool
		if rows.Scan(&id, &published) != nil {
			rows.Close()
			http.Error(w, "無法發布", 500)
			return
		}
		available[id] = published
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		http.Error(w, "無法發布", 500)
		return
	}
	if len(available) != len(ids) {
		http.Error(w, "文章已移除，請先調整主文章", 422)
		return
	}
	selected := map[string]bool{}
	for _, id := range req.PublishIDs {
		if _, ok := available[id]; !ok {
			http.Error(w, "無效的文章", 422)
			return
		}
		selected[id] = true
	}
	pub := spaceDocument{Title: doc.Title, Body: doc.Body, Blocks: []spaceBlock{}}
	for i, b := range doc.Blocks {
		if b.Type == "note" {
			if !available[b.ID] && !selected[b.ID] {
				continue
			}
			id, _ := uuid.Parse(b.ID)
			if !available[b.ID] {
				if b.Distribution != "" {
					_, err = tx.ExecContext(r.Context(), `UPDATE notes SET draft_distribution=$2 WHERE id=$1`, id, b.Distribution)
				}
				if err == nil {
					_, err = publishDocument(r.Context(), tx, id, u.ID, u.Handle)
				}
			} else if b.Distribution != "" {
				// Changing reach must never publish unfinished edits to this child.
				_, err = tx.ExecContext(r.Context(), `UPDATE notes SET distribution=$2,draft_distribution=NULL,edit_version=edit_version+1 WHERE id=$1`, id, b.Distribution)
			}
			if err != nil {
				status := 500
				if errors.Is(err, errPublishTitle) {
					status = 422
				}
				http.Error(w, "請先儲存文章標題再發布", status)
				return
			}
			doc.Blocks[i].Distribution = ""
			b.Distribution = ""
		}
		pub.Blocks = append(pub.Blocks, b)
	}
	publicRaw, _ := json.Marshal(pub)
	privateRaw, _ := json.Marshal(doc)
	_, err = tx.ExecContext(r.Context(), `UPDATE user_spaces SET document=$2,published=$3,revision=revision+1,updated_at=$4 WHERE user_id=$1`, u.ID, string(privateRaw), string(publicRaw), nowUnix())
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		http.Error(w, "無法發布", 500)
		return
	}
	deskJSON(w, 200, map[string]string{"url": "/" + u.Handle})
}

type readingBlock struct {
	Title, URL string
	HTML       template.HTML
}
type SpaceReadingProps struct {
	Handle, Title       string
	Body                template.HTML
	Blocks              []readingBlock
	IsSelf              bool
	AuthorID            uuid.UUID
	LoggedIn, Following bool
}

func (s *Server) loadSpaceReading(ctx context.Context, user uuid.UUID, handle string) (SpaceReadingProps, bool, error) {
	p := SpaceReadingProps{Handle: handle}
	var raw []byte
	err := s.DB.QueryRowContext(ctx, `SELECT published FROM user_spaces WHERE user_id=$1 AND published IS NOT NULL`, user).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return p, false, nil
	}
	if err != nil {
		return p, false, err
	}
	var doc spaceDocument
	if err = json.Unmarshal(raw, &doc); err != nil {
		return p, false, err
	}
	ids, err := spaceIDs(doc)
	if err != nil {
		return p, false, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id,title,slug FROM notes WHERE id=ANY($1::uuid[]) AND author_id=$2 AND published_at IS NOT NULL AND hidden_at IS NULL AND deleted_at IS NULL`, ids, user)
	if err != nil {
		return p, false, err
	}
	defer rows.Close()
	notes := map[string]readingBlock{}
	for rows.Next() {
		var id, title, slug string
		if err = rows.Scan(&id, &title, &slug); err != nil {
			return p, false, err
		}
		notes[id] = readingBlock{Title: title, URL: noteURL(handle, slug) + "?space=" + handle}
	}
	if err = rows.Err(); err != nil {
		return p, false, err
	}
	all := doc.Body
	for _, b := range doc.Blocks {
		all += "\n\n" + b.Text
	}
	resolver := s.buildResolver(ctx, handle, markdown.Extract(all))
	embed := s.buildEmbedResolver(ctx, handle, 2)
	render := func(text string) template.HTML {
		html, _ := markdown.Render(text, handle, resolver, embed)
		return template.HTML(html)
	}
	p.Title = doc.Title
	p.Body = render(doc.Body)
	for _, b := range doc.Blocks {
		if b.Type == "text" {
			p.Blocks = append(p.Blocks, readingBlock{HTML: render(b.Text)})
		} else if n, ok := notes[b.ID]; ok {
			p.Blocks = append(p.Blocks, n)
		}
	}
	return p, true, nil
}
