# Changelog

All notable changes are documented here, in [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) format. This project uses [Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.1.0] - 2026-11-02
### Added
- Lists GODEBUG defaults that change between two go lines, from the installed toolchain's `go list` output, offline and without editing go.mod
- Marks which binaries import the package each setting controls, and lists go command settings separately
- Prints a `godebug` block that keeps the old behavior
- Checks test binaries for modules with no main package, with `--tests` to add them otherwise
- Notes for the Go 1.22 loop variable change, existing `godebug` pins, old toolchain lines, go.work and needed `go mod tidy` changes
- `--from`, `--to`, `--json` and `--fail` options
- Claude Code plugin with the `checking-go-version-bumps` skill
