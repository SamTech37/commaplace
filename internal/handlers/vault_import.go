package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"commonplace/internal/auth"
	"commonplace/internal/markdown"
	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

const maxVaultFiles = 2000
const maxVaultBytes = 100 << 20
const maxVaultFileBytes = 2 << 20

// Bound simultaneous disk/DB work on the small beta instance.
var vaultImportSlots = make(chan struct{}, 2)

type vaultNote struct {
	Path     string    `json:"path"`
	Title    string    `json:"title"`
	Slug     string    `json:"slug"`
	URL      string    `json:"url"`
	Media    int       `json:"media_removed"`
	Warnings []string  `json:"warnings,omitempty"`
	ID       uuid.UUID `json:"id"`
	temp     string
	tags     []string
}

type vaultReport struct {
	Type       string       `json:"type"`
	Total      int          `json:"total"`
	Media      int          `json:"media_removed"`
	Unresolved int          `json:"unresolved_links"`
	ElapsedMS  int64        `json:"elapsed_ms"`
	URL        string       `json:"url"`
	Notes      []*vaultNote `json:"notes"`
}

// The client sends one part per Markdown file, with the vault-relative path as
// the part name. MultipartReader avoids Go's 1000-part form limit. Only one
// bounded note is held in memory; contents are spooled to private temp files.
func readVault(r *http.Request) ([]*vaultNote, func(), error) {
	var notes []*vaultNote
	cleanup := func() {
		for _, n := range notes {
			_ = os.Remove(n.temp)
		}
	}
	mr, err := r.MultipartReader()
	if err != nil {
		return nil, cleanup, fmt.Errorf("無法讀取資料夾")
	}
	seen := map[string]bool{}
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return notes, cleanup, fmt.Errorf("上傳未完成或超過 100 MiB")
		}
		p := strings.ReplaceAll(part.FormName(), "\\", "/")
		if !validVaultPath(p) || part.FileName() == "" {
			return notes, cleanup, fmt.Errorf("無效的 Markdown 路徑：%s", p)
		}
		if len(notes) >= maxVaultFiles {
			return notes, cleanup, fmt.Errorf("一次最多匯入 %d 篇", maxVaultFiles)
		}
		if seen[p] {
			return notes, cleanup, fmt.Errorf("重複路徑：%s", p)
		}
		seen[p] = true
		raw, err := io.ReadAll(io.LimitReader(part, maxVaultFileBytes+1))
		if err != nil || len(raw) > maxVaultFileBytes {
			return notes, cleanup, fmt.Errorf("%s：無法讀取或超過 2 MiB", p)
		}
		if !utf8.Valid(raw) || strings.ContainsRune(string(raw), '\x00') {
			return notes, cleanup, fmt.Errorf("%s：請使用 UTF-8 Markdown", p)
		}
		clean, removed := markdown.StripMedia(string(raw))
		prefix, body := markdown.SplitFrontmatter(clean)
		var fm struct {
			Title string `yaml:"title"`
			Tags  any    `yaml:"tags"`
		}
		if prefix != "" {
			lines := strings.Split(strings.TrimSpace(prefix), "\n")
			if err := yaml.Unmarshal([]byte(strings.Join(lines[1:len(lines)-1], "\n")), &fm); err != nil {
				return notes, cleanup, fmt.Errorf("%s：frontmatter 格式無法解析", p)
			}
		}
		title := strings.TrimSpace(fm.Title)
		if title == "" {
			title = extractH1(body)
		}
		if title == "" {
			title = strings.TrimSuffix(path.Base(p), path.Ext(p))
		}
		f, err := os.CreateTemp("", "comma-vault-*.md")
		if err != nil {
			return notes, cleanup, fmt.Errorf("暫存空間不足")
		}
		var tags []string
		switch value := fm.Tags.(type) {
		case string:
			tags = append(tags, value)
		case []any:
			for _, tag := range value {
				tags = append(tags, fmt.Sprint(tag))
			}
		}
		tags = append(tags, markdown.ExtractInlineTags(body)...)
		n := &vaultNote{Path: p, Title: title, ID: uuid.New(), Media: removed, temp: f.Name(), tags: parseTags(strings.Join(tags, ","))}
		notes = append(notes, n)
		_, err = f.WriteString(prefix + body)
		closeErr := f.Close()
		if err != nil || closeErr != nil {
			return notes, cleanup, fmt.Errorf("暫存寫入失敗")
		}
	}
	if len(notes) == 0 {
		return notes, cleanup, fmt.Errorf("資料夾內沒有 Markdown 檔案")
	}
	sort.Slice(notes, func(i, j int) bool { return notes[i].Path < notes[j].Path })
	return notes, cleanup, nil
}

func validVaultPath(p string) bool {
	if len(p) > 1024 || !strings.EqualFold(path.Ext(p), ".md") || path.IsAbs(p) || path.Clean(p) != p {
		return false
	}
	for _, s := range strings.Split(p, "/") {
		if s == "" || strings.HasPrefix(s, ".") || strings.ContainsAny(s, ":\x00\r\n") {
			return false
		}
	}
	return true
}

func (s *Server) PostVaultImport(w http.ResponseWriter, r *http.Request) {
	u, _ := s.Auth.CurrentUser(r)
	if u == nil {
		http.Error(w, "請先登入再匯入", http.StatusUnauthorized)
		return
	}
	batch, err := uuid.Parse(r.URL.Query().Get("batch"))
	if err != nil {
		http.Error(w, "缺少匯入識別碼", http.StatusBadRequest)
		return
	}
	select {
	case vaultImportSlots <- struct{}{}:
		defer func() { <-vaultImportSlots }()
	default:
		http.Error(w, "目前有其他匯入，請稍後重試", http.StatusTooManyRequests)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	r = r.WithContext(ctx)
	r.Body = http.MaxBytesReader(w, r.Body, maxVaultBytes)
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(5 * time.Minute))
	defer http.NewResponseController(w).SetReadDeadline(time.Time{})
	notes, cleanup, err := readVault(r)
	defer cleanup()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	enc := json.NewEncoder(w)
	emit := func(v any) error {
		if err := enc.Encode(v); err != nil {
			return err
		}
		return http.NewResponseController(w).Flush()
	}
	report, err := s.importVault(ctx, u, batch, notes, func(done int) error {
		return emit(map[string]any{"type": "progress", "done": done, "total": len(notes)})
	})
	if err != nil {
		log.Printf("vault import %s: %v", batch, err)
		_ = emit(map[string]string{"type": "error", "error": "匯入未完成。請重試同一批次；已完成的批次不會重複建立。"})
		return
	}
	_ = emit(report)
}

// Insert all note identities first, then resolve links. A single transaction
// publishes the entire vault together, and the receipt commits with it.
func (s *Server) importVault(ctx context.Context, u *auth.User, batch uuid.UUID, notes []*vaultNote, progress func(int) error) (*vaultReport, error) {
	start := time.Now()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// Serialize imports by author without blocking other authors' writes.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, u.ID.String()); err != nil {
		return nil, err
	}
	var receipt []byte
	err = tx.QueryRowContext(ctx, `SELECT report FROM vault_imports WHERE author_id=$1 AND id=$2`, u.ID, batch).Scan(&receipt)
	if err == nil {
		var report vaultReport
		err = json.Unmarshal(receipt, &report)
		return &report, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	used := map[string]bool{}
	rows, err := tx.QueryContext(ctx, `SELECT slug_ci FROM notes WHERE author_id=$1`, u.ID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var slug string
		if err = rows.Scan(&slug); err != nil {
			rows.Close()
			return nil, err
		}
		used[slug] = true
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	for _, n := range notes {
		base := kebabSlug(strings.TrimSuffix(path.Base(n.Path), path.Ext(n.Path)))
		if base == "" {
			base = "note-" + n.ID.String()[:8]
		}
		n.Slug = base
		for i := 2; used[strings.ToLower(n.Slug)]; i++ {
			n.Slug = fmt.Sprintf("%s-%d", base, i)
		}
		if n.Slug != base {
			n.Warnings = append(n.Warnings, "同名網址已加上編號")
		}
		used[strings.ToLower(n.Slug)] = true
		n.URL = noteURL(u.Handle, n.Slug)
	}
	index := newVaultIndex(notes)
	report := &vaultReport{Type: "complete", Total: len(notes), URL: "/" + u.Handle, Notes: notes}
	now := nowUnix()
	if err = progress(0); err != nil {
		return nil, err
	}
	for i, n := range notes {
		raw, err := os.ReadFile(n.temp)
		if err != nil {
			return nil, err
		}
		body := rewriteVaultLinks(string(raw), n, index)
		if _, err = tx.ExecContext(ctx, `INSERT INTO notes(id,author_id,slug,slug_ci,title,body_md,created_at,updated_at,published_at) VALUES($1,$2,$3,$3,$4,$5,$6,$6,$6)`, n.ID, u.ID, n.Slug, n.Title, body, now); err != nil {
			return nil, err
		}
		for _, tag := range n.tags {
			if _, err = tx.ExecContext(ctx, `INSERT INTO note_tags(note_id,tag,created_at) VALUES($1,$2,$3)`, n.ID, tag, now); err != nil {
				return nil, err
			}
		}
		if err = os.WriteFile(n.temp, []byte(body), 0600); err != nil {
			return nil, err
		}
		report.Media += n.Media
		if (i+1)%20 == 0 {
			if err = progress(i + 1); err != nil {
				return nil, err
			}
		}
	}
	for i, n := range notes {
		raw, err := os.ReadFile(n.temp)
		if err != nil {
			return nil, err
		}
		if err = recomputeLinks(ctx, tx, n.ID, u.Handle, string(raw)); err != nil {
			return nil, err
		}
		if err = backfillStubLinks(ctx, tx, n.ID, u.ID, n.Slug); err != nil {
			return nil, err
		}
		if (i+1)%20 == 0 {
			if err = progress(len(notes) + i + 1); err != nil {
				return nil, err
			}
		}
	}
	ids := make([]uuid.UUID, len(notes))
	for i, n := range notes {
		ids[i] = n.ID
	}
	byID := map[uuid.UUID]*vaultNote{}
	for _, n := range notes {
		byID[n.ID] = n
	}
	rows, err = tx.QueryContext(ctx, `SELECT source_note_id, raw_target FROM links WHERE source_note_id=ANY($1) AND resolved_target_id IS NULL`, ids)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id uuid.UUID
		var raw string
		if err = rows.Scan(&id, &raw); err != nil {
			rows.Close()
			return nil, err
		}
		report.Unresolved++
		if n := byID[id]; len(n.Warnings) < 100 {
			n.Warnings = append(n.Warnings, "未解析連結："+raw)
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	report.ElapsedMS = time.Since(start).Milliseconds()
	receipt, err = json.Marshal(report)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO vault_imports(author_id,id,report,created_at) VALUES($1,$2,$3,$4)`, u.ID, batch, receipt, now); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return report, nil
}
