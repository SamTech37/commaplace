package handlers

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// queryNotesInRange returns one author's notes updated in [start, end),
// oldest first — the shared time-bounded query behind both the calendar
// grid and the timeline view. Includes drafts (both surfaces are the
// author's own private writing-activity view, not a public listing).
func (s *Server) queryNotesInRange(ctx context.Context, authorID uuid.UUID, start, end time.Time) ([]feedCard, error) {
	rows, err := s.DB.QueryContext(ctx, fmt.Sprintf(`
		SELECT %s
		FROM notes n
		JOIN users u ON u.id = n.author_id
		WHERE n.author_id = $1 AND n.deleted_at IS NULL
		  AND n.updated_at >= $2 AND n.updated_at < $3
		ORDER BY n.updated_at ASC`, noteCardColumns),
		authorID, start.Unix(), end.Unix())
	if err != nil {
		return nil, err
	}
	return scanCards(rows)
}
