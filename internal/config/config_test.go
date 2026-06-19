package config_test

import (
	"strings"
	"testing"

	"commonplace/internal/config"
)

func TestDefaultSite(t *testing.T) {
	s := config.DefaultSite()
	if s.Title == "" {
		t.Error("Title empty")
	}
	if s.SessionName == "" {
		t.Error("SessionName empty")
	}
}

func TestDefaultPagination(t *testing.T) {
	p := config.DefaultPagination()
	for name, v := range map[string]int{
		"FeedPageSize":      p.FeedPageSize,
		"WikiQueryLimit":    p.WikiQueryLimit,
		"ProfileRecentNote": p.ProfileRecentNote,
		"TagsLimit":         p.TagsLimit,
		"LikesLimit":        p.LikesLimit,
		"AdminLimit":        p.AdminLimit,
		"SearchLimit":       p.SearchLimit,
	} {
		if v <= 0 {
			t.Errorf("%s: want > 0, got %d", name, v)
		}
	}
}

func TestDefaultAuth(t *testing.T) {
	a := config.DefaultAuth()
	if a.TokenTTL <= 0 {
		t.Error("TokenTTL must be > 0")
	}
	if a.TokenLength <= 0 {
		t.Error("TokenLength must be > 0")
	}
	if a.MaxCollisions <= 0 {
		t.Error("MaxCollisions must be > 0")
	}
}

func TestDefaultDatabase(t *testing.T) {
	d := config.DefaultDatabase()
	if d.BusyTimeout <= 0 {
		t.Error("BusyTimeout must be > 0")
	}
}

func TestDefaultEmail(t *testing.T) {
	e := config.DefaultEmail("MyApp")
	if !strings.Contains(e.SignInIntro, "MyApp") {
		t.Errorf("SignInIntro %q does not contain title", e.SignInIntro)
	}
	if !strings.Contains(e.SignInSubj, "MyApp") {
		t.Errorf("SignInSubj %q does not contain title", e.SignInSubj)
	}
	if e.TourHandle == "" {
		t.Error("TourHandle empty")
	}
}
