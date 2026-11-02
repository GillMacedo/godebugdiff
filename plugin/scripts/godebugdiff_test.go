package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestMinor(t *testing.T) {
	for in, want := range map[string]int{"1.21": 21, "1.24.3": 24, "go1.27.3": 27, "go1.26rc1": 26, " 1.22 ": 22} {
		got, err := minor(in)
		if err != nil || got != want {
			t.Errorf("minor(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "2.0", "1", "latest", "go1.x"} {
		if _, err := minor(bad); err == nil {
			t.Errorf("minor(%q) should fail", bad)
		}
	}
}

func TestReorder(t *testing.T) {
	got := reorder([]string{"examples/shop", "--to", "1.24", "--json"})
	want := []string{"--to", "1.24", "--json", "examples/shop"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("reorder = %q, want %q", got, want)
	}
	got = reorder([]string{"--to=1.24", "dir", "--fail"})
	want = []string{"--to=1.24", "--fail", "dir"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("reorder = %q, want %q", got, want)
	}
}

func TestParseGodebug(t *testing.T) {
	got := parseGodebug("httpmuxgo121=1,panicnil=1, x509usepolicies=0,")
	want := map[string]string{"httpmuxgo121": "1", "panicnil": "1", "x509usepolicies": "0"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
	if len(parseGodebug("")) != 0 {
		t.Error("empty string should give no settings")
	}
}

func TestKeepFlags(t *testing.T) {
	got := keepFlags("-mod=mod -tags=prod -modfile=x.mod -trimpath")
	if got != "-tags=prod -trimpath" {
		t.Errorf("keepFlags = %q", got)
	}
}

const tableSnippet = `package godebugs

var All = []Info{
	{Name: "httpmuxgo121", Package: "net/http", Changed: 22, Old: "1"},
	{Name: "netdns", Package: "net", Opaque: true},
	//{Name: "multipartfiles", Package: "mime/multipart"},
	{Name: "tlsmlkem", Package: "crypto/tls", Changed: 24, Old: "0", Opaque: true},
}

var Removed = []RemovedInfo{
	{Name: "x509sha1", Removed: 24, Old: one},
}
`

func TestParseTable(t *testing.T) {
	got := parseTable(strings.NewReader(tableSnippet))
	want := map[string]Setting{
		"httpmuxgo121": {Name: "httpmuxgo121", Package: "net/http", Changed: 22, Old: "1"},
		"netdns":       {Name: "netdns", Package: "net"},
		"tlsmlkem":     {Name: "tlsmlkem", Package: "crypto/tls", Changed: 24, Old: "0"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseTable = %+v", got)
	}
}

// The installed toolchain's own table must parse, or package names are lost.
func TestParseInstalledTable(t *testing.T) {
	goroot := strings.TrimSpace(run1(t, "go", "env", "GOROOT"))
	table := loadTable(goroot)
	if len(table) < 20 {
		t.Fatalf("only %d settings parsed from the installed toolchain", len(table))
	}
	if s := table["panicnil"]; s.Package != "runtime" || s.Changed != 21 || s.Old != "1" {
		t.Errorf("panicnil = %+v", s)
	}
	if _, err := os.Stat(filepath.Join(goroot, "doc", "godebug.md")); err != nil {
		t.Skip("this toolchain has no doc/godebug.md, as with toolchains the go command downloads")
	}
	docs := loadDocs(goroot)
	if d, ok := pickDoc(docs["httpmuxgo121"], 22); !ok || d.Minor != 22 || !strings.Contains(d.Text, "ServeMux") {
		t.Errorf("httpmuxgo121 doc = %+v", d)
	}
}

const docSnippet = "# GODEBUG\n\n## History {#history}\n\n### Go 1.26\n\n" +
	"Go 1.26 added a new `httpcookiemaxnum` setting that limits cookies. The default is `httpcookiemaxnum=3000`.\n\n" +
	"### Go 1.22\n\n" +
	"Go 1.22 changed the behavior of ServeMux to accept extended\npatterns, e.g. methods. This behavior can be controlled by the\n[`httpmuxgo121` setting](/pkg/net/http/#ServeMux).\n\n" +
	"A second paragraph about `httpmuxgo121=1` that is skipped.\n\n" +
	"### Go 1.24\n\nGo 1.24 removed the `x509sha1` setting.\n"

func TestParseDocs(t *testing.T) {
	docs := parseDocs(docSnippet)
	d, ok := pickDoc(docs["httpmuxgo121"], 22)
	if !ok || d.Text != "Go 1.22 changed the behavior of ServeMux to accept extended patterns, e.g. methods." || d.URL != "https://go.dev/doc/godebug#go-122" {
		t.Errorf("httpmuxgo121 = %+v", d)
	}
	if len(docs["httpmuxgo121"]) != 1 {
		t.Errorf("want one entry per section, got %d", len(docs["httpmuxgo121"]))
	}
	// Backported settings are described in a later section than Changed.
	d, ok = pickDoc(docs["httpcookiemaxnum"], 24)
	if !ok || d.Minor != 26 {
		t.Errorf("httpcookiemaxnum = %+v", d)
	}
	if _, ok := docs["x509sha1"]; !ok {
		t.Error("x509sha1 should be found")
	}
}

func TestCompare(t *testing.T) {
	table := map[string]Setting{
		"httpmuxgo121":        {Name: "httpmuxgo121", Package: "net/http", Changed: 22, Old: "1"},
		"randseednop":         {Name: "randseednop", Package: "math/rand", Changed: 24, Old: "0"},
		"gotestjsonbuildtext": {Name: "gotestjsonbuildtext", Package: "cmd/go", Changed: 24, Old: "1"},
		"tlssha1":             {Name: "tlssha1", Package: "crypto/tls", Changed: 25, Old: "1"},
	}
	deps := func(pkgs ...string) map[string]bool {
		m := map[string]bool{}
		for _, p := range pkgs {
			m[p] = true
		}
		return m
	}
	before := []Binary{
		{ImportPath: "x/api", Godebug: parseGodebug("httpmuxgo121=1,randseednop=0,gotestjsonbuildtext=1,tlssha1=1"), Deps: deps("net/http", "math/rand", "cmd/go")},
		{ImportPath: "x/worker", Godebug: parseGodebug("httpmuxgo121=1,randseednop=0,gotestjsonbuildtext=1,tlssha1=1"), Deps: deps("math/rand")},
	}
	after := []Binary{
		{ImportPath: "x/api", Godebug: parseGodebug("tlssha1=1"), Deps: deps("net/http", "math/rand")},
		{ImportPath: "x/worker", Godebug: parseGodebug("tlssha1=1"), Deps: deps("math/rand")},
	}
	changes, names := compare(before, after, table, nil)
	if !reflect.DeepEqual(names, []string{"x/api", "x/worker"}) {
		t.Errorf("names = %v", names)
	}
	got := map[string]Change{}
	for _, c := range changes {
		got[c.Name] = c
	}
	if len(changes) != 3 {
		t.Fatalf("want 3 changes, got %+v", changes)
	}
	if c := got["httpmuxgo121"]; c.Before != "1" || c.After != "" || !reflect.DeepEqual(c.ReachedBy, []string{"x/api"}) || c.URL != "https://go.dev/doc/godebug#go-122" {
		t.Errorf("httpmuxgo121 = %+v", c)
	}
	if c := got["randseednop"]; !reflect.DeepEqual(c.ReachedBy, []string{"x/api", "x/worker"}) {
		t.Errorf("randseednop = %+v", c)
	}
	if c := got["gotestjsonbuildtext"]; !c.GoCommand || len(c.ReachedBy) != 0 {
		t.Errorf("gotestjsonbuildtext = %+v", c)
	}
	if changes[0].Name != "httpmuxgo121" {
		t.Errorf("changes should be sorted by version, got %s first", changes[0].Name)
	}
}

func TestPrintNoChanges(t *testing.T) {
	var buf bytes.Buffer
	Print(&buf, &Result{Module: "x", From: "1.27", To: "1.27", Toolchain: "go1.27.3", Binaries: []string{"x"}})
	want := "go 1.27 -> 1.27 in x (go1.27.3, 1 binary)\n\nNo GODEBUG defaults change.\n"
	if buf.String() != want {
		t.Errorf("got %q", buf.String())
	}
}

func TestRunErrors(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{t.TempDir()}, &out, &errb); code != 2 || !strings.Contains(errb.String(), "no go.mod") {
		t.Errorf("no go.mod: code %d, stderr %q", code, errb.String())
	}
	errb.Reset()
	if code := run([]string{"--work", t.TempDir()}, &out, &errb); code != 2 || !strings.Contains(errb.String(), "no go.work") {
		t.Errorf("no go.work: code %d, stderr %q", code, errb.String())
	}
	errb.Reset()
	if code := run([]string{"a", "b"}, &out, &errb); code != 2 {
		t.Errorf("two dirs: code %d", code)
	}
	errb.Reset()
	if code := run([]string{"--bogus"}, &out, &errb); code != 2 {
		t.Errorf("bad flag: code %d", code)
	}
	out.Reset()
	if code := run([]string{"--version"}, &out, &errb); code != 0 || out.String() != "godebugdiff "+version+"\n" {
		t.Errorf("version: %d %q", code, out.String())
	}
}

func TestDiffOnTempModule(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "go.mod"), "module example.com/tmp\n\ngo 1.21\n")
	write(t, filepath.Join(dir, "main.go"), "package main\n\nimport \"net/http\"\n\nfunc main() { _ = http.NewServeMux() }\n")
	res, err := Diff(dir, "", "1.22", false, false)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, c := range res.Changes {
		if c.Changed == 22 && c.Package == "net/http" {
			names = append(names, c.Name)
		}
	}
	// Toolchains before Go 1.27 also list TLS settings that 1.27 removed.
	if !reflect.DeepEqual(names, []string{"httplaxcontentlength", "httpmuxgo121"}) {
		t.Errorf("1.21 -> 1.22 net/http changes = %v", names)
	}
	if _, err := Diff(dir, "", "1.99", false, false); err == nil || !strings.Contains(err.Error(), "newer than the installed toolchain") {
		t.Errorf("want toolchain error, got %v", err)
	}
	if _, err := Diff(dir, "banana", "", false, false); err == nil {
		t.Error("want error for --from banana")
	}
	data, _ := os.ReadFile(filepath.Join(dir, "go.mod"))
	if string(data) != "module example.com/tmp\n\ngo 1.21\n" {
		t.Errorf("go.mod changed: %q", data)
	}
}

func TestReadWorkFile(t *testing.T) {
	dir := t.TempDir()
	for _, m := range []string{"a", "b", "local"} {
		if err := os.Mkdir(filepath.Join(dir, m), 0o755); err != nil {
			t.Fatal(err)
		}
		write(t, filepath.Join(dir, m, "go.mod"), "module example.com/"+m+"\n\ngo 1.21\n")
	}
	write(t, filepath.Join(dir, "go.work"), "go 1.21\n\nuse (\n\t./a\n\t./b\n)\n\nreplace example.com/local => ./local\n")

	w, err := readWorkFile(dir, filepath.Join(dir, "go.work"))
	if err != nil {
		t.Fatal(err)
	}
	if w.Go != "1.21" || len(w.Use) != 2 || w.Use[0].DiskPath != "./a" || w.Use[1].DiskPath != "./b" {
		t.Errorf("workFile = %+v", w)
	}
	if len(w.Replace) != 1 || w.Replace[0].Old.Path != "example.com/local" || w.Replace[0].New.Path != "./local" {
		t.Errorf("replace = %+v", w.Replace)
	}
}

func TestCopyWork(t *testing.T) {
	dir := t.TempDir()
	for _, m := range []string{"a", "b", "local"} {
		if err := os.Mkdir(filepath.Join(dir, m), 0o755); err != nil {
			t.Fatal(err)
		}
		write(t, filepath.Join(dir, m, "go.mod"), "module example.com/"+m+"\n\ngo 1.21\n")
	}
	original := "go 1.21\n\nuse (\n\t./a\n\t./b\n)\n\nreplace example.com/local => ./local\n"
	write(t, filepath.Join(dir, "go.work"), original)
	write(t, filepath.Join(dir, "go.work.sum"), "example.com/local v0.0.0 h1:test\n")

	tmp := t.TempDir()
	copyPath, err := copyWork(dir, filepath.Join(dir, "go.work"), filepath.Join(tmp, "from"), "1.22")
	if err != nil {
		t.Fatal(err)
	}
	got, err := readWorkFile(tmp, copyPath)
	if err != nil {
		t.Fatal(err)
	}
	if got.Go != "1.22" {
		t.Errorf("go line = %q", got.Go)
	}
	for i, want := range []string{filepath.Join(dir, "a"), filepath.Join(dir, "b")} {
		if i >= len(got.Use) || got.Use[i].DiskPath != want {
			t.Errorf("use[%d] = %+v, want %q", i, got.Use, want)
		}
	}
	if len(got.Replace) != 1 || got.Replace[0].New.Path != filepath.Join(dir, "local") {
		t.Errorf("replace = %+v", got.Replace)
	}
	if data, err := os.ReadFile(filepath.Join(tmp, "from.work.sum")); err != nil || string(data) != "example.com/local v0.0.0 h1:test\n" {
		t.Errorf("go.work.sum = %q, %v", data, err)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "go.work")); string(data) != original {
		t.Errorf("original go.work changed: %q", data)
	}
}

func TestDiffWorkspace(t *testing.T) {
	dir := t.TempDir()
	for _, m := range []string{"app", "worker"} {
		if err := os.Mkdir(filepath.Join(dir, m), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write(t, filepath.Join(dir, "app", "go.mod"), "module example.com/ws/app\n\ngo 1.21\n")
	write(t, filepath.Join(dir, "app", "main.go"), "package main\n\nimport \"net/http\"\n\nfunc main() { _ = http.NewServeMux() }\n")
	write(t, filepath.Join(dir, "worker", "go.mod"), "module example.com/ws/worker\n\ngo 1.21\n")
	write(t, filepath.Join(dir, "worker", "main.go"), "package main\n\nimport (\n\t\"fmt\"\n\t\"math/rand\"\n)\n\nfunc main() { fmt.Println(rand.Int()) }\n")
	work := "go 1.21\n\nuse (\n\t./app\n\t./worker\n)\n"
	write(t, filepath.Join(dir, "go.work"), work)

	res, err := Diff(dir, "", "1.22", false, true)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Workspace || res.Module != "workspace" || res.From != "1.21" || res.To != "1.22" {
		t.Fatalf("result = %+v", res)
	}
	want := []string{"example.com/ws/app", "example.com/ws/worker"}
	if !reflect.DeepEqual(res.Binaries, want) {
		t.Errorf("binaries = %v, want %v", res.Binaries, want)
	}
	idx := map[string]Change{}
	for _, c := range res.Changes {
		idx[c.Name] = c
	}
	if c, ok := idx["httpmuxgo121"]; !ok || !reflect.DeepEqual(c.ReachedBy, []string{"example.com/ws/app"}) {
		t.Errorf("httpmuxgo121 = %+v", c)
	}
	if !strings.Contains(strings.Join(res.Notes, " "), "go line in go.work") {
		t.Errorf("notes = %v", res.Notes)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "go.work")); string(data) != work {
		t.Errorf("go.work changed: %q", data)
	}
	if _, err := Diff(dir, "", "1.22", false, false); err == nil || !strings.Contains(err.Error(), "no go.mod") {
		t.Errorf("without --work: %v", err)
	}
}

func TestDiffWorkspaceUsesTests(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "lib"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "lib", "go.mod"), "module example.com/lib\n\ngo 1.21\n")
	write(t, filepath.Join(dir, "lib", "lib.go"), "package lib\n\nfunc F() int { return 1 }\n")
	write(t, filepath.Join(dir, "lib", "lib_test.go"), "package lib\n\nimport \"testing\"\n\nfunc TestF(t *testing.T) { _ = F() }\n")
	write(t, filepath.Join(dir, "go.work"), "go 1.21\n\nuse ./lib\n")

	res, err := Diff(dir, "", "1.22", false, true)
	if err != nil {
		t.Fatal(err)
	}
	if !res.TestsOnly || !reflect.DeepEqual(res.Binaries, []string{"example.com/lib.test"}) {
		t.Fatalf("library workspace: %+v", res)
	}
}

func TestPrintWorkspacePins(t *testing.T) {
	var buf bytes.Buffer
	Print(&buf, &Result{
		Module:    "workspace",
		Workspace: true,
		From:      "1.21",
		To:        "1.22",
		Toolchain: "go1.27.3",
		Binaries:  []string{"example.com/app"},
		Changes: []Change{
			{
				Name: "httpmuxgo121", Package: "net/http", Changed: 22,
				Before: "1", After: "", ReachedBy: []string{"example.com/app"},
			},
			{
				Name: "gotestjsonbuildtext", Package: "cmd/go", Changed: 24,
				Before: "1", After: "", GoCommand: true,
			},
		},
	})
	s := buf.String()
	if !strings.Contains(s, "go 1.21 -> 1.22 in workspace") {
		t.Errorf("header:\n%s", s)
	}
	if !strings.Contains(s, "//go:debug httpmuxgo121=1") || strings.Contains(s, "godebug (") {
		t.Errorf("pin advice:\n%s", s)
	}
	if strings.Contains(s, "//go:debug gotestjsonbuildtext=1") {
		t.Errorf("go-command setting should not be pinned with //go:debug:\n%s", s)
	}
}

func write(t *testing.T, path, s string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func run1(t *testing.T, name string, args ...string) string {
	t.Helper()
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}
