package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
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
	byDay, err := s.notesByDay(ctx, aliceID, start, end, true)
	if err != nil {
		t.Fatal(err)
	}
	key := now.In(time.Local).Format("2006-01-02")
	notes := byDay[key]
	if len(notes) != 1 || notes[0].Title != "Today Note" || notes[0].URL != "/alice/today-note" {
		t.Errorf("byDay[%s] = %+v", key, notes)
	}
}

// The profile page's ?view=calendar mode replaced the standalone /me/calendar
// route — this guards the owner sees their own draft in the grid, and a
// stranger visiting the same URL does not.
func TestGetProfileCalendarViewShowsOwnDraftOnlyToSelf(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	alice := mkUser(t, s, "alice")
	bob := mkUser(t, s, "bob")

	if _, err := s.saveNote(ctx, alice, "alice", "today-note", "Today Note", "body", nil); err != nil {
		t.Fatalf("saveNote: %v", err)
	}
	if _, err := s.createDraft(ctx, alice); err != nil {
		t.Fatalf("createDraft: %v", err)
	}

	req := authedRequest(s, alice, http.MethodGet, "/alice?view=calendar", "")
	req.SetPathValue("user", "alice")
	w := httptest.NewRecorder()
	s.GetProfile(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetProfile (self): status %d body=%s", w.Code, w.Body)
	}
	body := w.Body.String()
	if !strings.Contains(body, "calendar-grid") {
		t.Fatalf("expected the calendar grid, got:\n%s", body)
	}
	if !strings.Contains(body, "Today Note") {
		t.Errorf("published note missing from own calendar:\n%s", body)
	}
	if !strings.Contains(body, untitledDraftLabel) {
		t.Errorf("own draft missing placeholder %q:\n%s", untitledDraftLabel, body)
	}

	req = authedRequest(s, bob, http.MethodGet, "/alice?view=calendar", "")
	req.SetPathValue("user", "alice")
	w = httptest.NewRecorder()
	s.GetProfile(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetProfile (stranger): status %d body=%s", w.Code, w.Body)
	}
	body = w.Body.String()
	if !strings.Contains(body, "Today Note") {
		t.Errorf("published note missing from stranger's view:\n%s", body)
	}
	if strings.Contains(body, untitledDraftLabel) {
		t.Error("stranger saw alice's draft on her calendar")
	}
}
