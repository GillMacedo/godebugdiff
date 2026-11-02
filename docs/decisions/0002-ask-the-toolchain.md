# 0002: Ask the installed toolchain, don't ship a table

**Decision:** defaults come from `go list -modfile` on temporary go.mod copies, and package names and descriptions from the toolchain's own files in `$GOROOT`. The tool has no list of settings of its own.

**Why:** a shipped table goes stale with every Go release and can't apply `godebug` lines and `//go:debug` directives the way the go command does. `go list` reports `DefaultGODEBUG` for each main package using the same code as `go build`, so the answer matches the binaries you'd build with that toolchain.

**Costs:** `--to` can't be newer than the installed toolchain, and settings the toolchain removed don't appear. Reading `internal/godebugs/table.go` depends on an internal file. If it changes shape, only package names are lost, and a unit test parses the installed copy so CI notices.
