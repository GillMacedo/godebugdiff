# 0003: Never edit go.mod, never download

**Decision:** the tool works on temporary copies of go.mod and go.sum, and runs every go command with `GOPROXY=off` and `GOTOOLCHAIN=local`. When modules are missing from the cache, it stops and says to run `go mod download`.

**Why:** people run it before deciding on a bump, often on a branch they don't want changed, and sometimes in CI without network access. Downloading or switching toolchains behind their back would also make results depend on the network.

**Alternatives:** running `go mod download` automatically. The skill can ask the user to do it instead.
