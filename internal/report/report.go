package report

import (
	"fmt"
	"html"
	"io"
	"regexp"
	"strings"

	"ifttt/internal/lint"
)

var waiverPattern = regexp.MustCompile(`(?:^|[^A-Za-z0-9_])NO_IFTTT(?:\(([^()\r\n]*)\))?=([^\r\n]*)`)
var htmlComment = regexp.MustCompile(`(?s)<!--.*?(?:-->|$)`)
var placeholder = regexp.MustCompile(`^<[^<>]*>`)

// Waiver is a NO_IFTTT(<scope>)=<reason> line in the PR description. The
// scope is a changed region's label, or <path>:<label> when a label is used
// by unrelated pairs.
type Waiver struct {
	Scope  string
	Reason string
}

// Waivers returns the scoped waivers in a PR description, and whether it has
// an unscoped NO_IFTTT=<reason>, which waives nothing. Text in HTML comments,
// which GitHub hides (for example, a PR template explaining waivers), and
// placeholders such as NO_IFTTT(<label>)=<reason> do not count.
func Waivers(body string) (waivers []Waiver, unscoped bool) {
	// Hidden text must not join visible tokens or move a reason onto the
	// waiver's line. Keep line boundaries and a space between fragments.
	body = htmlComment.ReplaceAllStringFunc(body, func(comment string) string {
		return " " + strings.Repeat("\n", strings.Count(comment, "\n")) + strings.Repeat("\r", strings.Count(comment, "\r"))
	})
	for _, m := range waiverPattern.FindAllStringSubmatch(body, -1) {
		scope, reason := strings.TrimSpace(m[1]), strings.TrimSpace(m[2])
		if reason == "" || placeholder.MatchString(reason) || placeholder.MatchString(scope) {
			continue
		}
		if scope == "" {
			unscoped = true
		} else {
			waivers = append(waivers, Waiver{scope, reason})
		}
	}
	return waivers, unscoped
}

// Report is a check result with the PR's waivers applied.
type Report struct {
	Result lint.Result
	// WaivedBy holds, for each finding, the waiver that covers it, or the
	// zero Waiver.
	WaivedBy []Waiver
	// Unused waivers cover no finding; each would waive a later one.
	Unused   []Waiver
	Unscoped bool
}

func covers(w Waiver, f lint.Finding) bool {
	return f.Label != "" && (w.Scope == f.Label || w.Scope == f.Path+":"+f.Label)
}

func Apply(result lint.Result, waivers []Waiver, unscoped bool) Report {
	r := Report{Result: result, WaivedBy: make([]Waiver, len(result.Findings)), Unscoped: unscoped}
	used := make([]bool, len(waivers))
	for i, f := range result.Findings {
		for j, w := range waivers {
			if covers(w, f) {
				used[j] = true
				if r.WaivedBy[i] == (Waiver{}) {
					r.WaivedBy[i] = w
				}
			}
		}
	}
	for j, w := range waivers {
		if !used[j] {
			r.Unused = append(r.Unused, w)
		}
	}
	return r
}

func (r Report) waived() int {
	n := 0
	for _, w := range r.WaivedBy {
		if w != (Waiver{}) {
			n++
		}
	}
	return n
}

func ExitCode(r Report, fail bool) int {
	if len(r.Result.Errors) > 0 {
		return 2
	}
	if r.waived() < len(r.Result.Findings) && fail {
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

func unusedMessage(w Waiver) string {
	return "NO_IFTTT(" + w.Scope + ") matches no coupling finding; remove it from the PR description."
}

// Write never prints a waiver's reason: it is raw PR text.
func Write(w io.Writer, r Report, github bool) error {
	var out strings.Builder
	emit := func(level, filename string, line int, message string) {
		if github {
			out.WriteString(annotation(level, filename, line, message))
		} else {
			fmt.Fprintln(&out, message)
		}
	}
	for _, d := range r.Result.Errors {
		if github {
			out.WriteString(annotation("error", d.Path, d.Line, d.Message))
		} else {
			fmt.Fprintf(&out, "ERROR: %s\n", d)
		}
	}
	for i, f := range r.Result.Findings {
		filename := f.Path
		if f.Deleted {
			filename = ""
		}
		// Waived findings stay listed, as notices, so a reviewer sees what
		// each waiver accepts.
		if by := r.WaivedBy[i]; by != (Waiver{}) {
			emit("notice", filename, f.Line, "IFTTT (waived by NO_IFTTT("+by.Scope+")): "+f.String()+".")
		} else if f.Label != "" {
			emit("warning", filename, f.Line, "IFTTT: "+f.String()+". If no update is needed, add NO_IFTTT("+f.Label+")=<reason> to the PR description.")
		} else {
			emit("warning", filename, f.Line, "IFTTT: "+f.String()+". If no update is needed, label the region, LINT.IfChange(<label>), and add NO_IFTTT(<label>)=<reason> to the PR description.")
		}
	}
	for _, u := range r.Unused {
		emit("warning", "", 0, "IFTTT: "+unusedMessage(u))
	}
	if r.Unscoped {
		emit("warning", "", 0, "IFTTT: an unscoped NO_IFTTT=<reason> waives nothing; name the region's label: NO_IFTTT(<label>)=<reason>.")
	}
	if n := r.waived(); n > 0 {
		fmt.Fprintf(&out, "IFTTT: %d coupling finding(s) waived.\n", n)
	}
	if len(r.Result.Findings) == 0 && len(r.Result.Errors) == 0 {
		out.WriteString("IFTTT: no findings.\n")
	}
	_, err := io.WriteString(w, out.String())
	return err
}

func markdown(s string) string {
	s = html.EscapeString(s)
	return strings.NewReplacer("|", "&#124;", "`", "&#96;", "\\", "&#92;", "\r", " ", "\n", " ").Replace(s)
}

func Summary(w io.Writer, r Report) error {
	var out strings.Builder
	out.WriteString("## IFTTT\n\n")
	if len(r.Result.Errors) > 0 {
		out.WriteString("### Annotation errors\n\n| Location | Error |\n| --- | --- |\n")
		for _, d := range r.Result.Errors {
			fmt.Fprintf(&out, "| %s:%d | %s |\n", markdown(d.Path), d.Line, markdown(d.Message))
		}
		out.WriteString("\n")
	}
	if len(r.Result.Findings) > 0 {
		out.WriteString("| Changed region | Missing counterpart | Waived by |\n| --- | --- | --- |\n")
		for i, f := range r.Result.Findings {
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
			by := ""
			if w := r.WaivedBy[i]; w != (Waiver{}) {
				by = "NO_IFTTT(" + w.Scope + "): " + w.Reason
			}
			fmt.Fprintf(&out, "| %s | %s | %s |\n", markdown(location), markdown(f.Target.String()), markdown(by))
		}
		if r.waived() < len(r.Result.Findings) {
			out.WriteString("\nIf no update is needed, add `NO_IFTTT(<label>)=<reason>` to the PR description; an unlabelled region needs a label first.\n")
		}
	} else if len(r.Result.Errors) == 0 {
		out.WriteString("No findings.\n")
	}
	for _, u := range r.Unused {
		out.WriteString("\n" + markdown(unusedMessage(u)) + "\n")
	}
	if r.Unscoped {
		out.WriteString("\nAn unscoped `NO_IFTTT=<reason>` waives nothing; name the region's label: `NO_IFTTT(<label>)=<reason>`.\n")
	}
	_, err := io.WriteString(w, out.String())
	return err
}
