package lint

import "fmt"

// Target names either an entire repository-relative file or one labeled region.
type Target struct {
	Path  string
	Label string
}

func (t Target) String() string {
	if t.Label != "" {
		return t.Path + ":" + t.Label
	}
	return t.Path
}

// Region excludes both marker lines and all ThenChange continuation lines.
type Region struct {
	Label   string
	Open    int
	Close   int
	End     int
	Targets []Target
}

type Diagnostic struct {
	Path    string
	Line    int
	Message string
}

func (d Diagnostic) String() string {
	return fmt.Sprintf("%s:%d: %s", d.Path, d.Line, d.Message)
}

type Finding struct {
	Path      string
	Label     string
	Start     int
	End       int
	Line      int // Head-side changed line, or zero when only an old-side location exists.
	Target    Target
	Deleted   bool
	BaseLines bool // Start and End refer to the merge-base version.
}

func (f Finding) String() string {
	label := ""
	if f.Label != "" {
		label = " (" + f.Label + ")"
	}
	if f.BaseLines {
		label += " [base lines]"
	}
	return fmt.Sprintf("%s:%d-%d%s changed, but %s was not", f.Path, f.Start, f.End, label, f.Target)
}

type Result struct {
	Findings []Finding
	Errors   []Diagnostic
}
