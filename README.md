# godebugdiff: see what changes before you bump the go line in go.mod or go.work

[![CI](https://github.com/GillMacedo/godebugdiff/actions/workflows/ci.yml/badge.svg)](https://github.com/GillMacedo/godebugdiff/actions/workflows/ci.yml) [![MIT license](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

**A Claude Code skill and Go CLI for Go developers about to raise the `go` directive in go.mod, or the go line in go.work, for example from `go 1.21` to `go 1.24`. It lists the GODEBUG defaults that change, which of your binaries import the package each setting controls, and the `godebug` lines or `//go:debug` directives that keep the old behavior. The answers come from your installed Go toolchain, offline, without editing go.mod or go.work.**

Since Go 1.21, the `go` line in your main module's go.mod (or in go.work, in workspace mode) sets the GODEBUG defaults your binaries are built with. Raising it switches `net/http` routing, TLS key exchange, x509 parsing, `math/rand.Seed`, GOMAXPROCS in containers and more to the new behavior, with no code change and no compile error. The list is spread across many sections of the [GODEBUG history](https://go.dev/doc/godebug#history). `godebugdiff` asks the toolchain itself for each binary's defaults at both go lines and shows the difference. The skill then has Claude check whether your code uses each changed API.

## What you can ask Claude

- "I want to bump go.mod from go 1.21 to 1.24. What behavior changes?"
- "We use a go.work workspace. What changes if we raise its go line?"
- "This Dependabot PR raises the go directive. Is anything going to break?"
- "After upgrading Go, our ServeMux routes return 404. Why?"
- "Which godebug lines do I need to keep the old TLS behavior after the bump?"

## Install

As a Claude Code plugin:

    claude plugin marketplace add GillMacedo/godebugdiff
    claude plugin install godebugdiff@godebugdiff-marketplace

Or as a CLI with Go 1.22 or newer. It has no dependencies:

    go run github.com/GillMacedo/godebugdiff/plugin/scripts@latest --to 1.24 .

## Example

`examples/muxcheck` is a small program on `go 1.21` that registers a Go 1.22 style route, `GET /orders/{id}`. From a clone of this repo:

    $ go run ./plugin/scripts --to 1.22 examples/muxcheck
    go 1.21 -> 1.22 in example.com/muxcheck (go1.27.3, 1 binary)

    2 GODEBUG defaults change. 2 reach a package your binaries import.

    httplaxcontentlength  1 -> default  Go 1.22, net/http
        imported by example.com/muxcheck
        Go 1.22 made it an error for a request or response read by a net/http client or server to have an empty Content-Length header.
        https://go.dev/doc/godebug#go-122

    httpmuxgo121  1 -> default  Go 1.22, net/http
        imported by example.com/muxcheck
        Go 1.22 changed the behavior of ServeMux to accept extended patterns and unescape both patterns and request paths by segment.
        https://go.dev/doc/godebug#go-122

    To keep the old behavior of a setting after the bump, pin it in go.mod:

        godebug (
            httplaxcontentlength=1
            httpmuxgo121=1
        )

    Note: Crossing Go 1.22 also changes the language: each loop iteration gets its own loop variable in this module's code. That isn't a GODEBUG setting, so it isn't listed above. See https://go.dev/blog/loopvar-preview.

Built at `go 1.21`, that program answers `GET /orders/42` with 404, because `httpmuxgo121=1` treats the pattern as a literal path. Built at `go 1.22` it answers 200. `tests/` builds and runs it both ways to check this.

`examples/shop` has two binaries: an HTTP service and a batch job that only imports `fmt` and `math/rand`. Going from 1.21 to 1.24 changes 15 defaults. The full output is in [`examples/shop-1.21-to-1.24.txt`](examples/shop-1.21-to-1.24.txt), and it marks which binary each one reaches:

    httpmuxgo121  1 -> default  Go 1.22, net/http
        imported by example.com/shop/cmd/api
    ...
    randseednop  0 -> default  Go 1.24, math/rand
        imported by all 2 binaries
        Go 1.24 changed the global math/rand.Seed to be a no-op.

A module whose go.mod already pins its defaults with `godebug default=go1.21` stays quiet:

    $ go run ./plugin/scripts --to 1.27 examples/pinned
    go 1.21 -> 1.27 in example.com/pinned (go1.27.3, 1 binary)

    No GODEBUG defaults change.

    Note: Crossing Go 1.22 also changes the language: each loop iteration gets its own loop variable in this module's code. That isn't a GODEBUG setting, so it isn't listed above. See https://go.dev/blog/loopvar-preview.

    Note: go.mod already pins default=go1.21. Pinned settings don't change with the go line, so they aren't listed.

A two-module workspace reads the go line from go.work with `--work`:

    $ go run ./plugin/scripts --work --to 1.24 examples/workspace
    go 1.21 -> 1.24 in workspace (go1.27.3, 2 binaries)

go.work has no `godebug` lines, so in workspace mode the report lists `//go:debug` directives for each main package instead of a go.mod block.

On a real project: [age](https://github.com/FiloSottile/age) at commit `b74dce4`, from its `go 1.25.0` to 1.27, has 8 binaries. 4 of the 6 changed defaults reach one of them, and `x509sslcertoverrideplatform` and `urlstrictcolons` reach only `cmd/age`. Output from five open-source projects is in [`examples/real/`](examples/real/).

## Usage

    godebugdiff [--from VERSION] [--to VERSION] [--tests] [--work] [--json] [--fail] [DIR]

- `DIR` is the module root (the directory with go.mod) or, with `--work`, the workspace root (the directory with go.work). It defaults to `.`.
- `--to` defaults to the installed toolchain's version, such as 1.27 for go1.27.3. It can't be newer than the toolchain, because older toolchains don't know about later settings.
- `--from` defaults to the go line in go.mod, or in go.work with `--work`. Pass the base branch's version to review a change that already raised it.
- `--tests` adds test binaries. A module with no main package, such as a library, is checked through its test binaries automatically.
- `--work` reads the go line from `DIR/go.work` instead of `DIR/go.mod`, and checks every module in the workspace.
- `--json` prints the full result. `--fail` exits 1 when a change reaches a package one of your binaries imports. Exit code 2 means bad usage, or the go command couldn't load the module.

In CI, on a pull request that changes go.mod:

    base=$(git show origin/main:go.mod | awk '$1 == "go" { print $2 }')
    go run github.com/GillMacedo/godebugdiff/plugin/scripts@latest --from "$base" .

For a workspace, read the base go line from go.work and pass `--work`:

    base=$(git show origin/main:go.work | awk '$1 == "go" { print $2 }')
    go run github.com/GillMacedo/godebugdiff/plugin/scripts@latest --work --from "$base" .

## How it works

It copies go.mod and go.sum to a temporary directory twice, sets the go line with `go mod edit -go`, and runs `go list -modfile` on each copy with `GOPROXY=off` and `GOTOOLCHAIN=local`. `go list` reports each main package's `DefaultGODEBUG` and its imports, so the result comes from the same code that sets defaults in `go build`. Package names and versions come from the toolchain's own table in `$GOROOT/src/internal/godebugs`, and the one-line descriptions from `$GOROOT/doc/godebug.md`. Existing `godebug` lines in go.mod and `//go:debug` directives are applied by the toolchain, so pinned settings don't show up.

With `--work`, it copies go.work and go.work.sum, sets the go line with `go work edit -go`, and rewrites relative `use` and local `replace` paths to absolute paths in the copy. It then runs `go list` with `GOWORK` pointing at the copy and checks every workspace module. The go line in go.work sets the defaults; `godebug` lines in module go.mod files are ignored in workspace mode.

## Limits

- **Imported isn't the same as used.** A binary that imports `net/http` gets the `httpmuxgo121` change listed even if it never creates a ServeMux. The skill has Claude search the code for the API each setting names.
- **Only GODEBUG defaults.** Other changes between Go releases, in the release notes, aren't covered. The language change to per-iteration loop variables in Go 1.22 is noted when you cross it, but not analyzed.
- **Only what your toolchain knows.** Settings removed from the toolchain no longer appear, and their new behavior already applies whatever your go line says. Use the newest toolchain you plan to build with.
- **Needs the module cache or a vendor directory.** It never downloads. If modules are missing it stops and says to run `go mod download`.
- **Crossing Go 1.17 adds requirements.** In module mode, if the new go line needs `go mod tidy` changes, it makes them in the temporary copy and says so. In workspace mode it stops and asks you to run `go mod tidy`; it never edits module files.
- **Workspace files need `--work`.** Without it, an active go.work is only noted. With `--work`, the go line in go.work sets the defaults and `godebug` lines in module go.mod files are ignored.
- **Descriptions need `$GOROOT/doc/godebug.md`.** Toolchains that the go command downloads by itself, through `GOTOOLCHAIN`, don't include `doc/`, so they show only the link for each setting.
- Descriptions are the first sentence of the matching paragraph in Go's own docs, and some settings were backported, so a description can name a later release than the one in the "Go 1.N" column. The link goes to the full text.

## FAQ

**What is GODEBUG and why does the go line in go.mod change it?** GODEBUG settings control behavior changes in the standard library and runtime. Since Go 1.21, a binary's defaults follow the go line of its main module, so raising it opts the binary into newer behavior. See [Go, Backwards Compatibility, and GODEBUG](https://go.dev/doc/godebug).

**Is it safe to upgrade the Go toolchain without changing go.mod?** Mostly. With the same go line, a newer toolchain keeps the old defaults for settings it still has. Settings that were removed are the exception. `godebugdiff` covers the go line change.

**How do I keep the old behavior after raising the go line?** Add a `godebug` block to go.mod with the old values. In a workspace, add `//go:debug` directives to each main package or set `GODEBUG` at run time; go.work has no `godebug` lines. `godebugdiff` prints the right form for every change that reaches a binary. These settings are removed in later Go releases, so treat pins as temporary.

**Why did my ServeMux routes start returning 404, or start matching differently, after a Go upgrade?** `httpmuxgo121` changed in Go 1.22. With the old value, patterns like `GET /items/{id}` are literal paths. See `examples/muxcheck`.

**Does my library's go line change GODEBUG for programs that use it?** No. Only the main module's go line (or the go.work go line in workspace mode) counts. Raising a library's go line does force those programs to need that Go version, which raises their go line too.

**Is it an MCP server?** No. It's a skill with one Go source file, so `go run` is all it needs.

## Tested on

- **Example modules in `examples/`** with Go 1.27.3 on Linux arm64. The integration tests in `tests/` check that the settings listed for `examples/shop` match the `DefaultGODEBUG` that `go build` records in each binary (read with `go version -m`), at both go lines.
- **A real behavior change:** `examples/muxcheck` built at go 1.21 returns 404 and at go 1.22 returns 200, and `godebugdiff` lists `httpmuxgo121` for it.
- **A workspace:** `examples/workspace` has two modules and a go.work with relative `use` paths. The integration test checks both binaries and that go.work is unchanged.
- **Healthy cases stay quiet:** `examples/pinned` with `godebug default=go1.21`, and any module compared at the same version, print "No GODEBUG defaults change."
- **Real projects**, to the installed 1.27: hey `5626f79` (go 1.24.0), fzf `26ff936` (go 1.23.0), age `b74dce4` (go 1.25.0, 8 binaries), cobra `adbc881` (a library on go 1.15, which needed the temporary tidy) and rclone `f0cb5dd` (which pins two settings in go.mod). Outputs are in `examples/real/`. A vendored copy of hey gave the same result with an empty module cache, and hey without a vendor directory and with an empty cache stopped with the "run go mod download" message. None of these checked whether the projects use each changed API.
- **Go 1.22.12**, a downloaded toolchain: the unit tests and the ServeMux test pass, and the tests that need a newer toolchain skip. On `examples/muxcheck` it also lists `tls10server`, `tlsrsakex` and `tlsunsafeekm`, which Go 1.27 removed, and shows links without descriptions.
- **Errors:** a `godebug` line with a removed setting, `--to` newer than the toolchain, and a directory with no go.mod.

It hasn't been tested on Windows, on toolchains other than 1.22.12 and 1.27.3, in a fresh Claude session, or through `go run ...@latest`, which needs the published repo.

## Contributing

Contributions are welcome, from people and from people working with AI agents. See [CONTRIBUTING.md](CONTRIBUTING.md). Good first issues are [listed here](https://github.com/GillMacedo/godebugdiff/issues?q=is%3Aissue+is%3Aopen+label%3A%22good+first+issue%22).

### How pull requests work

1. **Open a PR.** Fork the repo, work on a branch, and keep it to one change. Write a title that says what changes for users, because it becomes the changelog line: `feat:` goes under Added and `fix:` under Fixed.
2. **Automatic check.** Code changes need tests, and `make test` must pass. Changes over 30 lines need a linked open issue (`Closes #<number>`), and a PR can have at most 800 changed lines. If something is missing, a comment tells you what to fix.
3. **Review.** Claude reviews each PR automatically. Code PRs also get a security review and are tested in a sandbox. The review then merges the PR or asks for changes. PRs that change CI, agent settings, dependencies or build files go to the maintainer, as does anything the security review doesn't clear.
4. **Release.** A merge that changes what users install becomes a new version within about 20 minutes, with a GitHub Release and, if the project has one, its npm package. Docs and CI changes wait for the next release.

## AI disclosure

This project was largely generated with Claude Code and reviewed by its maintainer.

## About me

I'm Giulliano Macedo, and I build developer tools at GoTech Solutions. Follow along or get in touch:

- GitHub: [GillMacedo](https://github.com/GillMacedo)
- LinkedIn: [giulliano](https://www.linkedin.com/in/giulliano/)
- X: [@giumacedo](https://x.com/giumacedo)

If `godebugdiff` saved you some prompts, a star helps others find it.

---

Built by [Giulliano Macedo](https://github.com/GillMacedo), GoTech Solutions · [LinkedIn](https://www.linkedin.com/in/giulliano/) · [X](https://x.com/giumacedo)
