package handlers

import (
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/httprate"
)

// rateLimit caps requests per client IP, in memory (one instance, so no shared
// store). Render's Cloudflare edge already absorbs volumetric DDoS; these
// limits are for app-level abuse — magic-link email flooding above all.
//
//   - everything but /assets/: 1200/min. A page plus its avatars and card
//     images is tens of requests, and a classroom playtest shares one IP.
//   - any non-GET/HEAD: 300/min. Autosave (800 ms debounce) plus desk-state
//     saves (1.2 s throttle) peak around 125/min for one typist.
//   - POST /login and /auth/*: 60 per 10 min. Each sign-in is two requests.
//   - POST /login per email: 5 per 10 min, so nobody's inbox can be flooded.
func rateLimit(next http.Handler) http.Handler {
	all := newLimiter(1200, time.Minute)
	writes := newLimiter(300, time.Minute)
	auth := newLimiter(60, 10*time.Minute)
	email := newLimiter(5, 10*time.Minute)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.HasPrefix(path, "/assets/") {
			next.ServeHTTP(w, r)
			return
		}
		ip := clientIP(r)
		if all.RespondOnLimit(w, r, ip) {
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && writes.RespondOnLimit(w, r, ip) {
			return
		}
		login := r.Method == http.MethodPost && path == "/login"
		if (login || strings.HasPrefix(path, "/auth/")) && auth.RespondOnLimit(w, r, ip) {
			return
		}
		if login {
			if e := strings.TrimSpace(strings.ToLower(r.PostFormValue("email"))); e != "" && email.RespondOnLimit(w, r, e) {
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// newLimiter answers over-limit requests with a short plain-text 429 and only
// a Retry-After header. htmx 2 does not swap 4xx responses, and the editor's
// fetch calls show short plain-text errors as-is.
func newLimiter(n int, per time.Duration) *httprate.RateLimiter {
	return httprate.NewRateLimiter(n, per,
		httprate.WithResponseHeaders(httprate.ResponseHeaders{RetryAfter: "Retry-After"}),
		httprate.WithLimitHandler(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "請求太頻繁，請稍後再試", http.StatusTooManyRequests)
		}),
	)
}

// clientIP is the rate-limit key. On Render, RemoteAddr is Render's proxy, and
// X-Forwarded-For keeps whatever the client sent and appends to it, so neither
// identifies the client. Render's Cloudflare edge writes CF-Connecting-IP on
// every request, overwriting any client-sent value. Off Render (local dev)
// RemoteAddr is the real peer; the header is trusted unconditionally because
// production is always behind that edge.
func clientIP(r *http.Request) string {
	ip := r.Header.Get("CF-Connecting-IP")
	if ip == "" {
		ip = r.RemoteAddr
		if host, _, err := net.SplitHostPort(ip); err == nil {
			ip = host
		}
	}
	return httprate.CanonicalizeIP(ip)
}
