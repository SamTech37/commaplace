package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAssetVersionIsDerivedFromContent guards the fallback path: if the walk
// over the embedded assets ever fails, every URL silently shares one constant
// and cache busting quietly stops working.
func TestAssetVersionIsDerivedFromContent(t *testing.T) {
	if assetVersion == "" || assetVersion == "0" {
		t.Fatalf("assetVersion = %q — the walk over the embedded assets failed", assetVersion)
	}
	if got := computeAssetVersion(); got != assetVersion {
		t.Errorf("asset version is not stable: %q then %q", assetVersion, got)
	}
}

// TestAssetURLsCarryTheBuildVersion is the regression this exists for. Assets
// are served under fixed names with a one-day max-age while the HTML is never
// cached, so an unversioned URL lets a returning reader pair today's markup
// with yesterday's CSS and JS. That shipped once: cached opencc-toggle.js was
// still looking for #script-toggle and the 繁簡 control did nothing.
func TestAssetURLsCarryTheBuildVersion(t *testing.T) {
	html := renderChrome(t, nil)

	for _, name := range []string{
		"style.css", "fonts/tc/result.css", "copy.js", "prefs.js",
		"opencc-toggle.js", "graph.js", "reader.js", "palette.js",
	} {
		if strings.Contains(html, `"/assets/`+name+`"`) {
			t.Errorf("%s is referenced without ?v= — a stale copy can outlive a deploy", name)
		}
		if !strings.Contains(html, name+"?v="+assetVersion) {
			t.Errorf("%s does not carry the build's asset version", name)
		}
	}
}

// TestAssetURLBuildsAQueryNotAPath keeps the scheme the file server actually
// understands: http.FileServer ignores the query, so the bytes still resolve.
func TestAssetURLBuildsAQueryNotAPath(t *testing.T) {
	got := assetURL("style.css")
	if !strings.HasPrefix(got, "/assets/style.css?v=") {
		t.Errorf("assetURL(%q) = %q, want /assets/style.css?v=…", "style.css", got)
	}
}

// TestCacheControlMatchesURLPrecision pins the pairing the whole scheme rests
// on: a versioned URL names one build of one file and may be cached hard, a
// bare path may not, and debug builds cache nothing so local edits show up.
func TestCacheControlMatchesURLPrecision(t *testing.T) {
	for _, tc := range []struct {
		name, url string
		debug     bool
		want      string
	}{
		{"versioned", "/assets/style.css?v=abc123", false, "public, max-age=31536000, immutable"},
		{"bare", "/assets/style.css", false, "public, max-age=86400"},
		{"debug", "/assets/style.css?v=abc123", true, "no-store"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := cacheControl(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), tc.debug)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.url, nil))
			if got := rec.Header().Get("Cache-Control"); got != tc.want {
				t.Errorf("Cache-Control = %q, want %q", got, tc.want)
			}
		})
	}
}
