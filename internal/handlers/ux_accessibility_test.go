package handlers

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/a-h/templ"
)

func TestLayoutExposesReachableNavigation(t *testing.T) {
	var out strings.Builder
	if err := Layout(ChromeProps{}, "test", "", nil, templ.Raw("<p>body</p>")).Render(context.Background(), &out); err != nil {
		t.Fatalf("render layout: %v", err)
	}

	html := out.String()
	for _, want := range []string{
		`lang="zh-Hant"`,
		`class="skip-link"`,
		`id="main-content"`,
		`aria-label="主要導覽"`,
		`class="mobile-dock"`,
		`id="mobile-palette-btn"`,
		// The display preferences used to be mirrored here as three buttons,
		// because the topbar hid them below 800px. They now live in the account
		// menu (or the 設 menu when signed out), which renders at every width,
		// so the mirrors were removed rather than left as a second path to the
		// same settings.
		`data-pref-set="theme"`,
		`data-pref-set="motion"`,
		`data-pref-set="script"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("layout missing %s", want)
		}
	}
}

func TestWritePageKeepsEditorActionsAndStatus(t *testing.T) {
	var out strings.Builder
	if err := writePage(WriteProps{NoteID: "note-id", Document: "Title\n\nBody"}).Render(context.Background(), &out); err != nil {
		t.Fatalf("render editor: %v", err)
	}

	html := out.String()
	for _, want := range []string{
		`id="publish-btn"`,
		`id="save-status"`,
		`id="editor-recovery"`,
		`data-recover-draft`,
		`data-discard-draft`,
		`aria-label="筆記內容，第一行為標題"`,
		`id="word-count"`,
		`id="character-count"`,
		`id="cursor-position"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("editor missing %s", want)
		}
	}

	script, err := os.ReadFile("static/cmeditor.js")
	if err != nil {
		t.Fatalf("read editor script: %v", err)
	}
	js := string(script)
	for _, action := range []string{
		`tb("bold"`,
		`tb("italic"`,
		`tb("h1"`,
		`tb("h2"`,
		`tb("quote"`,
		`tb("bullets"`,
		`tb("numbers"`,
		`tb("wikilink"`,
		`tb("link"`,
		`tb("tag"`,
		`tb("image"`,
		`tb("code"`,
		`tb("mdupload"`,
		`tb("source"`,
		`tb("quote", EasyMDE.toggleBlockquote, "縮排"`,
		`tb("bullets", EasyMDE.toggleUnorderedList, "• 清單"`,
		`tb("numbers", EasyMDE.toggleOrderedList, "1. 編號"`,
		`previewImagesInEditor: true`,
		`inputStyle: "textarea"`,
		`cm-live-active-line`,
	} {
		if !strings.Contains(js, action) {
			t.Errorf("editor script missing action %s", action)
		}
	}

	stylesheet, err := os.ReadFile("static/style.css")
	if err != nil {
		t.Fatalf("read editor stylesheet: %v", err)
	}
	css := string(stylesheet)
	for _, rule := range []string{
		`.editor-page:not(.source-mode)`,
		`.CodeMirror-line:not(.cm-live-active-line)`,
		`:has([data-img-src])`,
		`.tb-source[aria-pressed="true"]`,
	} {
		if !strings.Contains(css, rule) {
			t.Errorf("editor stylesheet missing %s", rule)
		}
	}
}
