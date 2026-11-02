---
name: checking-go-version-bumps
description: Lists the GODEBUG behavior defaults that change when the go line in go.mod is raised (for example go 1.21 to go 1.24), which binaries import each affected package, and the godebug lines that keep the old behavior. Use when the user wants to bump the go directive or Go version in go.mod, reviews a PR or Dependabot/Renovate change that raises it, asks what breaks or changes when upgrading Go, or sees a behavior change (ServeMux routing, TLS, x509, GOMAXPROCS) after a Go upgrade.
---

# Checking a Go version bump

Raising the `go` line in go.mod changes more than the language version. Each main package gets the GODEBUG defaults of the new version, so behavior in `net/http`, `crypto/tls`, `crypto/x509`, `net/url`, `runtime` and others can change with no code change. The bundled tool asks the installed Go toolchain which defaults change and which binaries import the affected packages. It needs Go installed, reads go.mod without editing it, never builds or runs the user's code, and never uses the network.

## Run the tool

From the module root, or pass its directory:

    go run "${CLAUDE_PLUGIN_ROOT}/scripts/godebugdiff.go" --to 1.24 .

- `--to` defaults to the installed toolchain's version and can't be newer than it. If the user wants a newer target, say they need to install that Go version first.
- `--from` defaults to the current go line. To review a PR that already bumped it, pass the base branch's version: `--from 1.21`.
- Add `--json` for structured output, and `--tests` to include test binaries. A module with no main package is checked through its test binaries automatically.
- If it says modules aren't in the module cache, ask before running `go mod download`, since that uses the network.

## Check each change that reaches a binary

The tool lists settings by the package that reads them. Importing a package doesn't mean the changed code path is used, so check:

1. Read the doc sentence and link for the setting. Then search the code for the API it names, such as `http.NewServeMux` and `HandleFunc` patterns for `httpmuxgo121`, `tls.Config` for TLS settings, `x509.ParseCertificate` or `CreateCertificate` for x509 settings, and `rand.Seed` for `randseednop`.
2. Say for each setting whether the code uses that path: used, not used, or can't tell (for example a dependency calls it).
3. `runtime` settings such as `containermaxprocs` and `updatemaxprocs` apply to every binary. Point out ones that matter for how it's deployed, like containers with CPU limits.
4. Settings starting with `win` only matter on Windows. Say so if the project doesn't ship there.
5. Settings under "Changes the go command" affect `go test -json` or builds in CI, not the program.

## Report

1. A short table: setting, what changes, which binaries, and whether the code uses that path.
2. Recommend either taking the new default or pinning the old one. Only suggest pinning for settings the code really depends on, and use the tool's `godebug ( ... )` block. Pins are meant to be temporary: say that they're removed in later Go releases.
3. Pass on the tool's notes, such as the Go 1.22 loop variable change, or that a library's go line doesn't set GODEBUG for the programs that import it.

## Don't

- Don't edit go.mod unless the user asks.
- Don't say a bump is safe because nothing is listed. The tool only covers GODEBUG defaults. Release notes list other changes.
- Don't run the user's tests or binaries to check behavior unless they ask.
