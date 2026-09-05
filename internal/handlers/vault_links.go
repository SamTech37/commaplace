package handlers

import (
	"net/url"
	"path"
	"regexp"
	"strings"

	"commonplace/internal/markdown"
)

type vaultIndex struct {
	paths map[string]*vaultNote
	names map[string][]*vaultNote
}

func vaultKey(s string) string {
	s = strings.TrimSpace(s)
	if strings.EqualFold(path.Ext(s), ".md") {
		s = s[:len(s)-3]
	}
	return s
}

func newVaultIndex(notes []*vaultNote) vaultIndex {
	v := vaultIndex{map[string]*vaultNote{}, map[string][]*vaultNote{}}
	for _, n := range notes {
		k := vaultKey(n.Path)
		v.paths[k] = n
		v.names[path.Base(k)] = append(v.names[path.Base(k)], n)
	}
	return v
}

func (v vaultIndex) resolve(source, target string) (*vaultNote, bool) {
	k := vaultKey(target)
	if k == "" || strings.HasPrefix(k, "@") {
		return nil, false
	}
	// Explicit relative paths, then vault-root paths, then a same-folder
	// basename, finally an unambiguous basename elsewhere in the vault.
	if strings.HasPrefix(k, "./") || strings.HasPrefix(k, "../") {
		return v.paths[path.Clean(path.Join(path.Dir(source), k))], false
	}
	if strings.Contains(k, "/") {
		return v.paths[k], false
	}
	if n := v.paths[path.Join(path.Dir(source), k)]; n != nil {
		return n, false
	}
	if candidates := v.names[k]; len(candidates) == 1 {
		return candidates[0], false
	} else if len(candidates) > 1 {
		return nil, true
	}
	return nil, false
}

var vaultMarkdownLink = regexp.MustCompile(`^\[([^\]\n]+)\]\(([^\s)]+)([^)\n]*)\)`)

// Rewrite only link destinations. Keep aliases, heading/block fragments,
// note embeds, code fences, inline code and Obsidian comments intact.
func rewriteVaultLinks(body string, n *vaultNote, index vaultIndex) string {
	var out strings.Builder
	prefix, body := markdown.SplitFrontmatter(body)
	out.WriteString(prefix)
	fence := ""
	comment := false
	for _, line := range strings.SplitAfter(body, "\n") {
		trim := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~") {
			mark := trim[:1]
			run := len(trim) - len(strings.TrimLeft(trim, mark))
			if fence == "" {
				fence = strings.Repeat(mark, run)
			} else if strings.HasPrefix(trim, fence) {
				fence = ""
			}
			out.WriteString(line)
			continue
		}
		if fence != "" || strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t") {
			out.WriteString(line)
			continue
		}
		for i := 0; i < len(line); {
			if strings.HasPrefix(line[i:], "%%") {
				comment = !comment
				out.WriteString("%%")
				i += 2
				continue
			}
			if comment {
				out.WriteByte(line[i])
				i++
				continue
			}
			if line[i] == '\\' && i+1 < len(line) {
				out.WriteString(line[i : i+2])
				i += 2
				continue
			}
			if line[i] == '`' {
				end := i
				for end < len(line) && line[end] == '`' {
					end++
				}
				if j := strings.Index(line[end:], line[i:end]); j >= 0 {
					end += j + end - i
				}
				out.WriteString(line[i:end])
				i = end
				continue
			}
			if strings.HasPrefix(line[i:], "[[") {
				if j := strings.Index(line[i+2:], "]]"); j >= 0 {
					inner := line[i+2 : i+2+j]
					target := inner
					suffix := ""
					if cut := strings.IndexAny(inner, "#|"); cut >= 0 {
						target, suffix = inner[:cut], inner[cut:]
					}
					if to, ambiguous := index.resolve(n.Path, target); to != nil {
						if !strings.Contains(suffix, "|") {
							suffix += "|" + target
						}
						inner = to.Slug + suffix
					} else if ambiguous {
						// An unresolved unique target prevents silently pointing at an
						// arbitrary same-name file after the folder structure is flattened.
						n.Warnings = append(n.Warnings, "同名連結需指定資料夾："+target)
						if !strings.Contains(suffix, "|") {
							suffix += "|" + target
						}
						inner = "missing-" + n.ID.String() + "-" + kebabSlug(target) + suffix
					}
					out.WriteString("[[" + inner + "]]")
					i += j + 4
					continue
				}
			}
			if line[i] == '[' {
				if m := vaultMarkdownLink.FindStringSubmatch(line[i:]); m != nil {
					dest := strings.Trim(m[2], "<>")
					u, err := url.Parse(dest)
					if err == nil && !u.IsAbs() && u.Host == "" && strings.EqualFold(path.Ext(u.Path), ".md") {
						target := u.Path
						if !strings.HasPrefix(target, "/") {
							target = "./" + target
						}
						if to, _ := index.resolve(n.Path, target); to != nil {
							fragment := ""
							if u.Fragment != "" {
								fragment = "#" + u.Fragment
							}
							out.WriteString("[[" + to.Slug + fragment + "|" + m[1] + "]]")
							i += len(m[0])
							continue
						}
					}
				}
			}
			out.WriteByte(line[i])
			i++
		}
	}
	return out.String()
}
