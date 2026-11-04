package relverify

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/swingstate/oarfish/backend/internal/trustkey"
)

// ---- throwaway keys -------------------------------------------------------
//
// Every key in these tests is derived from a label and exists only here. The
// embedded release key is used by the golden test alone.

type testKey struct {
	priv ed25519.PrivateKey
	id   [8]byte // key id in file order
	key  trustkey.Key
}

func newTestKey(t testing.TB, label string) testKey {
	t.Helper()
	seed := sha256.Sum256([]byte("relverify test key seed " + label))
	idSrc := sha256.Sum256([]byte("relverify test key id " + label))
	var id [8]byte
	copy(id[:], idSrc[:8])
	return testKeyWithID(t, seed, id)
}

func testKeyWithID(t testing.TB, seed [32]byte, id [8]byte) testKey {
	t.Helper()
	priv := ed25519.NewKeyFromSeed(seed[:])
	payload := append(append([]byte("Ed"), id[:]...), priv.Public().(ed25519.PublicKey)...)
	file := "untrusted comment: minisign public key test\n" + base64.StdEncoding.EncodeToString(payload) + "\n"
	k, err := trustkey.Parse([]byte(file))
	if err != nil {
		t.Fatal(err)
	}
	return testKey{priv: priv, id: id, key: k}
}

// sigFile builds a signature file of the minisign layout around payload.
func sigFile(payload []byte, trusted string) []byte {
	global := make([]byte, 64)
	for i := range global {
		global[i] = byte(i)
	}
	return []byte("untrusted comment: signature from minisign secret key\n" +
		base64.StdEncoding.EncodeToString(payload) + "\n" +
		"trusted comment: " + trusted + "\n" +
		base64.StdEncoding.EncodeToString(global) + "\n")
}

func (k testKey) payload(algo string, msg []byte) []byte {
	p := append([]byte(algo), k.id[:]...)
	return append(p, ed25519.Sign(k.priv, msg)...)
}

// sign returns a legacy-mode signature file over msg.
func (k testKey) sign(msg []byte) []byte { return sigFile(k.payload("Ed", msg), "oarfish test") }

// ---- release fixtures -----------------------------------------------------

const testCommit = "0123456789abcdef0123456789abcdef01234567"

// elfStub is a 64-byte header of an ELF64 little-endian aarch64 executable.
func elfStub(label string) []byte {
	b := make([]byte, 64, 64+len(label))
	copy(b, "\x7fELF\x02\x01\x01")
	b[16], b[18] = 2, 0xb7
	return append(b, label...)
}

type tf struct {
	name string
	mode int64
	data []byte
}

func testFiles(version string, components bool) []tf {
	fs := []tf{
		{"LICENSE", 0o644, []byte("licence text\n")},
		{"THIRD_PARTY_NOTICES.txt", 0o644, []byte("notices\n")},
		{"oarfish", 0o755, elfStub("oarfish " + version)},
		{"oarfish.service", 0o644, []byte("[Service]\nExecStart=/opt/oarfish/current/oarfish\n")},
	}
	if components {
		fs = append(fs,
			tf{"oarfish-update-helper", 0o755, elfStub("helper " + version)},
			tf{"oarfish-update-helper.service", 0o644, []byte("[Service]\n# helper\n")},
			tf{"oarfish-update-helper.socket", 0o644, []byte("[Socket]\n# socket\n")},
			tf{"oarfish-update-recover.service", 0o644, []byte("[Service]\n# recover\n")},
		)
	}
	sort.Slice(fs, func(i, j int) bool { return fs[i].name < fs[j].name })
	return fs
}

type tarEnt struct {
	name string
	typ  byte
	body []byte
	link string
	mode int64
	uid  int
	gid  int
}

// makeTar writes a tar stream the way bundlegen does, except for what the
// entries say.
func makeTar(ents []tarEnt) []byte {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, e := range ents {
		typ := e.typ
		if typ == 0 {
			typ = tar.TypeReg
		}
		h := &tar.Header{Typeflag: typ, Name: e.name, Mode: e.mode, Linkname: e.link, Uid: e.uid, Gid: e.gid}
		if typ == tar.TypeReg {
			h.Size = int64(len(e.body))
		}
		if err := tw.WriteHeader(h); err != nil {
			panic(err)
		}
		if typ == tar.TypeReg {
			if _, err := tw.Write(e.body); err != nil {
				panic(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func gz(b []byte) []byte {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Write(b)
	zw.Close()
	return buf.Bytes()
}

func defaultEnts(root string, files []tf) []tarEnt {
	ents := []tarEnt{{name: root + "/", typ: tar.TypeDir, mode: 0o755}}
	for _, f := range files {
		ents = append(ents, tarEnt{name: root + "/" + f.name, body: f.data, mode: f.mode})
	}
	return ents
}

type release struct {
	t          testing.TB
	key        testKey
	version    string
	components bool
	root       string
	files      []tf
	bundle     []byte
	man        map[string]any
	extra      map[string][]byte // other files listed in checksums.txt
}

func newRelease(t testing.TB, version string, components bool) *release {
	t.Helper()
	r := &release{t: t, key: newTestKey(t, "release key"), version: version, components: components,
		root: "oarfish_" + version + "_linux_arm64", files: testFiles(version, components),
		extra: map[string][]byte{"install.sh": []byte("#!/bin/sh\n"), "sbom.json": []byte("{}\n")}}
	r.setBundle(defaultEnts(r.root, r.files))
	return r
}

func (r *release) bundleName() string { return r.root + ".tar.gz" }

// setBundle replaces the bundle with the given entries and rebuilds the
// manifest from the release's files and that bundle.
func (r *release) setBundle(ents []tarEnt) {
	r.bundle = gz(makeTar(ents))
	r.man = r.defaultManifest()
}

func (r *release) defaultManifest() map[string]any {
	var contents []any
	for _, f := range r.files {
		contents = append(contents, map[string]any{
			"path": r.root + "/" + f.name, "mode": fmt.Sprintf("%04o", f.mode), "size": len(f.data), "sha256": SumHex(f.data),
		})
	}
	m := map[string]any{
		"schema_version": 1, "name": "oarfish", "version": r.version, "tag": "v" + r.version,
		"prerelease": strings.Contains(r.version, "-"), "commit": testCommit, "build_date": "2023-11-14T22:13:20Z",
		"os": "linux", "architecture": "arm64",
		"requirements": map[string]any{
			"os": "linux", "architecture": "arm64", "cpu": "ARMv8-A 64-bit (aarch64)",
			"libc": "none (static binary, CGO_ENABLED=0)", "runtime_dependencies": []any{},
		},
		"bundle":   map[string]any{"file": r.bundleName(), "sha256": SumHex(r.bundle), "size": len(r.bundle)},
		"contents": contents,
	}
	if r.components {
		m["components"] = map[string]any{"key_id": "11f1c52777090bfc", "protocol": 1}
	}
	return m
}

// built is a release as the four files that reach a device.
type built struct {
	Checksums, Sig, Manifest, Bundle []byte
}

func (r *release) build() built {
	r.t.Helper()
	mj, err := json.MarshalIndent(r.man, "", "  ")
	if err != nil {
		r.t.Fatal(err)
	}
	mj = append(mj, '\n')
	return r.buildWith(mj)
}

// buildWith lists the given manifest bytes and signs the result.
func (r *release) buildWith(manifest []byte) built {
	files := map[string][]byte{ManifestName: manifest, r.bundleName(): r.bundle}
	for n, d := range r.extra {
		files[n] = d
	}
	var names []string
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	var sb strings.Builder
	for _, n := range names {
		fmt.Fprintf(&sb, "%s  %s\n", SumHex(files[n]), n)
	}
	cs := []byte(sb.String())
	return built{Checksums: cs, Sig: r.key.sign(cs), Manifest: manifest, Bundle: r.bundle}
}

func (r *release) meta(b built) Metadata {
	return Metadata{Key: r.key.key, Version: r.version, Architecture: "arm64",
		Checksums: b.Checksums, Signature: b.Sig, Manifest: b.Manifest}
}

// verified builds the release and returns its verified form.
func (r *release) verified() (*Release, built) {
	r.t.Helper()
	b := r.build()
	rel, err := VerifyMetadata(r.meta(b))
	if err != nil {
		r.t.Fatalf("VerifyMetadata: %v (%s)", err, detail(err))
	}
	return rel, b
}

// ---- manifest editing -----------------------------------------------------

// walk follows a dotted path ("requirements.os", "contents.2.mode") and
// returns the container and the last key.
func walk(t testing.TB, root any, path string) (any, string) {
	t.Helper()
	parts := strings.Split(path, ".")
	cur := root
	for _, p := range parts[:len(parts)-1] {
		switch c := cur.(type) {
		case map[string]any:
			cur = c[p]
		case []any:
			i, err := strconv.Atoi(p)
			if err != nil {
				t.Fatalf("bad index %q in %q", p, path)
			}
			cur = c[i]
		default:
			t.Fatalf("cannot walk %q at %q", path, p)
		}
	}
	return cur, parts[len(parts)-1]
}

func setPath(t testing.TB, root map[string]any, path string, v any) {
	t.Helper()
	c, last := walk(t, root, path)
	switch c := c.(type) {
	case map[string]any:
		c[last] = v
	case []any:
		i, _ := strconv.Atoi(last)
		c[i] = v
	}
}

func delPath(t testing.TB, root map[string]any, path string) {
	t.Helper()
	c, last := walk(t, root, path)
	if m, ok := c.(map[string]any); ok {
		delete(m, last)
	} else {
		t.Fatalf("cannot delete %q", path)
	}
}

// ---- assertions -----------------------------------------------------------

func codeOf(err error) Code {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func detail(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Detail()
	}
	return ""
}

func wantCode(t testing.TB, err error, want Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("accepted, want %s", want)
	}
	if got := codeOf(err); got != want {
		t.Fatalf("code %q (%s), want %s", got, detail(err), want)
	}
}

func wantDetail(t testing.TB, err error, want Code, contains string) {
	t.Helper()
	wantCode(t, err, want)
	if !strings.Contains(detail(err), contains) {
		t.Fatalf("detail %q does not contain %q", detail(err), contains)
	}
}

func mustVersion(t testing.TB, s string) Version {
	t.Helper()
	v, err := ParseVersion(s)
	if err != nil {
		t.Fatalf("ParseVersion(%q): %v", s, err)
	}
	return v
}

// ---- sinks ----------------------------------------------------------------

type memSink struct {
	files    map[string][]byte
	modes    map[string]uint32
	sizes    map[string]int64
	finished map[string]bool
	order    []string
	creates  int
	failAt   string // Create fails for this name
	failW    string // Write fails for this name
	failF    string // Finish fails for this name
}

func newMemSink() *memSink {
	return &memSink{files: map[string][]byte{}, modes: map[string]uint32{}, sizes: map[string]int64{}, finished: map[string]bool{}}
}

type memFile struct {
	s    *memSink
	name string
}

func (s *memSink) Create(name string, mode uint32, size int64) (File, error) {
	s.creates++
	if name == s.failAt {
		return nil, errors.New("create failed with /secret/path")
	}
	if _, dup := s.files[name]; dup {
		return nil, errors.New("exists")
	}
	s.files[name], s.modes[name], s.sizes[name] = nil, mode, size
	s.order = append(s.order, name)
	return &memFile{s, name}, nil
}

func (f *memFile) Write(p []byte) (int, error) {
	if f.name == f.s.failW {
		return 0, errors.New("write failed")
	}
	f.s.files[f.name] = append(f.s.files[f.name], p...)
	return len(p), nil
}

func (f *memFile) Finish() error {
	if f.name == f.s.failF {
		return errors.New("finish failed")
	}
	f.s.finished[f.name] = true
	return nil
}

// dirSink writes into a directory the way the helper's staging code will:
// O_EXCL, an explicit mode.
type dirSink struct {
	dir      string
	finished map[string]bool
}

type dirFile struct {
	s    *dirSink
	name string
	f    *os.File
}

func (s *dirSink) Create(name string, mode uint32, size int64) (File, error) {
	if name != filepath.Base(name) {
		return nil, errors.New("not a plain name")
	}
	f, err := os.OpenFile(filepath.Join(s.dir, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, os.FileMode(mode))
	if err != nil {
		return nil, err
	}
	return &dirFile{s, name, f}, nil
}

func (f *dirFile) Write(p []byte) (int, error) { return f.f.Write(p) }
func (f *dirFile) Finish() error {
	if err := f.f.Sync(); err != nil {
		return err
	}
	if f.s.finished == nil {
		f.s.finished = map[string]bool{}
	}
	f.s.finished[f.name] = true
	return f.f.Close()
}

// hexOf is a short helper for table tests.
func hexOf(b []byte) string { return hex.EncodeToString(b) }

var _ = io.EOF

func discardHash() hash.Hash { return sha256.New() }
