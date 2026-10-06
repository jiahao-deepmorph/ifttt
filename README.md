# IFTTT Lint

Declare related code regions with `LINT.IfChange` / `LINT.ThenChange` comments.
IFTTT warns when a pull request changes a guarded region without changing its
declared counterparts. It checks whether both sides were touched, not whether
the changes are correct.

```go
// LINT.IfChange(colors)
const Red, Green, Blue = 0, 1, 2
// LINT.ThenChange(ui/color_names.py:colors, docs/colors.md)
```

```python
# LINT.IfChange(colors)
COLORS = ["red", "green", "blue"]
# LINT.ThenChange(model/color.go:colors)
```

Paths are relative to the repository root. A leading `//` is optional. Use
`:label` for another region in the same file, `path:label` for a region in another
file, or `path` for a whole file. Links are directional; declare both directions
for a two-way coupling. `LINT.ThenChange()` makes a target-only region.

## GitHub Action

After the first release is published, add this workflow to your repository:

```yaml
name: ifttt
on:
  pull_request:
    types: [opened, synchronize, reopened, edited]
permissions:
  contents: read
jobs:
  ifttt:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
        with:
          fetch-depth: 0
      - uses: jiahao-deepmorph/ifttt@v1
        with:
          fail-on-findings: true # optional; defaults to warnings only
```

The checker reads the PR's base and head SHAs from the event and compares their
merge base to the head. This works with checkout's default merge commit because
file contents come from Git objects, not the working tree. Full history must be
available. See GitHub's [pull request event documentation](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#pull_request).

| Input | Default | Behavior |
| --- | --- | --- |
| `fail-on-findings` | `false` | Exit 1 when coupling findings remain |
| `base-ref` | PR base SHA | Override the ref used to compute the merge base |

Findings appear as workflow warnings and in the job summary. GitHub can display
annotations inline when they map to the PR diff. Removed files and deletion-only
changes use a general or file annotation plus the summary; they have no added
line to annotate. No review comments or write token are needed.

Put `NO_IFTTT=<reason>` in the PR description to suppress coupling warnings. The
reason must contain non-whitespace text on the same line. This suppresses all
coupling findings for that PR but **never annotation errors**. The `edited` event
in the example starts a fresh run with the updated description; rerunning an old
event is not a way to refresh its body. The reason is recorded in the summary.

## Build and run

Requires Go 1.23+ and Git. The Go binary uses only the standard library.

```sh
go build -o bin/ifttt ./cmd/ifttt
./bin/ifttt check --base main --fail-on-findings
./bin/ifttt check --base main --head my-branch
```

This checks **committed changes**; staged and unstaged edits are excluded. With a
PR event available, base, head, and override default from `GITHUB_EVENT_PATH`.
Use `--event=` to disable event loading for an explicit local check.

Additional flags: `--repo <directory>`, `--github` (workflow annotations),
`--summary <file>` (append Markdown), and `--event <file>` (PR event JSON).
GitHub output defaults on when `GITHUB_ACTIONS=true`; the summary defaults to
`GITHUB_STEP_SUMMARY`. Run `ifttt check --help` for details.

| Exit | Meaning |
| --- | --- |
| 0 | Clean, warnings only, or overridden |
| 1 | Coupling findings with `--fail-on-findings` |
| 2 | Invalid annotations, including missing targets |
| 3 | Invalid invocation, Git failure, event/output failure, or Action setup failure |

## Exact checking rules

- Bodies exclude both markers and all lines of a wrapped `ThenChange` list.
  Whitespace and comment edits in a body count. Marker-only edits do not.
- Existing regions match first by label, then by surviving marker or body lines
  mapped through the diff. Regions do not nest; labels are unique within a file
  and match `[A-Za-z0-9_.-]+`.
- Unmatched new regions do not trigger. Unmatched old regions trigger if body
  lines were removed. Removing only markers leaves the body unchanged and does
  not trigger. Empty regions have no body to trigger.
- When an existing body's content changes, the union of its old and new target
  lists applies. Removing a dependency in the same edit cannot erase an old
  obligation; use the PR override if the counterpart really needs no edit.
- A labeled target is satisfied by body additions at head or body removals at
  the merge base. Whole-file targets accept any Git change, including mode-only
  changes and deletion. Deleting an entire target file satisfies its obligation.
- Head annotations in every changed file are validated, even for new regions or
  marker-only edits. Referenced labeled targets are parsed too, but their outgoing
  links are not followed. Historical syntax errors do not prevent a PR from
  repairing or deleting an annotation.
- Deletion can satisfy a coupling while a surviving link in a changed source is
  still an error. References in **untouched source files are not validated**.
  Repository-wide broken-link detection is outside v1.
- Renames are delete-plus-add. Files are matched by exact, case-sensitive Git
  paths. Targets must be tracked regular files; symlinks, directories, and
  submodules are not targets. `..`, absolute paths, backslashes, control
  characters, commas, parentheses, and colons outside the label separator are
  rejected in target paths. Spaces in filenames are supported.

Target lists may wrap across consecutive comment lines:

```go
// LINT.IfChange(colors)
const Red, Green, Blue = 0, 1, 2
// LINT.ThenChange(
//   ui/color_names.py:colors,
//   docs/colors.md
// )
```

## Comment scanner

Directives must occupy a standalone comment line. Inline comments following code
are ignored. The extension table recognizes these comment families:

| Comment | Extensions / filenames |
| --- | --- |
| `//`, `/* … */` | Go, C/C++, Java, Kotlin, Swift, Rust, C#, JavaScript/TypeScript, Proto, Dart, CSS/SCSS |
| `#` | Python, Starlark, TOML, shell, Ruby, YAML, R, Perl; `Makefile`, `Dockerfile`, `BUILD`, `WORKSPACE`, `.gitignore` |
| `--` | SQL, Lua (also Lua `--[[ … ]]`) |
| `;`, `#` | INI/config, Emacs Lisp, Lisp, Clojure |
| `%` | TeX, `.m`, Erlang |
| `'`, `REM` | Visual Basic |
| `<!-- … -->` | HTML, XML, Markdown, SVG, Vue, Svelte |

The exact extension table is in [comments.go](internal/lint/comments.go).
Unknown extensions and files containing NUL bytes are ignored as annotation
sources; they can still be whole-file targets.

The lightweight lexer tracks ordinary quotes, backtick strings, common triple
quotes, and block comments across lines. It skips directives inside those
strings, Markdown fences, and YAML block scalars. It is not a full language
parser: heredocs, custom raw-string delimiters, regex literals, interpolation,
and embedded languages are not fully modeled.
Use ordinary standalone comments outside those constructs. This limitation is
explicit rather than a guarantee of perfect string detection in every language.

## Development and releases

```sh
go test -race ./...
go vet ./...
python3 -m unittest discover -s scripts -p 'test_*.py' -v
```

Tests construct temporary Git repositories and exercise real diffs, the CLI,
workflow output, and the Action launcher with fixture release assets. CI tests
Linux, macOS, Windows, and the minimum Go version. Launcher platform selection is
also tested without downloading releases.

Release procedure:

1. Set `action-version` to the intended `v1.x.y` and commit it with the release.
2. Push that exact tag. The release workflow verifies the embedded version,
   tests the project, and uses GoReleaser to publish archives and `checksums.txt`.
3. After publishing, the workflow moves `v1` to that release commit.

The Action reads its own `action-version` file. It never resolves a floating tag
to a separate “latest” release. Thus `@v1`, `@v1.x.y`, and a release commit SHA
all download the binary associated with the checked-out Action code. Untagged
development commits should be tested by building from source. Using the Action
requires a published release.

Release platforms: Linux amd64/arm64, macOS arm64, Windows amd64. The composite
Action requires Bash, Git, curl, tar, and `sha256sum` or `shasum`; these are
available on the corresponding GitHub-hosted runners. Assets are downloaded
over HTTPS and verified before extraction. Release downloads currently require
the Action repository and assets to be publicly accessible.

See [spec.md](spec.md) for the design and v1 scope.
