// Package tests runs godebugdiff on the example modules the way the skill
// does, and checks its answers against the go command's build output and
// against a real behavior change.
package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

type result struct {
	Module    string
	From, To  string
	Binaries  []string
	TestsOnly bool
	Changes   []struct {
		Name      string
		Package   string
		ChangedIn int
		Before    string
		After     string
		ReachedBy []string
		GoCommand bool
		Doc, URL  string
	}
	Notes []string
}

var bin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "godebugdiff")
	if err != nil {
		panic(err)
	}
	bin = filepath.Join(dir, "godebugdiff")
	// Build the single file, as the skill runs it.
	out, err := exec.Command("go", "build", "-o", bin, "../plugin/scripts/godebugdiff.go").CombinedOutput()
	if err != nil {
		panic(string(out))
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// needGo skips a test when the installed toolchain is older than Go 1.n.
func needGo(t *testing.T, n int) {
	t.Helper()
	out, err := exec.Command("go", "env", "GOVERSION").Output()
	if err != nil {
		t.Fatal(err)
	}
	var minor int
	fmt.Sscanf(strings.TrimPrefix(strings.TrimSpace(string(out)), "go1."), "%d", &minor)
	if minor < n {
		t.Skipf("needs Go 1.%d or newer, have %s", n, strings.TrimSpace(string(out)))
	}
}

func diff(t *testing.T, args ...string) (result, int, string) {
	t.Helper()
	cmd := exec.Command(bin, append([]string{"--json"}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	var r result
	if code != 2 {
		if err := json.Unmarshal(out, &r); err != nil {
			t.Fatalf("bad JSON: %v\n%s", err, out)
		}
	}
	return r, code, stderr.String()
}

func byName(r result) map[string]int {
	m := map[string]int{}
	for i, c := range r.Changes {
		m[c.Name] = i
	}
	return m
}

func TestShop(t *testing.T) {
	needGo(t, 24)
	before, _ := os.ReadFile("../examples/shop/go.mod")
	r, code, stderr := diff(t, "--to", "1.24", "--fail", "../examples/shop")
	if code != 1 {
		t.Fatalf("--fail should exit 1 when changes reach a binary, got %d: %s", code, stderr)
	}
	if r.From != "1.21" || r.To != "1.24" || !reflect.DeepEqual(r.Binaries, []string{"example.com/shop/cmd/api", "example.com/shop/cmd/worker"}) {
		t.Fatalf("header = %s -> %s, %v", r.From, r.To, r.Binaries)
	}
	idx := byName(r)
	api := []string{"example.com/shop/cmd/api"}
	both := r.Binaries
	for name, want := range map[string][]string{
		"httpmuxgo121":         api,
		"httplaxcontentlength": api,
		"tlsmlkem":             api,
		"x509usepolicies":      api,
		"randseednop":          both,
		"winsymlink":           both,
	} {
		i, ok := idx[name]
		if !ok {
			t.Errorf("%s missing", name)
			continue
		}
		if !reflect.DeepEqual(r.Changes[i].ReachedBy, want) {
			t.Errorf("%s reached by %v, want %v", name, r.Changes[i].ReachedBy, want)
		}
	}
	if c := r.Changes[idx["httpmuxgo121"]]; c.Before != "1" || c.After != "" || c.ChangedIn != 22 || !strings.Contains(c.Doc, "ServeMux") {
		t.Errorf("httpmuxgo121 = %+v", c)
	}
	if c := r.Changes[idx["gotestjsonbuildtext"]]; !c.GoCommand || len(c.ReachedBy) != 0 {
		t.Errorf("gotestjsonbuildtext = %+v", c)
	}
	// Settings changed after 1.24 must not appear.
	for _, late := range []string{"tlssha1", "containermaxprocs", "tracebacklabels"} {
		if _, ok := idx[late]; ok {
			t.Errorf("%s changed after Go 1.24 and shouldn't be listed", late)
		}
	}
	if !strings.Contains(strings.Join(r.Notes, " "), "loop variable") {
		t.Errorf("crossing 1.22 should note the loopvar change: %v", r.Notes)
	}
	after, _ := os.ReadFile("../examples/shop/go.mod")
	if !bytes.Equal(before, after) {
		t.Error("godebugdiff changed examples/shop/go.mod")
	}
}

// buildDefault builds pkg with a go.mod copy at goVersion and returns the
// DefaultGODEBUG the go command recorded in the binary's build info.
func buildDefault(t *testing.T, dir, pkg, goVersion string) map[string]string {
	t.Helper()
	tmp := t.TempDir()
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	mod := filepath.Join(tmp, "go.mod")
	os.WriteFile(mod, data, 0o644)
	os.WriteFile(filepath.Join(tmp, "go.sum"), nil, 0o644)
	if out, err := exec.Command("go", "mod", "edit", "-go="+goVersion, mod).CombinedOutput(); err != nil {
		t.Fatal(string(out))
	}
	exe := filepath.Join(tmp, "bin")
	build := exec.Command("go", "build", "-modfile="+mod, "-o", exe, pkg)
	build.Dir = dir
	build.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOPROXY=off")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatal(string(out))
	}
	out, err := exec.Command("go", "version", "-m", exe).Output()
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && f[0] == "build" && strings.HasPrefix(f[1], "DefaultGODEBUG=") {
			for _, kv := range strings.Split(strings.TrimPrefix(f[1], "DefaultGODEBUG="), ",") {
				k, v, _ := strings.Cut(kv, "=")
				m[k] = v
			}
		}
	}
	return m
}

// The settings godebugdiff lists must match what `go build` records in each
// binary's build info, which is a separate path through the go command.
func TestMatchesBuildInfo(t *testing.T) {
	needGo(t, 24)
	r, _, stderr := diff(t, "--to", "1.24", "../examples/shop")
	if len(r.Changes) == 0 {
		t.Fatalf("no changes: %s", stderr)
	}
	var listed []string
	for _, c := range r.Changes {
		listed = append(listed, c.Name)
	}
	sort.Strings(listed)
	for _, pkg := range []string{"./cmd/api", "./cmd/worker"} {
		old := buildDefault(t, "../examples/shop", pkg, "1.21")
		cur := buildDefault(t, "../examples/shop", pkg, "1.24")
		var built []string
		for k, v := range old {
			if cur[k] != v {
				built = append(built, k)
			}
		}
		for k := range cur {
			if _, ok := old[k]; !ok {
				built = append(built, k)
			}
		}
		sort.Strings(built)
		if !reflect.DeepEqual(built, listed) {
			t.Errorf("%s: build info differs in %v, godebugdiff listed %v", pkg, built, listed)
		}
	}
}

// httpmuxgo121 really changes how the program routes a request.
func TestServeMuxBehavior(t *testing.T) {
	dir := "../examples/muxcheck"
	r, _, stderr := diff(t, "--to", "1.22", dir)
	idx := byName(r)
	i, ok := idx["httpmuxgo121"]
	if !ok || !reflect.DeepEqual(r.Changes[i].ReachedBy, []string{"example.com/muxcheck"}) {
		t.Fatalf("httpmuxgo121 should reach muxcheck: %+v %s", r.Changes, stderr)
	}
	runAt := func(v string) string {
		tmp := t.TempDir()
		data, _ := os.ReadFile(filepath.Join(dir, "go.mod"))
		mod := filepath.Join(tmp, "go.mod")
		os.WriteFile(mod, data, 0o644)
		os.WriteFile(filepath.Join(tmp, "go.sum"), nil, 0o644)
		exec.Command("go", "mod", "edit", "-go="+v, mod).Run()
		cmd := exec.Command("go", "run", "-modfile="+mod, ".")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOPROXY=off")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("go run at %s: %v\n%s", v, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	if got := runAt("1.21"); got != "GET /orders/42: 404" {
		t.Errorf("go 1.21: %q", got)
	}
	if got := runAt("1.22"); got != "GET /orders/42: 200" {
		t.Errorf("go 1.22: %q", got)
	}
}

func TestPinnedStaysQuiet(t *testing.T) {
	needGo(t, 23)
	r, code, stderr := diff(t, "--to", "1.27", "--fail", "../examples/pinned")
	if code != 0 || len(r.Changes) != 0 {
		t.Fatalf("pinned module should have no changes, got code %d, %+v %s", code, r.Changes, stderr)
	}
	out, err := exec.Command(bin, "--to", "1.27", "../examples/pinned").Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "No GODEBUG defaults change.") || !strings.Contains(string(out), "default=go1.21") {
		t.Errorf("output:\n%s", out)
	}
}

func TestSameVersionStaysQuiet(t *testing.T) {
	needGo(t, 24)
	r, code, _ := diff(t, "--from", "1.24", "--to", "1.24", "--fail", "../examples/shop")
	if code != 0 || len(r.Changes) != 0 || len(r.Notes) != 0 {
		t.Errorf("same version: code %d, %+v, %v", code, r.Changes, r.Notes)
	}
}

func TestLibraryUsesTests(t *testing.T) {
	needGo(t, 27)
	r, _, stderr := diff(t, "--to", "1.27", "../examples/ledgerlib")
	if !r.TestsOnly || !reflect.DeepEqual(r.Binaries, []string{"example.com/ledgerlib.test"}) {
		t.Fatalf("library: %+v %s", r, stderr)
	}
	idx := byName(r)
	if i, ok := idx["urlstrictcolons"]; !ok || len(r.Changes[i].ReachedBy) != 1 {
		t.Errorf("urlstrictcolons should reach the test binary")
	}
	if i, ok := idx["tlsmlkem"]; ok && len(r.Changes[i].ReachedBy) != 0 {
		t.Errorf("tlsmlkem shouldn't reach a binary that doesn't import crypto/tls")
	}
	if !strings.Contains(strings.Join(r.Notes, " "), "doesn't set GODEBUG for programs that import it") {
		t.Errorf("notes: %v", r.Notes)
	}
}

func TestErrors(t *testing.T) {
	needGo(t, 24)
	if _, code, stderr := diff(t, "--to", "1.99", "../examples/shop"); code != 2 || !strings.Contains(stderr, "newer than the installed toolchain") {
		t.Errorf("--to 1.99: %d %s", code, stderr)
	}
	if _, code, stderr := diff(t, "../examples"); code != 2 || !strings.Contains(stderr, "no go.mod") {
		t.Errorf("no go.mod: %d %s", code, stderr)
	}
	// A godebug line for a removed setting is an error from the go command.
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/old\n\ngo 1.21\n\ngodebug x509sha1=1\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644)
	if _, code, stderr := diff(t, dir); code != 2 || !strings.Contains(stderr, `removed GODEBUG "x509sha1"`) {
		t.Errorf("removed setting: %d %s", code, stderr)
	}
}

func TestOnlyPackages(t *testing.T) {
	needGo(t, 24)
	r, code, stderr := diff(t, "../examples/shop", "--to", "1.24", "--only", "net/http,crypto/tls", "--fail")
	if code != 1 || len(r.Changes) == 0 {
		t.Fatalf("expected selected changes and exit 1, got %d: %s", code, stderr)
	}
	for _, change := range r.Changes {
		if change.Package != "net/http" && change.Package != "crypto/tls" {
			t.Errorf("unexpected package in filtered report: %s", change.Package)
		}
	}
	r, code, stderr = diff(t, "../examples/shop", "--to", "1.24", "--only=example.com/not-imported", "--fail")
	if code != 0 || len(r.Changes) != 0 {
		t.Fatalf("excluded changes must not trigger --fail: %d, %+v, %s", code, r.Changes, stderr)
	}
}
