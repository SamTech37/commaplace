package handlers

import (
	"net/url"
	"strings"
	"testing"
)

// TestObsidianURLEncoding guards the obsidian:// deep link against the two
// encoding bugs it shipped with: '+' for spaces (Obsidian decodes RFC 3986,
// where '+' is a literal plus) and double-encoding (%20 -> %2520 when
// html/template re-escapes an already-encoded query value).
func TestObsidianURLEncoding(t *testing.T) {
	body := "# Title\n\nhello world [[a link]]"
	got := string(obsidianURL("my-slug", body))

	if !strings.HasPrefix(got, "obsidian://new?name=my-slug&content=") {
		t.Fatalf("unexpected prefix: %s", got)
	}
	content := got[strings.Index(got, "content=")+len("content="):]

	// No '+' for spaces, no double-encoding.
	if strings.Contains(content, "+") {
		t.Errorf("content uses '+' for a space; Obsidian needs %%20:\n%s", content)
	}
	if strings.Contains(content, "%25") {
		t.Errorf("content is double-encoded (%%25 present):\n%s", content)
	}

	// Round-trips back to the exact body a receiver would decode.
	decoded, err := url.QueryUnescape(strings.ReplaceAll(content, "%20", "+"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded != body {
		t.Errorf("round-trip mismatch:\n got %q\nwant %q", decoded, body)
	}
}
