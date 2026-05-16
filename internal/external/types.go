// Package external indexes Obsidian Publish vaults.
//
// Flow:
//
//	admin pastes URL  ->  external_vaults row (status=pending)
//	background worker ->  Crawler.Crawl(vault) ->  external_notes + external_links
//	served at /x/<vault-slug>/<note-slug> in our own UI ("Pinterest immersion")
//
// The fetcher's Publish-endpoint shapes are isolated in publish.go and
// kept easy to swap when the upstream API changes.
package external

import (
	"context"
	"time"
)

// Vault is one registered external site.
type Vault struct {
	ID            int64
	SourceURL     string
	SiteID        string // Obsidian Publish UUID (publish-kind only)
	DisplayName   string
	Slug          string
	Kind          string // 'publish' | 'quartz'
	AddedAt       time.Time
	LastCrawledAt time.Time
	Status        string // pending | crawling | active | error
	ErrorMessage  string
	NoteCount     int
}

// Note is one cached markdown file from an external vault.
type Note struct {
	ID           int64
	VaultID      int64
	Path         string // path within the vault, eg "Folder/Page.md"
	Slug         string // url-safe path used in our routes
	Title        string
	BodyMD       string
	Excerpt      string
	ContentHash  string
	OriginalURL  string
	FetchedAt    time.Time
	UpdatedAt    time.Time
}

// FetchedFile is what the Fetcher returns for one note in a vault.
type FetchedFile struct {
	Path        string // path inside vault (eg "Folder/Page.md")
	Title       string // best-effort: first h1 in body, or path basename
	BodyMD      string
	UpdatedAt   time.Time
	OriginalURL string   // best-effort canonical URL on the author's site
	// PreResolvedLinks lists target slugs the upstream already resolved for
	// this file (Quartz's contentIndex provides these). Crawler stores them
	// directly as external_same links without doing title matching.
	PreResolvedLinks []string
}

// VaultMeta is the upstream-detected metadata after fetching the shell.
type VaultMeta struct {
	SourceURL   string // pasted/normalised root URL (carried for fetchers that need it)
	SiteID      string
	DisplayName string
}

// Fetcher is the abstract interface a crawler uses to read an external site.
// Swappable per host kind (Obsidian Publish today, Quartz / Astro digital
// garden tomorrow).
type Fetcher interface {
	// Detect inspects the source URL, fetches the shell, and returns the
	// vault-level metadata needed for subsequent calls (site UUID etc).
	Detect(ctx context.Context, sourceURL string) (VaultMeta, error)
	// List returns every file path the vault publishes.
	List(ctx context.Context, meta VaultMeta) ([]string, error)
	// Fetch returns one file's markdown body + metadata.
	Fetch(ctx context.Context, meta VaultMeta, path string) (FetchedFile, error)
}
