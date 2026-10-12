package report

import (
	"fmt"
	"html"
	"io"
	"regexp"
	"strings"

	"ifttt/internal/lint"
)

var overridePattern = regexp.MustCompile(`(?:^|[^A-Za-z0-9_])NO_IFTTT=([^\r\n]*)`)
var htmlComment = regexp.MustCompile(`(?s)<!--.*?(?:-->|$)`)
var placeholder = regexp.MustCompile(`^<[^<>]*>`)

// Override returns the first real reason. Text in HTML comments, which GitHub
// hides (for example, a PR template explaining the override), and placeholders
// such as NO_IFTTT=<reason> do not count.
func Override(body string) string {
	// Hidden text must not join visible tokens or move a reason onto the
	// override's line. Keep line boundaries and a space between fragments.
	body = htmlComment.ReplaceAllStringFunc(body, func(comment string) string {
		return " " + strings.Repeat("\n", strings.Count(comment, "\n")) + strings.Repeat("\r", strings.Count(comment, "\r"))
	})
	for _, m := range overridePattern.FindAllStringSubmatch(body, -1) {
		if reason := strings.TrimSpace(m[1]); reason != "" && !placeholder.MatchString(reason) {
			return reason
		}
	}
	return ""
}

func ExitCode(result lint.Result, reason string, fail bool) int {
	if len(result.Errors) > 0 {
		return 2
	}
	if len(result.Findings) > 0 && reason == "" && fail {
		return 1
	}
	return 0
}

func escape(s string, property bool) string {
	s = strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A").Replace(s)
	if property {
		s = strings.NewReplacer(":", "%3A", ",", "%2C").Replace(s)
	}
	return s
}

func annotation(level, filename string, line int, message string) string {
	properties := ""
	if filename != "" {
		properties = " file=" + escape(filename, true)
	}
	if line > 0 {
		properties += fmt.Sprintf(",line=%d", line)
	}
	return "::" + level + properties + "::" + escape(message, false) + "\n"
}

func Write(w io.Writer, result lint.Result, reason string, github bool) error {
	var out strings.Builder
	for _, d := range result.Errors {
		if github {
			out.WriteString(annotation("error", d.Path, d.Line, d.Message))
		} else {
			fmt.Fprintf(&out, "ERROR: %s\n", d)
		}
	}
	if reason == "" {
		for _, f := range result.Findings {
			message := "IFTTT: " + f.String() + ". If no update is needed, add NO_IFTTT=<reason> to the PR description."
			if github {
				filename := f.Path
				if f.Deleted {
					filename = ""
				}
				out.WriteString(annotation("warning", filename, f.Line, message))
			} else {
				fmt.Fprintln(&out, message)
			}
		}
	} else {
		// Never print a raw PR body into workflow command output.
		fmt.Fprintf(&out, "IFTTT: %d coupling finding(s) overridden.\n", len(result.Findings))
	}
	if len(result.Findings) == 0 && len(result.Errors) == 0 {
		out.WriteString("IFTTT: no findings.\n")
	}
	_, err := io.WriteString(w, out.String())
	return err
}

func markdown(s string) string {
	s = html.EscapeString(s)
	return strings.NewReplacer("|", "&#124;", "`", "&#96;", "\\", "&#92;", "\r", " ", "\n", " ").Replace(s)
}

func Summary(w io.Writer, result lint.Result, reason string) error {
	var out strings.Builder
	out.WriteString("## IFTTT\n\n")
	if reason != "" {
		fmt.Fprintf(&out, "Coupling findings overridden: %s\n\n", markdown(reason))
	}
	if len(result.Errors) > 0 {
		out.WriteString("### Annotation errors\n\n| Location | Error |\n| --- | --- |\n")
		for _, d := range result.Errors {
			fmt.Fprintf(&out, "| %s:%d | %s |\n", markdown(d.Path), d.Line, markdown(d.Message))
		}
		out.WriteString("\n")
	}
	if reason == "" && len(result.Findings) > 0 {
		out.WriteString("| Changed region | Missing counterpart |\n| --- | --- |\n")
		for _, f := range result.Findings {
			location := fmt.Sprintf("%s:%d-%d", f.Path, f.Start, f.End)
			if f.Label != "" {
				location += " (" + f.Label + ")"
			}
			if f.Deleted {
				location += " [deleted; base lines]"
			} else if f.BaseLines {
				location += " [base lines]"
			} else if f.Line == 0 {
				location += " [deletion-only change]"
			}
			fmt.Fprintf(&out, "| %s | %s |\n", markdown(location), markdown(f.Target.String()))
		}
		out.WriteString("\nIf no update is needed, add `NO_IFTTT=<reason>` to the PR description.\n")
	} else if len(result.Errors) == 0 && reason == "" {
		out.WriteString("No findings.\n")
	}
	_, err := io.WriteString(w, out.String())
	return err
}
