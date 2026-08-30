package handlers

import (
	"strings"
	"testing"
)

// A leading blockquote renders in its own card slot; the excerpt must continue
// after it instead of repeating it.
func TestQuoteCardExcerptSkipsQuote(t *testing.T) {
	body := "> Downloaded format are mostly `.pdf` or `.epub`\n\n- Somewhat shady\n- Reliable sources follow\n"
	variant, excerpt, _, quote, _ := analyzeCardBody(body)
	if variant != "quote" {
		t.Fatalf("variant = %q, want quote", variant)
	}
	if !strings.Contains(quote, "Downloaded format") {
		t.Fatalf("quote = %q", quote)
	}
	if strings.Contains(excerpt, "Downloaded format") {
		t.Fatalf("excerpt repeats the quote: %q", excerpt)
	}
	if !strings.Contains(excerpt, "Somewhat shady") {
		t.Fatalf("excerpt = %q", excerpt)
	}
}

// The links variant renders wiki links as chips; the excerpt must not repeat
// the same slugs as prose.
func TestLinksCardExcerptDropsLinkText(t *testing.T) {
	body := "Reading list below.\n\n[[gutenberg]] [[standard-ebooks]] [[@bob/arxiv]] are the reliable ones.\n"
	variant, excerpt, _, _, chips := analyzeCardBody(body)
	if variant != "links" {
		t.Fatalf("variant = %q, want links", variant)
	}
	if len(chips) != 3 {
		t.Fatalf("chips = %v", chips)
	}
	for _, slug := range []string{"gutenberg", "standard-ebooks", "arxiv"} {
		if strings.Contains(excerpt, slug) {
			t.Fatalf("excerpt repeats chip %q: %q", slug, excerpt)
		}
	}
	if !strings.Contains(excerpt, "Reading list below") {
		t.Fatalf("excerpt = %q", excerpt)
	}
}

// Importers take the title from a leading H1 and leave it in the body; the
// card already shows the title, so the excerpt must start after it.
func TestExcerptSkipsLeadingH1(t *testing.T) {
	_, excerpt, _, _, _ := analyzeCardBody("# Ebook Sources\n\nWhere to find books.\n")
	if strings.HasPrefix(excerpt, "Ebook Sources") {
		t.Fatalf("excerpt repeats the title: %q", excerpt)
	}
	if !strings.Contains(excerpt, "Where to find books") {
		t.Fatalf("excerpt = %q", excerpt)
	}
}
