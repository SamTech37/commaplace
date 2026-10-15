// Package config centralizes application configuration and constants.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Site contains core site metadata.
type Site struct {
	Title       string
	Description string
	SessionName string // session cookie name
}

// Nav holds user-facing display names for navigation links.
// Changing a value here renames the label everywhere it appears,
// without grepping templates. URL paths are deliberately not coupled.
type Nav struct {
	Write  string
	Feed   string
	Graph  string
	Login  string
	Logout string
}

// Pagination contains pagination limits.
type Pagination struct {
	FeedPageSize      int
	WikiQueryLimit    int
	ProfileRecentNote int
	TagsLimit         int
	LikesLimit        int
	AdminLimit        int
	SearchLimit       int
}

// Feed tunes the recommended feed's ranking (see internal/feedrank). The
// defaults are starting guesses, to be tuned from real usage; each can be
// overridden by the env var named beside it.
type Feed struct {
	BatchMin    int           // FEED_BATCH_MIN: smallest import shown as one batch card
	BurstStep   time.Duration // FEED_BURST_STEP: push-back per earlier note in a posting burst
	BurstWindow time.Duration // FEED_BURST_WINDOW: how far back a burst counts an author's notes
}

// Auth contains authentication-related configuration.
type Auth struct {
	TokenTTL      time.Duration
	TokenLength   int
	MaxCollisions int
}

// Database contains database-related configuration.
type Database struct {
	BusyTimeout time.Duration
}

// Email contains email-related configuration.
type Email struct {
	SignInIntro string // email body intro
	SignInSubj  string // email subject
	ReportSubj  string
	TourHandle  string
}

// DefaultSite returns default site configuration.
func DefaultSite() Site {
	return Site{
		Title:       "Comma,",
		Description: "Markdown notes that link to each other, including across other people's vaults.",
		SessionName: "Comma_session",
	}
}

// DefaultNav returns the canonical display names. Rename here, not in templates.
func DefaultNav() Nav {
	return Nav{
		Write:  "書寫",
		Feed:   "閱覽",
		Graph:  "圖譜",
		Login:  "登入",
		Logout: "登出",
	}
}

// DefaultPagination returns default pagination limits.
func DefaultPagination() Pagination {
	return Pagination{
		FeedPageSize:      50,
		WikiQueryLimit:    10,
		ProfileRecentNote: 20,
		TagsLimit:         200,
		LikesLimit:        200,
		AdminLimit:        200,
		SearchLimit:       50,
	}
}

// DefaultFeed returns the feed-ranking defaults — starting guesses, not
// measurements; tune them from real usage.
func DefaultFeed() Feed {
	return Feed{
		BatchMin:    3,
		BurstStep:   2 * time.Hour,
		BurstWindow: 24 * time.Hour,
	}
}

// FeedFromEnv returns DefaultFeed with any FEED_* env vars applied. Durations
// use Go syntax ("90m", "2h"). A malformed or negative value is an error, not
// a silent fallback, so a typo in a deploy's env cannot quietly retune the feed.
func FeedFromEnv() (Feed, error) {
	f := DefaultFeed()
	if v := os.Getenv("FEED_BATCH_MIN"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return f, fmt.Errorf("FEED_BATCH_MIN=%q: want an integer >= 1", v)
		}
		f.BatchMin = n
	}
	for key, dst := range map[string]*time.Duration{"FEED_BURST_STEP": &f.BurstStep, "FEED_BURST_WINDOW": &f.BurstWindow} {
		if v := os.Getenv(key); v != "" {
			d, err := time.ParseDuration(v)
			if err != nil || d < 0 {
				return f, fmt.Errorf("%s=%q: want a non-negative duration such as 2h", key, v)
			}
			*dst = d
		}
	}
	return f, nil
}

// DefaultAuth returns default auth configuration.
func DefaultAuth() Auth {
	return Auth{
		TokenTTL:      15 * time.Minute,
		TokenLength:   32,
		MaxCollisions: 10000,
	}
}

// DefaultDatabase returns default database configuration.
func DefaultDatabase() Database {
	return Database{
		BusyTimeout: 5 * time.Second,
	}
}

// DefaultEmail returns default email configuration derived from the site title.
func DefaultEmail(title string) Email {
	return Email{
		SignInIntro: "Sign in to " + title,
		SignInSubj:  "Your " + title + " sign-in link",
		ReportSubj:  "[" + title + "] new report",
		TourHandle:  "comma-tour",
	}
}
