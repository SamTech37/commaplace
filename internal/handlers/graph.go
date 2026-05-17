package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// GetGraph renders the graph page shell. The actual node/edge data is
// fetched client-side from /api/graph and laid out with a force simulation.
func (s *Server) GetGraph(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "graph", nil)
}

type graphNode struct {
	ID         string `json:"id"` // "n:<noteID>" internal, "x:<noteID>" external
	Title      string `json:"title"`
	URL        string `json:"url"`
	Author     string `json:"author"`
	IsExternal bool   `json:"ext,omitempty"`
}

type graphEdge struct {
	Source string `json:"s"`
	Target string `json:"t"`
}

type graphPayload struct {
	Nodes []graphNode `json:"nodes"`
	Edges []graphEdge `json:"edges"`
}

func internalNodeID(id int64) string { return fmt.Sprintf("n:%d", id) }
func externalNodeID(id int64) string { return fmt.Sprintf("x:%d", id) }
func userNodeKey(handle, slug string) string {
	return "u:" + handle + "/" + slug
}

func (s *Server) GetGraphData(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	payload := graphPayload{Nodes: []graphNode{}, Edges: []graphEdge{}}

	// ---- internal notes ----
	intRows, err := s.DB.QueryContext(ctx, `
		SELECT n.id, n.title, n.slug, u.handle
		FROM notes n
		JOIN users u ON u.id = n.author_id
		WHERE n.hidden_at IS NULL AND n.deleted_at IS NULL
		ORDER BY n.id`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer intRows.Close()
	internalKeep := map[int64]bool{}
	// for cross-vault edge resolution: (handle/slug) -> internal node ID
	userKeyToNode := map[string]string{}
	for intRows.Next() {
		var id int64
		var title, slug, handle string
		if err := intRows.Scan(&id, &title, &slug, &handle); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		nodeID := internalNodeID(id)
		payload.Nodes = append(payload.Nodes, graphNode{
			ID:     nodeID,
			Title:  title,
			URL:    noteURL(handle, slug),
			Author: handle,
		})
		internalKeep[id] = true
		userKeyToNode[userNodeKey(handle, slug)] = nodeID
	}

	// ---- external notes ----
	extRows, err := s.DB.QueryContext(ctx, `
		SELECT en.id, en.title, en.slug, ev.slug,
		       COALESCE(NULLIF(ev.display_name, ''), ev.slug)
		FROM external_notes en
		JOIN external_vaults ev ON ev.id = en.vault_id
		ORDER BY en.id`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer extRows.Close()
	externalKeep := map[int64]bool{}
	for extRows.Next() {
		var (
			id          int64
			title       string
			noteSlug    string
			vaultSlug   string
			displayName string
		)
		if err := extRows.Scan(&id, &title, &noteSlug, &vaultSlug, &displayName); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		payload.Nodes = append(payload.Nodes, graphNode{
			ID:         externalNodeID(id),
			Title:      title,
			URL:        "/x/" + vaultSlug + "/" + noteSlug,
			Author:     displayName,
			IsExternal: true,
		})
		externalKeep[id] = true
	}

	// ---- internal -> internal edges ----
	intEdges, err := s.DB.QueryContext(ctx, `
		SELECT source_note_id, resolved_target_id
		FROM links
		WHERE resolved_target_id IS NOT NULL`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer intEdges.Close()
	seen := map[[2]string]bool{}
	addEdge := func(src, tgt string) {
		if src == "" || tgt == "" || src == tgt {
			return
		}
		key := [2]string{src, tgt}
		if seen[key] {
			return
		}
		seen[key] = true
		payload.Edges = append(payload.Edges, graphEdge{Source: src, Target: tgt})
	}
	for intEdges.Next() {
		var src, tgt int64
		if err := intEdges.Scan(&src, &tgt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if !internalKeep[src] || !internalKeep[tgt] {
			continue
		}
		addEdge(internalNodeID(src), internalNodeID(tgt))
	}

	// ---- external -> {external, internal} edges ----
	extEdges, err := s.DB.QueryContext(ctx, `
		SELECT source_external_note_id, target_kind, target_external_note_id,
		       target_user_handle, target_slug
		FROM external_links`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer extEdges.Close()
	for extEdges.Next() {
		var (
			srcID            int64
			kind             string
			targetExtID      *int64
			targetUserHandle string
			targetSlug       string
		)
		if err := extEdges.Scan(&srcID, &kind, &targetExtID, &targetUserHandle, &targetSlug); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if !externalKeep[srcID] {
			continue
		}
		switch kind {
		case "external_same", "external_cross":
			if targetExtID != nil && externalKeep[*targetExtID] {
				addEdge(externalNodeID(srcID), externalNodeID(*targetExtID))
			}
		case "commonplace_user":
			if tgt, ok := userKeyToNode[userNodeKey(targetUserHandle, targetSlug)]; ok {
				addEdge(externalNodeID(srcID), tgt)
			}
		}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(payload)
}

// GetGraphLocal returns the ego graph (center note + 1-hop neighbors + all
// edges among them) for a single note. The note ref is "n:<id>" for internal
// notes and "x:<id>" for external notes.
func (s *Server) GetGraphLocal(w http.ResponseWriter, r *http.Request) {
	ref := r.URL.Query().Get("note")
	kind, id, err := parseNodeRef(ref)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	ctx := r.Context()

	// nodeIDs: maps "n:<id>" / "x:<id>" → struct{} for fast membership.
	internalIDs := map[int64]bool{}
	externalIDs := map[int64]bool{}
	switch kind {
	case "n":
		internalIDs[id] = true
	case "x":
		externalIDs[id] = true
	}

	// Step 1: collect 1-hop neighbors via every link relationship.
	if err := s.collectNeighbors(ctx, kind, id, internalIDs, externalIDs); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Step 2: realise nodes from the two id sets.
	payload := graphPayload{Nodes: []graphNode{}, Edges: []graphEdge{}}
	userKeyToNode := map[string]string{}
	if len(internalIDs) > 0 {
		ids := keysInt64(internalIDs)
		rows, err := s.DB.QueryContext(ctx, `
			SELECT n.id, n.title, n.slug, u.handle
			FROM notes n JOIN users u ON u.id = n.author_id
			WHERE n.id IN (`+inPlaceholders(len(ids))+`) AND n.hidden_at IS NULL AND n.deleted_at IS NULL`,
			toAnySlice(ids)...)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		for rows.Next() {
			var nid int64
			var title, slug, handle string
			if err := rows.Scan(&nid, &title, &slug, &handle); err != nil {
				rows.Close()
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			nodeID := internalNodeID(nid)
			payload.Nodes = append(payload.Nodes, graphNode{
				ID:     nodeID,
				Title:  title,
				URL:    noteURL(handle, slug),
				Author: handle,
			})
			userKeyToNode[userNodeKey(handle, slug)] = nodeID
		}
		rows.Close()
	}
	if len(externalIDs) > 0 {
		ids := keysInt64(externalIDs)
		rows, err := s.DB.QueryContext(ctx, `
			SELECT en.id, en.title, en.slug, ev.slug,
			       COALESCE(NULLIF(ev.display_name, ''), ev.slug)
			FROM external_notes en JOIN external_vaults ev ON ev.id = en.vault_id
			WHERE en.id IN (`+inPlaceholders(len(ids))+`)`,
			toAnySlice(ids)...)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		for rows.Next() {
			var (
				eid        int64
				title      string
				noteSlug   string
				vaultSlug  string
				vaultName  string
			)
			if err := rows.Scan(&eid, &title, &noteSlug, &vaultSlug, &vaultName); err != nil {
				rows.Close()
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			payload.Nodes = append(payload.Nodes, graphNode{
				ID:         externalNodeID(eid),
				Title:      title,
				URL:        "/x/" + vaultSlug + "/" + noteSlug,
				Author:     vaultName,
				IsExternal: true,
			})
		}
		rows.Close()
	}

	// Step 3: gather edges where both endpoints are inside the node set.
	if err := s.collectEdges(ctx, internalIDs, externalIDs, userKeyToNode, &payload); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Stamp the center node so the client can render it differently.
	centerID := ""
	switch kind {
	case "n":
		centerID = internalNodeID(id)
	case "x":
		centerID = externalNodeID(id)
	}
	type localPayload struct {
		graphPayload
		Center string `json:"center"`
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(localPayload{graphPayload: payload, Center: centerID})
}

func parseNodeRef(ref string) (kind string, id int64, err error) {
	if i := strings.Index(ref, ":"); i > 0 {
		kind = ref[:i]
		id, err = strconv.ParseInt(ref[i+1:], 10, 64)
		if err == nil && (kind == "n" || kind == "x") {
			return kind, id, nil
		}
	}
	return "", 0, errors.New(`note ref must be "n:<id>" or "x:<id>"`)
}

// collectNeighbors finds every note (internal + external) that has a direct
// link to/from the center and adds their IDs to the running sets.
func (s *Server) collectNeighbors(ctx context.Context, kind string, id int64, internalIDs, externalIDs map[int64]bool) error {
	q := func(query string, args []any, dst map[int64]bool) error {
		rows, err := s.DB.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		return scanIDs(rows, dst)
	}
	switch kind {
	case "n":
		if err := q(`SELECT resolved_target_id FROM links WHERE source_note_id = ? AND resolved_target_id IS NOT NULL`, []any{id}, internalIDs); err != nil {
			return err
		}
		if err := q(`SELECT source_note_id FROM links WHERE resolved_target_id = ?`, []any{id}, internalIDs); err != nil {
			return err
		}
		var handle, slug string
		if err := s.DB.QueryRowContext(ctx, `
			SELECT u.handle, n.slug FROM notes n
			JOIN users u ON u.id = n.author_id
			WHERE n.id = ?`, id).Scan(&handle, &slug); err == nil {
			if err := q(`
				SELECT source_external_note_id FROM external_links
				WHERE target_kind = 'commonplace_user'
				  AND target_user_handle = ? AND target_slug = ?`,
				[]any{handle, slug}, externalIDs); err != nil {
				return err
			}
		}
	case "x":
		if err := q(`SELECT target_external_note_id FROM external_links
			 WHERE source_external_note_id = ? AND target_external_note_id IS NOT NULL`,
			[]any{id}, externalIDs); err != nil {
			return err
		}
		if err := q(`SELECT source_external_note_id FROM external_links WHERE target_external_note_id = ?`,
			[]any{id}, externalIDs); err != nil {
			return err
		}
		if err := q(`
			SELECT n.id FROM external_links el
			JOIN users u ON u.handle = el.target_user_handle
			JOIN notes n ON n.author_id = u.id AND n.slug = el.target_slug
			WHERE el.source_external_note_id = ? AND el.target_kind = 'commonplace_user'`,
			[]any{id}, internalIDs); err != nil {
			return err
		}
	}
	return nil
}

// collectEdges adds every edge whose endpoints are both already in the node
// set. Avoids duplicates by canonicalising direction-insensitive keys.
func (s *Server) collectEdges(ctx context.Context, internalIDs, externalIDs map[int64]bool, userKeyToNode map[string]string, payload *graphPayload) error {
	seen := map[[2]string]bool{}
	add := func(src, tgt string) {
		if src == "" || tgt == "" || src == tgt {
			return
		}
		key := [2]string{src, tgt}
		if seen[key] {
			return
		}
		seen[key] = true
		payload.Edges = append(payload.Edges, graphEdge{Source: src, Target: tgt})
	}

	if len(internalIDs) > 0 {
		ids := keysInt64(internalIDs)
		ph := inPlaceholders(len(ids))
		args := toAnySlice(ids)
		// internal-internal edges
		rows, err := s.DB.QueryContext(ctx, `
			SELECT source_note_id, resolved_target_id FROM links
			WHERE resolved_target_id IS NOT NULL
			  AND source_note_id IN (`+ph+`)
			  AND resolved_target_id IN (`+ph+`)`,
			append(args, args...)...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var src, tgt int64
			if err := rows.Scan(&src, &tgt); err != nil {
				rows.Close()
				return err
			}
			add(internalNodeID(src), internalNodeID(tgt))
		}
		rows.Close()
	}

	if len(externalIDs) > 0 {
		ids := keysInt64(externalIDs)
		ph := inPlaceholders(len(ids))
		args := toAnySlice(ids)
		// external→external edges (both ends in set)
		rows, err := s.DB.QueryContext(ctx, `
			SELECT source_external_note_id, target_external_note_id FROM external_links
			WHERE target_external_note_id IS NOT NULL
			  AND source_external_note_id IN (`+ph+`)
			  AND target_external_note_id IN (`+ph+`)`,
			append(args, args...)...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var src, tgt int64
			if err := rows.Scan(&src, &tgt); err != nil {
				rows.Close()
				return err
			}
			add(externalNodeID(src), externalNodeID(tgt))
		}
		rows.Close()

		// external→commonplace user edges (target is internal, in set)
		rows2, err := s.DB.QueryContext(ctx, `
			SELECT el.source_external_note_id, n.id
			FROM external_links el
			JOIN users u ON u.handle = el.target_user_handle
			JOIN notes n ON n.author_id = u.id AND n.slug = el.target_slug
			WHERE el.target_kind = 'commonplace_user'
			  AND el.source_external_note_id IN (`+ph+`)`, args...)
		if err != nil {
			return err
		}
		for rows2.Next() {
			var src, tgt int64
			if err := rows2.Scan(&src, &tgt); err != nil {
				rows2.Close()
				return err
			}
			if internalIDs[tgt] {
				add(externalNodeID(src), internalNodeID(tgt))
			}
		}
		rows2.Close()
	}
	return nil
}

// ---------- small helpers ----------

func keysInt64(m map[int64]bool) []int64 {
	out := make([]int64, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func toAnySlice(ids []int64) []any {
	out := make([]any, len(ids))
	for i, v := range ids {
		out[i] = v
	}
	return out
}

func inPlaceholders(n int) string {
	if n == 0 {
		return ""
	}
	return strings.Repeat("?,", n-1) + "?"
}

// scanIDs reads single-column int64 rows and adds them to dst.
// If dst is nil this becomes a noop drain (used to consume the row stream
// while still wanting to surface errors).
func scanIDs(rows interface {
	Next() bool
	Scan(...any) error
	Close() error
	Err() error
}, dst map[int64]bool) error {
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return err
		}
		if dst != nil {
			dst[id] = true
		}
	}
	return rows.Err()
}
