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
	res, err := Diff(dir, "", "1.22", false)
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
	if _, err := Diff(dir, "", "1.99", false); err == nil || !strings.Contains(err.Error(), "newer than the installed toolchain") {
		t.Errorf("want toolchain error, got %v", err)
	}
	if _, err := Diff(dir, "banana", "", false); err == nil {
		t.Error("want error for --from banana")
	}
	data, _ := os.ReadFile(filepath.Join(dir, "go.mod"))
	if string(data) != "module example.com/tmp\n\ngo 1.21\n" {
		t.Errorf("go.mod changed: %q", data)
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

func TestFilterPackages(t *testing.T) {
	r := &Result{
		Binaries: []string{"example.com/app"},
		Changes: []Change{
			{Name: "http", Package: "net/http", ReachedBy: []string{"example.com/app"}},
			{Name: "child", Package: "net/http/httputil", ReachedBy: []string{"example.com/app"}},
			{Name: "tls", Package: "crypto/tls"},
			{Name: "unknown", Package: ""},
		},
		reachCount: 2,
	}
	if err := filterPackages(r, " net/http,crypto/tls,net/http "); err != nil {
		t.Fatal(err)
	}
	if len(r.Changes) != 2 || r.Changes[0].Name != "http" || r.Changes[1].Name != "tls" || r.reachCount != 1 {
		t.Fatalf("filter did not preserve exact matches and recount reach: %+v", r)
	}
	if !reflect.DeepEqual(r.Only, []string{"net/http", "crypto/tls"}) {
		t.Fatalf("filter names = %v", r.Only)
	}
	if err := filterPackages(r, "net"); err != nil || len(r.Changes) != 0 || r.reachCount != 0 {
		t.Fatalf("prefix must not match subpackages: %+v, %v", r, err)
	}
	var output bytes.Buffer
	Print(&output, r)
	if !strings.Contains(output.String(), "No GODEBUG defaults change in the selected packages.") {
		t.Fatalf("empty filtered result needs scoped wording: %s", output.String())
	}
}

func TestFilterPackagesValidation(t *testing.T) {
	for _, input := range []string{",", "net/http,", " ,net/http"} {
		if err := filterPackages(&Result{}, input); err == nil {
			t.Errorf("expected empty package name in %q to be rejected", input)
		}
	}
	r := &Result{Changes: []Change{{Name: "keep"}}, reachCount: 1}
	if err := filterPackages(r, ""); err != nil || len(r.Changes) != 1 || r.reachCount != 1 {
		t.Fatalf("omitted filter changed the result: %+v, %v", r, err)
	}
}
