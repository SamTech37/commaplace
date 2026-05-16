package external

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// QuartzFetcher reads digital gardens built with Quartz v4. Quartz emits
// /static/contentIndex.json containing every page's slug, title, content
// (plain text), pre-resolved internal links, and tags — so one HTTP fetch
// hydrates the whole vault.
type QuartzFetcher struct {
	HTTP      *http.Client
	UserAgent string

	// cache is shared across Detect/List/Fetch so we only download the
	// 1-8 MB index once per crawl run. Keyed by canonical source URL.
	cache sync.Map // map[string]*quartzCacheEntry
}

type quartzEntry struct {
	Slug    string   `json:"slug"`
	Title   string   `json:"title"`
	Content string   `json:"content"`
	Links   []string `json:"links"`
	Tags    []string `json:"tags"`
}

type quartzCacheEntry struct {
	loadedAt time.Time
	entries  map[string]quartzEntry
}

func NewQuartzFetcher() *QuartzFetcher {
	return &QuartzFetcher{
		HTTP:      &http.Client{Timeout: 30 * time.Second},
		UserAgent: "commonplace-bot/0.1 (+https://commonplace.example/about)",
	}
}

func (q *QuartzFetcher) Detect(ctx context.Context, sourceURL string) (VaultMeta, error) {
	cache, err := q.load(ctx, sourceURL)
	if err != nil {
		return VaultMeta{}, err
	}
	meta := VaultMeta{SourceURL: sourceURL} // Quartz has no UUID; identity is the URL
	// Best-effort display name: the entry whose slug is "" or "index" wins,
	// else fall back to the host.
	for slug, e := range cache.entries {
		if slug == "" || slug == "index" {
			if e.Title != "" {
				meta.DisplayName = e.Title
				break
			}
		}
	}
	if meta.DisplayName == "" {
		meta.DisplayName = hostOf(sourceURL)
	}
	return meta, nil
}

func (q *QuartzFetcher) List(ctx context.Context, meta VaultMeta) ([]string, error) {
	cache, err := q.load(ctx, meta.SourceURL)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(cache.entries))
	for slug := range cache.entries {
		out = append(out, slug)
	}
	return out, nil
}

func (q *QuartzFetcher) Fetch(ctx context.Context, meta VaultMeta, slug string) (FetchedFile, error) {
	cache, err := q.load(ctx, meta.SourceURL)
	if err != nil {
		return FetchedFile{}, err
	}
	e, ok := cache.entries[slug]
	if !ok {
		return FetchedFile{}, fmt.Errorf("slug not in index: %s", slug)
	}
	title := e.Title
	if title == "" {
		title = lastSegment(slug)
	}
	body := strings.TrimSpace(e.Content)
	if len(e.Tags) > 0 {
		body = body + "\n\n_tags: " + strings.Join(e.Tags, ", ") + "_"
	}
	return FetchedFile{
		Path:             slug + ".md",
		Title:            title,
		BodyMD:           body,
		OriginalURL:      strings.TrimRight(meta.SourceURL, "/") + "/" + slug,
		UpdatedAt:        time.Now(),
		PreResolvedLinks: e.Links,
	}, nil
}

// ---------- internal ----------

func (q *QuartzFetcher) load(ctx context.Context, sourceURL string) (*quartzCacheEntry, error) {
	if v, ok := q.cache.Load(sourceURL); ok {
		c := v.(*quartzCacheEntry)
		if time.Since(c.loadedAt) < 5*time.Minute {
			return c, nil
		}
	}
	indexURL := strings.TrimRight(sourceURL, "/") + "/static/contentIndex.json"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, indexURL, nil)
	if err != nil {
		return nil, err
	}
	if q.UserAgent != "" {
		req.Header.Set("User-Agent", q.UserAgent)
	}
	resp, err := q.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch contentIndex.json: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("contentIndex.json HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20)) // 64 MB cap
	if err != nil {
		return nil, err
	}
	var raw map[string]quartzEntry
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode contentIndex.json: %w", err)
	}
	if len(raw) == 0 {
		return nil, errors.New("contentIndex.json is empty")
	}
	c := &quartzCacheEntry{loadedAt: time.Now(), entries: raw}
	q.cache.Store(sourceURL, c)
	return c, nil
}

func hostOf(u string) string {
	s := strings.TrimPrefix(u, "https://")
	s = strings.TrimPrefix(s, "http://")
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	return s
}

func lastSegment(slug string) string {
	if i := strings.LastIndex(slug, "/"); i >= 0 {
		return slug[i+1:]
	}
	return slug
}
