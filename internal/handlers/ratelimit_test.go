package handlers

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

var okHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

// hit sends n requests through h and returns the status of the last one.
func hit(t *testing.T, h http.Handler, n int, mk func(i int) *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	var rec *httptest.ResponseRecorder
	for i := range n {
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, mk(i))
		if i < n-1 && rec.Code >= 400 {
			t.Fatalf("request %d: got %d before the limit", i+1, rec.Code)
		}
	}
	return rec
}

func from(ip, method, path string) func(int) *http.Request {
	return func(int) *http.Request {
		r := httptest.NewRequest(method, path, nil)
		r.RemoteAddr = ip + ":1234"
		return r
	}
}

func login(ip string, email func(i int) string) func(int) *http.Request {
	return func(i int) *http.Request {
		r := httptest.NewRequest("POST", "/login", strings.NewReader(url.Values{"email": {email(i)}}.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.RemoteAddr = ip + ":1234"
		return r
	}
}

func assert429(t *testing.T, rec *httptest.ResponseRecorder, retryAfter string) {
	t.Helper()
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got != retryAfter {
		t.Errorf("Retry-After = %q, want %q", got, retryAfter)
	}
	if !strings.Contains(rec.Body.String(), "請求太頻繁") {
		t.Errorf("body = %q", rec.Body.String())
	}
}

func TestRateLimitGlobalAndWrites(t *testing.T) {
	h := rateLimit(okHandler)
	assert429(t, hit(t, h, 1201, from("203.0.113.1", "GET", "/feed")), "60")
	assert429(t, hit(t, h, 301, from("203.0.113.2", "PATCH", "/api/notes/x")), "60")
	// Another IP is unaffected.
	if rec := hit(t, h, 1, from("203.0.113.3", "PATCH", "/api/notes/x")); rec.Code != http.StatusOK {
		t.Fatalf("other IP got %d", rec.Code)
	}
}

func TestRateLimitAssetsExempt(t *testing.T) {
	h := rateLimit(okHandler)
	if rec := hit(t, h, 1500, from("203.0.113.1", "GET", "/assets/style.css")); rec.Code != http.StatusOK {
		t.Fatalf("assets got %d", rec.Code)
	}
	// ...and did not use up the page budget.
	if rec := hit(t, h, 1, from("203.0.113.1", "GET", "/feed")); rec.Code != http.StatusOK {
		t.Fatalf("page after assets got %d", rec.Code)
	}
}

func TestRateLimitLogin(t *testing.T) {
	h := rateLimit(okHandler)
	// Per email, across IPs.
	same := func(int) string { return "Victim@example.com " }
	for i := range 5 {
		hit(t, h, 1, login(fmt.Sprintf("198.51.100.%d", i), same))
	}
	assert429(t, hit(t, h, 1, login("198.51.100.99", func(int) string { return "victim@example.com" })), "600")
	// Per IP, across emails; /auth/* shares the budget.
	distinct := func(i int) string { return fmt.Sprintf("u%d@example.com", i) }
	hit(t, h, 30, login("203.0.113.9", distinct))
	assert429(t, hit(t, h, 31, from("203.0.113.9", "GET", "/auth/token")), "600")
}

func TestRateLimitClientIP(t *testing.T) {
	h := rateLimit(okHandler)
	// A client rotating X-Forwarded-For still shares one bucket.
	spoof := func(i int) *http.Request {
		r := from("10.0.0.1", "POST", "/api/like")(i)
		r.Header.Set("X-Forwarded-For", fmt.Sprintf("192.0.2.%d", i%250))
		return r
	}
	assert429(t, hit(t, h, 301, spoof), "60")

	// Behind Render every request arrives from the proxy; CF-Connecting-IP
	// tells clients apart, and XFF is ignored next to it.
	cf := func(ip string) func(int) *http.Request {
		return func(i int) *http.Request {
			r := spoof(i)
			r.Header.Set("CF-Connecting-IP", ip)
			return r
		}
	}
	assert429(t, hit(t, h, 301, cf("198.51.100.7")), "60")
	if rec := hit(t, h, 1, cf("198.51.100.8")); rec.Code != http.StatusOK {
		t.Fatalf("other CF client got %d", rec.Code)
	}
	// IPv6 is keyed by /64, so rotating within one's own prefix does not help.
	assert429(t, hit(t, h, 301, func(i int) *http.Request {
		return cf(fmt.Sprintf("2001:db8::%x", i))(i)
	}), "60")
}

func TestRateLimitForgetsOldWindows(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := rateLimit(okHandler)
		assert429(t, hit(t, h, 301, from("203.0.113.1", "POST", "/api/like")), "60")
		// Two windows on, the counters holding this IP have been cleared.
		time.Sleep(2 * time.Minute)
		if rec := hit(t, h, 300, from("203.0.113.1", "POST", "/api/like")); rec.Code != http.StatusOK {
			t.Fatalf("after two windows got %d", rec.Code)
		}
	})
}

// DEBUG (local dev, scripts/bench-load.sh) runs without limits; deploys don't.
// "/assets" (no slash) is a DB-free redirect that still counts toward limits.
func TestRateLimitOffInDebug(t *testing.T) {
	for _, debug := range []bool{true, false} {
		rec := hit(t, (&Server{Debug: debug}).Routes(), 1201, func(int) *http.Request {
			return httptest.NewRequest("GET", "/assets", nil)
		})
		if limited := rec.Code == http.StatusTooManyRequests; limited == debug {
			t.Errorf("debug=%v: last status %d", debug, rec.Code)
		}
	}
}
