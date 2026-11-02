# godebugdiff

Claude Code skill and Go CLI that lists the GODEBUG defaults that change when the go line in go.mod is raised, and which binaries import the affected packages.

## Architecture
`plugin/scripts/godebugdiff.go` holds all the logic in one standard-library file, so `go run` needs no setup. `Diff` copies go.mod and go.sum to a temporary directory for each version, `listAt` runs `go list -modfile -json` offline on each copy, `compare` diffs each binary's `DefaultGODEBUG` and checks its imports, `loadTable` and `loadDocs` read package names and descriptions from `$GOROOT`, and `Print` writes the report. `plugin/skills/checking-go-version-bumps/SKILL.md` tells Claude how to use it. See `docs/architecture.md`.

## Commands
- `make test`: gofmt, vet, unit tests and integration tests in `tests/`
- `make lint`: plugin and marketplace validation
- `make demo`: run on the example modules

## Conventions
- Standard library only. The tool runs only `go env`, `go mod edit -json` to read go.mod, `go mod edit -go` on its temporary copies, and `go list`, always with `GOPROXY=off` and `GOTOOLCHAIN=local`. Never edit the user's go.mod, never build or run their code, never use the network.
- The toolchain is the source of truth. Don't add a hand-written table of settings: read them from `go list` and `$GOROOT`.
- README examples are real output from the example modules. Regenerate them after changing the output format.

## Do and don't
- Do say "imported by", not "affected": importing a package doesn't mean the changed code path runs.
- Don't say a bump is safe because nothing is listed. Only GODEBUG defaults are covered.

## Definition of done
`make test` and `make lint` pass, and `ROADMAP.md` and docs are updated when relevant.
