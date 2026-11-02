Add an example module for this case: $ARGUMENTS

1. Create a small made-up module in `examples/` with its own go.mod and no dependencies outside the standard library.
2. Run `go run ./plugin/scripts --json` on it and check the result against `go build` and `go version -m` at both go lines, the way `TestMatchesBuildInfo` in `tests/integration_test.go` does.
3. Add an integration test for it, and a line in `examples/README.md`.
4. Run `make test`.
