package markdown

import (
	"reflect"
	"testing"
)

func TestParseLink(t *testing.T) {
	cases := []struct {
		in   string
		ok   bool
		want WikiLink
	}{
		// 4 canonical syntax variants
		{"foo", true, WikiLink{Slug: "foo", Raw: "foo"}},
		{"bar/baz", true, WikiLink{Folder: "bar", Slug: "baz", Raw: "bar/baz"}},
		{"@alice/foo", true, WikiLink{User: "alice", Slug: "foo", Raw: "@alice/foo"}},
		{"@alice/music/why", true, WikiLink{User: "alice", Folder: "music", Slug: "why", Raw: "@alice/music/why"}},

		// nested folders
		{"a/b/c/slug", true, WikiLink{Folder: "a/b/c", Slug: "slug", Raw: "a/b/c/slug"}},
		{"@bob/a/b/c/slug", true, WikiLink{User: "bob", Folder: "a/b/c", Slug: "slug", Raw: "@bob/a/b/c/slug"}},

		// whitespace tolerance
		{"  foo  ", true, WikiLink{Slug: "foo", Raw: "  foo  "}},

		// unicode in slug allowed (DB will decide what's actually valid)
		{"日記", true, WikiLink{Slug: "日記", Raw: "日記"}},

		// invalid
		{"", false, WikiLink{}},
		{"   ", false, WikiLink{}},
		{"@", false, WikiLink{}},
		{"@alice", false, WikiLink{}},
		{"@alice/", false, WikiLink{}},
		{"@/foo", false, WikiLink{}},
		{"/foo", false, WikiLink{}},
		{"foo/", false, WikiLink{}},
		{"foo//bar", false, WikiLink{}},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, ok := ParseLink(c.in)
			if ok != c.ok {
				t.Fatalf("ParseLink(%q) ok=%v want %v", c.in, ok, c.ok)
			}
			if ok && !reflect.DeepEqual(got, c.want) {
				t.Fatalf("ParseLink(%q)\n got: %+v\nwant: %+v", c.in, got, c.want)
			}
		})
	}
}

func TestURL(t *testing.T) {
	cases := []struct {
		l    WikiLink
		me   string
		want string
	}{
		{WikiLink{Slug: "foo"}, "shawn", "/shawn/foo"},
		{WikiLink{Folder: "music", Slug: "why"}, "shawn", "/shawn/music/why"},
		{WikiLink{User: "alice", Slug: "foo"}, "shawn", "/alice/foo"},
		{WikiLink{User: "alice", Folder: "music", Slug: "why"}, "shawn", "/alice/music/why"},
		{WikiLink{User: "alice", Folder: "a/b", Slug: "c"}, "shawn", "/alice/a/b/c"},
	}
	for _, c := range cases {
		got := c.l.URL(c.me)
		if got != c.want {
			t.Errorf("URL(%+v, %q) = %q, want %q", c.l, c.me, got, c.want)
		}
	}
}

func TestExtract(t *testing.T) {
	body := `Hello [[foo]] and [[bar/baz]] and [[@alice/note]]
some text [[@bob/folder/x]] and a duplicate [[foo]] and broken [[]] and unclosed [[xyz`
	got := Extract(body)
	want := []WikiLink{
		{Slug: "foo", Raw: "foo"},
		{Folder: "bar", Slug: "baz", Raw: "bar/baz"},
		{User: "alice", Slug: "note", Raw: "@alice/note"},
		{User: "bob", Folder: "folder", Slug: "x", Raw: "@bob/folder/x"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Extract\n got: %+v\nwant: %+v", got, want)
	}
}
