package external

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// GitHubFetcher treats a public GitHub repository (optionally a subdirectory
// within it) as a markdown vault. One repo+branch+subpath = one vault.
//
// URL forms accepted:
//
//	https://github.com/<owner>/<repo>
//	https://github.com/<owner>/<repo>/tree/<branch>
//	https://github.com/<owner>/<repo>/tree/<branch>/<subpath>
//
// API budget per crawl: 1 repo lookup (Detect) + 1 git-tree call (List).
// Per-file downloads hit raw.githubusercontent.com and don't count against
// the REST quota. Optional GITHUB_TOKEN lifts anon limits from 60→5000/hr.
type GitHubFetcher struct {
	HTTP      *http.Client
	UserAgent string
	Token     string

	cache sync.Map // map[sourceURL]*ghCacheEntry
}

type ghCacheEntry struct {
	loadedAt    time.Time
	owner       string
	repo        string
	branch      string
	subpath     string // "" or "knowledge" (no leading/trailing slash)
	displayName string
	paths       []string // repo-relative paths to .md blobs under subpath
}

func NewGitHubFetcher() *GitHubFetcher {
	return &GitHubFetcher{
		HTTP:      &http.Client{Timeout: 30 * time.Second},
		UserAgent: "comma-bot/0.1 (+https://comma.example/about)",
		Token:     os.Getenv("GITHUB_TOKEN"),
	}
}

func (g *GitHubFetcher) Detect(ctx context.Context, sourceURL string) (VaultMeta, error) {
	c, err := g.load(ctx, sourceURL)
	if err != nil {
		return VaultMeta{}, err
	}
	return VaultMeta{
		SourceURL:   sourceURL,
		SiteID:      ghSiteID(c),
		DisplayName: c.displayName,
	}, nil
}

func (g *GitHubFetcher) List(ctx context.Context, meta VaultMeta) ([]string, error) {
	c, err := g.load(ctx, meta.SourceURL)
	if err != nil {
		return nil, err
	}
	return c.paths, nil
}

func (g *GitHubFetcher) Fetch(ctx context.Context, meta VaultMeta, path string) (FetchedFile, error) {
	c, err := g.load(ctx, meta.SourceURL)
	if err != nil {
		return FetchedFile{}, err
	}
	rawURL := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s",
		c.owner, c.repo, c.branch, pathEscape(path))
	body, err := g.get(ctx, rawURL, false)
	if err != nil {
		return FetchedFile{}, fmt.Errorf("fetch %s: %w", path, err)
	}
	md := stripFrontmatter(string(body))
	vaultPath := path
	if c.subpath != "" {
		vaultPath = strings.TrimPrefix(strings.TrimPrefix(path, c.subpath), "/")
	}
	return FetchedFile{
		Path:        vaultPath,
		Title:       inferTitle(md, vaultPath),
		BodyMD:      md,
		UpdatedAt:   time.Now(),
		OriginalURL: fmt.Sprintf("https://github.com/%s/%s/blob/%s/%s", c.owner, c.repo, c.branch, pathEscape(path)),
	}, nil
}

// ---------- internal ----------

func (g *GitHubFetcher) load(ctx context.Context, sourceURL string) (*ghCacheEntry, error) {
	if v, ok := g.cache.Load(sourceURL); ok {
		c := v.(*ghCacheEntry)
		if time.Since(c.loadedAt) < 5*time.Minute {
			return c, nil
		}
	}
	owner, repo, branch, subpath, err := parseGitHubURL(sourceURL)
	if err != nil {
		return nil, err
	}
	c := &ghCacheEntry{owner: owner, repo: repo, branch: branch, subpath: subpath}

	// 1. Resolve default branch + display name if branch was omitted.
	repoInfo, err := g.fetchRepoInfo(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	if c.branch == "" {
		c.branch = repoInfo.DefaultBranch
		if c.branch == "" {
			c.branch = "main"
		}
	}
	c.displayName = repoInfo.FullName
	if repoInfo.Description != "" {
		c.displayName = repoInfo.Description
	}

	// 2. List .md files via git tree (recursive).
	paths, truncated, err := g.fetchTree(ctx, owner, repo, c.branch)
	if err != nil {
		return nil, err
	}
	if truncated {
		// >100k entries — taiwan-md is far smaller, so log and proceed.
		// The crawler caller will see a partial list rather than an error.
		fmt.Fprintf(os.Stderr, "[external/github] WARN tree truncated for %s/%s@%s\n", owner, repo, c.branch)
	}
	prefix := ""
	if c.subpath != "" {
		prefix = c.subpath + "/"
	}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if !strings.HasSuffix(strings.ToLower(p), ".md") {
			continue
		}
		if prefix != "" && !strings.HasPrefix(p, prefix) {
			continue
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		if prefix != "" {
			return nil, fmt.Errorf("no .md files under %s in %s/%s@%s", prefix, owner, repo, c.branch)
		}
		return nil, fmt.Errorf("no .md files in %s/%s@%s", owner, repo, c.branch)
	}
	c.paths = out
	c.loadedAt = time.Now()
	g.cache.Store(sourceURL, c)
	return c, nil
}

type ghRepoInfo struct {
	FullName      string `json:"full_name"`
	Description   string `json:"description"`
	DefaultBranch string `json:"default_branch"`
}

func (g *GitHubFetcher) fetchRepoInfo(ctx context.Context, owner, repo string) (ghRepoInfo, error) {
	u := fmt.Sprintf("https://api.github.com/repos/%s/%s", owner, repo)
	body, err := g.get(ctx, u, true)
	if err != nil {
		return ghRepoInfo{}, fmt.Errorf("repo lookup: %w", err)
	}
	var info ghRepoInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return ghRepoInfo{}, fmt.Errorf("decode repo info: %w", err)
	}
	return info, nil
}

type ghTreeResp struct {
	Tree []struct {
		Path string `json:"path"`
		Type string `json:"type"`
	} `json:"tree"`
	Truncated bool `json:"truncated"`
}

func (g *GitHubFetcher) fetchTree(ctx context.Context, owner, repo, branch string) ([]string, bool, error) {
	u := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/trees/%s?recursive=1", owner, repo, url.PathEscape(branch))
	body, err := g.get(ctx, u, true)
	if err != nil {
		return nil, false, fmt.Errorf("git tree: %w", err)
	}
	var resp ghTreeResp
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, false, fmt.Errorf("decode git tree: %w", err)
	}
	out := make([]string, 0, len(resp.Tree))
	for _, e := range resp.Tree {
		if e.Type == "blob" {
			out = append(out, e.Path)
		}
	}
	return out, resp.Truncated, nil
}

func (g *GitHubFetcher) get(ctx context.Context, u string, isAPI bool) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if g.UserAgent != "" {
		req.Header.Set("User-Agent", g.UserAgent)
	}
	if isAPI {
		req.Header.Set("Accept", "application/vnd.github+json")
		if g.Token != "" {
			req.Header.Set("Authorization", "Bearer "+g.Token)
		}
	}
	resp, err := g.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20)) // 32 MB cap
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d for %s: %s", resp.StatusCode, u, snippet(body))
	}
	return body, nil
}

func ghSiteID(c *ghCacheEntry) string {
	id := c.owner + "/" + c.repo + "@" + c.branch
	if c.subpath != "" {
		id += ":" + c.subpath
	}
	return id
}

// parseGitHubURL handles:
//
//	github.com/<owner>/<repo>
//	github.com/<owner>/<repo>/tree/<branch>
//	github.com/<owner>/<repo>/tree/<branch>/<subpath...>
//
// Returns empty branch when omitted so caller can resolve the repo default.
func parseGitHubURL(raw string) (owner, repo, branch, subpath string, err error) {
	u, perr := url.Parse(raw)
	if perr != nil {
		return "", "", "", "", fmt.Errorf("parse URL: %w", perr)
	}
	if !strings.EqualFold(u.Host, "github.com") {
		return "", "", "", "", errors.New("not a github.com URL")
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", "", "", errors.New("github URL needs <owner>/<repo>")
	}
	owner = parts[0]
	repo = strings.TrimSuffix(parts[1], ".git")
	if len(parts) >= 4 && parts[2] == "tree" {
		branch = parts[3]
		if len(parts) > 4 {
			subpath = strings.Trim(strings.Join(parts[4:], "/"), "/")
		}
	}
	return
}
