package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// GetGraph renders the graph page shell. The actual node/edge data is
// fetched client-side from /api/graph and laid out with a force simulation.
func (s *Server) GetGraph(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, r, pageTitle("graph"), "", nil, graphPage("", "/api/graph"))
}

type graphNode struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	URL    string `json:"url"`
	Author string `json:"author"`
}

type graphEdge struct {
	Source string `json:"s"`
	Target string `json:"t"`
}

type graphPayload struct {
	Nodes []graphNode `json:"nodes"`
	Edges []graphEdge `json:"edges"`
}

// GetUserGraph renders a graph view filtered to one user's notes.
// Route: GET /u/{user}/graph
func (s *Server) GetUserGraph(w http.ResponseWriter, r *http.Request) {
	handle := r.PathValue("user")
	title := "@" + handle
	s.renderPage(w, r, pageTitle(title+" · graph"), "", nil, graphPage(title, "/api/graph?user="+handle))
}

func (s *Server) GetTagGraph(w http.ResponseWriter, r *http.Request) {
	tag := r.PathValue("tag")
	title := "#" + tag
	s.renderPage(w, r, pageTitle(title+" · graph"), "", nil, graphPage(title, "/api/graph?tag="+tag))
}

func (s *Server) GetGraphData(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	payload := graphPayload{Nodes: []graphNode{}, Edges: []graphEdge{}}

	user := r.URL.Query().Get("user")
	tag := r.URL.Query().Get("tag")

	var (
		rows *sql.Rows
		err  error
	)
	switch {
	case user != "":
		rows, err = s.DB.QueryContext(ctx, `
			SELECT n.id, n.title, n.slug, u.handle
			FROM notes n JOIN users u ON u.id = n.author_id
			WHERE n.hidden_at IS NULL AND n.deleted_at IS NULL AND n.published_at IS NOT NULL
			  AND u.handle = $1
			ORDER BY n.id`, user)
	case tag != "":
		rows, err = s.DB.QueryContext(ctx, `
			SELECT DISTINCT n.id, n.title, n.slug, u.handle
			FROM notes n JOIN users u ON u.id = n.author_id
			JOIN note_tags t ON t.note_id = n.id
			WHERE n.hidden_at IS NULL AND n.deleted_at IS NULL AND n.published_at IS NOT NULL
			  AND t.tag = $1
			ORDER BY n.id`, tag)
	default:
		rows, err = s.DB.QueryContext(ctx, `
			SELECT n.id, n.title, n.slug, u.handle
			FROM notes n
			JOIN users u ON u.id = n.author_id
			WHERE n.hidden_at IS NULL AND n.deleted_at IS NULL AND n.published_at IS NOT NULL
			ORDER BY n.id`)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	keep := map[uuid.UUID]bool{}
	for rows.Next() {
		var id uuid.UUID
		var title, slug, handle string
		if err := rows.Scan(&id, &title, &slug, &handle); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		payload.Nodes = append(payload.Nodes, graphNode{
			ID:     id.String(),
			Title:  title,
			URL:    noteURL(handle, slug),
			Author: handle,
		})
		keep[id] = true
	}

	edgeRows, err := s.DB.QueryContext(ctx, `
		SELECT source_note_id, resolved_target_id
		FROM links
		WHERE resolved_target_id IS NOT NULL`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer edgeRows.Close()
	seen := map[[2]uuid.UUID]bool{}
	for edgeRows.Next() {
		var src, tgt uuid.UUID
		if err := edgeRows.Scan(&src, &tgt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if !keep[src] || !keep[tgt] || src == tgt {
			continue
		}
		key := [2]uuid.UUID{src, tgt}
		if seen[key] {
			continue
		}
		seen[key] = true
		payload.Edges = append(payload.Edges, graphEdge{Source: src.String(), Target: tgt.String()})
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(payload)
}

// GetGraphLocal returns the ego graph (center note + 1-hop neighbors + all
// edges among them) for a single note.
func (s *Server) GetGraphLocal(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.URL.Query().Get("note"))
	if err != nil {
		http.Error(w, `note ref must be a note id`, http.StatusBadRequest)
		return
	}
	ctx := r.Context()

	ids := map[uuid.UUID]bool{id: true}
	if err := s.collectNeighbors(ctx, id, ids); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	payload := graphPayload{Nodes: []graphNode{}, Edges: []graphEdge{}}
	idList := keysUUID(ids)
	rows, err := s.DB.QueryContext(ctx, `
		SELECT n.id, n.title, n.slug, u.handle
		FROM notes n JOIN users u ON u.id = n.author_id
		WHERE n.id IN (`+inPlaceholders(len(idList))+`) AND n.hidden_at IS NULL AND n.deleted_at IS NULL AND n.published_at IS NOT NULL`,
		toAnySlice(idList)...)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for rows.Next() {
		var nid uuid.UUID
		var title, slug, handle string
		if err := rows.Scan(&nid, &title, &slug, &handle); err != nil {
			rows.Close()
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		payload.Nodes = append(payload.Nodes, graphNode{
			ID:     nid.String(),
			Title:  title,
			URL:    noteURL(handle, slug),
			Author: handle,
		})
	}
	rows.Close()

	if err := s.collectEdges(ctx, ids, &payload); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	type localPayload struct {
		graphPayload
		Center string `json:"center"`
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(localPayload{graphPayload: payload, Center: id.String()})
}

// collectNeighbors finds every note with a direct link to/from the center
// and adds their IDs to the running set.
func (s *Server) collectNeighbors(ctx context.Context, id uuid.UUID, ids map[uuid.UUID]bool) error {
	q := func(query string, args []any) error {
		rows, err := s.DB.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		return scanIDs(rows, ids)
	}
	if err := q(`SELECT resolved_target_id FROM links WHERE source_note_id = $1 AND resolved_target_id IS NOT NULL`, []any{id}); err != nil {
		return err
	}
	if err := q(`SELECT source_note_id FROM links WHERE resolved_target_id = $1`, []any{id}); err != nil {
		return err
	}
	return nil
}

// collectEdges adds every edge whose endpoints are both already in the node
// set. Avoids duplicates by canonicalising direction-insensitive keys.
func (s *Server) collectEdges(ctx context.Context, ids map[uuid.UUID]bool, payload *graphPayload) error {
	seen := map[[2]uuid.UUID]bool{}
	add := func(src, tgt uuid.UUID) {
		if src == tgt {
			return
		}
		key := [2]uuid.UUID{src, tgt}
		if seen[key] {
			return
		}
		seen[key] = true
		payload.Edges = append(payload.Edges, graphEdge{Source: src.String(), Target: tgt.String()})
	}

	idList := keysUUID(ids)
	if len(idList) == 0 {
		return nil
	}
	ph := inPlaceholders(len(idList))
	args := toAnySlice(idList)
	n := len(args)
	args2 := make([]any, 0, n*2)
	args2 = append(args2, args...)
	args2 = append(args2, args...)
	rows, err := s.DB.QueryContext(ctx, `
		SELECT source_note_id, resolved_target_id FROM links
		WHERE resolved_target_id IS NOT NULL
		  AND source_note_id IN (`+ph+`)
		  AND resolved_target_id IN (`+inPlaceholdersFrom(n+1, n)+`)`,
		args2...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var src, tgt uuid.UUID
		if err := rows.Scan(&src, &tgt); err != nil {
			return err
		}
		add(src, tgt)
	}
	return rows.Err()
}

// ---------- small helpers ----------

func keysUUID(m map[uuid.UUID]bool) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func toAnySlice(ids []uuid.UUID) []any {
	out := make([]any, len(ids))
	for i, v := range ids {
		out[i] = v
	}
	return out
}

func inPlaceholders(n int) string {
	return inPlaceholdersFrom(1, n)
}

// inPlaceholdersFrom emits "$start,$start+1,...,$start+n-1".
func inPlaceholdersFrom(start, n int) string {
	if n == 0 {
		return ""
	}
	parts := make([]string, n)
	for i := 0; i < n; i++ {
		parts[i] = "$" + strconv.Itoa(start+i)
	}
	return strings.Join(parts, ",")
}

// scanIDs reads single-column uuid rows and adds them to dst.
func scanIDs(rows interface {
	Next() bool
	Scan(...any) error
	Close() error
	Err() error
}, dst map[uuid.UUID]bool) error {
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return err
		}
		if dst != nil {
			dst[id] = true
		}
	}
	return rows.Err()
}
