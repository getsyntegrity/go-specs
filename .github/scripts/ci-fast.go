// Command ci-fast builds affected packages and runs only short, in-process unit tests.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type pkg struct {
	Dir, ImportPath                              string
	GoFiles, CgoFiles, TestGoFiles, XTestGoFiles []string
	Imports, TestImports, XTestImports           []string
	Module                                       *struct{ Dir string }
}

type function struct {
	name         string
	calls        []string
	unsafe, test bool
}

var excluded = regexp.MustCompile(`(?i)(integration|architecture|guard|judge|benchmark|testdata|fixture|e2e|system_test|timing|stress|subprocess|realprocess|allocation_contract)`)

var external = map[string]bool{
	"os/exec": true, "net": true, "net/http/httptest": true,
}

func run(ctx context.Context, dir string, args ...string) []byte {
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir, cmd.Stderr = dir, os.Stderr
	b, err := cmd.Output()
	if err != nil {
		panic(fmt.Errorf("%s: %w", strings.Join(args, " "), err))
	}
	return b
}

func stream(ctx context.Context, dir string, args ...string) {
	fmt.Println("+", strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir, cmd.Stdout, cmd.Stderr = dir, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		panic(err)
	}
}

// Analyze helpers as well as test bodies; a helper that starts a subprocess
// excludes its callers. Imported peer methods and dynamic dispatch require explicit integration naming.
func tests(p pkg) []string {
	functions := map[string][]*function{}
	initialUnsafe := false
	files := append(append(append([]string{}, p.GoFiles...), p.TestGoFiles...), p.XTestGoFiles...)
	for _, name := range files {
		f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(p.Dir, name), nil, 0)
		if err != nil {
			panic(err)
		}
		imports := map[string]string{}
		fileUnsafe := strings.HasSuffix(name, "_test.go") && excluded.MatchString(name)
		for _, i := range f.Imports {
			path, err := strconv.Unquote(i.Path.Value)
			if err != nil {
				panic(err)
			}
			alias := filepath.Base(path)
			if i.Name != nil {
				alias = i.Name.Name
			}
			imports[alias] = path
			if strings.HasSuffix(name, "_test.go") && alias == "." {
				fileUnsafe = true
			}
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok {
				// Package initializers run even when no test is selected. If an
				// initializer calls an external service, omit the whole test binary.
				ast.Inspect(d, func(n ast.Node) bool {
					if s, ok := n.(*ast.SelectorExpr); ok {
						if a, ok := s.X.(*ast.Ident); ok && external[imports[a.Name]] {
							initialUnsafe = true
						}
					}
					return true
				})
				continue
			}
			x := &function{name: fn.Name.Name, unsafe: fileUnsafe || excluded.MatchString(fn.Name.Name)}
			x.test = fn.Recv == nil && strings.HasSuffix(name, "_test.go") && strings.HasPrefix(x.name, "Test") && x.name != "TestMain"
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if c, ok := n.(*ast.CallExpr); ok {
					switch v := c.Fun.(type) {
					case *ast.Ident:
						x.calls = append(x.calls, v.Name)
					case *ast.SelectorExpr:
						if a, ok := v.X.(*ast.Ident); ok {
							if imports[a.Name] == p.ImportPath {
								x.calls = append(x.calls, v.Sel.Name)
							}
							path := imports[a.Name]
							if external[path] || path == "net/http" && v.Sel.Name != "NewRequest" && v.Sel.Name != "NewRequestWithContext" ||
								path == "os" && v.Sel.Name == "StartProcess" ||
								path == "time" && (v.Sel.Name == "Sleep" || v.Sel.Name == "After" || v.Sel.Name == "NewTimer" || v.Sel.Name == "NewTicker") ||
								path == "testing" && v.Sel.Name == "Short" {
								x.unsafe = true
							}
						}
					}
				}
				return true
			})
			functions[x.name] = append(functions[x.name], x)
		}
	}
	for changed := true; changed; {
		changed = false
		for _, fs := range functions {
			for _, f := range fs {
				if f.unsafe {
					continue
				}
				for _, c := range f.calls {
					for _, callee := range functions[c] {
						if callee.unsafe {
							f.unsafe = true
							changed = true
						}
					}
				}
			}
		}
	}
	if initialUnsafe {
		return nil
	}
	for _, name := range []string{"init", "TestMain"} {
		for _, f := range functions[name] {
			if f.unsafe {
				return nil
			}
		}
	}
	var names []string
	for _, fs := range functions {
		for _, f := range fs {
			if f.test && !f.unsafe {
				names = append(names, f.name)
			}
		}
	}
	sort.Strings(names)
	return names
}

func main() {
	defer func() {
		if err := recover(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	root, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	// No ambient instrumentation or caller-provided flags in this CI path.
	os.Setenv("GOFLAGS", "")
	changed := []string{}
	base := os.Getenv("CI_BASE_SHA")
	if base != "" {
		changed = strings.Split(strings.TrimSpace(string(run(ctx, root, "git", "diff", "--name-only", base, "HEAD"))), "\n")
	}
	mods := []string{}
	if _, err := os.Stat("go.work"); err == nil {
		var work struct{ Use []struct{ DiskPath string } }
		if err := json.Unmarshal(run(ctx, root, "go", "work", "edit", "-json"), &work); err != nil {
			panic(err)
		}
		for _, u := range work.Use {
			mods = append(mods, u.DiskPath)
		}
	} else {
		for _, f := range strings.Split(strings.TrimSpace(string(run(ctx, root, "git", "ls-files", "*go.mod"))), "\n") {
			if f != "" && !excluded.MatchString(f) {
				mods = append(mods, filepath.Dir(f))
			}
		}
	}
	if len(mods) == 0 {
		panic("no Go modules found")
	}
	all := map[string]pkg{}
	for _, mod := range mods {
		decoder := json.NewDecoder(bytes.NewReader(run(ctx, mod, "go", "list", "-json", "./...")))
		for {
			var p pkg
			err := decoder.Decode(&p)
			if err == io.EOF {
				break
			}
			if err != nil {
				panic(err)
			}
			if !excluded.MatchString(p.ImportPath) {
				all[p.ImportPath] = p
			}
		}
	}
	selected := map[string]bool{}
	full := base == ""
	for _, f := range changed {
		if f == "" {
			continue
		}
		if strings.HasPrefix(f, ".github/") || strings.HasSuffix(f, "go.mod") || strings.HasSuffix(f, "go.sum") || strings.HasPrefix(f, "go.work") || f == ".go-version" || f == "Makefile" {
			full = true
		}
		abs := filepath.Join(root, filepath.FromSlash(f))
		found := false
		for name, p := range all {
			if filepath.Dir(abs) == p.Dir {
				selected[name] = true
				found = true
			}
		}
		// Fixtures, deletions and other ambiguous code changes require all
		// fast unit packages, never integration or the full local suite.
		if !found && !strings.HasSuffix(f, ".md") {
			full = true
		}
	}
	if full {
		for name := range all {
			selected[name] = true
		}
	}
	for changed := true; changed; {
		changed = false
		for name, p := range all {
			if selected[name] {
				continue
			}
			imports := append(append(append([]string{}, p.Imports...), p.TestImports...), p.XTestImports...)
			for _, dep := range imports {
				if selected[dep] {
					selected[name] = true
					changed = true
					break
				}
			}
		}
	}
	names := []string{}
	for name := range selected {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		fmt.Println("No affected Go packages; not applicable.")
		return
	}
	// Formatting is scoped to affected directories, including their test files.
	for _, name := range names {
		p := all[name]
		files := append(append(append([]string{}, p.GoFiles...), p.TestGoFiles...), p.XTestGoFiles...)
		if len(files) > 0 {
			bad := run(ctx, p.Dir, append([]string{"gofmt", "-l"}, files...)...)
			if len(bytes.TrimSpace(bad)) > 0 {
				panic(fmt.Sprintf("formatting: %s", bad))
			}
		}
	}
	for _, mod := range mods {
		dir, err := filepath.Abs(mod)
		if err != nil {
			panic(err)
		}
		build := []string{}
		for _, name := range names {
			p := all[name]
			if p.Module != nil && p.Module.Dir == dir && len(p.GoFiles)+len(p.CgoFiles) > 0 {
				build = append(build, name)
			}
		}
		if len(build) > 0 {
			stream(ctx, dir, append([]string{"go", "build"}, build...)...)
		}
	}
	for _, name := range names {
		p := all[name]
		ts := tests(p)
		if len(ts) == 0 {
			fmt.Println("No eligible in-process unit tests:", name)
			continue
		}
		fmt.Printf("%s: %d selected unit tests\n", name, len(ts))
		pattern := "^(" + strings.Join(ts, "|") + ")$"
		stream(ctx, p.Module.Dir, "go", "test", "-vet=off", "-timeout=30s", "-run", pattern, name)
	}
}
