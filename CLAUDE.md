# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

sysdtui is a Go TUI (Bubble Tea + Lip Gloss) for editing systemd unit files, with each directive's documentation (parsed from systemd's man pages) shown beside it. README.md describes user-facing behavior.

## Commands

```sh
make build                    # ./bin/sysdtui
make test                     # go test ./...
make lint                     # gofmt -l must print nothing, then go vet (CI runs this)
make catalog                  # regenerate internal/catalog/systemd.json.gz from /usr/share/man/man5
go test ./internal/catalog -run TestEmbedded  # a single test
bin/sysdtui --dump-catalog Service            # inspect what the parser produced
bin/sysdtui --lint /usr/lib/systemd/system/*.service  # find false positives on real units
SYSDTUI_UNIT_PATH=/tmp/etc:/tmp/vendor bin/sysdtui     # try it without touching /etc
```

## Architecture

Data flows: **man pages → `catalog.Catalog`**, **unit file text → `unitfile.File` → rows in the editor → text**.

- `internal/catalog`: directives per section. `troff.go` reads the man pages' troff source directly (no groff needed): a directive is `.PP`, tag line(s) like `\fIBefore=\fR, \fIAfter=\fR`, then an `.RS 4 … .RE` body, rendered to `[]Para`. `catalog.go` maps pages to sections (`Pages`; `systemd.unit` is split by its `[UNIT]`/`[INSTALL]` headings), infers `Kind`/`Values`/`Multi`/`Resets` from the wording, and loads the installed pages or the embedded `systemd.json.gz`. `overrides.go` corrects inference for common directives. **Run `make catalog` (or `go run ./cmd/gencatalog -man DIR`) after changing parsing or inference**, or the embedded catalog goes stale.
- `internal/unitfile`: a lossless parser. Unchanged lines keep their raw text (`Line.raw`); `Set` clears it so the line is re-rendered. Continuations and comments inside them are handled. `Values` applies systemd's empty-assignment reset.
- `internal/lint`: checks against the catalog. Drop-in checks need `Input.Base`, the merged unit being overridden.
- `internal/systemd`: search paths (`Paths`, overridable with `SYSDTUI_UNIT_PATH`), discovery, drop-in resolution, templates, `systemctl`/`sudo` command building, `Write` (atomic, with `~` backup), and `Verify` (systemd-analyze on a temp copy).
- `internal/ui`: one Bubble Tea `Model` with modes `modeBrowse`, `modeEdit`, `modePick` (directive/section picker), `modeNew`, `modeSave`. `editor.go` holds the file being edited and builds `rows` (sections, entries, inherited values for drop-ins); `edit_update.go` handles keys; `view.go` renders. Commands that need the terminal (sudo, status, journal) run through `tea.ExecProcess` with a script from `systemd.Script`.
- `cmd/sysdtui`: flags, `--lint`, `--list`, `--dump-catalog`. `cmd/gencatalog`: builds the embedded catalog.

## Rules

- Lint must not flag stock units. Before and after a change to `lint` or the catalog, run `--lint` over `/usr/lib/systemd/system` and compare.
- The editor must never rewrite lines the user did not touch: keep `TestRoundTrip` passing.
- User-visible flags or keys appear in README.md, `docs/sysdtui.1` and the footer hints in `view.go`. Update them together.
- Releases: set `version` in `cmd/sysdtui/main.go` and add a section to `RELEASE_NOTES.md`.
