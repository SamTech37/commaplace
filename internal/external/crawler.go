package external

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"strings"
	"time"
)

// Crawler runs the fetch loop for one vault.
type Crawler struct {
	Store    *Store
	Fetchers map[string]Fetcher // keyed by Vault.Kind ('publish' | 'quartz')

	// Tunables — kept small for v1 so the server stays responsive.
	PerFileDelay time.Duration // sleep between per-file fetches
	MaxFiles     int           // safety cap per crawl
}

// NewCrawler accepts a kind→Fetcher map so the worker can dispatch the right
// fetcher per vault.
func NewCrawler(store *Store, fetchers map[string]Fetcher) *Crawler {
	return &Crawler{
		Store:        store,
		Fetchers:     fetchers,
		PerFileDelay: 250 * time.Millisecond,
		MaxFiles:     20000,
	}
}

// CrawlOne pulls the next pending vault and processes it. Returns
// sql.ErrNoRows when the queue is empty so the worker loop can sleep.
func (c *Crawler) CrawlOne(ctx context.Context) error {
	v, err := c.Store.NextPending(ctx, 24*time.Hour)
	if err != nil {
		return err
	}
	return c.crawl(ctx, v)
}

// CrawlByID forces a crawl of a specific vault regardless of schedule.
func (c *Crawler) CrawlByID(ctx context.Context, id int64) error {
	v, err := c.Store.NextPending(ctx, 0)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	// just fetch the row directly
	row := c.Store.DB.QueryRowContext(ctx, `
		SELECT id, source_url, site_id, display_name, slug, kind, added_at, last_crawled_at, status, error_message, note_count
		FROM external_vaults WHERE id = ?`, id)
	v, err = scanVault(row)
	if err != nil {
		return err
	}
	return c.crawl(ctx, v)
}

func (c *Crawler) crawl(ctx context.Context, v Vault) error {
	log.Printf("[external] crawling vault id=%d kind=%s slug=%s url=%s", v.ID, v.Kind, v.Slug, v.SourceURL)
	kind := v.Kind
	if kind == "" {
		kind = "publish"
	}
	fetcher := c.Fetchers[kind]
	if fetcher == nil {
		msg := "no fetcher registered for kind=" + kind
		c.Store.MarkError(ctx, v.ID, msg)
		return errors.New(msg)
	}
	if err := c.Store.MarkCrawling(ctx, v.ID); err != nil {
		return err
	}

	meta, err := fetcher.Detect(ctx, v.SourceURL)
	if err != nil {
		c.Store.MarkError(ctx, v.ID, "detect: "+err.Error())
		return err
	}
	if meta.SourceURL == "" {
		meta.SourceURL = v.SourceURL
	}
	if v.SiteID != "" && meta.SiteID == "" {
		meta.SiteID = v.SiteID
	}

	paths, err := fetcher.List(ctx, meta)
	if err != nil {
		c.Store.MarkError(ctx, v.ID, "list: "+err.Error())
		return err
	}
	if len(paths) > c.MaxFiles {
		paths = paths[:c.MaxFiles]
	}

	keep := make(map[string]bool, len(paths))
	type fetched struct {
		noteID int64
		body   string
		pre    []string
	}
	var fresh []fetched
	for i, p := range paths {
		if i > 0 && c.PerFileDelay > 0 {
			select {
			case <-ctx.Done():
				c.Store.MarkError(ctx, v.ID, "cancelled mid-crawl")
				return ctx.Err()
			case <-time.After(c.PerFileDelay):
			}
		}
		ff, err := fetcher.Fetch(ctx, meta, p)
		if err != nil {
			log.Printf("[external] vault=%s skip %s: %v", v.Slug, p, err)
			continue
		}
		// Original URL: prefer what the fetcher provided, otherwise synthesize.
		if ff.OriginalURL == "" {
			ff.OriginalURL = canonicalSiteURL(v.SourceURL, p)
		}
		// keep[] tracks the actual path stored in DB (ff.Path), not the
		// upstream slug — they may differ if the fetcher rewrites paths.
		keep[ff.Path] = true
		id, changed, err := c.Store.UpsertNote(ctx, v.ID, ff)
		if err != nil {
			log.Printf("[external] vault=%s upsert %s: %v", v.Slug, p, err)
			continue
		}
		if changed {
			fresh = append(fresh, fetched{noteID: id, body: ff.BodyMD, pre: ff.PreResolvedLinks})
		}
	}

	if err := c.Store.DeleteMissingNotes(ctx, v.ID, keep); err != nil {
		log.Printf("[external] vault=%s prune: %v", v.Slug, err)
	}

	for _, f := range fresh {
		var links []ParsedLink
		if len(f.pre) > 0 {
			// Upstream already resolved the link graph — store directly.
			links = preResolvedLinks(ctx, c.Store, v.ID, f.pre)
		} else {
			links = parseAndResolveLinks(ctx, c.Store, v.ID, f.body)
		}
		if err := c.Store.ReplaceLinks(ctx, f.noteID, links); err != nil {
			log.Printf("[external] vault=%s replace links: %v", v.Slug, err)
		}
	}

	displayName := meta.DisplayName
	if displayName == "" {
		displayName = v.DisplayName
	}
	if displayName == "" {
		displayName = v.Slug
	}

	return c.Store.MarkActive(ctx, v.ID, meta.SiteID, displayName, len(keep))
}

// preResolvedLinks converts a Quartz-style pre-resolved slug list into
// ParsedLink rows, resolving each target slug to an external_notes.id.
func preResolvedLinks(ctx context.Context, store *Store, vaultID int64, slugs []string) []ParsedLink {
	out := []ParsedLink{}
	for _, raw := range slugs {
		slug := strings.TrimSpace(raw)
		if slug == "" {
			continue
		}
		l := ParsedLink{Raw: slug, Kind: "unresolved"}
		if nid, err := store.LookupExternalBySlug(ctx, vaultID, slug); err == nil {
			l.Kind = "external_same"
			l.TargetVaultID = vaultID
			l.TargetExternalNoteID = nid
		}
		out = append(out, l)
	}
	return out
}

// canonicalSiteURL returns the public deep-link to a file given the vault
// root + the file path. Strips the .md and percent-encodes each segment so
// CJK / spaces survive. Works for both Obsidian Publish and Quartz.
func canonicalSiteURL(vaultRoot, path string) string {
	p := strings.TrimSuffix(path, ".md")
	return strings.TrimRight(vaultRoot, "/") + "/" + pathEscape(p)
}

// parseAndResolveLinks walks [[ ... ]] in body and tries to bind each link.
//
//	[[Foo]]            -> external_same (same vault, by title/basename)
//	[[@other/Foo]]     -> commonplace_user OR external_cross (if other matches
//	                      a registered external vault slug)
//	[[unmatched]]      -> unresolved (still stored, lets us count broken links)
func parseAndResolveLinks(ctx context.Context, store *Store, vaultID int64, body string) []ParsedLink {
	out := []ParsedLink{}
	for _, raw := range extractWikiLinkBodies(body) {
		l := ParsedLink{Raw: raw, Kind: "unresolved"}
		core := raw
		// strip alias/heading: "Foo|bar" -> "Foo"; "Foo#anchor" -> "Foo"
		if i := strings.IndexAny(core, "|#"); i >= 0 {
			core = core[:i]
		}
		core = strings.TrimSpace(core)
		if core == "" {
			continue
		}
		if strings.HasPrefix(core, "@") {
			rest := core[1:]
			parts := strings.SplitN(rest, "/", 2)
			handle := strings.ToLower(strings.TrimSpace(parts[0]))
			slug := ""
			if len(parts) == 2 {
				slug = strings.TrimSpace(parts[1])
			}
			// commonplace user?
			if store.UserExists(ctx, handle) {
				l.Kind = "commonplace_user"
				l.TargetUserHandle = handle
				l.TargetSlug = slug
			} else if otherVaultID, err := store.LookupVaultBySlug(ctx, handle); err == nil {
				l.Kind = "external_cross"
				l.TargetVaultID = otherVaultID
				if slug != "" {
					if nid, err := store.LookupExternalByTitle(ctx, otherVaultID, slug); err == nil {
						l.TargetExternalNoteID = nid
					}
				}
			}
		} else {
			if nid, err := store.LookupExternalByTitle(ctx, vaultID, core); err == nil {
				l.Kind = "external_same"
				l.TargetVaultID = vaultID
				l.TargetExternalNoteID = nid
			}
		}
		out = append(out, l)
	}
	return out
}

// extractWikiLinkBodies returns the raw text inside each [[ ... ]] match in
// the source. We don't use the markdown package's extractor because we want
// to capture links exactly as the author wrote them (including aliases and
// heading anchors), and we don't need rendering.
func extractWikiLinkBodies(body string) []string {
	var out []string
	for {
		i := strings.Index(body, "[[")
		if i < 0 {
			return out
		}
		body = body[i+2:]
		j := strings.Index(body, "]]")
		if j < 0 {
			return out
		}
		token := body[:j]
		body = body[j+2:]
		if strings.Contains(token, "\n") {
			continue
		}
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		out = append(out, token)
	}
}
