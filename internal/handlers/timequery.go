package handlers

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// queryNotesInRange returns one author's notes updated in [start, end),
// oldest first — the shared time-bounded query behind the profile calendar
// view. includeDrafts is true only when the viewer is the author (their own
// private writing-activity view); everyone else sees published notes only.
func (s *Server) queryNotesInRange(ctx context.Context, authorID uuid.UUID, start, end time.Time, includeDrafts bool) ([]feedCard, error) {
	query := fmt.Sprintf(`
		SELECT %s
		FROM notes n
		JOIN users u ON u.id = n.author_id
		WHERE n.author_id = $1 AND n.deleted_at IS NULL
		  AND n.updated_at >= $2 AND n.updated_at < $3`, noteCardColumns)
	if !includeDrafts {
		query += ` AND n.published_at IS NOT NULL`
	}
	query += ` ORDER BY n.updated_at ASC`
	rows, err := s.DB.QueryContext(ctx, query, authorID, start.Unix(), end.Unix())
	if err != nil {
		return nil, err
	}
	return scanCards(rows)
}
