package markdown

import "testing"

func TestStripMedia(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		removed int
	}{
		{"markdown image", "before ![alt](https://x/y.png) after", "before  after", 1},
		{"image alone on a line", "![](/a.gif)", "", 1},
		{"obsidian attachment", "see ![[Pasted image 20240101.png]] here", "see  here", 1},
		{"attachment with size", "![[shot.png|300]]", "", 1},
		{"audio and video attachments", "![[a.mp3]] ![[b.mp4]]", " ", 2},
		{"note embed survives", "![[Some Note]] stays", "![[Some Note]] stays", 0},
		{"note embed with anchor survives", "![[Note#Heading]]", "![[Note#Heading]]", 0},
		{"cross-vault note embed survives", "![[@bob/their-note]]", "![[@bob/their-note]]", 0},
		{"html media", `text <img src="https://x/y.png"> more`, "text  more", 1},
		{"html video pair", "<video src=x></video>", "", 2},
		{"hyperlink survives", "[a link](https://example.com) stays", "[a link](https://example.com) stays", 0},
		{"bare url survives", "https://example.com/pic.png", "https://example.com/pic.png", 0},
		{"plain text untouched", "just words", "just words", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, n := StripMedia(tc.in)
			if got != tc.want {
				t.Errorf("body:\n got %q\nwant %q", got, tc.want)
			}
			if n != tc.removed {
				t.Errorf("removed count: got %d want %d", n, tc.removed)
			}
		})
	}
}

// A note embed is not an image. FirstImageURL used to search ahead for "](",
// so ![[a note]] paired with an unrelated link later in the paragraph and the
// feed card hotlinked that link's URL as its thumbnail.
func TestFirstImageURLIgnoresNoteEmbeds(t *testing.T) {
	cases := map[string]string{
		"![[a note]] and [a link](https://example.com)": "",
		"![[a note]]":                              "",
		"[just a link](https://example.com)":       "",
		"![alt](https://x/y.png)":                  "https://x/y.png",
		"![[a note]] then ![alt](https://x/y.png)": "https://x/y.png",
	}
	for in, want := range cases {
		if got := FirstImageURL(in); got != want {
			t.Errorf("FirstImageURL(%q) = %q, want %q", in, got, want)
		}
	}
}

// The feed card's thumbnail is the first image in the body, hotlinked into
// every viewer's feed. After stripping there must be nothing left to hotlink.
func TestStripMediaLeavesNoThumbnail(t *testing.T) {
	body, n := StripMedia("intro\n\n![](https://tracker.example/px.gif)\n\nrest")
	if n != 1 {
		t.Fatalf("expected 1 removal, got %d", n)
	}
	if got := FirstImageURL(body); got != "" {
		t.Errorf("a thumbnail URL survived: %q", got)
	}
}
