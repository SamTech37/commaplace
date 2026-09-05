package markdown

import (
	"regexp"
	"strings"
)

// mediaExts are the file extensions that make an ![[...]] embed an attachment
// rather than a note embed. Extensionless targets are notes and are kept.
var mediaExts = map[string]bool{
	"png": true, "jpg": true, "jpeg": true, "gif": true, "webp": true,
	"svg": true, "bmp": true, "avif": true, "ico": true, "tiff": true,
	"mp4": true, "webm": true, "mov": true, "avi": true, "mkv": true, "m4v": true,
	"mp3": true, "wav": true, "ogg": true, "oga": true, "flac": true,
	"m4a": true, "aac": true, "opus": true,
}

// mediaTagRe matches an HTML tag that carries or plays media. goldmark renders
// with raw HTML disabled, so these are inert text today; they are stripped so
// the stored markdown cannot come alive if that ever changes.
var mediaTagRe = regexp.MustCompile(`(?is)<\s*/?\s*(img|video|audio|source|track|picture|iframe|embed|object|svg)\b[^>]*>`)

// StripMedia removes every way a note can pull in an image, video, audio clip
// or other non-text object, and reports how many it removed. Hyperlinks and
// their text survive untouched — a link to a video is text, playing one is not.
//
// Imported notes need this because there is nowhere to put an attachment: the
// project does not host uploaded files (no object storage, deliberately), so a
// surviving reference can only point somewhere else. That is worse than a
// missing picture. FirstImageURL feeds a note's first image straight into the
// feed card as a thumbnail, so one imported ![](https://…) hotlinks a stranger's
// server on every viewer's feed — a tracking pixel with a guaranteed audience.
//
// The note's own cover image (POST /api/notes/{id}/image) is unaffected: it is
// bytes in our database, not a reference to somebody else's host.
func StripMedia(md string) (string, int) {
	out, n := stripMediaEmbeds(md)
	out, m := stripImageLinks(out)
	n += m

	out = mediaTagRe.ReplaceAllStringFunc(out, func(string) string {
		n++
		return ""
	})
	return out, n
}

// stripMediaEmbeds drops ![[file.png]] but keeps ![[some note]] — the same
// syntax means "attachment" or "embed another note" depending on the target.
func stripMediaEmbeds(md string) (string, int) {
	var b strings.Builder
	n := 0
	for {
		i := strings.Index(md, "![[")
		if i < 0 {
			b.WriteString(md)
			break
		}
		end := strings.Index(md[i:], "]]")
		if end < 0 {
			b.WriteString(md)
			break
		}
		target := md[i+3 : i+end]
		// "file.png|300" and "file.png#anchor" still name a file.
		if cut := strings.IndexAny(target, "|#"); cut >= 0 {
			target = target[:cut]
		}
		b.WriteString(md[:i])
		if isMediaPath(target) {
			n++
		} else {
			b.WriteString(md[i : i+end+2])
		}
		md = md[i+end+2:]
	}
	return b.String(), n
}

// stripImageLinks removes inline and reference-style images, with balanced
// delimiters. Ordinary hyperlinks and Obsidian note embeds remain intact.
func stripImageLinks(md string) (string, int) {
	// Reference images also load remote media, even though their URL is on a
	// separate line. Keep definitions: ordinary hyperlinks may share them.
	references := map[string]bool{}
	for _, m := range imageReferenceDefinition.FindAllStringSubmatch(md, -1) {
		references[imageReferenceKey(m[1])] = true
	}
	var b strings.Builder
	n := 0
	for {
		i := strings.Index(md, "![")
		if i < 0 {
			b.WriteString(md)
			break
		}
		// ![[...]] is a note embed, not an image — stripMediaEmbeds has
		// already had its say about those. Step over it.
		if strings.HasPrefix(md[i:], "![[") {
			b.WriteString(md[:i+3])
			md = md[i+3:]
			continue
		}
		alt := mediaDelimiterEnd(md, i+1, '[', ']')
		end := -1
		if alt >= 0 {
			label := md[i+2 : alt]
			switch {
			case alt+1 < len(md) && md[alt+1] == '(':
				end = mediaDelimiterEnd(md, alt+1, '(', ')')
			case alt+1 < len(md) && md[alt+1] == '[':
				if refEnd := mediaDelimiterEnd(md, alt+1, '[', ']'); refEnd >= 0 {
					ref := md[alt+2 : refEnd]
					if ref == "" {
						ref = label
					}
					if references[imageReferenceKey(ref)] {
						end = refEnd
					}
				}
			default:
				if references[imageReferenceKey(label)] {
					end = alt
				}
			}
		}
		if end < 0 {
			b.WriteString(md[:i+2])
			md = md[i+2:]
			continue
		}
		b.WriteString(md[:i])
		n++
		md = md[end+1:]
	}
	return b.String(), n
}

var imageReferenceDefinition = regexp.MustCompile(`(?m)^ {0,3}\[([^\]\n]+)\]:`)

func imageReferenceKey(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }

func mediaDelimiterEnd(s string, start int, open, close byte) int {
	depth := 0
	for i := start; i < len(s); i++ {
		if s[i] == '\\' {
			i++
			continue
		}
		if s[i] == open {
			depth++
		}
		if s[i] == close {
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func isMediaPath(target string) bool {
	dot := strings.LastIndexByte(target, '.')
	if dot < 0 {
		return false
	}
	return mediaExts[strings.ToLower(strings.TrimSpace(target[dot+1:]))]
}
