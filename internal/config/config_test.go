package config_test

import (
	"strings"
	"testing"
	"time"

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

func TestFeedFromEnv(t *testing.T) {
	f, err := config.FeedFromEnv()
	if err != nil || f != config.DefaultFeed() {
		t.Fatalf("no env: got %+v, %v; want defaults", f, err)
	}
	if d := config.DefaultFeed(); d.BatchMin != 3 || d.BurstStep != 2*time.Hour || d.BurstWindow != 24*time.Hour {
		t.Fatalf("defaults changed: %+v", d)
	}

	t.Setenv("FEED_BATCH_MIN", "5")
	t.Setenv("FEED_BURST_STEP", "90m")
	t.Setenv("FEED_BURST_WINDOW", "12h")
	f, err = config.FeedFromEnv()
	if err != nil || f != (config.Feed{BatchMin: 5, BurstStep: 90 * time.Minute, BurstWindow: 12 * time.Hour}) {
		t.Fatalf("env overrides: got %+v, %v", f, err)
	}

	// Rank is whole seconds: a zero, negative, sub-second or fractional
	// duration would silently truncate, so each is refused at boot.
	bad := []struct{ key, value string }{
		{"FEED_BATCH_MIN", "0"}, {"FEED_BATCH_MIN", "x"},
		{"FEED_BURST_STEP", "2"}, {"FEED_BURST_STEP", "0s"}, {"FEED_BURST_STEP", "500ms"}, {"FEED_BURST_STEP", "1.5s"},
		{"FEED_BURST_WINDOW", "-1h"}, {"FEED_BURST_WINDOW", "0"}, {"FEED_BURST_WINDOW", "999ms"},
	}
	for _, b := range bad {
		t.Run(b.key+"="+b.value, func(t *testing.T) {
			t.Setenv(b.key, b.value)
			if _, err := config.FeedFromEnv(); err == nil {
				t.Fatalf("%s=%s accepted", b.key, b.value)
			}
		})
	}
}
