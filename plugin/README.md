# godebugdiff plugin

This plugin adds the `checking-go-version-bumps` skill to Claude Code. When you plan to raise the `go` line in go.mod, or review a change that does, Claude runs the bundled `scripts/godebugdiff.go` with `go run`. It lists the GODEBUG defaults that change, which of your binaries import the package each setting controls, and the `godebug` lines that keep the old behavior. Claude then checks whether your code uses the changed API. It needs Go 1.22 or newer. See the repository README for examples and limits.

Install:

    claude plugin marketplace add GillMacedo/godebugdiff
    claude plugin install godebugdiff@godebugdiff-marketplace
