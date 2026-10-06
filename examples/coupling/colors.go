package coupling

type Color string

// LINT.IfChange(colors)
const (
	Red   Color = "red"
	Green Color = "green"
	Blue  Color = "blue"
)

// LINT.ThenChange(examples/coupling/color_names.py:colors)
