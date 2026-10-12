package lint

import (
	"fmt"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"
)

// quote is one string delimiter. A single-line string ends with its line
// unless the line ends in a backslash continuation, so a stray quote character
// cannot hide the directives that follow it.
type quote struct {
	delim     string
	multiline bool
	raw       bool // A backslash does not escape the next character.
}

var (
	cQuotes      = []quote{{delim: `"`}, {delim: "'"}}
	tripleQuotes = []quote{{delim: `"""`, multiline: true}, {delim: "'''", multiline: true}, {delim: `"`}, {delim: "'"}}
	shellQuotes  = []quote{{delim: `"`, multiline: true}, {delim: "'", multiline: true, raw: true}, {delim: "`", multiline: true}}
	scriptQuotes = []quote{{delim: `"`, multiline: true}, {delim: "'", multiline: true}, {delim: "`", multiline: true}}
	// YAML quoted scalars may span lines, but Helm and other templates put
	// quotes mid-line (for example {{ join "," . }}); single-line state is safer.
	yamlQuotes   = []quote{{delim: `"`}, {delim: "'", raw: true}}
	htmlQuotes   = []quote{{delim: `"`, multiline: true, raw: true}, {delim: "'", multiline: true, raw: true}}
	mdQuotes     = append(htmlQuotes[:2:2], quote{delim: "`", raw: true})
	markupBlocks = [][2]string{{"<!--", "-->"}}
)

type commentStyle struct {
	lines    []string
	blocks   [][2]string
	quotes   []quote // Longer delimiters first.
	escapes  bool    // A backslash outside strings escapes the next character.
	rust     bool    // A single quote starts a char literal or a lifetime, never a string.
	markup   bool    // Quotes open strings only inside tags.
	markdown bool
	yaml     bool
}

func styleFor(filename string) (commentStyle, bool) {
	c := commentStyle{lines: []string{"//"}, blocks: [][2]string{{"/*", "*/"}}, quotes: cQuotes}
	switch strings.ToLower(path.Ext(filename)) {
	case ".go":
		c.quotes = append(cQuotes[:2:2], quote{delim: "`", multiline: true, raw: true})
		return c, true
	case ".js", ".jsx", ".ts", ".tsx":
		c.quotes = append(cQuotes[:2:2], quote{delim: "`", multiline: true})
		return c, true
	case ".rs":
		c.quotes, c.rust = []quote{{delim: `"`, multiline: true}}, true
		return c, true
	case ".c", ".h", ".cc", ".cpp", ".cxx", ".hpp", ".java", ".kt", ".kts", ".swift", ".cs", ".proto", ".dart":
		c.quotes = tripleQuotes
		return c, true
	case ".css", ".scss":
		return c, true
	case ".py", ".pyi", ".bzl", ".bazel", ".toml":
		return commentStyle{lines: []string{"#"}, quotes: tripleQuotes}, true
	case ".yml", ".yaml":
		return commentStyle{lines: []string{"#"}, quotes: yamlQuotes, yaml: true}, true
	case ".sh", ".bash", ".zsh":
		return commentStyle{lines: []string{"#"}, quotes: shellQuotes, escapes: true}, true
	case ".rb", ".r", ".pl":
		return commentStyle{lines: []string{"#"}, quotes: scriptQuotes, escapes: true}, true
	case ".sql", ".lua":
		return commentStyle{lines: []string{"--"}, blocks: [][2]string{{"/*", "*/"}, {"--[[", "]]"}}, quotes: cQuotes}, true
	case ".ini", ".cfg", ".conf":
		return commentStyle{lines: []string{";", "#"}}, true
	case ".el", ".lisp", ".clj":
		return commentStyle{lines: []string{";", "#"}, quotes: []quote{{delim: `"`, multiline: true}}, escapes: true}, true
	case ".tex":
		return commentStyle{lines: []string{"%"}}, true
	case ".m", ".erl":
		return commentStyle{lines: []string{"%"}, quotes: cQuotes}, true
	case ".vb", ".vbs", ".bas":
		return commentStyle{lines: []string{"'", "REM"}, quotes: []quote{{delim: `"`}}}, true
	case ".md", ".mdx":
		return commentStyle{blocks: markupBlocks, quotes: mdQuotes, markup: true, markdown: true}, true
	case ".html", ".htm", ".xml", ".svg", ".vue", ".svelte":
		return commentStyle{blocks: markupBlocks, quotes: htmlQuotes, markup: true}, true
	}
	switch path.Base(filename) {
	case "Makefile", "Dockerfile":
		return commentStyle{lines: []string{"#"}, quotes: cQuotes}, true
	case "BUILD", "WORKSPACE":
		return commentStyle{lines: []string{"#"}, quotes: tripleQuotes}, true
	case ".gitignore":
		return commentStyle{lines: []string{"#"}}, true
	}
	return commentStyle{}, false
}

// opens returns the string delimiter that starts at line[i], if any.
func (c commentStyle) opens(line string, i int, inTag bool) *quote {
	for k := range c.quotes {
		q := &c.quotes[k]
		if !strings.HasPrefix(line[i:], q.delim) {
			continue
		}
		if c.markup && q.delim != "`" && !inTag {
			return nil
		}
		return q
	}
	return nil
}

// rustChar returns the length of the char literal at s, or 1 for a lifetime.
func rustChar(s string) int {
	if len(s) > 3 && s[1] == '\\' {
		if end := strings.IndexByte(s[3:], '\''); end >= 0 {
			return end + 4
		}
		return 1
	}
	_, size := utf8.DecodeRuneInString(s[1:])
	if 1+size < len(s) && s[1+size] == '\'' {
		return size + 2
	}
	return 1
}

func tagStart(b byte) bool {
	return b == '/' || b == '!' || b == '?' || ('a' <= b && b <= 'z') || ('A' <= b && b <= 'Z')
}

func isDirective(text string) bool {
	return strings.Contains(text, "LINT.IfChange") || strings.Contains(text, "LINT.ThenChange")
}

var yamlScalar = regexp.MustCompile(`(?:[:\-][ \t]+|^[ \t]*)(?:[|>][+-]?[1-9]?|[|>][1-9]?[+-]?)[ \t]*(?:#.*)?$`)

// commentLines returns comment text only where the entire line is a comment.
// String and block-comment state is carried between lines. This deliberately
// does not try to parse interpolation, heredocs, or embedded languages. If a
// string that never closes hides directive text, that is reported, because a
// directive the scanner cannot see guards nothing.
func commentLines(filename, content string) ([]string, []Diagnostic) {
	style, ok := styleFor(filename)
	if !ok || strings.ContainsRune(content, 0) {
		return nil, nil
	}
	lines := strings.Split(content, "\n")
	result := make([]string, len(lines))
	var blockEnd string
	var open *quote
	var openLine int
	var hidden []int
	var inTag bool
	var fence string
	scalarIndent := -1
	for n, line := range lines {
		line = strings.TrimSuffix(line, "\r")
		trimmed := strings.TrimSpace(line)
		if open != nil && isDirective(trimmed) {
			hidden = append(hidden, n+1)
		}
		if style.yaml && scalarIndent >= 0 {
			indent := len(line) - len(strings.TrimLeft(line, " \t"))
			if trimmed == "" || indent > scalarIndent {
				continue
			}
			scalarIndent = -1
		}
		scalarStart := -1
		if style.yaml {
			if match := yamlScalar.FindStringIndex(line); match != nil {
				scalarStart = match[0] + strings.IndexAny(line[match[0]:match[1]], "|>")
			}
		}
		if style.markdown && blockEnd == "" && open == nil {
			if fence != "" {
				if strings.HasPrefix(trimmed, fence) && strings.Trim(trimmed, fence[:1]) == "" {
					fence = ""
				}
				continue
			}
			if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
				fence = trimmed[:len(trimmed)-len(strings.TrimLeft(trimmed, trimmed[:1]))]
				continue
			}
		}
		standalone, continued := true, false
		for i := 0; i < len(line); {
			if blockEnd != "" {
				end := strings.Index(line[i:], blockEnd)
				if end < 0 {
					if standalone {
						result[n] = cleanBlock(line[i:])
					}
					break
				}
				if standalone && strings.TrimSpace(line[i+end+len(blockEnd):]) == "" {
					result[n] = cleanBlock(line[i : i+end])
				}
				i += end + len(blockEnd)
				blockEnd = ""
				standalone = false
				continue
			}
			if open != nil {
				standalone = false
				if !open.raw && line[i] == '\\' {
					continued = i+1 == len(line)
					i += 2
				} else if strings.HasPrefix(line[i:], open.delim) {
					i += len(open.delim)
					open, hidden = nil, nil
				} else {
					i++
				}
				continue
			}
			if line[i] == ' ' || line[i] == '\t' {
				i++
				continue
			}
			matched := false
			for _, pair := range style.blocks {
				if strings.HasPrefix(line[i:], pair[0]) {
					blockEnd = pair[1]
					i += len(pair[0])
					matched = true
					break
				}
			}
			if matched {
				continue
			}
			for _, leader := range style.lines {
				isComment := strings.HasPrefix(line[i:], leader)
				if leader == "REM" {
					isComment = len(line[i:]) >= 3 && strings.EqualFold(line[i:i+3], "REM") && (len(line[i:]) == 3 || line[i+3] == ' ' || line[i+3] == '\t')
				}
				if isComment {
					if standalone {
						result[n] = strings.TrimSpace(line[i+len(leader):])
					}
					i = len(line)
					matched = true
					break
				}
			}
			if matched {
				break
			}
			standalone = false
			// Recognize scalar headers only outside strings and comments.
			// A mapping in a sequence has its key indented past the dash;
			// sibling keys/comments at that indentation end the scalar.
			if i == scalarStart {
				scalarIndent = len(line) - len(strings.TrimLeft(line, " \t"))
				prefix := strings.TrimSpace(line[:i])
				if strings.HasSuffix(prefix, ":") {
					for strings.HasPrefix(prefix, "- ") || strings.HasPrefix(prefix, "-\t") {
						key := strings.TrimLeft(prefix[1:], " \t")
						scalarIndent += len(prefix) - len(key)
						prefix = key
					}
				}
			}
			if style.markup && line[i] == '<' && i+1 < len(line) && tagStart(line[i+1]) {
				inTag = true
			}
			if style.markup && line[i] == '>' {
				inTag = false
			}
			if style.escapes && line[i] == '\\' {
				i += 2
			} else if style.rust && line[i] == '\'' {
				i += rustChar(line[i:])
			} else if q := style.opens(line, i, inTag); q != nil {
				open, openLine = q, n+1
				i += len(q.delim)
			} else {
				i++
			}
		}
		if open != nil && !open.multiline && !continued {
			open, hidden = nil, nil
		}
	}
	if open != nil && len(hidden) > 0 {
		return result, []Diagnostic{{filename, hidden[0], fmt.Sprintf("LINT directive ignored: the comment scanner reads it as part of a string that starts at line %d and never closes", openLine)}}
	}
	return result, nil
}

func cleanBlock(s string) string {
	s = strings.TrimSpace(s)
	return strings.TrimSpace(strings.TrimPrefix(s, "*"))
}
