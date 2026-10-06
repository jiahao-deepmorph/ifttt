# Coupling demonstration

`colors.go` and `color_names.py` declare a two-way coupling. Both files contain
the same three colors on the `demo/coupling-base` branch.

The sample PR targets that branch and adds a color only to the Go file. IFTTT
reports that the Python `colors` region was not updated, and the dedicated
`IFTTT coupling demo` check intentionally fails.

This baseline is necessary because adding a brand-new guarded region in a PR
does not trigger a coupling warning. The checker must compare an existing
region against its changed version.

To make the sample pass, add the new color to `COLOR_NAMES` as well. The demo
branches are separate from `main` and do not require a published Action release.
