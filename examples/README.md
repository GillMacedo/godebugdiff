# Examples

All modules here are made up and use only the standard library.

- `muxcheck/`: go 1.21, registers a `GET /orders/{id}` route. It returns 404 at go 1.21 and 200 at go 1.22, because of `httpmuxgo121`. `tests/` runs it both ways.
- `shop/`: go 1.21, two binaries. `cmd/api` serves HTTP and `cmd/worker` only imports `fmt` and `math/rand`, so most changes reach only the API. `shop-1.21-to-1.24.txt` is its real output, from `make examples`.
- `pinned/`: go 1.21 with `godebug default=go1.21`, so raising the go line changes no defaults. This is the healthy case.
- `ledgerlib/`: a library with no main package, checked through its test binary.
- `workspace/`: a two-module workspace on go 1.21. `app` serves HTTP and `worker` imports `math/rand`; `go.work` uses relative paths. `godebugdiff --work --to 1.24 examples/workspace` reads the go line from `go.work`.

## real/

Real output from `godebugdiff` with no flags, on Go 1.27.3, on shallow clones of open-source projects taken on 2026-11-02 after `go mod download`. Only the output is stored here, no code from the projects.

| File | Project | Commit | License |
|---|---|---|---|
| `hey.txt` | github.com/rakyll/hey | `5626f79` | Apache-2.0 |
| `fzf.txt` | github.com/junegunn/fzf | `26ff936` | MIT |
| `age.txt` | github.com/FiloSottile/age | `b74dce4` | BSD-3-Clause |
| `cobra.txt` | github.com/spf13/cobra | `adbc881` | Apache-2.0 |
| `rclone.txt` | github.com/rclone/rclone | `f0cb5dd` | MIT |

To redo one: clone the project, check out the commit, run `go mod download`, then `go run ./plugin/scripts PATH` from this repo. The results depend on the toolchain you run.
