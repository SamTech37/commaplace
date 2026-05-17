package external

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Store wraps DB access for external vaults / notes / links.
type Store struct {
	DB *sql.DB
}

// NormalizeSourceURL canonicalizes a user-pasted URL into the form we store,
// and also returns the inferred fetcher kind and a URL-safe vault slug.
//
//	https://publish.obsidian.md/sebh/Notes/Foo  →  publish | https://publish.obsidian.md/sebh
//	https://garden.oldwinter.top/Cards/foo      →  quartz  | https://garden.oldwinter.top
//	garden.oldwinter.top                        →  quartz  | https://garden.oldwinter.top
//
// Obsidian Publish URLs are detected by host; everything else is treated as
// a candidate Quartz site (the crawler verifies on first fetch).
func NormalizeSourceURL(raw string) (canon, slug, kind string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", "", errors.New("empty URL")
	}
	if !strings.HasPrefix(strings.ToLower(raw), "http") {
		raw = "https://" + raw
	}
	u, perr := url.Parse(raw)
	if perr != nil {
		return "", "", "", fmt.Errorf("parse URL: %w", perr)
	}
	host := strings.ToLower(u.Host)
	if host == "" {
		return "", "", "", errors.New("URL has no host")
	}
	if host == "publish.obsidian.md" {
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) == 0 || parts[0] == "" {
			return "", "", "", errors.New("publish.obsidian.md URL needs a vault slug: https://publish.obsidian.md/<vault>")
		}
		slug = strings.ToLower(parts[0])
		canon = "https://publish.obsidian.md/" + slug
		kind = "publish"
		return
	}
	if host == "github.com" {
		owner, repo, branch, subpath, perr := parseGitHubURL(raw)
		if perr != nil {
			return "", "", "", perr
		}
		canon = "https://github.com/" + owner + "/" + repo
		if branch != "" {
			canon += "/tree/" + branch
			if subpath != "" {
				canon += "/" + subpath
			}
		}
		s := strings.ToLower(owner + "-" + repo)
		if subpath != "" {
			s += "-" + strings.ToLower(strings.ReplaceAll(subpath, "/", "-"))
		}
		slug = s
		kind = "github"
		return
	}
	// Treat anything else as a candidate Quartz site at the root of the host.
	canon = "https://" + host
	slug = hostToSlug(host)
	kind = "quartz"
	return
}

// hostToSlug turns "garden.oldwinter.top" into "garden-oldwinter-top".
func hostToSlug(host string) string {
	s := strings.ToLower(host)
	s = strings.ReplaceAll(s, ".", "-")
	s = strings.ReplaceAll(s, ":", "-")
	return strings.Trim(s, "-")
}

// AddVault inserts a new pending vault. Returns the inserted vault, or the
// existing one if the source URL is already registered.
func (s *Store) AddVault(ctx context.Context, sourceURL string) (Vault, bool, error) {
	canon, slug, kind, err := NormalizeSourceURL(sourceURL)
	if err != nil {
		return Vault{}, false, err
	}
	now := time.Now().Unix()
	res, err := s.DB.ExecContext(ctx, `
		INSERT INTO external_vaults (source_url, slug, kind, added_at, status)
		VALUES (?, ?, ?, ?, 'pending')
		ON CONFLICT(source_url) DO NOTHING`,
		canon, slug, kind, now)
	if err != nil {
		return Vault{}, false, err
	}
	rows, _ := res.RowsAffected()
	v, err := s.VaultBySourceURL(ctx, canon)
	if err != nil {
		return Vault{}, false, err
	}
	return v, rows > 0, nil
}

func (s *Store) VaultBySourceURL(ctx context.Context, canon string) (Vault, error) {
	return scanVault(s.DB.QueryRowContext(ctx,
		`SELECT id, source_url, site_id, display_name, slug, kind, added_at, last_crawled_at, status, error_message, note_count
		   FROM external_vaults WHERE source_url = ?`, canon))
}

func (s *Store) VaultBySlug(ctx context.Context, slug string) (Vault, error) {
	return scanVault(s.DB.QueryRowContext(ctx,
		`SELECT id, source_url, site_id, display_name, slug, kind, added_at, last_crawled_at, status, error_message, note_count
		   FROM external_vaults WHERE slug = ?`, slug))
}

func (s *Store) ListVaults(ctx context.Context) ([]Vault, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, source_url, site_id, display_name, slug, kind, added_at, last_crawled_at, status, error_message, note_count
		FROM external_vaults
		ORDER BY added_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Vault
	for rows.Next() {
		v, err := scanVault(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// NextPending returns one vault to crawl, or sql.ErrNoRows if none are
// due. Crawl-due = status 'pending' OR (status='active' AND last_crawled_at older than maxAge).
func (s *Store) NextPending(ctx context.Context, maxAge time.Duration) (Vault, error) {
	cutoff := time.Now().Add(-maxAge).Unix()
	return scanVault(s.DB.QueryRowContext(ctx, `
		SELECT id, source_url, site_id, display_name, slug, kind, added_at, last_crawled_at, status, error_message, note_count
		FROM external_vaults
		WHERE status = 'pending'
		   OR (status = 'active' AND last_crawled_at < ?)
		ORDER BY (status = 'pending') DESC, last_crawled_at ASC
		LIMIT 1`, cutoff))
}

func (s *Store) MarkCrawling(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE external_vaults SET status='crawling', error_message='' WHERE id = ?`, id)
	return err
}

func (s *Store) MarkActive(ctx context.Context, id int64, siteID, displayName string, noteCount int) error {
	_, err := s.DB.ExecContext(ctx, `
		UPDATE external_vaults
		   SET status='active',
		       error_message='',
		       site_id = CASE WHEN ? <> '' THEN ? ELSE site_id END,
		       display_name = CASE WHEN ? <> '' THEN ? ELSE display_name END,
		       note_count = ?,
		       last_crawled_at = ?
		 WHERE id = ?`,
		siteID, siteID, displayName, displayName, noteCount, time.Now().Unix(), id)
	return err
}

func (s *Store) MarkError(ctx context.Context, id int64, msg string) error {
	_, err := s.DB.ExecContext(ctx, `
		UPDATE external_vaults SET status='error', error_message=?, last_crawled_at=?
		WHERE id = ?`, msg, time.Now().Unix(), id)
	return err
}

// UpsertNote inserts or updates one external note by (vault_id, path).
func (s *Store) UpsertNote(ctx context.Context, vaultID int64, ff FetchedFile) (int64, bool, error) {
	hash := sha256Hex(ff.BodyMD)
	excerpt := makeExcerpt(ff.BodyMD)
	slug := noteSlugFromPath(ff.Path)
	now := time.Now().Unix()
	updated := ff.UpdatedAt.Unix()
	if updated == 0 {
		updated = now
	}
	// If existing row has same content_hash, we still update fetched_at but
	// skip rewriting body / triggering FTS churn.
	var existingID int64
	var existingHash string
	row := s.DB.QueryRowContext(ctx,
		`SELECT id, content_hash FROM external_notes WHERE vault_id = ? AND path = ?`,
		vaultID, ff.Path)
	err := row.Scan(&existingID, &existingHash)
	if err != nil && err != sql.ErrNoRows {
		return 0, false, err
	}
	if existingID != 0 && existingHash == hash {
		_, err := s.DB.ExecContext(ctx,
			`UPDATE external_notes SET fetched_at=? WHERE id = ?`, now, existingID)
		return existingID, false, err
	}
	if existingID != 0 {
		_, err := s.DB.ExecContext(ctx, `
			UPDATE external_notes
			   SET title=?, body_md=?, excerpt=?, content_hash=?, original_url=?, fetched_at=?, updated_at=?, slug=?
			 WHERE id = ?`,
			ff.Title, ff.BodyMD, excerpt, hash, ff.OriginalURL, now, updated, slug, existingID)
		return existingID, true, err
	}
	res, err := s.DB.ExecContext(ctx, `
		INSERT INTO external_notes
		  (vault_id, path, slug, title, body_md, excerpt, content_hash, original_url, fetched_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		vaultID, ff.Path, slug, ff.Title, ff.BodyMD, excerpt, hash, ff.OriginalURL, now, updated)
	if err != nil {
		return 0, false, err
	}
	id, _ := res.LastInsertId()
	return id, true, nil
}

func (s *Store) DeleteMissingNotes(ctx context.Context, vaultID int64, keepPaths map[string]bool) error {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, path FROM external_notes WHERE vault_id = ?`, vaultID)
	if err != nil {
		return err
	}
	defer rows.Close()
	var toDelete []int64
	for rows.Next() {
		var id int64
		var p string
		if err := rows.Scan(&id, &p); err != nil {
			return err
		}
		if !keepPaths[p] {
			toDelete = append(toDelete, id)
		}
	}
	for _, id := range toDelete {
		if _, err := s.DB.ExecContext(ctx, `DELETE FROM external_notes WHERE id = ?`, id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ReplaceLinks(ctx context.Context, sourceNoteID int64, links []ParsedLink) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM external_links WHERE source_external_note_id = ?`, sourceNoteID); err != nil {
		return err
	}
	for _, l := range links {
		var (
			targetVault sql.NullInt64
			targetNote  sql.NullInt64
		)
		if l.TargetVaultID != 0 {
			targetVault.Valid, targetVault.Int64 = true, l.TargetVaultID
		}
		if l.TargetExternalNoteID != 0 {
			targetNote.Valid, targetNote.Int64 = true, l.TargetExternalNoteID
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO external_links
			  (source_external_note_id, raw_link, target_kind, target_vault_id,
			   target_external_note_id, target_user_handle, target_slug)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			sourceNoteID, l.Raw, l.Kind, targetVault, targetNote, l.TargetUserHandle, l.TargetSlug); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ParsedLink is the resolved form of a [[wiki link]] in an external note.
type ParsedLink struct {
	Raw                  string
	Kind                 string // external_same | external_cross | commonplace_user | unresolved
	TargetVaultID        int64
	TargetExternalNoteID int64
	TargetUserHandle     string
	TargetSlug           string
}

// LookupExternalByTitle finds an external note within a vault by case-folded
// title or basename match.
func (s *Store) LookupExternalByTitle(ctx context.Context, vaultID int64, name string) (int64, error) {
	want := strings.ToLower(strings.TrimSpace(name))
	var id int64
	err := s.DB.QueryRowContext(ctx, `
		SELECT id FROM external_notes
		 WHERE vault_id = ?
		   AND (LOWER(title) = ? OR LOWER(REPLACE(REPLACE(path,'.md',''),'/','-')) = ?)
		 LIMIT 1`, vaultID, want, strings.ReplaceAll(want, " ", "-")).Scan(&id)
	return id, err
}

// LookupExternalBySlug finds an external note by its exact slug column.
// Used when the upstream provides pre-resolved link slugs (Quartz).
func (s *Store) LookupExternalBySlug(ctx context.Context, vaultID int64, slug string) (int64, error) {
	var id int64
	err := s.DB.QueryRowContext(ctx,
		`SELECT id FROM external_notes WHERE vault_id = ? AND slug = ? LIMIT 1`,
		vaultID, strings.ToLower(slug)).Scan(&id)
	return id, err
}

// LookupVaultBySlug helper for cross-vault resolution.
func (s *Store) LookupVaultBySlug(ctx context.Context, slug string) (int64, error) {
	var id int64
	err := s.DB.QueryRowContext(ctx,
		`SELECT id FROM external_vaults WHERE slug = ?`, strings.ToLower(slug)).Scan(&id)
	return id, err
}

// UserExists returns true if a commonplace user with the given handle exists.
// Used by link resolution in both crawler and renderer paths.
func (s *Store) UserExists(ctx context.Context, handle string) bool {
	if handle == "" {
		return false
	}
	var id int64
	err := s.DB.QueryRowContext(ctx, `SELECT id FROM users WHERE handle = ?`, handle).Scan(&id)
	return err == nil
}

// ---------- scanning ----------

type rowScanner interface {
	Scan(dest ...any) error
}

func scanVault(r rowScanner) (Vault, error) {
	var (
		v       Vault
		added, last int64
	)
	if err := r.Scan(&v.ID, &v.SourceURL, &v.SiteID, &v.DisplayName, &v.Slug, &v.Kind,
		&added, &last, &v.Status, &v.ErrorMessage, &v.NoteCount); err != nil {
		return Vault{}, err
	}
	v.AddedAt = time.Unix(added, 0)
	if last > 0 {
		v.LastCrawledAt = time.Unix(last, 0)
	}
	return v, nil
}

// ---------- misc helpers ----------

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func makeExcerpt(body string) string {
	s := body
	// strip lines that look like front-matter or pure metadata
	for strings.HasPrefix(s, "---") {
		i := strings.Index(s[3:], "---")
		if i < 0 {
			break
		}
		s = strings.TrimSpace(s[3+i+3:])
	}
	// take the first ~200 visible chars
	var b strings.Builder
	for _, r := range s {
		if r == '#' || r == '*' || r == '_' || r == '`' || r == '>' {
			continue
		}
		if r == '\n' {
			b.WriteByte(' ')
			continue
		}
		b.WriteRune(r)
		if b.Len() >= 220 {
			break
		}
	}
	out := strings.TrimSpace(b.String())
	if len(out) > 200 {
		out = strings.TrimRightFunc(out[:200], func(r rune) bool { return false }) + "…"
	}
	return out
}

func noteSlugFromPath(path string) string {
	s := strings.TrimSuffix(path, ".md")
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ToLower(s)
	return s
}
