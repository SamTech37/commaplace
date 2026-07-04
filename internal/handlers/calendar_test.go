package handlers

import (
	"context"
	"testing"
	"time"
)

func TestNotesByDayBucketsByLocalDate(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	aliceID := mkUser(t, s, "alice")
	if _, err := s.saveNote(ctx, aliceID, "alice", "today-note", "Today Note", "body", nil); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	start := now.AddDate(0, 0, -1)
	end := now.AddDate(0, 0, 1)
	byDay, err := s.notesByDay(ctx, aliceID, start, end)
	if err != nil {
		t.Fatal(err)
	}
	key := now.In(time.Local).Format("2006-01-02")
	notes := byDay[key]
	if len(notes) != 1 || notes[0].Title != "Today Note" || notes[0].URL != "/alice/today-note" {
		t.Errorf("byDay[%s] = %+v", key, notes)
	}
}
