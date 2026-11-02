# Architecture

    go.mod --> two temporary copies, with the go line set by `go mod edit -go`
    each copy --> `go list -e -modfile=COPY -json ./...` with GOPROXY=off, GOTOOLCHAIN=local, GOWORK=off
             --> main packages (or test binaries) with DefaultGODEBUG and Deps
    compare --> settings whose value differs, per binary --> one Change per setting, with the binaries that import its package
    $GOROOT/src/internal/godebugs/table.go --> package and version for each setting
    $GOROOT/doc/godebug.md --> first sentence of the paragraph that names the setting, and a link to its section
    Print / JSON --> report, godebug block to pin old values, notes

- **Source of truth:** `go list` computes `DefaultGODEBUG` with the same code `go build` uses. It applies the go line, `godebug` lines in go.mod and `//go:debug` directives. The integration tests check the result against `go version -m` on built binaries.
- **Reach:** a setting reaches a binary when its package, from the toolchain's table, is in the binary's `Deps`. Settings whose package starts with `cmd/` control the go command and are listed separately.
- **Libraries:** a module with no main package is checked through `go list -test`, which gives each test binary's defaults.
- **Errors:** if a binary's imports don't load, results would be incomplete, so the tool stops with exit code 2. If the new go line needs `go mod tidy` changes, which happens when crossing Go 1.17, it retries on a fresh copy with `-mod=mod`, using only the module cache.
- **Reading GOROOT:** `table.go` is an internal file and could change shape. If it can't be parsed, changes are still listed, without package names, so they can't be marked as reaching a binary.

Key choices: one standard-library Go file run with `go run`, and the toolchain as the only data source. See `decisions/`.
