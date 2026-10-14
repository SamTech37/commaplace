package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Self-replacing fragments must re-include their own hx-* (htmx-rules 5),
// including hx-disabled-elt, or the second click loses its in-flight state.
func TestSwapFragmentsKeepDisabledElt(t *testing.T) {
	s := newTestServer(t)
	alice := mkUser(t, s, "alice")
	bob := mkUser(t, s, "bob")
	noteID, err := s.saveNote(context.Background(), alice, "alice", "n1", "N1", "body", nil)
	if err != nil {
		t.Fatalf("saveNote: %v", err)
	}
	cases := []struct {
		name string
		h    http.HandlerFunc
		path string
		body string
	}{
		{"like", s.PostLike, "/api/like", "note_id=" + noteID.String()},
		{"save", s.PostSave, "/api/save", "note_id=" + noteID.String()},
		{"follow", s.PostFollow, "/api/follow", "user_id=" + bob.String()},
	}
	for _, tc := range cases {
		w := httptest.NewRecorder()
		tc.h(w, authedRequest(s, alice, http.MethodPost, tc.path, tc.body))
		if !strings.Contains(w.Body.String(), `hx-disabled-elt="find button"`) {
			t.Errorf("%s fragment lost hx-disabled-elt: %s", tc.name, w.Body)
		}
	}
}
