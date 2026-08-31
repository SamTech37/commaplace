package handlers

import (
	"context"
	"database/sql"
	"unicode/utf8"

	"github.com/google/uuid"
)

// loadTagsForNote returns a note's tags, alphabetical.
func loadTagsForNote(ctx context.Context, db *sql.DB, noteID uuid.UUID) ([]string, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT tag FROM note_tags WHERE note_id = $1 ORDER BY tag`, noteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

type authorStats struct {
	Followers int
	Notes     int
}

func loadAuthorStats(ctx context.Context, db *sql.DB, authorID uuid.UUID) (authorStats, error) {
	var s authorStats
	err := db.QueryRowContext(ctx, `
		SELECT
			(SELECT COUNT(*) FROM follows WHERE followed_id = $1),
			(SELECT COUNT(*) FROM notes  WHERE author_id   = $1
			                           AND hidden_at IS NULL
			                           AND deleted_at IS NULL
			                           AND published_at IS NOT NULL)`,
		authorID,
	).Scan(&s.Followers, &s.Notes)
	return s, err
}

func readingMinutes(body string) int {
	n := utf8.RuneCountInString(body)
	m := n / 350
	if m < 1 {
		return 1
	}
	return m
}
