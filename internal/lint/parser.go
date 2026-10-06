package lint

import (
	"fmt"
	"path"
	"regexp"
	"strings"
	"unicode"
)

var labelPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
var openPattern = regexp.MustCompile(`^LINT\.IfChange(?:\(([A-Za-z0-9_.-]+)\))?$`)

// Parse reads standalone comment directives. Unsupported extensions are ignored.
// The scanner is intentionally a small lexer, not a language parser; see README.
func Parse(filename, content string) ([]Region, []Diagnostic) {
	comments := commentLines(filename, content)
	var regions []Region
	var diagnostics []Diagnostic
	var active *Region
	labels := map[string]bool{}
	report := func(line int, message string) {
		diagnostics = append(diagnostics, Diagnostic{filename, line, message})
	}
	for i := 0; i < len(comments); i++ {
		text := strings.TrimSpace(comments[i])
		line := i + 1
		if strings.HasPrefix(text, "LINT.IfChange") {
			m := openPattern.FindStringSubmatch(text)
			if m == nil {
				report(line, "invalid IfChange syntax")
				continue
			}
			if active != nil {
				report(line, "nested IfChange region")
				continue
			}
			if m[1] != "" {
				if labels[m[1]] {
					report(line, "duplicate label "+m[1])
				}
				labels[m[1]] = true
			}
			active = &Region{Label: m[1], Open: line}
		} else if strings.HasPrefix(text, "LINT.ThenChange") {
			if !strings.HasPrefix(text, "LINT.ThenChange(") {
				report(line, "invalid ThenChange syntax")
				continue
			}
			for !strings.Contains(text, ")") && i+1 < len(comments) {
				next := strings.TrimSpace(comments[i+1])
				if next == "" || strings.HasPrefix(next, "LINT.") {
					break
				}
				i++
				text += " " + next
			}
			if !strings.HasSuffix(text, ")") || strings.Count(text, ")") != 1 {
				report(line, "unterminated or invalid ThenChange target list")
				continue
			}
			if active == nil {
				report(line, "orphan ThenChange without IfChange")
				continue
			}
			active.Close, active.End = line, i+1
			list := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(text, "LINT.ThenChange("), ")"))
			seen := map[Target]bool{}
			if list != "" {
				for _, item := range strings.Split(list, ",") {
					target, err := parseTarget(filename, strings.TrimSpace(item))
					if err != nil {
						report(line, err.Error())
					} else if !seen[target] {
						active.Targets = append(active.Targets, target)
						seen[target] = true
					}
				}
			}
			regions = append(regions, *active)
			active = nil
		}
	}
	if active != nil {
		report(active.Open, "unclosed IfChange region")
	}
	return regions, diagnostics
}

func parseTarget(source, value string) (Target, error) {
	invalid := func() (Target, error) { return Target{}, fmt.Errorf("invalid target %q", value) }
	if value == "" || strings.ContainsAny(value, "\\(),") || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return invalid()
	}
	parts := strings.Split(value, ":")
	if len(parts) > 2 {
		return invalid()
	}
	t := Target{Path: strings.TrimPrefix(parts[0], "//")}
	if len(parts) == 2 {
		t.Label = parts[1]
		if !labelPattern.MatchString(t.Label) {
			return invalid()
		}
	}
	if t.Path == "" && t.Label != "" {
		t.Path = source
	}
	if t.Path == "" || strings.HasPrefix(t.Path, "/") {
		return invalid()
	}
	for _, part := range strings.Split(t.Path, "/") {
		if part == ".." {
			return invalid()
		}
	}
	t.Path = path.Clean(t.Path)
	if t.Path == "." {
		return invalid()
	}
	return t, nil
}
