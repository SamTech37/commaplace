// Package config centralizes application configuration and constants.
package config

import "time"

// Site contains core site metadata.
type Site struct {
	Title       string
	Description string
	SessionName string // session cookie name
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
	SignInIntro  string // email body intro
	SignInSubj   string // email subject
	ReportSubj   string
	TourHandle   string
}

// DefaultSite returns default site configuration.
func DefaultSite() Site {
	return Site{
		Title:       "commonplace",
		Description: "Markdown notes that link to each other, including across other people's vaults.",
		SessionName: "commonplace_session",
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

// DefaultEmail returns default email configuration.
func DefaultEmail() Email {
	return Email{
		SignInIntro: "Sign in to commonplace:",
		SignInSubj:  "Your commonplace sign-in link",
		ReportSubj:  "[commonplace] new report",
		TourHandle:  "commonplace-tour",
	}
}
