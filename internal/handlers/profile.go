package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/google/uuid"
	nethtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"commonplace/internal/auth"
	"commonplace/internal/markdown"
)

func (s *Server) GetProfile(w http.ResponseWriter, r *http.Request) {
	handle := r.PathValue("user")

	if auth.IsReservedHandle(strings.ToLower(handle)) {
		s.renderError(w, r, http.StatusNotFound, "not found")
		return
	}

	var profile struct {
		ID         uuid.UUID
		Handle     string
		HomeTitle  string
		HomeBio    string
		HomeTopics string
		Showcases  string
	}
	err := s.DB.QueryRowContext(r.Context(),
		`SELECT id, handle, profile_title, profile_bio, profile_topic_tags, profile_showcases::text FROM users WHERE handle = $1`, handle,
	).Scan(&profile.ID, &profile.Handle, &profile.HomeTitle, &profile.HomeBio, &profile.HomeTopics, &profile.Showcases)
	if errors.Is(err, sql.ErrNoRows) {
		s.renderError(w, r, http.StatusNotFound, "no such user")
		return
	}
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	viewer, _ := s.Auth.CurrentUser(r)
	var viewerID uuid.UUID
	if viewer != nil {
		viewerID = viewer.ID
	}
	isSelf := viewer != nil && viewer.ID == profile.ID

	mode := r.URL.Query().Get("view")
	if mode != "calendar" && mode != "graph" {
		mode = "timeline"
	}

	var (
		view     NoteListView
		tab      string
		calendar *calendarGridProps
	)
	switch mode {
	case "calendar":
		grid, err := s.buildCalendarGrid(r.Context(), profile.ID, r.URL.Query().Get("m"), isSelf)
		if err != nil {
			s.renderError(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		calendar = &grid
	case "graph":
		// Nothing to load server-side — the graph box fetches
		// /api/graph?user=... itself, same as /u/{user}/graph.
	default:
		olderThan := parseFeedCursor(r.URL.Query().Get("older"), r.URL.Query().Get("older_id"))
		tab = r.URL.Query().Get("tab")
		if tab != "drafts" && tab != "all" {
			tab = ""
		}

		recent, nextCursor, err := loadRecentNotes(r, s.DB, profile.ID, profile.Handle, viewerID, tab, olderThan)
		if err != nil {
			s.renderError(w, r, http.StatusInternalServerError, err.Error())
			return
		}

		view = NoteListView{
			Cards:       recent,
			Layout:      "list",
			OlderURL:    profileOlderURL(profile.Handle, nextCursor, tab),
			Empty:       profileEmpty(tab),
			GroupByDate: true,
		}
		view.Manage = isSelf
		if r.Header.Get("HX-Request") == "true" {
			s.renderFragment(w, r, notesFragment(view))
			return
		}
	}

	following, _ := userFollows(r.Context(), s.DB, viewerID, profile.ID)
	followers, _ := followerCount(r.Context(), s.DB, profile.ID)
	followingN, _ := followingCount(r.Context(), s.DB, profile.ID)

	var noteCount int
	_ = s.DB.QueryRowContext(r.Context(),
		profileNoteCountQuery(isSelf),
		profile.ID,
	).Scan(&noteCount)

	var createdAt int64
	_ = s.DB.QueryRowContext(r.Context(),
		`SELECT created_at FROM users WHERE id = $1`,
		profile.ID,
	).Scan(&createdAt)
	estYear := 0
	if createdAt > 0 {
		estYear = time.Unix(createdAt, 0).Year()
	}

	var pinned *pinnedNote
	pinned, _ = pinnedNoteForUser(r.Context(), s.DB, profile.ID, isSelf)

	home, err := s.loadProfileHome(r.Context(), profile.ID, profile.Handle, profile.HomeTitle, profile.HomeBio, profile.HomeTopics, profile.Showcases, isSelf)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	s.renderPage(w, r, pageTitle("@"+profile.Handle), "", nil, profilePage(ProfilePageProps{
		Handle:         profile.Handle,
		ProfileID:      profile.ID,
		Mode:           mode,
		View:           view,
		Calendar:       calendar,
		Tab:            tab,
		IsSelf:         isSelf,
		ViewerLoggedIn: viewer != nil,
		Following:      following,
		FollowerCount:  followers,
		FollowingCount: followingN,
		NoteCount:      noteCount,
		EstYear:        estYear,
		Pinned:         pinned,
		Home:           home,
	}))
}

func profileNoteCountQuery(includePrivate bool) string {
	q := `SELECT COUNT(*) FROM notes WHERE author_id = $1 AND hidden_at IS NULL AND deleted_at IS NULL`
	if !includePrivate {
		q += ` AND published_at IS NOT NULL`
	}
	return q
}

type profileHome struct {
	Title      string
	BodyMD     string
	BodyHTML   template.HTML
	TopicInput string
	Showcases  []profileShowcase
}

type profileShowcase struct {
	Type      string        `json:"type"`
	Title     string        `json:"title"`
	Ref       string        `json:"ref"`
	BodyMD    string        `json:"body"`
	BodyHTML  template.HTML `json:"-"`
	NoteTitle string        `json:"-"`
	NoteURL   string        `json:"-"`
	Excerpt   string        `json:"-"`
	HasNote   bool          `json:"-"`
	IsDraft   bool          `json:"-"`
}

func (h profileHome) HasDisplay() bool {
	return h.Title != "" || h.BodyMD != "" || len(h.Showcases) > 0
}

func profileHomeTitle(h profileHome, handle string) string {
	if h.Title != "" {
		return h.Title
	}
	return "進入 @" + handle + " 的小屋"
}

const (
	profileShowcaseNote = "note"
	profileShowcaseTag  = "tag"
	profileShowcaseText = "text"
)

func (s *Server) loadProfileHome(ctx context.Context, authorID uuid.UUID, handle, title, bodyMD, topicInput, showcaseJSON string, includePrivate bool) (profileHome, error) {
	h := profileHome{
		Title:      strings.TrimSpace(title),
		BodyMD:     strings.TrimSpace(bodyMD),
		TopicInput: profileTopicInput(topicInput),
	}
	if h.BodyMD != "" {
		bodyHTML, err := s.renderProfileHomeMarkdown(ctx, handle, h.BodyMD)
		if err != nil {
			return h, err
		}
		h.BodyHTML = bodyHTML
	}
	showcases := profileShowcasesFromJSON(showcaseJSON)
	if len(showcases) == 0 {
		showcases = legacyTopicShowcases(topicInput)
	}
	for _, item := range showcases {
		showcase, err := s.loadProfileShowcase(ctx, authorID, handle, item, includePrivate)
		if err != nil {
			return h, err
		}
		if profileShowcaseVisible(showcase, includePrivate) {
			h.Showcases = append(h.Showcases, showcase)
		}
	}
	return h, nil
}

func (s *Server) renderProfileHomeMarkdown(ctx context.Context, handle, bodyMD string) (template.HTML, error) {
	rendered, err := markdown.Render(bodyMD, handle, s.buildProfileHomeResolver(ctx, handle, markdown.Extract(bodyMD)), nil)
	if err != nil {
		return "", err
	}
	return stripUnresolvedProfileLinks(rendered)
}

func profileTopicInput(input string) string {
	return strings.Join(profileTopicTags(input), ", ")
}

func profileTopicTags(input string) []string {
	tags := parseTags(input)
	if len(tags) > profileTopicLimit {
		tags = tags[:profileTopicLimit]
	}
	return tags
}

const profileTopicLimit = 8

const (
	profileShowcaseLimit    = 6
	profileShowcaseMinSlots = 4
)

func profileShowcasesFromJSON(raw string) []profileShowcase {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var showcases []profileShowcase
	if err := json.Unmarshal([]byte(raw), &showcases); err != nil {
		return nil
	}
	return normalizeProfileShowcases(showcases)
}

func normalizeProfileShowcases(input []profileShowcase) []profileShowcase {
	out := make([]profileShowcase, 0, min(len(input), profileShowcaseLimit))
	for _, item := range input {
		item.Type = normalizeProfileShowcaseType(item.Type)
		item.Title = strings.TrimSpace(item.Title)
		item.BodyMD = strings.TrimSpace(item.BodyMD)
		item.Ref = normalizeProfileShowcaseRef(item.Type, item.Ref)
		if item.Type == "" || profileShowcaseBlank(item) {
			continue
		}
		out = append(out, item)
		if len(out) == profileShowcaseLimit {
			break
		}
	}
	return out
}

func normalizeProfileShowcaseType(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case profileShowcaseNote, profileShowcaseTag, profileShowcaseText:
		return strings.ToLower(strings.TrimSpace(kind))
	default:
		return ""
	}
}

func normalizeProfileShowcaseRef(kind, ref string) string {
	ref = strings.TrimSpace(ref)
	switch kind {
	case profileShowcaseNote:
		ref = strings.TrimPrefix(strings.TrimSuffix(ref, "]]"), "[[")
		if u, err := url.Parse(ref); err == nil && u.Path != "" {
			ref = u.Path
		}
		ref = strings.Trim(ref, "/")
		if idx := strings.LastIndex(ref, "/"); idx >= 0 {
			ref = ref[idx+1:]
		}
		return kebabSlug(ref)
	case profileShowcaseTag:
		return normalizeTag(ref)
	default:
		return ""
	}
}

func profileShowcaseBlank(item profileShowcase) bool {
	if item.Type == profileShowcaseText {
		return item.Title == "" && item.BodyMD == ""
	}
	return item.Title == "" && item.Ref == "" && item.BodyMD == ""
}

func legacyTopicShowcases(topicInput string) []profileShowcase {
	tags := profileTopicTags(topicInput)
	showcases := make([]profileShowcase, 0, len(tags))
	for _, tag := range tags {
		showcases = append(showcases, profileShowcase{
			Type: profileShowcaseTag,
			Ref:  tag,
		})
	}
	return showcases
}

func profileShowcaseVisible(item profileShowcase, includePrivate bool) bool {
	if item.Type == profileShowcaseText {
		return item.Title != "" || item.BodyMD != ""
	}
	if item.HasNote {
		return true
	}
	return includePrivate
}

func (s *Server) loadProfileShowcase(ctx context.Context, authorID uuid.UUID, handle string, item profileShowcase, includePrivate bool) (profileShowcase, error) {
	if item.BodyMD != "" {
		bodyHTML, err := s.renderProfileHomeMarkdown(ctx, handle, item.BodyMD)
		if err != nil {
			return item, err
		}
		item.BodyHTML = bodyHTML
	}
	switch item.Type {
	case profileShowcaseNote:
		if item.Ref == "" {
			return item, nil
		}
		return loadProfileShowcaseNote(ctx, s.DB, authorID, handle, item, includePrivate)
	case profileShowcaseTag:
		if item.Ref == "" {
			return item, nil
		}
		return loadProfileShowcaseTag(ctx, s.DB, authorID, handle, item)
	default:
		return item, nil
	}
}

func loadProfileShowcaseNote(ctx context.Context, db *sql.DB, authorID uuid.UUID, handle string, item profileShowcase, includePrivate bool) (profileShowcase, error) {
	query := `
		SELECT title, slug, body_md, published_at IS NULL
		FROM notes
		WHERE author_id = $1 AND slug = $2
		  AND hidden_at IS NULL AND deleted_at IS NULL`
	if !includePrivate {
		query += ` AND published_at IS NOT NULL`
	}
	query += ` LIMIT 1`
	var title, slug, body string
	var draft bool
	err := db.QueryRowContext(ctx, query, authorID, item.Ref).Scan(&title, &slug, &body, &draft)
	if errors.Is(err, sql.ErrNoRows) {
		return item, nil
	}
	if err != nil {
		return item, err
	}
	item.HasNote = true
	item.IsDraft = draft
	item.NoteTitle = title
	item.NoteURL = noteURL(handle, slug)
	item.Excerpt = markdown.Excerpt(body, 110)
	return item, nil
}

func loadProfileShowcaseTag(ctx context.Context, db *sql.DB, authorID uuid.UUID, handle string, item profileShowcase) (profileShowcase, error) {
	var title, slug, body string
	err := db.QueryRowContext(ctx, `
		SELECT n.title, n.slug, n.body_md
		FROM note_tags nt
		JOIN notes n ON n.id = nt.note_id
		WHERE n.author_id = $1 AND lower(nt.tag) = $2
		  AND n.hidden_at IS NULL AND n.deleted_at IS NULL AND n.published_at IS NOT NULL
		ORDER BY n.updated_at DESC, n.id DESC
		LIMIT 1`, authorID, item.Ref).Scan(&title, &slug, &body)
	if errors.Is(err, sql.ErrNoRows) {
		return item, nil
	}
	if err != nil {
		return item, err
	}
	item.HasNote = true
	item.NoteTitle = title
	item.NoteURL = noteURL(handle, slug)
	item.Excerpt = markdown.Excerpt(body, 110)
	return item, nil
}

func profileShowcaseEditorSlots(showcases []profileShowcase) []profileShowcase {
	slots := make([]profileShowcase, 0, profileShowcaseLimit)
	for _, showcase := range showcases {
		if len(slots) == profileShowcaseLimit {
			break
		}
		slots = append(slots, showcase)
	}
	for len(slots) < profileShowcaseMinSlots {
		slots = append(slots, profileShowcase{})
	}
	return slots
}

func profileShowcaseKindLabel(kind string) string {
	switch kind {
	case profileShowcaseNote:
		return "手選筆記"
	case profileShowcaseTag:
		return "主題房間"
	case profileShowcaseText:
		return "自由文字"
	default:
		return "展示櫃"
	}
}

func profileShowcaseTitle(item profileShowcase) string {
	if item.Title != "" {
		return item.Title
	}
	if item.Type == profileShowcaseTag && item.Ref != "" {
		return item.Ref
	}
	return profileShowcaseKindLabel(item.Type)
}

func profileShowcaseKey(item profileShowcase) string {
	return item.Type + ":" + item.Ref
}

func profileShowcaseEmptyText(item profileShowcase) string {
	switch item.Type {
	case profileShowcaseNote:
		return "待選一篇公開筆記"
	case profileShowcaseTag:
		return "待放一篇公開筆記"
	default:
		return "空展示櫃"
	}
}

func (s *Server) buildProfileHomeResolver(ctx context.Context, vaultHandle string, links []markdown.WikiLink) markdown.Resolver {
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
			WHERE u.handle_ci = lower($1) AND n.slug = $2
			  AND n.hidden_at IS NULL AND n.deleted_at IS NULL AND n.published_at IS NOT NULL`,
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

func stripUnresolvedProfileLinks(src template.HTML) (template.HTML, error) {
	nodes, err := nethtml.ParseFragment(strings.NewReader(string(src)), &nethtml.Node{
		Type:     nethtml.ElementNode,
		DataAtom: atom.Div,
		Data:     "div",
	})
	if err != nil {
		return "", err
	}
	for _, n := range nodes {
		stripUnresolvedProfileLinkNode(n)
	}
	var b bytes.Buffer
	for _, n := range nodes {
		if err := nethtml.Render(&b, n); err != nil {
			return "", err
		}
	}
	return template.HTML(b.String()), nil
}

func stripUnresolvedProfileLinkNode(n *nethtml.Node) {
	if n.Type == nethtml.ElementNode && n.Data == "a" && profileLinkUnresolved(n) {
		n.DataAtom = atom.Span
		n.Data = "span"
		attrs := n.Attr[:0]
		for _, attr := range n.Attr {
			if attr.Key != "href" {
				attrs = append(attrs, attr)
			}
		}
		n.Attr = attrs
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		stripUnresolvedProfileLinkNode(c)
	}
}

func profileLinkUnresolved(n *nethtml.Node) bool {
	for _, attr := range n.Attr {
		if attr.Key != "class" {
			continue
		}
		for _, cls := range strings.Fields(attr.Val) {
			if cls == "wiki-unresolved" || cls == "wiki-cross-unresolved" {
				return true
			}
		}
	}
	return false
}

// profileOlderURL builds the infinite-scroll cursor URL, or "" when there's
// no next page (an unset cursor, as returned by loadRecentNotes).
func profileOlderURL(handle string, nextCursor feedCursor, tab string) string {
	if !nextCursor.set() {
		return ""
	}
	href := "/" + handle + "?older=" + strconv.FormatInt(nextCursor.UpdatedAt, 10) + "&older_id=" + nextCursor.NoteID.String()
	if tab != "" {
		href += "&tab=" + url.QueryEscape(tab)
	}
	return href
}

func profileEmpty(tab string) templ.Component {
	if tab == "drafts" {
		return emptyText("還沒有草稿。")
	}
	return emptyText("還沒有筆記。")
}

// loadRecentNotes lists a user's notes as feedCards (the shared card model —
// see notes_view.templ). Unpublished drafts are included only when the
// viewer is the author themselves.
func loadRecentNotes(r *http.Request, db *sql.DB, authorID uuid.UUID, handle string, viewerID uuid.UUID, tab string, olderThan feedCursor) ([]feedCard, feedCursor, error) {
	query := `SELECT ` + noteCardColumns + `
		FROM notes n
		JOIN users u ON u.id = n.author_id
		WHERE n.author_id = $1 AND n.hidden_at IS NULL AND n.deleted_at IS NULL`
	args := []any{authorID}
	isSelf := viewerID == authorID
	if !isSelf {
		query += ` AND n.published_at IS NOT NULL`
	} else if tab == "drafts" {
		query += ` AND n.published_at IS NULL`
	} else if tab != "all" {
		query += ` AND n.published_at IS NOT NULL`
	}
	if olderThan.set() {
		args = append(args, olderThan.UpdatedAt, olderThan.NoteID)
		query += ` AND (n.updated_at, n.id) < ($2, $3)`
	}
	query += ` ORDER BY n.updated_at DESC, n.id DESC LIMIT 20`
	rows, err := db.QueryContext(r.Context(), query, args...)
	if err != nil {
		return nil, feedCursor{}, err
	}
	out, err := scanCards(rows)
	if err != nil {
		return nil, feedCursor{}, err
	}
	// AuthorHandle blanked: it's always this profile's own handle, redundant
	// on your own vault page (listCard omits it when empty).
	for i := range out {
		out[i].AuthorHandle = ""
	}
	var nextCursor feedCursor
	if len(out) == 20 {
		last := out[len(out)-1]
		nextCursor = feedCursor{UpdatedAt: last.UpdatedAt, NoteID: last.NoteID}
	}
	return out, nextCursor, nil
}
