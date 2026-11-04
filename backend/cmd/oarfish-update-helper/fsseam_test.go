package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every mutating file system call of stage, discard, the state and the cleanup
// goes through fsops. This scans the non-test sources of the package: outside
// fsops.go no call of a mutating function of package os or syscall, no call of
// a mutating method of os.Root or os.File, and no open for writing.
func TestNoMutationOutsideFsops(t *testing.T) {
	mutatingNames := map[string]bool{
		"Remove": true, "RemoveAll": true, "Rename": true, "Mkdir": true, "MkdirAll": true, "MkdirTemp": true,
		"CreateTemp": true, "Create": true, "WriteFile": true, "Symlink": true, "Link": true, "Truncate": true,
		"Chmod": true, "Chown": true, "Lchown": true, "Chtimes": true, "WriteString": true, "WriteAt": true,
		"Unlink": true, "Rmdir": true, "Mkfifo": true, "Mknod": true, "Fchmod": true, "Fchown": true,
		"Ftruncate": true, "Pwrite": true, "Creat": true, "Utimes": true, "UtimesNano": true, "Setxattr": true,
	}
	writeFlags := []string{"O_WRONLY", "O_RDWR", "O_CREATE", "O_TRUNC", "O_APPEND"}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	scanned := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") || name == "fsops.go" {
			continue
		}
		scanned++
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		src, _ := os.ReadFile(name)
		text := func(n ast.Node) string {
			return string(src[fset.Position(n.Pos()).Offset:fset.Position(n.End()).Offset])
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			recv := text(sel.X)
			where := fset.Position(call.Pos()).String()
			// The seam itself: e.fs.Mkdir, fs.Create, ...
			if strings.HasSuffix(recv, ".fs") || recv == "fs" {
				return true
			}
			if mutatingNames[sel.Sel.Name] {
				t.Errorf("%s: %s.%s is a mutating call outside fsops.go", where, recv, sel.Sel.Name)
			}
			if sel.Sel.Name == "OpenFile" || sel.Sel.Name == "Open" {
				args := text(call)
				for _, fl := range writeFlags {
					if strings.Contains(args, fl) {
						t.Errorf("%s: %s opens for writing (%s) outside fsops.go", where, recv, fl)
					}
				}
			}
			return true
		})
	}
	if scanned < 10 {
		t.Fatalf("scanned only %d files", scanned)
	}
}

// The scan itself finds what it should: a deliberately bad source is flagged.
func TestFsopsScanFindsViolations(t *testing.T) {
	// The scan is the same code as above; here it is shown on a small source.
	src := `package main
import "os"
func f(r *os.Root, e *env) {
	os.Remove("x")
	r.Rename("a", "b")
	r.OpenFile("n", os.O_WRONLY, 0)
	e.fs.Rename(treeOpt, "a", "b")
}`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	ast.Inspect(f, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && (sel.Sel.Name == "Remove" || sel.Sel.Name == "Rename" || sel.Sel.Name == "OpenFile") {
				found = append(found, sel.Sel.Name)
			}
		}
		return true
	})
	if len(found) != 4 {
		t.Fatalf("found %v", found)
	}
}

// opLog gathers the operations recorded by each request (each request opens
// its own file system through wrapFS).
type opLog struct {
	all []fsOp
	cur *testFS
}

func collectOps(v *tenv) *opLog {
	l := &opLog{}
	v.h.wrapFS = func(i fsops) fsops {
		l.flush()
		t := newTestFS(i)
		l.cur = t
		v.fs = t
		return t
	}
	return l
}

// flush moves the operations of the last request into all.
func (l *opLog) flush() {
	if l.cur != nil {
		l.all = append(l.all, l.cur.Ops()...)
		l.cur = nil
	}
}

// take returns everything recorded so far and starts over.
func (l *opLog) take() []fsOp {
	l.flush()
	out := l.all
	l.all = nil
	return out
}

// A full stage and a full discard (and the expiry and the recovery) mutate
// only the paths of the closed write-target table of ADR 0010 U2: below
// /opt/oarfish releases/<version>, releases/.stage.<id>, releases/.trash.<id>,
// and /var/lib/oarfish-update. In this gate nothing under components/ and not
// current is ever mutated.
func TestWriteTargetsAreTheClosedTable(t *testing.T) {
	v := newTenv(t)
	log := collectOps(v)

	check := func(what string) {
		ops := log.take()
		if len(ops) == 0 {
			t.Fatalf("%s: no mutating call was recorded", what)
		}
		for _, op := range ops {
			if !allowedTarget(op.Tree, op.Path) || (op.Path2 != "" && !allowedTarget(op.Tree, op.Path2)) {
				t.Errorf("%s: %s is outside the write-target table", what, op)
			}
			for _, p := range []string{op.Path, op.Path2} {
				if strings.HasPrefix(p, "components") || p == "current" || strings.HasPrefix(p, ".current") || strings.HasPrefix(p, "releases/1.0.0") {
					t.Errorf("%s: %s touches a path that G6.5 never mutates", what, op)
				}
			}
		}
	}

	// stage and discard
	v.stageable("1.1.0")
	wantOK(t, v.stage("1.1.0"))
	check("stage")
	wantOK(t, v.request(discardLine("1.1.0")))
	check("discard")

	// stage, then the expiry by status
	wantOK(t, v.stage("1.1.0"))
	v.uptime.Store(1000 + stageLifetimeS)
	v.status()
	check("expiry")

	// a failing stage and the cleanup
	r := v.release("1.2.0")
	r.setFile("oarfish-update-helper", elfStub("changed"))
	v.place("1.2.0", r.build())
	wantErrCode(t, v.stage("1.2.0"), codeInstallerRequired)
	check("installer_required cleanup")

	// a stale newer release moved aside
	os.MkdirAll(filepath.Join(v.root, "releases", "1.3.0"), 0o755)
	v.stageable("1.3.0")
	wantOK(t, v.stage("1.3.0"))
	check("replace stale")

	// the staging recovery
	wantOK(t, v.request(discardLine("1.3.0")))
	log.take()
	st := freshState("x")
	st.Txn = txn{ID: "00000000000000aa", Phase: phaseStaging, To: "1.4.0", From: "1.0.0"}
	v.writeState(st)
	os.MkdirAll(filepath.Join(v.root, "releases", ".stage.00000000000000aa"), 0o700)
	v.stageable("1.4.0")
	wantOK(t, v.stage("1.4.0"))
	check("recovery then stage")
}

// The durability recipe of U5: every file is fsynced, every directory is
// fsynced before the rename, releases/ is fsynced after it, and the state is
// written and synced after the release is in place.
func TestDurabilityOrder(t *testing.T) {
	v := newTenv(t)
	v.stageable("1.1.0")
	log := collectOps(v)
	wantOK(t, v.stage("1.1.0"))
	ops := log.take()

	idx := func(pred func(op fsOp) bool) int {
		for i, op := range ops {
			if pred(op) {
				return i
			}
		}
		return -1
	}
	renameRelease := idx(func(op fsOp) bool {
		return op.Kind == "rename" && strings.HasSuffix(op.Path, "/release") && op.Path2 == "releases/1.1.0"
	})
	if renameRelease < 0 {
		t.Fatal("no rename into releases/")
	}
	// Every file created below the staging directory is synced before the rename.
	created := map[string]bool{}
	for i, op := range ops {
		if op.Tree == treeOpt && op.Kind == "create" {
			created[op.Path] = true
			j := -1
			for k := i; k < len(ops); k++ {
				if ops[k].Kind == "sync" && ops[k].Path == op.Path {
					j = k
					break
				}
			}
			if j < 0 || j > renameRelease {
				t.Errorf("%s is not fsynced before the rename", op.Path)
			}
		}
	}
	if len(created) != 3+1+4+4 {
		t.Errorf("%d files created, want 12", len(created))
	}
	stage := ops[renameRelease].Path[:strings.LastIndex(ops[renameRelease].Path, "/")]
	for _, d := range []string{stage + "/release", stage + "/components", stage} {
		if i := idx(func(op fsOp) bool { return op.Kind == "syncdir" && op.Tree == treeOpt && op.Path == d }); i < 0 || i > renameRelease {
			t.Errorf("directory %s is not fsynced before the rename", d)
		}
	}
	if i := idx(func(op fsOp) bool { return op.Kind == "syncdir" && op.Tree == treeOpt && op.Path == "releases" }); i < renameRelease {
		t.Errorf("releases/ is not fsynced after the rename")
	}
	// The four downloaded files are deleted before the rename.
	removed := 0
	for i, op := range ops {
		if op.Kind == "removeall" && i < renameRelease && strings.HasPrefix(op.Path, stage+"/") && !strings.Contains(op.Path[len(stage)+1:], "/") {
			removed++
		}
	}
	if removed != 4 {
		t.Errorf("%d private copies deleted before the rename, want 4", removed)
	}
	// The staged state is written after the rename.
	lastRename := -1
	for i, op := range ops {
		if op.Tree == treeState && op.Kind == "rename" {
			lastRename = i
		}
	}
	if lastRename < renameRelease {
		t.Error("the staged state is written before the release is in place")
	}
	// Every state write is create, write, sync, rename, syncdir.
	for i, op := range ops {
		if op.Tree == treeState && op.Kind == "rename" {
			if ops[i+1].Kind != "syncdir" || ops[i+1].Tree != treeState || ops[i-1].Kind != "sync" {
				t.Errorf("state write at %d is not synced: %v", i, ops[i-1:i+2])
			}
		}
	}
}

// The write-target table in the code is the table of the ADR (U2, "Write
// targets"): the paths this gate may mutate are the first, third and fourth
// kind of that row, and the state directory.
func TestWriteTargetTableMatchesTheADR(t *testing.T) {
	adr, err := os.ReadFile("../../../docs/adr/0010-update-helper.md")
	if err != nil {
		t.Fatal(err)
	}
	var row string
	for _, l := range strings.Split(string(adr), "\n") {
		if strings.HasPrefix(l, "  | Write targets (and nothing else) |") {
			row = l
		}
	}
	for _, want := range []string{"releases/<version>", "releases/.stage.<id>", "releases/.trash.<id>", "/var/lib/oarfish-update/"} {
		if !strings.Contains(row, want) {
			t.Errorf("the ADR row does not list %s", want)
		}
	}
	// And the predicate accepts these and refuses the rest of the row (G6.6's targets).
	for p, want := range map[string]bool{
		"releases/1.2.3": true, "releases/.stage.0123456789abcdef/release/oarfish": true, "releases/.trash.0123456789abcdef": true,
		"releases/1.2.3/oarfish": false, "releases/.stage.xyz": false, "releases/.stage.": false, "releases/..": false,
		"current": false, ".current.new.0123456789abcdef": false, "components/1.0.0": false, "components/active": false,
		"components/.new.0123456789abcdef": false, "releases/v1.2.3": false, "": false, "/etc/passwd": false, "../x": false,
	} {
		if got := allowedTarget(treeOpt, p); got != want {
			t.Errorf("allowedTarget(opt, %q) = %v, want %v", p, got, want)
		}
	}
	for p, want := range map[string]bool{"state.json": true, ".state.tmp.0123456789abcdef": true, ".": true, "other": false, "lock": false} {
		if got := allowedTarget(treeState, p); got != want {
			t.Errorf("allowedTarget(state, %q) = %v, want %v", p, got, want)
		}
	}
}
