package handlers

import (
	"bytes"
	"context"
	"reflect"
	"testing"
)

// TestSearchSnippetEscaping guards the stored-XSS fix in searchSnippet:
// ts_headline's raw output is real note body text with literal <mark>/</mark>
// inserted around matches — everything else must come out HTML-escaped, and
// only the two delimiter strings searchSnippet itself emits may pass through
// as real tags.
func TestSearchSnippetEscaping(t *testing.T) {
	for _, tc := range []struct {
		name, in, want string
	}{
		{
			name: "script tag",
			in:   `hello <script>alert(1)</script> <mark>world</mark>`,
			want: `hello &lt;script&gt;alert(1)&lt;/script&gt; <mark>world</mark>`,
		},
		{
			name: "attribute breakout",
			in:   `<mark>foo</mark>" onerror="alert(1)`,
			want: `<mark>foo</mark>&#34; onerror=&#34;alert(1)`,
		},
		{
			name: "spoofed mark delimiter as plain text",
			in:   `no real match here, just the text <mark> and </mark> typed out`,
			want: `no real match here, just the text <mark> and </mark> typed out`,
		},
		{
			name: "ampersand and quotes outside any match",
			in:   `Tom & Jerry's "great" escape`,
			want: `Tom &amp; Jerry&#39;s &#34;great&#34; escape`,
		},
		{
			name: "no match markers at all",
			in:   `plain text, no mark tags`,
			want: `plain text, no mark tags`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := searchSnippet(tc.in).Render(context.Background(), &buf); err != nil {
				t.Fatalf("render: %v", err)
			}
			if got := buf.String(); got != tc.want {
				t.Errorf("searchSnippet(%q) rendered %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestSearchVariants(t *testing.T) {
	for _, tc := range []struct {
		q    string
		want []string
	}{
		{"hello world", []string{"hello world"}}, // no Han chars: fast path, untouched
		{"", []string{""}},
		{"数论", []string{"数论", "數論"}},   // Simplified → adds Traditional
		{"數論", []string{"數論", "数论"}},   // Traditional → adds Simplified
		{"alice 数论", []string{"alice 数论", "alice 數論"}}, // mixed Latin + Han
	} {
		if got := searchVariants(tc.q); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("searchVariants(%q) = %q, want %q", tc.q, got, tc.want)
		}
	}
}

func TestBuildTSQueryVariants(t *testing.T) {
	for _, tc := range []struct{ q, want string }{
		{"foo bar!", "(foo:* & bar:*)"}, // non-Chinese: same terms as buildTSQuery, just grouped
		{"", ""},
		{"!!!", ""},
		{"数论", "(数论:*) | (數論:*)"},
		{"數論", "(數論:*) | (数论:*)"},
	} {
		if got := buildTSQueryVariants(tc.q); got != tc.want {
			t.Errorf("buildTSQueryVariants(%q) = %q, want %q", tc.q, got, tc.want)
		}
	}
}
