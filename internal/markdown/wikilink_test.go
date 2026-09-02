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
		{"bar/baz", true, WikiLink{Slug: "baz", Raw: "bar/baz"}},
		{"@alice/foo", true, WikiLink{User: "alice", Slug: "foo", Raw: "@alice/foo"}},
		{"@alice/music/why", true, WikiLink{User: "alice", Slug: "why", Raw: "@alice/music/why"}},

		// nested folders
		{"a/b/c/slug", true, WikiLink{Slug: "slug", Raw: "a/b/c/slug"}},
		{"@bob/a/b/c/slug", true, WikiLink{User: "bob", Slug: "slug", Raw: "@bob/a/b/c/slug"}},

		// whitespace tolerance
		{"  foo  ", true, WikiLink{Slug: "foo", Raw: "  foo  "}},

		// unicode in slug allowed (DB will decide what's actually valid)
		{"日記", true, WikiLink{Slug: "日記", Raw: "日記"}},

		// aliases
		{"foo|Custom Label", true, WikiLink{Slug: "foo", Alias: "Custom Label", Raw: "foo|Custom Label"}},
		{"bar/baz|My Note", true, WikiLink{Slug: "baz", Alias: "My Note", Raw: "bar/baz|My Note"}},
		{"@alice/foo|Her Note", true, WikiLink{User: "alice", Slug: "foo", Alias: "Her Note", Raw: "@alice/foo|Her Note"}},

		// heading anchors
		{"foo#Introduction", true, WikiLink{Slug: "foo", Anchor: "Introduction", Raw: "foo#Introduction"}},
		{"bar/baz#Section 2", true, WikiLink{Slug: "baz", Anchor: "Section 2", Raw: "bar/baz#Section 2"}},
		{"@alice/foo#Header", true, WikiLink{User: "alice", Slug: "foo", Anchor: "Header", Raw: "@alice/foo#Header"}},

		// same-note heading links
		{"#Introduction", true, WikiLink{Anchor: "Introduction", Raw: "#Introduction"}},
		{"#形式謬誤", true, WikiLink{Anchor: "形式謬誤", Raw: "#形式謬誤"}},

		// combined alias + anchor
		{"foo#Section|See this", true, WikiLink{Slug: "foo", Anchor: "Section", Alias: "See this", Raw: "foo#Section|See this"}},
		{"#Intro|Click here", true, WikiLink{Anchor: "Intro", Alias: "Click here", Raw: "#Intro|Click here"}},

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
		{"#", false, WikiLink{}},
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
		{WikiLink{Slug: "why"}, "shawn", "/shawn/why"},
		{WikiLink{User: "alice", Slug: "foo"}, "shawn", "/alice/foo"},
		{WikiLink{User: "alice", Slug: "why"}, "shawn", "/alice/why"},
		{WikiLink{User: "alice", Slug: "c"}, "shawn", "/alice/c"},
		// with anchor
		{WikiLink{Slug: "foo", Anchor: "Introduction"}, "shawn", "/shawn/foo#introduction"},
		{WikiLink{Slug: "foo", Anchor: "Section 2"}, "shawn", "/shawn/foo#section-2"},
		{WikiLink{Slug: "foo", Anchor: "形式謬誤"}, "shawn", "/shawn/foo#形式謬誤"},
		// same-note
		{WikiLink{Anchor: "Introduction"}, "shawn", "#introduction"},
		{WikiLink{Anchor: "形式謬誤"}, "shawn", "#形式謬誤"},
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
		{Slug: "baz", Raw: "bar/baz"},
		{User: "alice", Slug: "note", Raw: "@alice/note"},
		{User: "bob", Slug: "x", Raw: "@bob/folder/x"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Extract\n got: %+v\nwant: %+v", got, want)
	}
}

func TestExtractSkipsSameNoteLinks(t *testing.T) {
	body := `See [[#Introduction]] and [[foo#Section]] and [[bar]]`
	got := Extract(body)
	// [[#Introduction]] is same-note, skipped
	// [[foo#Section]] links to note "foo" (Slug="foo"), included
	// [[bar]] included
	want := []WikiLink{
		{Slug: "foo", Anchor: "Section", Raw: "foo#Section"},
		{Slug: "bar", Raw: "bar"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ExtractSkipsSameNote\n got: %+v\nwant: %+v", got, want)
	}
}

func TestHeadingAnchor(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Introduction", "introduction"},
		{"Section 2", "section-2"},
		{"Hello, World!", "hello-world"},
		{"形式謬誤", "形式謬誤"},
		{"  Trim Me  ", "trim-me"},
		{"", ""},
		{"---", ""},
	}
	for _, c := range cases {
		got := headingAnchor(c.in)
		if got != c.want {
			t.Errorf("headingAnchor(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestExtractInlineTags(t *testing.T) {
	cases := []struct {
		name string
		body string
		want []string
	}{
		{
			"basic tags",
			"I like #philosophy and #logic.",
			[]string{"philosophy", "logic"},
		},
		{
			"dedup",
			"#foo and #foo again",
			[]string{"foo"},
		},
		{
			"word boundary blocks mid-word",
			"foo#bar and baz2#tag",
			nil,
		},
		{
			"all digits rejected",
			"#123 is not a tag",
			nil,
		},
		{
			"nested tag",
			"tagged as #philosophy/logic here",
			[]string{"philosophy/logic"},
		},
		{
			"CJK tag",
			"tag is #哲學",
			[]string{"哲學"},
		},
		{
			"url fragment is not a tag",
			"see [x](https://ex.com/a/#frag) and https://y.io/b#other, but #real counts",
			[]string{"real"},
		},
		{
			"frontmatter skipped",
			"---\ntags: [philosophy]\n---\n\n#logic is cool",
			[]string{"logic"},
		},
		{
			"headings are not tags",
			"# Heading\n## Subheading\n\nBody #real",
			[]string{"real"},
		},
		{
			"consecutive hashes do not open a tag",
			"##不是標籤",
			nil,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ExtractInlineTags(c.body)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("ExtractInlineTags(%q)\n got: %v\nwant: %v", c.body, got, c.want)
			}
		})
	}
}
