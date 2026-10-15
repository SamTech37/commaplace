package markdown

import (
	"strings"
	"testing"
)

func withBlockRefs(t *testing.T, on bool) {
	t.Helper()
	old := blockRefs
	blockRefs = on
	t.Cleanup(func() { blockRefs = old })
}

const sectionDoc = `# Top
intro
## Alpha
alpha body
### Deep
deep body
## Beta: Notes?
beta body
` + "```" + `
## not a heading
` + "```" + `
# Next
next body
## Alpha
second alpha
`

// Numbers refer to "Behavior rules" in the spec.
func TestSectionHeading(t *testing.T) {
	cases := []struct {
		name, anchor, want string
		ok                 bool
	}{
		{"3: ends at same level, keeps deeper", "Alpha", "## Alpha\nalpha body\n### Deep\ndeep body\n", true},
		{"3: ends at higher level", "Deep", "### Deep\ndeep body\n", true},
		{"3: fenced # is not a heading", "Beta: Notes?", "## Beta: Notes?\nbeta body\n```\n## not a heading\n```\n", true},
		{"3: runs to end of file", "Next#Alpha", "## Alpha\nsecond alpha\n", true},
		{"2: punctuation and case fold", "beta notes", "## Beta: Notes?\nbeta body\n```\n## not a heading\n```\n", true},
		{"2: path skips levels", "Top#Deep", "### Deep\ndeep body\n", true},
		{"2: first match wins", "Alpha", "## Alpha\nalpha body\n### Deep\ndeep body\n", true},
		{"2: path must go deeper", "Deep#Alpha", "", false},
		{"2: fenced text never matches", "not a heading", "", false},
		{"6: missing heading", "Gamma", "", false},
		{"1: empty parts dropped", "#Top##Deep#", "### Deep\ndeep body\n", true},
		{"1: ^ after a heading is a heading lookup", "Alpha#^x", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := Section(sectionDoc, c.anchor)
			if ok != c.ok || got != c.want {
				t.Errorf("Section(%q) = %q, %v; want %q, %v", c.anchor, got, ok, c.want, c.ok)
			}
		})
	}
}

func TestSectionBlockOffEmbedsWholeNote(t *testing.T) {
	withBlockRefs(t, false)
	md := "a ^x\n\nb\n"
	if got, ok := Section(md, "^x"); !ok || got != md {
		t.Errorf("Section(^x) with block refs off = %q, %v; want whole note", got, ok)
	}
}

const blockDoc = `para one ^p1

| a | b |
|---|---|
| 1 | 2 |

^tbl

- item one ^Li
  - child
- item two
  - nested ^deep
    - grandchild

> quote line
> more ^q

after
`

func TestSectionBlock(t *testing.T) {
	withBlockRefs(t, true)
	cases := []struct {
		name, anchor, want string
		ok                 bool
	}{
		{"4: inline id at end of paragraph", "^p1", "para one ^p1\n", true},
		{"4: standalone id names the block before", "^tbl", "| a | b |\n|---|---|\n| 1 | 2 |\n", true},
		{"5: list item includes its sub-list", "^li", "- item one ^Li\n  - child\n", true},
		{"5: nested item is dedented", "^deep", "- nested ^deep\n  - grandchild\n", true},
		{"4: id inside a quote names the quote", "^q", "> quote line\n> more ^q\n", true},
		{"6: missing block", "^nope", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := Section(blockDoc, c.anchor)
			if ok != c.ok || got != c.want {
				t.Errorf("Section(%q) = %q, %v; want %q, %v", c.anchor, got, ok, c.want, c.ok)
			}
		})
	}
}

// Markers never reach the reader, whether or not block refs are on.
func TestBlockMarkersHidden(t *testing.T) {
	for _, on := range []bool{false, true} {
		withBlockRefs(t, on)
		out := render(t, blockDoc+"\nmulti\nline\n^tail\n\n"+"```\ncode ^keep\n```\n")
		for _, marker := range []string{"^p1", "^tbl", "^Li", "^deep", "^q", "^tail"} {
			if strings.Contains(out, marker+"<") || strings.Contains(out, marker+"\n") || strings.Contains(out, " "+marker) {
				t.Errorf("blockRefs=%v: marker %s visible in:\n%s", on, marker, out)
			}
		}
		if !strings.Contains(out, "code ^keep") {
			t.Errorf("blockRefs=%v: marker inside code was stripped:\n%s", on, out)
		}
		if !strings.Contains(out, "multi\nline</p>") {
			t.Errorf("blockRefs=%v: own-line marker left a break:\n%s", on, out)
		}
		hasIDs := strings.Contains(out, `id="^p1"`)
		if hasIDs != on {
			t.Errorf("blockRefs=%v: id attributes present=%v:\n%s", on, hasIDs, out)
		}
	}
}

func TestBlockIDAttributes(t *testing.T) {
	withBlockRefs(t, true)
	out := render(t, blockDoc+"\n> [!note] Title\n> body ^cal\n")
	for _, want := range []string{
		`<p id="^p1">para one</p>`,
		`<table id="^tbl">`,
		`<li id="^Li">item one`,
		`<li id="^deep">nested`,
		`<blockquote id="^q">`,
		`<div class="callout callout-note" id="^cal">`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s in:\n%s", want, out)
		}
	}
}

// "foo^x" and "^x foo" are not markers: the id must end the block, after
// whitespace.
func TestBlockIDNeedsWhitespaceAndEnd(t *testing.T) {
	withBlockRefs(t, true)
	out := render(t, "foo^x\n\n^y foo\n")
	if !strings.Contains(out, "foo^x") || !strings.Contains(out, "^y foo") || strings.Contains(out, `id="`) {
		t.Errorf("non-markers treated as markers:\n%s", out)
	}
}

func TestLinkFragment(t *testing.T) {
	cases := []struct {
		raw     string
		refsOn  bool
		wantURL string
	}{
		{"note#Alpha", false, "/alice/note#alpha"},
		{"note#Top#Deep", false, "/alice/note#deep"},
		{"note#^abc", false, "/alice/note"},
		{"note#^abc", true, "/alice/note#^abc"},
		{"#^abc", true, "#^abc"},
	}
	for _, c := range cases {
		withBlockRefs(t, c.refsOn)
		l, ok := ParseLink(c.raw)
		if !ok {
			t.Fatalf("ParseLink(%q) failed", c.raw)
		}
		if got := l.URL("alice"); got != c.wantURL {
			t.Errorf("URL(%q, refs=%v) = %q, want %q", c.raw, c.refsOn, got, c.wantURL)
		}
	}
}

func render(t *testing.T, md string) string {
	t.Helper()
	out, err := Render(md, "alice", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestExcerptHidesBlockMarkers(t *testing.T) {
	got := Excerpt("Para one ^p1\n\n- item ^li\n\n^tbl\n\nfoo^bar", 200)
	if want := "Para one\n\nitem\n\nfoo^bar"; got != want {
		t.Errorf("Excerpt = %q, want %q", got, want)
	}
}
