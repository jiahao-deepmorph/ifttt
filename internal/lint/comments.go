package lint

import (
	"path"
	"regexp"
	"strings"
)

type commentStyle struct {
	lines    []string
	blocks   [][2]string
	triple   bool
	raw      bool
	markup   bool
	markdown bool
	yaml     bool
}

func styleFor(filename string) (commentStyle, bool) {
	c := commentStyle{lines: []string{"//"}, blocks: [][2]string{{"/*", "*/"}}}
	switch strings.ToLower(path.Ext(filename)) {
	case ".go":
		c.raw = true
		return c, true
	case ".c", ".h", ".cc", ".cpp", ".cxx", ".hpp", ".java", ".kt", ".kts", ".swift", ".rs", ".cs", ".js", ".jsx", ".ts", ".tsx", ".proto", ".dart":
		c.triple = true
		return c, true
	case ".py", ".pyi", ".bzl", ".bazel", ".toml":
		return commentStyle{lines: []string{"#"}, triple: true}, true
	case ".yml", ".yaml":
		return commentStyle{lines: []string{"#"}, yaml: true}, true
	case ".sh", ".bash", ".zsh", ".rb", ".r", ".pl":
		return commentStyle{lines: []string{"#"}}, true
	case ".sql", ".lua":
		return commentStyle{lines: []string{"--"}, blocks: [][2]string{{"/*", "*/"}, {"--[[", "]]"}}}, true
	case ".ini", ".cfg", ".conf", ".el", ".lisp", ".clj":
		return commentStyle{lines: []string{";", "#"}}, true
	case ".tex", ".m", ".erl":
		return commentStyle{lines: []string{"%"}}, true
	case ".vb", ".vbs", ".bas":
		return commentStyle{lines: []string{"'", "REM"}}, true
	case ".md", ".mdx":
		return commentStyle{blocks: [][2]string{{"<!--", "-->"}}, markup: true, markdown: true}, true
	case ".html", ".htm", ".xml", ".svg", ".vue", ".svelte":
		return commentStyle{blocks: [][2]string{{"<!--", "-->"}}, markup: true}, true
	case ".css", ".scss":
		return c, true
	}
	switch path.Base(filename) {
	case "Makefile", "Dockerfile", "BUILD", "WORKSPACE", ".gitignore":
		return commentStyle{lines: []string{"#"}}, true
	}
	return commentStyle{}, false
}

var yamlScalar = regexp.MustCompile(`(?:[:\-][ \t]+|^[ \t]*)(?:[|>][+-]?[1-9]?|[|>][1-9]?[+-]?)[ \t]*(?:#.*)?$`)

// commentLines returns comment text only where the entire line is a comment.
// String and block-comment state is carried between lines. This deliberately
// does not try to parse interpolation, heredocs, or embedded languages.
func commentLines(filename, content string) []string {
	style, ok := styleFor(filename)
	if !ok || strings.ContainsRune(content, 0) {
		return nil
	}
	lines := strings.Split(content, "\n")
	result := make([]string, len(lines))
	var blockEnd, quote string
	var raw, inTag bool
	var fence string
	scalarIndent := -1
	for n, line := range lines {
		line = strings.TrimSuffix(line, "\r")
		trimmed := strings.TrimSpace(line)
		if style.yaml && scalarIndent >= 0 {
			indent := len(line) - len(strings.TrimLeft(line, " \t"))
			if trimmed == "" || indent > scalarIndent {
				continue
			}
			scalarIndent = -1
		}
		if style.yaml && quote == "" && !strings.HasPrefix(trimmed, "#") && yamlScalar.MatchString(line) {
			scalarIndent = len(line) - len(strings.TrimLeft(line, " \t"))
		}
		if style.markdown && blockEnd == "" && quote == "" {
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
		standalone := true
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
			if quote != "" {
				standalone = false
				if !raw && line[i] == '\\' {
					i += 2
				} else if strings.HasPrefix(line[i:], quote) {
					i += len(quote)
					quote = ""
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
			if style.markup && line[i] == '<' {
				inTag = true
			}
			if style.markup && line[i] == '>' {
				inTag = false
			}
			isQuote := (!style.markup || inTag) && (line[i] == '\'' || line[i] == '"')
			if isQuote || (line[i] == '`' && (!style.markup || style.markdown)) {
				quote = string(line[i])
				raw = style.raw && line[i] == '`'
				if style.triple && strings.HasPrefix(line[i:], strings.Repeat(quote, 3)) {
					quote = strings.Repeat(quote, 3)
				}
				i += len(quote)
			} else {
				i++
			}
		}
	}
	return result
}

func cleanBlock(s string) string {
	s = strings.TrimSpace(s)
	return strings.TrimSpace(strings.TrimPrefix(s, "*"))
}
