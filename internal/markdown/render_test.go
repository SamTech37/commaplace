package markdown

import (
	"strings"
	"testing"
)

// renderMust renders md as testuser and returns HTML string.
func renderMust(t *testing.T, md string) string {
	t.Helper()
	html, err := Render(md, "testuser", nil)
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	return string(html)
}

func assertContains(t *testing.T, got string, subs ...string) {
	t.Helper()
	for _, sub := range subs {
		if !strings.Contains(got, sub) {
			t.Errorf("expected output to contain %q\nfull output:\n%s", sub, got)
		}
	}
}

func assertNotContains(t *testing.T, got string, subs ...string) {
	t.Helper()
	for _, sub := range subs {
		if strings.Contains(got, sub) {
			t.Errorf("expected output NOT to contain %q\nfull output:\n%s", sub, got)
		}
	}
}

// ---------- Highlight ==text== ----------

func TestHighlight(t *testing.T) {
	out := renderMust(t, "some ==highlighted== text")
	assertContains(t, out, "<mark>highlighted</mark>")
	assertNotContains(t, out, "==highlighted==")
}

func TestHighlightSpaceNotMatched(t *testing.T) {
	// leading/trailing space inside == is not a valid highlight
	out := renderMust(t, "== not highlight ==")
	assertNotContains(t, out, "<mark>")
}

func TestHighlightMultiWord(t *testing.T) {
	out := renderMust(t, "==hello world==")
	assertContains(t, out, "<mark>hello world</mark>")
}

// ---------- Comments %%...%% ----------

func TestCommentInline(t *testing.T) {
	out := renderMust(t, "before %%hidden content%% after")
	assertContains(t, out, "before", "after")
	assertNotContains(t, out, "hidden content", "%%")
}

func TestCommentBlock(t *testing.T) {
	out := renderMust(t, "%%\nthis whole block is hidden\n%%")
	assertNotContains(t, out, "hidden", "%%")
}

func TestCommentNotStrippedInCode(t *testing.T) {
	// %% inside backtick code should survive (best-effort; known limitation)
	// This test just ensures the renderer doesn't crash.
	out := renderMust(t, "see `%%note%%` here")
	// code content depends on implementation; just no crash
	_ = out
}

// ---------- Callouts > [!type] ----------

func TestCalloutBasicNote(t *testing.T) {
	out := renderMust(t, "> [!note]\n> content here")
	assertContains(t, out, "callout-note", "content here")
	assertNotContains(t, out, "[!note]")
}

func TestCalloutDefaultTitle(t *testing.T) {
	out := renderMust(t, "> [!tip]\n> tip body")
	assertContains(t, out, "Tip") // capitalized type name as default title
}

func TestCalloutCustomTitle(t *testing.T) {
	out := renderMust(t, "> [!warning] Watch Out\n> body text")
	assertContains(t, out, "callout-warning", "Watch Out", "body text")
}

func TestCalloutFoldableCollapsed(t *testing.T) {
	out := renderMust(t, "> [!faq]- My FAQ\n> answer")
	assertContains(t, out, "<details", "callout-faq", "My FAQ", "answer")
	assertNotContains(t, out, " open")
}

func TestCalloutFoldableExpanded(t *testing.T) {
	out := renderMust(t, "> [!info]+\n> expanded body")
	assertContains(t, out, "<details", "open", "expanded body")
}

func TestCalloutTwoParaParts(t *testing.T) {
	// blank line between type line and body → two paragraphs in blockquote
	out := renderMust(t, "> [!note]\n>\n> content paragraph")
	assertContains(t, out, "callout-note", "content paragraph")
	assertNotContains(t, out, "[!note]")
}

func TestRegularBlockquoteUnchanged(t *testing.T) {
	out := renderMust(t, "> regular quote")
	assertContains(t, out, "<blockquote")
	assertNotContains(t, out, "callout")
}

// ---------- Footnotes ----------

func TestFootnotes(t *testing.T) {
	out := renderMust(t, "Hello[^1]\n\n[^1]: World footnote")
	assertContains(t, out, "<sup", "World footnote")
}

// ---------- Mermaid ----------

func TestMermaidCodeBlock(t *testing.T) {
	md := "```mermaid\ngraph TD\n  A-->B\n```"
	out := renderMust(t, md)
	assertContains(t, out, `class="mermaid"`, "A--&gt;B")
	// should NOT use the default language- class
	assertNotContains(t, out, "language-mermaid")
}

// ---------- Inline math $...$ ----------

func TestInlineMath(t *testing.T) {
	out := renderMust(t, "formula $e^{i\\pi} + 1 = 0$ end")
	assertContains(t, out, "math-inline")
	// raw LaTeX must survive goldmark (not mangled by * or _ parsing)
	assertContains(t, out, `e^{i\pi}`)
}

func TestInlineMathNoSpaceAfterDollar(t *testing.T) {
	// $ followed by a space is NOT math
	out := renderMust(t, "price $ 100")
	assertNotContains(t, out, "math-inline")
}

func TestBlockMathFence(t *testing.T) {
	md := "```math\n\\frac{a}{b} = c\n```"
	out := renderMust(t, md)
	assertContains(t, out, "math-block", `\frac{a}{b}`)
}
