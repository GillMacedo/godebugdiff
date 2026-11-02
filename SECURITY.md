# Security

Report vulnerabilities through [GitHub's private vulnerability reporting](https://github.com/GillMacedo/godebugdiff/security/advisories/new), not in a public issue.

`godebugdiff` runs only `go env`, `go mod edit -json` to read go.mod, `go mod edit -go` on temporary copies of it, and `go list`, with the network turned off. Please report privately if a crafted module could make it write to the user's files, use the network, or run code. A missed or wrong setting is a normal bug, unless the output could lead someone to pin an insecure setting without warning.
