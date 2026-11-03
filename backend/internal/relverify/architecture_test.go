package relverify

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const modulePath = "github.com/swingstate/oarfish/backend/"

// The package runs in a root process: besides the standard library it may use
// this repository's trustkey package and nothing else (ADR 0010 U1, G6.4).
func TestImports(t *testing.T) {
	allowed := map[string]bool{
		modulePath + "internal/relverify": true,
		modulePath + "internal/trustkey":  true,
	}
	forbidden := []string{"net/http", "os/exec", "plugin", "net", "regexp", "unsafe"}
	goTool := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(goTool); err != nil {
		goTool = "go"
	}
	for _, target := range []struct{ goos, goarch, cgo string }{
		{"linux", "arm64", "0"}, {"linux", "amd64", "0"}, {runtime.GOOS, runtime.GOARCH, "0"}, {runtime.GOOS, runtime.GOARCH, "1"},
	} {
		t.Run(target.goos+"/"+target.goarch+"/cgo"+target.cgo, func(t *testing.T) {
			cmd := exec.Command(goTool, "list", "-deps", "-f", "{{.ImportPath}} {{.Standard}}", ".")
			cmd.Env = append(os.Environ(), "GOOS="+target.goos, "GOARCH="+target.goarch, "CGO_ENABLED="+target.cgo)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("go list: %v\n%s", err, out)
			}
			seen := 0
			for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
				path, std, ok := strings.Cut(line, " ")
				if !ok {
					t.Fatalf("unexpected line %q", line)
				}
				seen++
				if std != "true" && !allowed[path] {
					t.Errorf("the package depends on %s, which is neither the standard library nor an allowed package of this repository", path)
				}
				for _, f := range forbidden {
					// unsafe, net and the like are in the closure of the standard
					// library itself; only a direct import is forbidden (below).
					if path == f && (f == "net/http" || f == "os/exec" || f == "plugin" || f == "regexp") {
						t.Errorf("the package depends on %s", path)
					}
				}
			}
			if seen < 20 {
				t.Fatalf("only %d packages listed: %s", seen, out)
			}
		})
	}
}

// sourceFiles parses the non-test files of the package.
func sourceFiles(t *testing.T) (*token.FileSet, map[string]*ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	matches, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]*ast.File{}
	for _, m := range matches {
		if strings.HasSuffix(m, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, m, nil, parser.ParseComments)
		if err != nil {
			t.Fatal(err)
		}
		files[m] = f
	}
	if len(files) < 8 {
		t.Fatalf("only %d source files", len(files))
	}
	return fset, files
}

// Direct imports: a fixed list of standard-library packages plus trustkey. No
// os (no files, no environment), no time (no clock), no net, no reflect or
// unsafe, no regexp, no fmt (details are literals), no log.
func TestDirectImportsAreAllowListed(t *testing.T) {
	allowed := map[string]bool{
		"archive/tar": true, "bufio": true, "bytes": true, "compress/gzip": true, "crypto/ed25519": true,
		"crypto/sha256": true, "encoding/base64": true, "encoding/hex": true, "encoding/json": true,
		"errors": true, "hash": true, "io": true, "strings": true,
		modulePath + "internal/trustkey": true,
	}
	_, files := sourceFiles(t)
	for name, f := range files {
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			if !allowed[p] {
				t.Errorf("%s imports %s", name, p)
			}
		}
	}
}

// No goroutine, no unsafe, no package-level variable (the package has no
// mutable global state), no panic call, no use of the clock or the environment.
func TestNoGoroutinesGlobalsPanics(t *testing.T) {
	fset, files := sourceFiles(t)
	for name, f := range files {
		for _, d := range f.Decls {
			if g, ok := d.(*ast.GenDecl); ok && g.Tok == token.VAR {
				for _, s := range g.Specs {
					for _, n := range s.(*ast.ValueSpec).Names {
						if n.Name != "_" {
							t.Errorf("%s: package-level variable %s", name, n.Name)
						}
					}
				}
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.GoStmt:
				t.Errorf("%s: goroutine at %s", name, fset.Position(x.Pos()))
			case *ast.CallExpr:
				if id, ok := x.Fun.(*ast.Ident); ok && (id.Name == "panic" || id.Name == "recover") {
					t.Errorf("%s: %s at %s", name, id.Name, fset.Position(x.Pos()))
				}
			}
			return true
		})
	}
}

// The codes this package can return are the ones declared in errors.go; each of
// them is in the closed list of ADR 0010 U2 (read from the ADR, like the
// helper's own cross-check does); every call of fail names a declared code and
// gives a string literal as the detail, so no detail can contain input bytes.
func TestErrorCodesAreInTheADR(t *testing.T) {
	adr, err := os.ReadFile("../../../docs/adr/0010-update-helper.md")
	if err != nil {
		t.Fatal(err)
	}
	var line string
	for _, l := range strings.Split(string(adr), "\n") {
		if strings.Contains(l, "errors are typed with a closed set of codes") {
			line = l
		}
	}
	if line == "" {
		t.Fatal("the sentence on the error codes is not in the ADR")
	}
	line, _, _ = strings.Cut(line, "An error never echoes")
	inADR := map[string]bool{}
	for _, m := range regexp.MustCompile("`([a-z_]+)`").FindAllStringSubmatch(line, -1) {
		inADR[m[1]] = true
	}
	if len(inADR) != 21 {
		t.Fatalf("read %d codes from the ADR, want 21", len(inADR))
	}

	_, files := sourceFiles(t)
	declared := map[string]string{} // constant name -> code string
	for _, d := range files["errors.go"].Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok || g.Tok != token.CONST {
			continue
		}
		for _, s := range g.Specs {
			vs := s.(*ast.ValueSpec)
			if id, ok := vs.Type.(*ast.Ident); !ok || id.Name != "Code" {
				continue
			}
			for i, n := range vs.Names {
				lit := vs.Values[i].(*ast.BasicLit)
				v, _ := strconv.Unquote(lit.Value)
				declared[n.Name] = v
			}
		}
	}
	if len(declared) < 8 {
		t.Fatalf("only %d codes declared", len(declared))
	}
	var names []string
	for n, code := range declared {
		names = append(names, n)
		if !inADR[code] {
			t.Errorf("%s = %q is not in the list of ADR 0010 U2", n, code)
		}
		if txt := Text(Code(code)); txt == "" || !strings.HasSuffix(txt, ".") {
			t.Errorf("code %s has no fixed text", code)
		}
	}
	sort.Strings(names)

	calls := 0
	for name, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			c, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if id, ok := c.Fun.(*ast.Ident); !ok || id.Name != "fail" {
				return true
			}
			calls++
			if len(c.Args) != 2 {
				t.Errorf("%s: fail with %d arguments", name, len(c.Args))
				return true
			}
			if id, ok := c.Args[0].(*ast.Ident); !ok || declared[id.Name] == "" {
				t.Errorf("%s: fail with a code that is not a declared constant", name)
			}
			if lit, ok := c.Args[1].(*ast.BasicLit); !ok || lit.Kind != token.STRING {
				t.Errorf("%s: fail with a detail that is not a string literal", name)
			}
			return true
		})
	}
	if calls < 60 {
		t.Fatalf("only %d calls of fail found; the check is not looking at the code", calls)
	}
	// The fixed texts are the ones of the helper protocol.
	helper, err := os.ReadFile("../../cmd/oarfish-update-helper/protocol.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		if txt := Text(Code(declared[n])); !strings.Contains(string(helper), `"`+txt+`"`) {
			t.Errorf("the text of %s differs from the helper's protocol text", declared[n])
		}
	}
}

// Text of an unknown code is the internal-error text; Error and Detail are
// separate.
func TestErrorType(t *testing.T) {
	e := fail(CodeChecksumMismatch, "some detail")
	if !strings.HasPrefix(e.Error(), "checksum_mismatch: ") || strings.Contains(e.Error(), "some detail") {
		t.Fatal(e.Error())
	}
	if e.Detail() != "some detail" {
		t.Fatal(e.Detail())
	}
	if Text("nonsense") != Text(CodeInternal) {
		t.Fatal("unknown code")
	}
}
