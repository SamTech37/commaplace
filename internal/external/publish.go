package external

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

// PublishFetcher implements Fetcher for publish.obsidian.md sites.
//
// Detection strategy:
//
//   1. Fetch the SPA shell at the source URL.
//   2. Scan the HTML for the site UUID — Publish embeds it as
//      `window.uid = "<uuid>"` or `"uid":"<uuid>"` in inlined options.
//   3. Once we have the UUID, hit publishCacheURL/publishAccessURL for the
//      file tree and per-file markdown.
//
// The exact endpoint paths are isolated as constants at the top so future
// upstream changes can be patched in one place.
type PublishFetcher struct {
	HTTP    *http.Client
	UserAgent string
}

// Publish endpoints. Confirmed against publish.obsidian.md/help on 2026-05-14:
//
//	GET https://publish-01.obsidian.md/cache/<uid>          → JSON map { "<path>": "<hash-or-null>", ... }
//	GET https://publish-01.obsidian.md/access/<uid>/<path>  → text/markdown body (or 200 + "## Not Found" page)
const (
	publishCDNHost   = "https://publish-01.obsidian.md"
	publishCacheURL  = publishCDNHost + "/cache/%s"      // file index
	publishAccessURL = publishCDNHost + "/access/%s/%s"  // file content
)

// New returns a PublishFetcher with sensible defaults.
func NewPublishFetcher() *PublishFetcher {
	return &PublishFetcher{
		HTTP: &http.Client{
			Timeout: 25 * time.Second,
		},
		UserAgent: "comma-bot/0.1 (+https://comma.example/about)",
	}
}

// uuidPattern matches Obsidian Publish site UUIDs in the SPA shell.
var (
	reUIDQuoted   = regexp.MustCompile(`["']uid["']\s*:\s*["']([0-9a-f]{32})["']`)
	reUIDWindow   = regexp.MustCompile(`window\.uid\s*=\s*["']([0-9a-f]{32})["']`)
	reUIDAnyHex32 = regexp.MustCompile(`/cache/([0-9a-f]{32})`)
	reTitleHTML   = regexp.MustCompile(`(?is)<title>(.+?)</title>`)
)

func (p *PublishFetcher) Detect(ctx context.Context, sourceURL string) (VaultMeta, error) {
	shell, err := p.get(ctx, sourceURL)
	if err != nil {
		return VaultMeta{}, fmt.Errorf("fetch shell: %w", err)
	}
	body := string(shell)
	uid := ""
	for _, re := range []*regexp.Regexp{reUIDQuoted, reUIDWindow, reUIDAnyHex32} {
		if m := re.FindStringSubmatch(body); len(m) == 2 {
			uid = m[1]
			break
		}
	}
	if uid == "" {
		return VaultMeta{}, errors.New("could not find site UUID in page shell — is this a public publish.obsidian.md URL?")
	}
	meta := VaultMeta{SourceURL: sourceURL, SiteID: uid}
	if m := reTitleHTML.FindStringSubmatch(body); len(m) == 2 {
		meta.DisplayName = strings.TrimSpace(m[1])
	}
	return meta, nil
}

func (p *PublishFetcher) List(ctx context.Context, meta VaultMeta) ([]string, error) {
	body, err := p.get(ctx, fmt.Sprintf(publishCacheURL, meta.SiteID))
	if err != nil {
		return nil, fmt.Errorf("fetch cache index: %w", err)
	}
	// Response is a flat object whose keys are file paths inside the vault.
	var idx map[string]any
	if err := json.Unmarshal(body, &idx); err != nil {
		return nil, fmt.Errorf("decode cache index: %w", err)
	}
	out := make([]string, 0, len(idx))
	for k := range idx {
		if strings.HasSuffix(strings.ToLower(k), ".md") {
			out = append(out, k)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("cache index contained no .md files")
	}
	sort.Strings(out)
	return out, nil
}

func (p *PublishFetcher) Fetch(ctx context.Context, meta VaultMeta, path string) (FetchedFile, error) {
	encoded := pathEscape(path)
	raw, err := p.get(ctx, fmt.Sprintf(publishAccessURL, meta.SiteID, encoded))
	if err != nil {
		return FetchedFile{}, fmt.Errorf("fetch %s: %w", path, err)
	}
	body := string(raw)
	// The access endpoint returns 200 even for missing files, with a
	// "## Not Found" markdown body — detect and skip those.
	if strings.HasPrefix(strings.TrimSpace(body), "## Not Found") {
		return FetchedFile{}, fmt.Errorf("vault missing file: %s", path)
	}
	body = stripFrontmatter(body)
	ff := FetchedFile{
		Path:      path,
		Title:     inferTitle(body, path),
		BodyMD:    body,
		UpdatedAt: time.Now(),
	}
	return ff, nil
}

// stripFrontmatter removes a leading `--- ... ---` YAML block so it doesn't
// appear in rendered HTML. Frontmatter starts on the very first line.
func stripFrontmatter(s string) string {
	if !strings.HasPrefix(s, "---") {
		return s
	}
	// must be `---\n` or `---\r\n`
	rest := s[3:]
	if !strings.HasPrefix(rest, "\n") && !strings.HasPrefix(rest, "\r\n") {
		return s
	}
	if i := strings.Index(rest, "\n---"); i >= 0 {
		// trim newline-or-CRLF that follows the closing fence
		tail := rest[i+4:]
		tail = strings.TrimLeft(tail, "\r\n")
		return tail
	}
	return s
}

// ---------- helpers ----------

func (p *PublishFetcher) get(ctx context.Context, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if p.UserAgent != "" {
		req.Header.Set("User-Agent", p.UserAgent)
	}
	req.Header.Set("Accept", "*/*")
	resp, err := p.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20)) // 8 MB cap per response
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d for %s: %s", resp.StatusCode, u, snippet(body))
	}
	return body, nil
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 160 {
		s = s[:160] + "…"
	}
	return s
}

// pathEscape escapes each path segment but keeps the slashes.
func pathEscape(p string) string {
	parts := strings.Split(p, "/")
	for i, s := range parts {
		parts[i] = url.PathEscape(s)
	}
	return strings.Join(parts, "/")
}

// inferTitle picks the first non-empty heading or the basename.
func inferTitle(body, path string) string {
	for _, ln := range strings.SplitN(body, "\n", 100) {
		ln = strings.TrimSpace(ln)
		if strings.HasPrefix(ln, "# ") {
			return strings.TrimSpace(ln[2:])
		}
	}
	base := path
	if i := strings.LastIndex(path, "/"); i >= 0 {
		base = path[i+1:]
	}
	base = strings.TrimSuffix(base, ".md")
	return base
}

