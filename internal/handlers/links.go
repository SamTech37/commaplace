package handlers

import (
	"context"
	"database/sql"
	"errors"
	"html/template"

	"github.com/google/uuid"

	"commonplace/internal/markdown"
)

// ---------- backlinks / outgoing links ----------

type backlink struct {
	Title        string
	AuthorHandle string
	URL          string
	UpdatedRel   string
	SameVault    bool
}

func (s *Server) loadBacklinksSplit(ctx context.Context, noteID uuid.UUID, vaultHandle string) (sameVault, crossVault []backlink, err error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT n.title, n.slug, u.handle, n.updated_at
		FROM links l
		JOIN notes n ON n.id = l.source_note_id
		JOIN users u ON u.id = n.author_id
		WHERE l.resolved_target_id = $1
		  AND n.hidden_at IS NULL AND n.deleted_at IS NULL AND n.published_at IS NOT NULL
		ORDER BY n.updated_at DESC`, noteID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var title, slug, handle string
		var updated int64
		if err := rows.Scan(&title, &slug, &handle, &updated); err != nil {
			return nil, nil, err
		}
		bl := backlink{
			Title:        title,
			AuthorHandle: handle,
			URL:          noteURL(handle, slug),
			UpdatedRel:   relativeTime(updated),
			SameVault:    handle == vaultHandle,
		}
		if bl.SameVault {
			sameVault = append(sameVault, bl)
		} else {
			crossVault = append(crossVault, bl)
		}
	}
	return sameVault, crossVault, rows.Err()
}

type outlink struct {
	Title        string
	AuthorHandle string
	URL          string
	SameVault    bool
}

func (s *Server) loadOutgoingSplit(ctx context.Context, noteID uuid.UUID, vaultHandle string) (same, cross []outlink, err error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT DISTINCT n.id, n.title, n.slug, u.handle
		FROM links l
		JOIN notes n ON n.id = l.resolved_target_id
		JOIN users u ON u.id = n.author_id
		WHERE l.source_note_id = $1 AND l.resolved_target_id IS NOT NULL
		  AND n.hidden_at IS NULL AND n.deleted_at IS NULL AND n.published_at IS NOT NULL`, noteID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var title, slug, handle string
		if err := rows.Scan(&id, &title, &slug, &handle); err != nil {
			return nil, nil, err
		}
		ol := outlink{
			Title:        title,
			AuthorHandle: handle,
			URL:          noteURL(handle, slug),
			SameVault:    handle == vaultHandle,
		}
		if ol.SameVault {
			same = append(same, ol)
		} else {
			cross = append(cross, ol)
		}
	}
	return same, cross, rows.Err()
}

// ---------- link recomputation ----------

// RecomputeLinks is exported so the seed package can call it.
func RecomputeLinks(ctx context.Context, tx *sql.Tx, sourceID uuid.UUID, sourceAuthorHandle, body string) error {
	return recomputeLinks(ctx, tx, sourceID, sourceAuthorHandle, body)
}

func recomputeLinks(ctx context.Context, tx *sql.Tx, sourceID uuid.UUID, sourceAuthorHandle, body string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM links WHERE source_note_id = $1`, sourceID); err != nil {
		return err
	}
	for _, l := range markdown.Extract(body) {
		targetHandle := l.User
		if targetHandle == "" {
			targetHandle = sourceAuthorHandle
		}
		// The target *user* almost always exists even when the target note
		// does not, so the edge is stored as a uuid and survives a rename.
		// A link to a handle nobody owns leaves it NULL.
		var targetUserID *uuid.UUID
		var uid uuid.UUID
		switch err := tx.QueryRowContext(ctx,
			`SELECT id FROM users WHERE handle_ci = lower($1)`, targetHandle,
		).Scan(&uid); {
		case err == nil:
			targetUserID = &uid
		case errors.Is(err, sql.ErrNoRows):
		default:
			return err
		}
		var resolved *uuid.UUID
		var found uuid.UUID
		if targetUserID != nil {
			switch err := tx.QueryRowContext(ctx,
				`SELECT id FROM notes WHERE author_id = $1 AND slug = $2`,
				*targetUserID, l.Slug,
			).Scan(&found); {
			case err == nil:
				resolved = &found
			case errors.Is(err, sql.ErrNoRows):
			default:
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO links(source_note_id, target_user_id, target_slug, raw_target, resolved_target_id)
			VALUES($1, $2, $3, $4, $5)`,
			sourceID, targetUserID, l.Slug, l.Raw, resolved,
		); err != nil {
			return err
		}
	}
	return nil
}

// backfillStubLinks resolves any link row still waiting (resolved_target_id
// IS NULL) for the (handle, slug) that noteID just started answering to —
// covers both a brand-new note and a slug change on an existing one.
func backfillStubLinks(ctx context.Context, tx *sql.Tx, noteID uuid.UUID, authorID uuid.UUID, slug string) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE links SET resolved_target_id = $1
		WHERE resolved_target_id IS NULL
		  AND target_user_id = $2
		  AND target_slug = $3`,
		noteID, authorID, slug,
	)
	return err
}

// ---------- wiki-link resolvers (for markdown.Render) ----------

// resolverKey identifies a link by what the author typed, not by who it points
// at. A target's handle can change while the body text does not, so the key is
// built from the typed (user, slug) rather than the target's current identity.
func resolverKey(vaultHandle string, l markdown.WikiLink) string {
	h := l.User
	if h == "" {
		h = vaultHandle
	}
	return h + "\x00" + l.Slug
}

// buildResolver resolves wiki links by live-looking-up the typed (handle, slug).
// Used by PostPreview, where the body is unsaved and has no stored link rows.
func (s *Server) buildResolver(ctx context.Context, vaultHandle string, links []markdown.WikiLink) markdown.Resolver {
	resolved := map[string]markdown.ResolvedTarget{}
	for _, l := range links {
		h := l.User
		if h == "" {
			h = vaultHandle
		}
		var rt markdown.ResolvedTarget
		err := s.DB.QueryRowContext(ctx, `
			SELECT u.handle, n.slug, n.title FROM notes n
			JOIN users u ON u.id = n.author_id
			WHERE u.handle = $1 AND n.slug = $2`,
			h, l.Slug,
		).Scan(&rt.Handle, &rt.Slug, &rt.Title)
		if err == nil {
			resolved[resolverKey(vaultHandle, l)] = rt
		}
	}
	return func(l markdown.WikiLink) *markdown.ResolvedTarget {
		if rt, ok := resolved[resolverKey(vaultHandle, l)]; ok {
			return &rt
		}
		return nil
	}
}

// buildResolverForNote resolves a saved note's wiki links through the stored
// links.resolved_target_id uuid, so a renamed target's current handle/slug is
// returned — inbound links follow renames without rewriting any note body.
func (s *Server) buildResolverForNote(ctx context.Context, vaultHandle string, sourceID uuid.UUID) markdown.Resolver {
	resolved := map[string]markdown.ResolvedTarget{}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT l.raw_target, u.handle, n.slug, n.title
		FROM links l
		JOIN notes n ON n.id = l.resolved_target_id
		JOIN users u ON u.id = n.author_id
		WHERE l.source_note_id = $1`, sourceID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var raw string
			var rt markdown.ResolvedTarget
			if err := rows.Scan(&raw, &rt.Handle, &rt.Slug, &rt.Title); err != nil {
				break
			}
			// raw_target is the payload between [[ and ]] exactly as authored,
			// so re-parsing it reproduces the key the renderer will look up —
			// no stored copy of anyone's handle required.
			typed, ok := markdown.ParseLink(raw)
			if !ok {
				continue
			}
			resolved[resolverKey(vaultHandle, typed)] = rt
		}
	}
	return func(l markdown.WikiLink) *markdown.ResolvedTarget {
		if rt, ok := resolved[resolverKey(vaultHandle, l)]; ok {
			return &rt
		}
		return nil
	}
}

// buildEmbedResolver returns an EmbedResolver that looks up ![[note]] targets
// and renders their bodies inline, capped at maxDepth levels of nesting.
// Beyond the cap, the inner Render gets a nil EmbedResolver so further embeds
// render as "too deep" placeholders rather than crashing the server.
func (s *Server) buildEmbedResolver(ctx context.Context, vaultHandle string, maxDepth int) markdown.EmbedResolver {
	var build func(depth int) markdown.EmbedResolver
	build = func(depth int) markdown.EmbedResolver {
		if depth >= maxDepth {
			return nil
		}
		return func(link markdown.WikiLink) (string, template.HTML, bool) {
			handle := link.User
			if handle == "" {
				handle = vaultHandle
			}
			var title, body string
			err := s.DB.QueryRowContext(ctx, `
				SELECT n.title, n.body_md FROM notes n
				JOIN users u ON u.id = n.author_id
				WHERE u.handle = $1 AND n.slug = $2
				  AND n.hidden_at IS NULL AND n.deleted_at IS NULL
				  AND n.published_at IS NOT NULL`,
				handle, link.Slug,
			).Scan(&title, &body)
			if err != nil {
				return "", "", false
			}
			subLinks := markdown.Extract(body)
			subResolver := s.buildResolver(ctx, handle, subLinks)
			rendered, err := markdown.Render(body, handle, subResolver, build(depth+1))
			if err != nil {
				return title, "", true
			}
			return title, rendered, true
		}
	}
	return build(0)
}
