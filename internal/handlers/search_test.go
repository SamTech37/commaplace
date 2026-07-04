package handlers

import (
	"reflect"
	"testing"
)

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
