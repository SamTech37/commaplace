package handlers

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// PostFollow updates the count with an out-of-band swap, which htmx applies by
// matching the fragment's id against the live DOM. A missing id is not an
// error — htmx silently does nothing (.claude/htmx-rules.md 7) — so the only
// way this stays wired is a test that both surfaces still carry the target.
func TestFollowCountOOBTargetExists(t *testing.T) {
	const target = `id="follow-count-followers"`

	var profile strings.Builder
	err := profilePage(ProfilePageProps{
		Handle:    "alice",
		ProfileID: uuid.New(),
		Mode:      "timeline",
		View:      NoteListView{Empty: emptyText("")},
	}).Render(context.Background(), &profile)
	if err != nil {
		t.Fatalf("render profile: %v", err)
	}
	if !strings.Contains(profile.String(), target) {
		t.Errorf("profile page is missing the OOB count target %s", target)
	}

	var note strings.Builder
	err = noteContent(NoteViewProps{
		AuthorHandle: "alice",
		AuthorID:     uuid.New(),
	}).Render(context.Background(), &note)
	if err != nil {
		t.Fatalf("render note: %v", err)
	}
	if !strings.Contains(note.String(), target) {
		t.Errorf("note page is missing the OOB count target %s", target)
	}
}
