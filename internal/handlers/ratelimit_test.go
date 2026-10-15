package handlers

import (
	"bytes"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/uuid"

	"commonplace/internal/auth"
)

var okHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

var testAuth = &auth.Auth{Secret: []byte("test")}

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

// from is a request as Render's edge delivers it: CF-Connecting-IP set to the
// client, RemoteAddr the proxy.
func from(ip, method, path string) func(int) *http.Request {
	return func(int) *http.Request {
		r := httptest.NewRequest(method, path, nil)
		r.RemoteAddr = "10.0.0.1:1234"
		r.Header.Set("CF-Connecting-IP", ip)
		return r
	}
}

func as(uid uuid.UUID, mk func(int) *http.Request) func(int) *http.Request {
	return func(i int) *http.Request {
		r := mk(i)
		r.AddCookie(&http.Cookie{Name: auth.SessionCookie, Value: testAuth.Sign(uid)})
		return r
	}
}

func login(ip string, email func(i int) string) func(int) *http.Request {
	return func(i int) *http.Request {
		r := httptest.NewRequest("POST", "/login", strings.NewReader(url.Values{"email": {email(i)}}.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("CF-Connecting-IP", ip)
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

func assertOK(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestRateLimitGlobalAndWrites(t *testing.T) {
	h := rateLimit(testAuth, okHandler)
	assert429(t, hit(t, h, 1201, from("203.0.113.1", "GET", "/feed")), "60")
	assert429(t, hit(t, h, 301, from("203.0.113.2", "PATCH", "/api/notes/x")), "60")
	// Another IP is unaffected.
	assertOK(t, hit(t, h, 1, from("203.0.113.3", "PATCH", "/api/notes/x")))
}

// Signed-in writes count per user: two typists behind one NAT don't share a
// budget, and one user hopping IPs doesn't get a fresh one.
func TestRateLimitWritesPerUser(t *testing.T) {
	h := rateLimit(testAuth, okHandler)
	alice, bob := uuid.New(), uuid.New()
	nat := from("203.0.113.1", "PATCH", "/api/notes/x")
	assertOK(t, hit(t, h, 300, as(alice, nat)))
	assertOK(t, hit(t, h, 300, as(bob, nat)))
	assertOK(t, hit(t, h, 300, nat)) // anonymous, keyed by the NAT's IP
	assert429(t, hit(t, h, 1, as(alice, from("198.51.100.1", "PATCH", "/api/notes/x"))), "60")

	// A forged cookie falls back to the IP, which is spent.
	assert429(t, hit(t, h, 1, func(i int) *http.Request {
		r := nat(i)
		r.AddCookie(&http.Cookie{Name: auth.SessionCookie, Value: uuid.NewString() + ".00"})
		return r
	}), "60")
}

func TestRateLimitAssetsExempt(t *testing.T) {
	h := rateLimit(testAuth, okHandler)
	assertOK(t, hit(t, h, 1500, from("203.0.113.1", "GET", "/assets/style.css")))
	// ...and did not use up the page budget.
	assertOK(t, hit(t, h, 1, from("203.0.113.1", "GET", "/feed")))
}

func TestRateLimitSignIn(t *testing.T) {
	h := rateLimit(testAuth, okHandler)
	// Per IP across emails; /_dev/login shares the budget.
	distinct := func(i int) string { return fmt.Sprintf("u%d@example.com", i) }
	hit(t, h, 200, login("203.0.113.9", distinct))
	assert429(t, hit(t, h, 101, from("203.0.113.9", "GET", "/_dev/login?as=x&key=guess")), "600")
	// HEAD reaches the same handler, so it shares the budget.
	h = rateLimit(testAuth, okHandler)
	assert429(t, hit(t, h, 301, from("203.0.113.9", "HEAD", "/_dev/login?as=x&key=guess")), "600")
	// A classroom on that IP still completes sign-ins already under way.
	assertOK(t, hit(t, h, 100, func(i int) *http.Request {
		return from("203.0.113.9", "GET", fmt.Sprintf("/auth/google/callback?state=s%d&code=c%d", i, i))(i)
	}))
	assertOK(t, hit(t, h, 100, func(i int) *http.Request {
		return from("203.0.113.9", "GET", fmt.Sprintf("/auth/tok%d", i))(i)
	}))
}

// The middleware reads the login email without parsing multipart bodies, so
// an anonymous POST /login can't make it spool file parts to disk.
func TestRateLimitLoginSkipsMultipart(t *testing.T) {
	var parsed bool
	h := rateLimit(testAuth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parsed = r.MultipartForm != nil
	}))
	body := "--b\r\nContent-Disposition: form-data; name=\"f\"; filename=\"f\"\r\n\r\nx\r\n--b--\r\n"
	r := httptest.NewRequest("POST", "/login", strings.NewReader(body))
	r.Header.Set("Content-Type", "multipart/form-data; boundary=b")
	r.Header.Set("CF-Connecting-IP", "203.0.113.7")
	h.ServeHTTP(httptest.NewRecorder(), r)
	if parsed {
		t.Fatal("middleware parsed a multipart body")
	}
}

func TestEmailKey(t *testing.T) {
	for in, want := range map[string]string{
		" Victim@Example.com ":            "victim@example.com",
		"victim+x@example.com":            "victim@example.com",
		"vic.tim@example.com":             "vic.tim@example.com",
		"V.i.c.t.i.m+spam@googlemail.com": "victim@gmail.com",
		"victim@gmail.com":                "victim@gmail.com",
		"not-an-email":                    "not-an-email",
	} {
		if got := emailKey(in); got != want {
			t.Errorf("emailKey(%q) = %q, want %q", in, got, want)
		}
	}
}

type stubMailer struct{ sent []string }

func (m *stubMailer) Send(to, subject, body string) error {
	m.sent = append(m.sent, to)
	return nil
}

// Over the per-email limit, /login answers exactly as if it had sent the link,
// and sends nothing — so locking someone out is invisible, and nothing
// reveals whether an address is registered.
func TestRateLimitEmailSilent(t *testing.T) {
	s := newTestServer(t)
	mail := &stubMailer{}
	s.Auth.Mailer = mail
	h := s.Routes()
	post := func(ip, email string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, login(ip, func(int) string { return email })(0))
		return rec
	}

	sent := post("198.51.100.1", "victim@gmail.com")
	if sent.Code != http.StatusSeeOther || len(mail.sent) != 1 {
		t.Fatalf("first send: status %d, %d mails", sent.Code, len(mail.sent))
	}
	// Plus-tags and dots are the same inbox, so they share the 5/10 min.
	for _, e := range []string{"victim+a@gmail.com", "vic.tim@gmail.com", "victim+b@googlemail.com", "Victim@gmail.com"} {
		post("198.51.100.1", e)
	}
	if len(mail.sent) != 5 {
		t.Fatalf("sent %d before the limit, want 5", len(mail.sent))
	}
	quiet := post("198.51.100.1", "v.ictim+c@gmail.com")
	if len(mail.sent) != 5 {
		t.Fatalf("over the limit still sent: %v", mail.sent)
	}
	if quiet.Code != sent.Code || !reflect.DeepEqual(quiet.Header(), sent.Header()) || quiet.Body.String() != sent.Body.String() {
		t.Fatalf("over-limit response differs:\n%d %v %q\nvs success\n%d %v %q",
			quiet.Code, quiet.Header(), quiet.Body, sent.Code, sent.Header(), sent.Body)
	}

	// Other IPs still get through, up to 20 an hour across all of them.
	for i := range 15 {
		post(fmt.Sprintf("198.51.100.%d", 10+i), "victim@gmail.com")
	}
	if len(mail.sent) != 20 {
		t.Fatalf("sent %d, want 20", len(mail.sent))
	}
	post("198.51.100.99", "victim@gmail.com")
	if len(mail.sent) != 20 {
		t.Fatalf("over the hourly cap still sent: %d", len(mail.sent))
	}
}

func TestRateLimitClientIP(t *testing.T) {
	h := rateLimit(testAuth, okHandler)
	// XFF is ignored next to CF-Connecting-IP: rotating it doesn't help.
	cf := func(ip string) func(int) *http.Request {
		return func(i int) *http.Request {
			r := from(ip, "POST", "/api/like")(i)
			r.Header.Set("X-Forwarded-For", fmt.Sprintf("192.0.2.%d", i%250))
			return r
		}
	}
	assert429(t, hit(t, h, 301, cf("198.51.100.7")), "60")
	assertOK(t, hit(t, h, 1, cf("198.51.100.8")))
	// IPv6 is keyed by /64, so rotating within one's own prefix does not help.
	assert429(t, hit(t, h, 301, func(i int) *http.Request {
		return cf(fmt.Sprintf("2001:db8::%x", i))(i)
	}), "60")
}

// Without CF-Connecting-IP every client looks like Render's proxy; sharing one
// bucket would be a site-wide outage, so per-IP limits fail open (once
// logged), while per-user and per-email limits still hold.
func TestRateLimitMissingHeader(t *testing.T) {
	var logs bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(prev) })

	h := rateLimit(testAuth, okHandler)
	proxy := func(method, path string) func(int) *http.Request {
		return func(i int) *http.Request {
			r := httptest.NewRequest(method, path, nil)
			r.RemoteAddr = "10.0.0.1:1234"
			r.Header.Set("X-Forwarded-For", "192.0.2.1")
			return r
		}
	}
	assertOK(t, hit(t, h, 1300, proxy("GET", "/feed")))
	assertOK(t, hit(t, h, 400, proxy("POST", "/api/like")))
	assertOK(t, hit(t, h, 400, proxy("GET", "/_dev/login")))
	assert429(t, hit(t, h, 301, as(uuid.New(), proxy("POST", "/api/like"))), "60")
	if n := strings.Count(logs.String(), "CF-Connecting-IP"); n != 1 {
		t.Fatalf("warning logged %d times, want once:\n%s", n, logs.String())
	}

	// The hourly per-email cap needs no IP.
	for range 20 {
		rec := hit(t, h, 1, login("", func(int) string { return "victim@example.com" }))
		if rec.Header().Get("Location") != "" {
			t.Fatal("under the cap, the request should reach the handler")
		}
	}
	if rec := hit(t, h, 1, login("", func(int) string { return "victim@example.com" })); rec.Header().Get("Location") != "/login?sent=1" {
		t.Fatalf("over the cap: %d %v", rec.Code, rec.Header())
	}
}

func TestRateLimitForgetsOldWindows(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := rateLimit(testAuth, okHandler)
		assert429(t, hit(t, h, 301, from("203.0.113.1", "POST", "/api/like")), "60")
		// Two windows on, the counters holding this IP have been cleared.
		time.Sleep(2 * time.Minute)
		assertOK(t, hit(t, h, 300, from("203.0.113.1", "POST", "/api/like")))
	})
}

// DEBUG (local dev, scripts/bench-load.sh) runs without limits; deploys don't.
// "/assets" (no slash) is a DB-free redirect that still counts toward limits.
func TestRateLimitOffInDebug(t *testing.T) {
	for _, debug := range []bool{true, false} {
		rec := hit(t, (&Server{Debug: debug, Auth: testAuth}).Routes(), 1201, from("203.0.113.1", "GET", "/assets"))
		if limited := rec.Code == http.StatusTooManyRequests; limited == debug {
			t.Errorf("debug=%v: last status %d", debug, rec.Code)
		}
	}
}
