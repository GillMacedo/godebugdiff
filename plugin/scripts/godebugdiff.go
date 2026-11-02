// Command godebugdiff lists the GODEBUG defaults that change when you move
// the go line in go.mod, and which of your binaries import the packages
// each setting controls.
//
// It asks the installed Go toolchain: it copies go.mod to a temporary file
// for each version, runs `go list -modfile` on both with the network off,
// and compares each main package's DefaultGODEBUG. It never edits go.mod,
// never builds or runs your code, and never uses the network.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const version = "0.1.0"

const usage = `usage: godebugdiff [flags] [DIR]

Lists GODEBUG defaults that change when the go line in DIR/go.mod moves
from --from to --to, and which binaries import the affected packages.

  --from VERSION  go line to compare from (default: the one in go.mod)
  --to VERSION    go line to compare to (default: the installed toolchain)
  --only PACKAGES comma-separated exact package names to include
  --tests         also check test binaries (always on for modules with no main package)
  --json          print the result as JSON
  --fail          exit 1 when a change reaches a package a binary imports
  --version       print the version

Exit codes: 0 done, 1 --fail and a change reaches your code, 2 error.
`

// Setting describes one GODEBUG setting, from the toolchain's own table.
type Setting struct {
	Name    string `json:"name"`
	Package string `json:"package"`
	Changed int    `json:"changed,omitempty"` // minor version when the default changed
	Old     string `json:"old,omitempty"`
}

// Binary is one main package (or test binary) and its default GODEBUG.
type Binary struct {
	ImportPath string            `json:"importPath"`
	Godebug    map[string]string `json:"-"`
	Deps       map[string]bool   `json:"-"`
	Error      string            `json:"error,omitempty"`
}

// Change is one GODEBUG setting whose default differs between the two go lines.
type Change struct {
	Name      string   `json:"name"`
	Package   string   `json:"package"`
	Changed   int      `json:"changedIn,omitempty"`
	Before    string   `json:"before"` // value, or "" for the toolchain default
	After     string   `json:"after"`
	ReachedBy []string `json:"reachedBy"` // binaries that import Package
	GoCommand bool     `json:"goCommand"` // the setting controls the go command, not binaries
	Doc       string   `json:"doc,omitempty"`
	URL       string   `json:"url,omitempty"`
}

// Result is the full comparison.
type Result struct {
	Module     string   `json:"module"`
	Dir        string   `json:"dir"`
	Toolchain  string   `json:"toolchain"`
	From       string   `json:"from"`
	To         string   `json:"to"`
	Binaries   []string `json:"binaries"`
	TestsOnly  bool     `json:"testsOnly"`
	Changes    []Change `json:"changes"`
	Notes      []string `json:"notes,omitempty"`
	Only       []string `json:"only,omitempty"`
	reachCount int
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("godebugdiff", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, usage) }
	from := fs.String("from", "", "")
	to := fs.String("to", "", "")
	tests := fs.Bool("tests", false, "")
	only := fs.String("only", "", "")
	asJSON := fs.Bool("json", false, "")
	fail := fs.Bool("fail", false, "")
	showVersion := fs.Bool("version", false, "")
	if err := fs.Parse(reorder(args)); err != nil {
		return 2
	}
	if *showVersion {
		fmt.Fprintln(stdout, "godebugdiff", version)
		return 0
	}
	if fs.NArg() > 1 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	dir := "."
	if fs.NArg() == 1 {
		dir = fs.Arg(0)
	}
	res, err := Diff(dir, *from, *to, *tests)
	if err != nil {
		fmt.Fprintln(stderr, "godebugdiff:", err)
		return 2
	}
	if err := filterPackages(res, *only); err != nil {
		fmt.Fprintln(stderr, "godebugdiff:", err)
		return 2
	}
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		enc.Encode(res)
	} else {
		Print(stdout, res)
	}
	if *fail && res.reachCount > 0 {
		return 1
	}
	return 0
}

// filterPackages applies the same exact-package selection to text, JSON and --fail.
func filterPackages(r *Result, only string) error {
	if only == "" {
		return nil
	}
	selected := make(map[string]bool)
	var packages []string
	for _, name := range strings.Split(only, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			return fmt.Errorf("--only requires comma-separated non-empty package names")
		}
		if !selected[name] {
			packages = append(packages, name)
			selected[name] = true
		}
	}
	r.Only = packages
	changes := make([]Change, 0)
	r.reachCount = 0
	for _, c := range r.Changes {
		if selected[c.Package] {
			changes = append(changes, c)
			if len(c.ReachedBy) > 0 {
				r.reachCount++
			}
		}
	}
	r.Changes = changes
	return nil
}

// reorder moves flags before the directory argument, so
// `godebugdiff DIR --to 1.24` works like `godebugdiff --to 1.24 DIR`.
func reorder(args []string) []string {
	var flags, rest []string
	valued := map[string]bool{"from": true, "to": true, "only": true}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			rest = append(rest, args[i+1:]...)
			break
		}
		if strings.HasPrefix(a, "-") && a != "-" {
			flags = append(flags, a)
			name := strings.TrimLeft(a, "-")
			if valued[name] && i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
			continue
		}
		rest = append(rest, a)
	}
	return append(flags, rest...)
}

var versionRE = regexp.MustCompile(`^(?:go)?1\.(\d+)(?:\.(\d+))?(?:rc\d+)?$`)

// minor returns 24 for "1.24", "1.24.3", "go1.24.3" and "go1.24rc1".
func minor(v string) (int, error) {
	m := versionRE.FindStringSubmatch(strings.TrimSpace(v))
	if m == nil {
		return 0, fmt.Errorf("%q is not a Go version like 1.24", v)
	}
	return strconv.Atoi(m[1])
}

// normalize turns "go1.24.3" or "1.24" into the form go.mod uses.
func normalize(v string) string {
	return strings.TrimPrefix(strings.TrimSpace(v), "go")
}

// Diff compares the default GODEBUG of every binary in dir between two go lines.
func Diff(dir, from, to string, tests bool) (*Result, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	gomod := filepath.Join(abs, "go.mod")
	if _, err := os.Stat(gomod); err != nil {
		return nil, fmt.Errorf("no go.mod in %s. Run it from a module root or pass the directory", abs)
	}
	modInfo, err := readModFile(abs, gomod)
	if err != nil {
		return nil, err
	}
	goroot, toolchain, err := goEnv(abs)
	if err != nil {
		return nil, err
	}
	res := &Result{Module: modInfo.Module.Path, Dir: dir, Toolchain: toolchain}
	if from == "" {
		from = modInfo.Go
	}
	if to == "" {
		to = strings.TrimPrefix(toolchain, "go")
		if i := strings.IndexAny(to, " -"); i >= 0 {
			to = to[:i]
		}
		if m, err := minor(to); err == nil {
			to = fmt.Sprintf("1.%d", m)
		}
	}
	from, to = normalize(from), normalize(to)
	if _, err := minor(from); err != nil {
		return nil, fmt.Errorf("--from: %v", err)
	}
	toMinor, err := minor(to)
	if err != nil {
		return nil, fmt.Errorf("--to: %v", err)
	}
	if tm, err := minor(toolchain); err == nil && toMinor > tm {
		return nil, fmt.Errorf("--to %s is newer than the installed toolchain %s, which only knows settings up to its own version. Install Go %s or newer first", to, toolchain, to)
	}
	res.From, res.To = from, to

	tmp, err := os.MkdirTemp("", "godebugdiff")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)

	var updated []string
	list := func(name, v string, tests bool) ([]Binary, error) {
		bins, up, err := listAt(abs, gomod, filepath.Join(tmp, name), v, tests)
		if err != nil {
			return nil, fmt.Errorf("with go %s: %v", v, err)
		}
		if up {
			updated = append(updated, v)
		}
		return bins, nil
	}
	before, err := list("from", from, tests)
	if err != nil {
		return nil, err
	}
	if !hasBinaries(before) && !tests {
		tests = true
		res.TestsOnly = true
		updated = nil
		if before, err = list("from", from, true); err != nil {
			return nil, err
		}
	}
	after, err := list("to", to, tests)
	if err != nil {
		return nil, err
	}
	if len(updated) > 0 {
		res.Notes = append(res.Notes, "With go "+strings.Join(updated, " and ")+", the go command needed to add requirements to go.mod, as `go mod tidy` would. godebugdiff did that in a temporary copy from the module cache, so expect go.mod changes with the bump.")
	}

	table := loadTable(goroot)
	docs := loadDocs(goroot)
	res.Changes, res.Binaries = compare(before, after, table, docs)
	for _, c := range res.Changes {
		if len(c.ReachedBy) > 0 {
			res.reachCount++
		}
	}
	if res.TestsOnly {
		res.Notes = append(res.Notes, "This module has no main package, so these are its test binaries. A library's go line doesn't set GODEBUG for programs that import it: they use their own go.mod. Raising it does make those programs need Go "+to+" or newer, which raises their go line too.")
	}
	fromMinor, _ := minor(from)
	if fromMinor < 22 && toMinor >= 22 {
		res.Notes = append(res.Notes, "Crossing Go 1.22 also changes the language: each loop iteration gets its own loop variable in this module's code. That isn't a GODEBUG setting, so it isn't listed above. See https://go.dev/blog/loopvar-preview.")
	}
	if modInfo.Toolchain != "" {
		if tm, err := minor(modInfo.Toolchain); err == nil && tm < toMinor {
			res.Notes = append(res.Notes, fmt.Sprintf("go.mod has \"toolchain %s\", which is older than go %s. Go ignores a toolchain line older than the go line, so remove or raise it with the bump.", modInfo.Toolchain, to))
		}
	}
	if gw := goworkPath(abs); gw != "" {
		res.Notes = append(res.Notes, "A go.work file is active ("+gw+"). In workspace mode, the go line in go.work sets GODEBUG defaults, not go.mod. These results ignore go.work.")
	}
	if len(modInfo.Godebug) > 0 {
		var pins []string
		for _, g := range modInfo.Godebug {
			pins = append(pins, g.Key+"="+g.Value)
		}
		res.Notes = append(res.Notes, "go.mod already pins "+strings.Join(pins, ", ")+". Pinned settings don't change with the go line, so they aren't listed.")
	}
	return res, nil
}

type modFile struct {
	Module    struct{ Path string }
	Go        string
	Toolchain string
	Godebug   []struct{ Key, Value string }
}

func readModFile(dir, gomod string) (*modFile, error) {
	out, err := goCmd(dir, "mod", "edit", "-json", gomod)
	if err != nil {
		return nil, err
	}
	var m modFile
	if err := json.Unmarshal(out, &m); err != nil {
		return nil, fmt.Errorf("reading go mod edit -json: %v", err)
	}
	if m.Go == "" {
		m.Go = "1.16" // go.mod files with no go line are treated as Go 1.16
	}
	return &m, nil
}

func goEnv(dir string) (goroot, version string, err error) {
	out, err := goCmd(dir, "env", "GOROOT", "GOVERSION")
	if err != nil {
		return "", "", err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return "", "", fmt.Errorf("unexpected go env output: %q", out)
	}
	return strings.TrimSpace(lines[0]), strings.TrimSpace(lines[1]), nil
}

func goworkPath(dir string) string {
	cmd := exec.Command("go", "env", "GOWORK")
	cmd.Dir = dir
	cmd.Env = baseEnv(false)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	p := strings.TrimSpace(string(out))
	if p == "off" {
		return ""
	}
	return p
}

// baseEnv keeps the go command offline and on the installed toolchain.
func baseEnv(workOff bool) []string {
	env := append(os.Environ(), "GOTOOLCHAIN=local", "GOPROXY=off", "GOFLAGS="+keepFlags(os.Getenv("GOFLAGS")))
	if workOff {
		env = append(env, "GOWORK=off")
	}
	return env
}

// keepFlags drops -modfile and -mod=mod from GOFLAGS, since this tool picks
// its own go.mod copy and must not update it.
func keepFlags(goflags string) string {
	var kept []string
	for _, f := range strings.Fields(goflags) {
		if strings.HasPrefix(f, "-modfile") || strings.HasPrefix(f, "--modfile") || f == "-mod=mod" || f == "--mod=mod" {
			continue
		}
		kept = append(kept, f)
	}
	return strings.Join(kept, " ")
}

func goCmd(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = baseEnv(true)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return out, errors.New(msg)
	}
	return out, nil
}

// listAt copies go.mod and go.sum, sets the go line, and lists binaries.
// When the new go line needs more requirements in go.mod, which happens
// when crossing Go 1.17, it lets the go command add them to the copy.
func listAt(dir, gomod, tmpBase, goVersion string, tests bool) ([]Binary, bool, error) {
	mod, err := copyMod(dir, gomod, tmpBase, goVersion)
	if err != nil {
		return nil, false, err
	}
	out, err := goCmd(dir, listArgs(mod, tests, false)...)
	if err != nil && strings.Contains(err.Error(), "updates to go.mod needed") {
		if mod, err = copyMod(dir, gomod, tmpBase, goVersion); err != nil {
			return nil, false, err
		}
		out, err = goCmd(dir, listArgs(mod, tests, true)...)
		if err != nil {
			return nil, false, cleanErr(err)
		}
		bins, err := parseList(out, tests)
		return bins, true, err
	}
	if err != nil {
		return nil, false, cleanErr(err)
	}
	bins, err := parseList(out, tests)
	return bins, false, err
}

func copyMod(dir, gomod, tmpBase, goVersion string) (string, error) {
	data, err := os.ReadFile(gomod)
	if err != nil {
		return "", err
	}
	mod := tmpBase + ".mod"
	if err := os.WriteFile(mod, data, 0o600); err != nil {
		return "", err
	}
	sum, err := os.ReadFile(filepath.Join(dir, "go.sum"))
	if err != nil {
		sum = nil
	}
	if err := os.WriteFile(tmpBase+".sum", sum, 0o600); err != nil {
		return "", err
	}
	if _, err := goCmd(dir, "mod", "edit", "-go="+goVersion, mod); err != nil {
		return "", err
	}
	return mod, nil
}

func listArgs(mod string, tests, update bool) []string {
	args := []string{"list", "-e", "-modfile=" + mod, "-json=ImportPath,Name,DefaultGODEBUG,Deps,Error,DepsErrors,ForTest"}
	if update {
		args = append(args, "-mod=mod")
	}
	if tests {
		args = append(args, "-test")
	}
	return append(args, "./...")
}

var tmpModRE = regexp.MustCompile(`[^\s:]*godebugdiff[0-9]+[/\\](?:from|to)\.mod`)

// cleanErr replaces the temporary go.mod path in toolchain errors.
func cleanErr(err error) error {
	msg := tmpModRE.ReplaceAllString(err.Error(), "go.mod")
	if strings.Contains(msg, "GOPROXY=off") || strings.Contains(msg, "module lookup disabled") {
		msg += "\nSome modules aren't in the module cache. Run `go mod download` first. godebugdiff never downloads."
	}
	return errors.New(msg)
}

type listPkg struct {
	ImportPath     string
	Name           string
	DefaultGODEBUG string
	Deps           []string
	ForTest        string
	Error          *struct{ Err string }
	DepsErrors     []struct{ Err string }
}

func parseList(out []byte, tests bool) ([]Binary, error) {
	dec := json.NewDecoder(bytes.NewReader(out))
	var bins []Binary
	for {
		var p listPkg
		if err := dec.Decode(&p); err == io.EOF {
			break
		} else if err != nil {
			return nil, fmt.Errorf("reading go list output: %v", err)
		}
		if p.Name != "main" {
			continue
		}
		isTest := strings.HasSuffix(p.ImportPath, ".test")
		if p.ForTest != "" && !isTest {
			continue // a test variant of a main package; its .test binary is listed
		}
		if isTest && !tests {
			continue
		}
		b := Binary{ImportPath: p.ImportPath, Godebug: parseGodebug(p.DefaultGODEBUG), Deps: map[string]bool{}}
		for _, d := range p.Deps {
			b.Deps[d] = true
		}
		if p.Error != nil {
			b.Error = p.Error.Err
		} else if len(p.DepsErrors) > 0 {
			b.Error = "an import couldn't be loaded: " + p.DepsErrors[0].Err
		}
		bins = append(bins, b)
	}
	sort.Slice(bins, func(i, j int) bool { return bins[i].ImportPath < bins[j].ImportPath })
	// A binary whose imports didn't load has an incomplete import list, so
	// "imported by" could miss it. Stop rather than report too little.
	var failed []string
	for _, b := range bins {
		if b.Error != "" {
			failed = append(failed, b.ImportPath+": "+b.Error)
		}
	}
	if len(failed) > 0 {
		msg := strings.Join(failed[:min(len(failed), 3)], "\n")
		if len(failed) > 3 {
			msg += fmt.Sprintf("\nand %d more", len(failed)-3)
		}
		if strings.Contains(msg, "GOPROXY=off") || strings.Contains(msg, "module lookup disabled") {
			msg += "\nSome modules aren't in the module cache. Run `go mod download` first. godebugdiff never downloads."
		}
		return nil, errors.New(msg)
	}
	return bins, nil
}

func hasBinaries(bins []Binary) bool { return len(bins) > 0 }

func parseGodebug(s string) map[string]string {
	m := map[string]string{}
	for _, kv := range strings.Split(s, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(kv), "=")
		if ok && k != "" {
			m[k] = v
		}
	}
	return m
}

func compare(before, after []Binary, table map[string]Setting, docs map[string][]docEntry) ([]Change, []string) {
	afterBy := map[string]Binary{}
	for _, b := range after {
		afterBy[b.ImportPath] = b
	}
	byName := map[string]*Change{}
	var names []string
	for _, b := range before {
		a, ok := afterBy[b.ImportPath]
		if !ok {
			continue
		}
		names = append(names, b.ImportPath)
		keys := map[string]bool{}
		for k := range b.Godebug {
			keys[k] = true
		}
		for k := range a.Godebug {
			keys[k] = true
		}
		for k := range keys {
			bv, av := b.Godebug[k], a.Godebug[k]
			if bv == av {
				continue
			}
			c := byName[k]
			if c == nil {
				s := table[k]
				c = &Change{Name: k, Package: s.Package, Changed: s.Changed, Before: bv, After: av, ReachedBy: []string{}}
				c.GoCommand = strings.HasPrefix(s.Package, "cmd/")
				if d, ok := pickDoc(docs[k], s.Changed); ok {
					c.Doc, c.URL = d.Text, d.URL
					if c.Changed == 0 {
						c.Changed = d.Minor
					}
				}
				if c.URL == "" && c.Changed > 0 {
					c.URL = fmt.Sprintf("https://go.dev/doc/godebug#go-1%d", c.Changed)
				}
				byName[k] = c
			}
			if c.Package != "" && !c.GoCommand && (a.Deps[c.Package] || b.Deps[c.Package]) {
				c.ReachedBy = append(c.ReachedBy, b.ImportPath)
			}
		}
	}
	var changes []Change
	for _, c := range byName {
		changes = append(changes, *c)
	}
	sort.Slice(changes, func(i, j int) bool {
		if changes[i].Changed != changes[j].Changed {
			return changes[i].Changed < changes[j].Changed
		}
		return changes[i].Name < changes[j].Name
	})
	return changes, names
}

var tableRE = regexp.MustCompile(`^\s*\{Name:\s*"([^"]+)",\s*Package:\s*"([^"]+)"(.*)\},?`)
var changedRE = regexp.MustCompile(`Changed:\s*(\d+)`)
var oldRE = regexp.MustCompile(`Old:\s*"([^"]*)"`)

// loadTable reads the toolchain's own GODEBUG table from GOROOT. It's an
// internal file, so a missing or changed one only loses package names.
func loadTable(goroot string) map[string]Setting {
	f, err := os.Open(filepath.Join(goroot, "src", "internal", "godebugs", "table.go"))
	if err != nil {
		return map[string]Setting{}
	}
	defer f.Close()
	return parseTable(f)
}

func parseTable(r io.Reader) map[string]Setting {
	t := map[string]Setting{}
	sc := bufio.NewScanner(r)
	inAll := false
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "var All = ") {
			inAll = true
			continue
		}
		if inAll && strings.HasPrefix(line, "}") {
			break
		}
		if !inAll {
			continue
		}
		m := tableRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		s := Setting{Name: m[1], Package: m[2]}
		if c := changedRE.FindStringSubmatch(m[3]); c != nil {
			s.Changed, _ = strconv.Atoi(c[1])
		}
		if o := oldRE.FindStringSubmatch(m[3]); o != nil {
			s.Old = o[1]
		}
		t[s.Name] = s
	}
	return t
}

type docEntry struct {
	Minor int
	Text  string
	URL   string
}

var sectionRE = regexp.MustCompile(`^### Go 1\.(\d+)\s*$`)
var linkRE = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)

// loadDocs reads doc/godebug.md from GOROOT and keeps, for each setting and
// release section, the first sentence of the first paragraph that names it.
func loadDocs(goroot string) map[string][]docEntry {
	data, err := os.ReadFile(filepath.Join(goroot, "doc", "godebug.md"))
	if err != nil {
		return map[string][]docEntry{}
	}
	return parseDocs(string(data))
}

// pickDoc prefers the paragraph in the release section where the default
// changed, and otherwise takes the first one, which is the newest release.
func pickDoc(entries []docEntry, changed int) (docEntry, bool) {
	for _, e := range entries {
		if e.Minor == changed {
			return e, true
		}
	}
	if len(entries) > 0 {
		return entries[0], true
	}
	return docEntry{}, false
}

func parseDocs(md string) map[string][]docEntry {
	docs := map[string][]docEntry{}
	minorV := 0
	var para []string
	flush := func() {
		if minorV == 0 || len(para) == 0 {
			para = nil
			return
		}
		text := strings.Join(para, " ")
		para = nil
		seen := map[string]bool{}
		for _, name := range settingNames(text) {
			if seen[name] {
				continue
			}
			seen[name] = true
			if list := docs[name]; len(list) > 0 && list[len(list)-1].Minor == minorV {
				continue // keep the first paragraph in each section
			}
			docs[name] = append(docs[name], docEntry{Minor: minorV, Text: firstSentence(text), URL: fmt.Sprintf("https://go.dev/doc/godebug#go-1%d", minorV)})
		}
	}
	for _, line := range strings.Split(md, "\n") {
		if strings.HasPrefix(line, "#") {
			flush()
			if m := sectionRE.FindStringSubmatch(line); m != nil {
				minorV, _ = strconv.Atoi(m[1])
			} else {
				minorV = 0
			}
			continue
		}
		if strings.TrimSpace(line) == "" {
			flush()
			continue
		}
		para = append(para, strings.TrimSpace(line))
	}
	flush()
	return docs
}

var nameInDocRE = regexp.MustCompile("`([a-z0-9]+)(?:=[^`]*)?` setting|`([a-z0-9]+)=")

func settingNames(text string) []string {
	var names []string
	for _, m := range nameInDocRE.FindAllStringSubmatch(text, -1) {
		n := m[1]
		if n == "" {
			n = m[2]
		}
		names = append(names, n)
	}
	return names
}

func firstSentence(text string) string {
	text = linkRE.ReplaceAllString(text, "$1")
	text = strings.ReplaceAll(text, "`", "")
	text = strings.Join(strings.Fields(text), " ")
	for i := 0; i+1 < len(text); i++ {
		if text[i] == '.' && text[i+1] == ' ' && i > 0 && text[i-1] != '.' {
			// Skip abbreviations such as "e.g." and version numbers.
			prev := text[:i]
			if strings.HasSuffix(prev, "e.g") || strings.HasSuffix(prev, "i.e") {
				continue
			}
			return text[:i+1]
		}
	}
	return text
}

func value(v string) string {
	if v == "" {
		return "default"
	}
	return v
}

// Print writes the human-readable report.
func Print(w io.Writer, r *Result) {
	kind := "binaries"
	if r.TestsOnly {
		kind = "test binaries"
	}
	count := kind
	if len(r.Binaries) == 1 {
		count = strings.TrimSuffix(kind, "aries") + "ary"
	}
	fmt.Fprintf(w, "go %s -> %s in %s (%s, %d %s)\n", r.From, r.To, r.Module, r.Toolchain, len(r.Binaries), count)
	if len(r.Only) > 0 {
		fmt.Fprintf(w, "Packages: %s (exact matches; other changes omitted)\n", strings.Join(r.Only, ", "))
	}
	if len(r.Binaries) == 0 {
		fmt.Fprintf(w, "\nNo main packages or tests found, so there's nothing to compare.\n")
		printNotes(w, r)
		return
	}
	if len(r.Changes) == 0 {
		if len(r.Only) > 0 {
			fmt.Fprintf(w, "\nNo GODEBUG defaults change in the selected packages.\n")
		} else {
			fmt.Fprintf(w, "\nNo GODEBUG defaults change.\n")
		}
		printNotes(w, r)
		return
	}
	var reached, unreached, gocmd []Change
	for _, c := range r.Changes {
		switch {
		case c.GoCommand:
			gocmd = append(gocmd, c)
		case len(c.ReachedBy) > 0:
			reached = append(reached, c)
		default:
			unreached = append(unreached, c)
		}
	}
	fmt.Fprintf(w, "\n%d GODEBUG %s change. %d %s a package your %s import.\n",
		len(r.Changes), plural(len(r.Changes), "default", "defaults"),
		len(reached), plural(len(reached), "reaches", "reach"), kind)
	for _, c := range reached {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "%s  %s -> %s  %s, %s\n", c.Name, value(c.Before), value(c.After), changedIn(c), c.Package)
		fmt.Fprintf(w, "    imported by %s\n", who(c.ReachedBy, len(r.Binaries), kind))
		if c.Doc != "" {
			fmt.Fprintf(w, "    %s\n", c.Doc)
		}
		if c.URL != "" {
			fmt.Fprintf(w, "    %s\n", c.URL)
		}
	}
	if len(gocmd) > 0 {
		fmt.Fprintf(w, "\nChanges the go command, not your %s:\n", kind)
		for _, c := range gocmd {
			fmt.Fprintf(w, "  %s  %s -> %s  %s\n", c.Name, value(c.Before), value(c.After), changedIn(c))
			if c.Doc != "" {
				fmt.Fprintf(w, "    %s\n", c.Doc)
			}
		}
	}
	if len(unreached) > 0 {
		fmt.Fprintf(w, "\nNo %s imports the package, so no effect:\n", strings.TrimSuffix(kind, "aries")+"ary")
		for _, c := range unreached {
			pkg := c.Package
			if pkg == "" {
				pkg = "package unknown"
			}
			fmt.Fprintf(w, "  %s (%s)\n", c.Name, pkg)
		}
	}
	var pins []string
	for _, c := range append(reached, gocmd...) {
		if c.Before != "" && c.After == "" {
			pins = append(pins, c.Name+"="+c.Before)
		}
	}
	if len(pins) > 0 {
		fmt.Fprintf(w, "\nTo keep the old behavior of a setting after the bump, pin it in go.mod:\n\n    godebug (\n")
		for _, p := range pins {
			fmt.Fprintf(w, "        %s\n", p)
		}
		fmt.Fprintf(w, "    )\n")
	}
	printNotes(w, r)
}

func printNotes(w io.Writer, r *Result) {
	for _, n := range r.Notes {
		fmt.Fprintf(w, "\nNote: %s\n", n)
	}
}

func changedIn(c Change) string {
	if c.Changed == 0 {
		return "version unknown"
	}
	return fmt.Sprintf("Go 1.%d", c.Changed)
}

func who(bins []string, total int, kind string) string {
	if len(bins) == total && total > 1 {
		return fmt.Sprintf("all %d %s", total, kind)
	}
	if len(bins) > 4 {
		return strings.Join(bins[:4], ", ") + fmt.Sprintf(" and %d more", len(bins)-4)
	}
	return strings.Join(bins, ", ")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
