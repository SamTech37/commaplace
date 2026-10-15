package handlers

import (
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/httprate"

	"commonplace/internal/auth"
)

// rateLimit caps requests in memory (one instance, so no shared store).
// Render's Cloudflare edge already absorbs volumetric DDoS; these limits are
// for app-level abuse — magic-link email flooding above all.
//
//   - everything but /assets/: 1200/min per IP. A page plus its avatars and
//     card images is tens of requests, and a classroom playtest shares one IP.
//   - any non-GET/HEAD: 300/min per signed-in user, else per IP. Autosave
//     (800 ms debounce) plus desk-state saves (1.2 s throttle) peak around
//     125/min for one typist.
//   - POST /login and GET /_dev/login: 300 per 10 min per IP — a 100-person
//     classroom behind one NAT signing in at once, three tries each. The
//     /_dev/login key is a shared password, so it needs this cap too.
//     /auth/{token} and the OAuth callback are not counted: single-use tokens
//     and OAuth state already gate them.
//   - POST /login per email (see emailKey): 5 per 10 min from one IP, 20 per
//     hour from anywhere — enough for a few lost or spam-filtered links, too
//     few to flood an inbox. Over it, the user sees the normal "sent" page
//     and nothing is sent, so a lockout is invisible to whoever caused it and
//     nothing reveals whether an address is registered.
//
// Without CF-Connecting-IP every request looks like Render's proxy, so per-IP
// limits are skipped (fail open) rather than shared site-wide.
func rateLimit(a *auth.Auth, next http.Handler) http.Handler {
	all := newLimiter(1200, time.Minute)
	writes := newLimiter(300, time.Minute)
	signin := newLimiter(300, 10*time.Minute)
	emailIP := quietLimiter(5, 10*time.Minute)
	email := quietLimiter(20, time.Hour)
	var warn sync.Once
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.HasPrefix(path, "/assets/") {
			next.ServeHTTP(w, r)
			return
		}
		ip := clientIP(r)
		if ip == "" {
			warn.Do(func() { log.Print("ratelimit: request without CF-Connecting-IP; per-IP limits are off") })
		}
		if ip != "" && all.RespondOnLimit(w, r, ip) {
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if key := userKey(a, r, ip); key != "" && writes.RespondOnLimit(w, r, key) {
				return
			}
		}
		login := r.Method == http.MethodPost && path == "/login"
		if ip != "" && (login || (r.Method == http.MethodGet && path == "/_dev/login")) && signin.RespondOnLimit(w, r, ip) {
			return
		}
		if login {
			if e := emailKey(r.PostFormValue("email")); e != "" &&
				((ip != "" && emailIP.OnLimit(w, r, e+" "+ip)) || email.OnLimit(w, r, e)) {
				redirectLoginSent(w, r, r.PostFormValue("next"))
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

// quietLimiter sets no headers, so a limited response looks like any other.
func quietLimiter(n int, per time.Duration) *httprate.RateLimiter {
	return httprate.NewRateLimiter(n, per, httprate.WithResponseHeaders(httprate.ResponseHeaders{}))
}

// clientIP is the per-IP key, or "" when unknown. On Render, RemoteAddr is
// Render's proxy, and X-Forwarded-For keeps whatever the client sent and
// appends to it, so neither identifies the client. Render's Cloudflare edge
// writes CF-Connecting-IP on every request, overwriting any client-sent
// value; the header is trusted unconditionally because production is always
// behind that edge (and DEBUG, used locally, turns limits off).
func clientIP(r *http.Request) string {
	return httprate.CanonicalizeIP(r.Header.Get("CF-Connecting-IP"))
}

// userKey is the signed-in user, from the HMAC-signed session cookie alone
// (no DB lookup), falling back to ip.
func userKey(a *auth.Auth, r *http.Request, ip string) string {
	if c, err := r.Cookie(auth.SessionCookie); err == nil {
		if uid, err := a.Verify(c.Value); err == nil {
			return "u:" + uid.String()
		}
	}
	return ip
}

// emailKey folds the aliases of one inbox into one limiter key: no +tag, and
// for Gmail no dots and googlemail.com = gmail.com. Only the key; the address
// itself is stored and mailed as typed.
func emailKey(email string) string {
	e := strings.TrimSpace(strings.ToLower(email))
	at := strings.LastIndexByte(e, '@')
	if at < 0 {
		return e
	}
	local, domain := e[:at], e[at+1:]
	local, _, _ = strings.Cut(local, "+")
	if domain == "googlemail.com" {
		domain = "gmail.com"
	}
	if domain == "gmail.com" {
		local = strings.ReplaceAll(local, ".", "")
	}
	return local + "@" + domain
}
