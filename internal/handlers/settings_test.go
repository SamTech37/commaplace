package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"commonplace/internal/auth"
)

func postHandle(t *testing.T, s *Server, uid uuid.UUID, newHandle string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{"handle": {newHandle}}
	req := httptest.NewRequest("POST", "/settings/handle", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: auth.SessionCookie, Value: s.Auth.Sign(uid)})
	rec := httptest.NewRecorder()
	s.PostHandleSetting(rec, req)
	return rec
}

func postProfile(t *testing.T, s *Server, uid uuid.UUID, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/settings/profile", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: auth.SessionCookie, Value: s.Auth.Sign(uid)})
	rec := httptest.NewRecorder()
	s.PostProfileSetting(rec, req)
	return rec
}

func TestPostHandleSetting(t *testing.T) {
	s := newTestServer(t)
	alice := mkUser(t, s, "alice")
	mkUser(t, s, "bob")

	// happy path: rename succeeds, redirects to the new profile, DB updated.
	rec := postHandle(t, s, alice, "alice2")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("rename: want 303, got %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/alice2" {
		t.Fatalf("rename: want redirect /alice2, got %q", loc)
	}
	var h, hci string
	if err := s.DB.QueryRow(`SELECT handle, handle_ci FROM users WHERE id=$1`, alice).Scan(&h, &hci); err != nil {
		t.Fatal(err)
	}
	if h != "alice2" || hci != "alice2" {
		t.Fatalf("db: want alice2/alice2, got %q/%q", h, hci)
	}

	cases := []struct {
		name, handle string
		want         int
	}{
		{"reserved", "feed", http.StatusBadRequest},
		{"taken", "bob", http.StatusConflict},
		{"bad-format", "Bad_Name!", http.StatusBadRequest},
		{"leading-hyphen", "-nope", http.StatusBadRequest},
		{"uppercase-folds-to-taken", "BOB", http.StatusConflict},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := postHandle(t, s, alice, c.handle)
			if rec.Code != c.want {
				t.Fatalf("%s=%q: want %d, got %d", c.name, c.handle, c.want, rec.Code)
			}
		})
	}
}

func TestPostProfileSetting(t *testing.T) {
	s := newTestServer(t)
	alice := mkUser(t, s, "alice")

	rec := postProfile(t, s, alice, url.Values{
		"profile_title":  {"  未完成想法的溫室  "},
		"profile_bio":    {"  先看房間，再看時間軸。  "},
		"showcase_type":  {"tag", "note", "text", ""},
		"showcase_title": {"  主題溫室  ", "入口筆記", "訪客備忘", ""},
		"showcase_ref":   {" Systems!! ", "[[Entry Note]]", "ignored", ""},
		"showcase_body":  {"  正在拆問題  ", "  先從這篇開始  ", "  這段是自由文字  ", ""},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("profile setting: want 303, got %d body=%s", rec.Code, rec.Body)
	}
	if loc := rec.Header().Get("Location"); loc != "/alice#profile-home" {
		t.Fatalf("profile setting: want redirect /alice#profile-home, got %q", loc)
	}

	var title, bio, topics, showcaseJSON string
	if err := s.DB.QueryRow(`SELECT profile_title, profile_bio, profile_topic_tags, profile_showcases::text FROM users WHERE id = $1`, alice).
		Scan(&title, &bio, &topics, &showcaseJSON); err != nil {
		t.Fatal(err)
	}
	if title != "未完成想法的溫室" {
		t.Fatalf("title = %q", title)
	}
	if bio != "先看房間，再看時間軸。" {
		t.Fatalf("bio = %q", bio)
	}
	if topics != "systems" {
		t.Fatalf("topics = %q", topics)
	}
	var showcases []profileShowcase
	if err := json.Unmarshal([]byte(showcaseJSON), &showcases); err != nil {
		t.Fatalf("showcases json: %v", err)
	}
	if len(showcases) != 3 {
		t.Fatalf("showcases len = %d; json=%s", len(showcases), showcaseJSON)
	}
	want := []profileShowcase{
		{Type: profileShowcaseTag, Title: "主題溫室", Ref: "systems", BodyMD: "正在拆問題"},
		{Type: profileShowcaseNote, Title: "入口筆記", Ref: "entry-note", BodyMD: "先從這篇開始"},
		{Type: profileShowcaseText, Title: "訪客備忘", BodyMD: "這段是自由文字"},
	}
	for i := range want {
		if showcases[i].Type != want[i].Type || showcases[i].Title != want[i].Title || showcases[i].Ref != want[i].Ref || showcases[i].BodyMD != want[i].BodyMD {
			t.Fatalf("showcase %d = %#v, want %#v", i, showcases[i], want[i])
		}
	}
}
