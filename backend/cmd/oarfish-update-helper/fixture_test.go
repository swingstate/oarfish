package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"sort"
	"strings"
	"testing"

	"github.com/swingstate/oarfish/backend/internal/relverify"
	"github.com/swingstate/oarfish/backend/internal/trustkey"
)

// The fixture builder: signed releases made with a throwaway Ed25519 key in
// the minisign legacy format, both bundle shapes, and hostile variants. It is
// a small copy of the builder in internal/relverify's tests (those tests are
// not refactored). Nothing here reads, prints or stores a real key.

type testKey struct {
	priv ed25519.PrivateKey
	id   [8]byte
	key  trustkey.Key
}

func newTestKey(t testing.TB, label string) testKey {
	t.Helper()
	seed := sha256.Sum256([]byte("helper test key seed " + label))
	idSrc := sha256.Sum256([]byte("helper test key id " + label))
	var id [8]byte
	copy(id[:], idSrc[:8])
	priv := ed25519.NewKeyFromSeed(seed[:])
	payload := append(append([]byte("Ed"), id[:]...), priv.Public().(ed25519.PublicKey)...)
	file := "untrusted comment: minisign public key test\n" + base64.StdEncoding.EncodeToString(payload) + "\n"
	k, err := trustkey.Parse([]byte(file))
	if err != nil {
		t.Fatal(err)
	}
	return testKey{priv: priv, id: id, key: k}
}

func (k testKey) sign(msg []byte) []byte {
	global := make([]byte, 64)
	payload := append(append([]byte("Ed"), k.id[:]...), ed25519.Sign(k.priv, msg)...)
	return []byte("untrusted comment: signature from minisign secret key\n" +
		base64.StdEncoding.EncodeToString(payload) + "\n" +
		"trusted comment: oarfish test\n" + base64.StdEncoding.EncodeToString(global) + "\n")
}

const testCommit = "0123456789abcdef0123456789abcdef01234567"

// elfStub is a header of an ELF64 little-endian aarch64 executable.
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

// The component files and oarfish.service do not depend on the version, so a
// release can be built that has exactly the installed ones. The unit texts pass
// the unit lint (U15 rule 3): each helper unit is its first accepted form with
// a comment, and oarfish.service is a small unit with the whole baseline. The
// units of install/ are checked by the tests of the lint, not used here, so
// that these fixtures do not depend on the repository tree.
var baseComponents = map[string][]byte{
	"oarfish-update-helper":          elfStub("helper"),
	"oarfish-update-helper.service":  []byte("# helper\n" + helperForms[unitHelperService][0] + "\n"),
	"oarfish-update-helper.socket":   []byte("# socket\n" + helperForms[unitHelperSocket][0] + "\n"),
	"oarfish-update-recover.service": []byte("# recover\n" + helperForms[unitRecover][0] + "\n"),
}

var baseServiceUnit = []byte(`# fixture
[Unit]
Description=Oarfish test unit
[Service]
Type=simple
User=oarfish
Group=oarfish
ExecStart=/opt/oarfish/current/oarfish -listen 127.0.0.1:8484
Restart=on-failure
RestartSec=2
TimeoutStopSec=15
StateDirectory=oarfish
StateDirectoryMode=0700
CacheDirectory=oarfish
RuntimeDirectory=oarfish
RuntimeDirectoryMode=0750
UMask=0077
NoNewPrivileges=yes
CapabilityBoundingSet=
AmbientCapabilities=
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
PrivateDevices=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectKernelLogs=yes
ProtectControlGroups=yes
ProtectClock=yes
ProtectHostname=yes
RestrictNamespaces=yes
RestrictRealtime=yes
RestrictSUIDSGID=yes
LockPersonality=yes
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
SystemCallArchitectures=native
SystemCallFilter=@system-service
SystemCallErrorNumber=EPERM
[Install]
WantedBy=multi-user.target
`)

// changedUnit is the fixture unit of a name with one more comment line: other
// bytes that the unit lint still accepts.
func changedUnit(name string) []byte {
	base := baseServiceUnit
	if name != unitService {
		base = baseComponents[name]
	}
	return append(append([]byte(nil), base...), "# changed\n"...)
}

func testFiles(version string, components bool) []tf {
	fs := []tf{
		{"LICENSE", 0o644, []byte("licence text\n")},
		{"THIRD_PARTY_NOTICES.txt", 0o644, []byte("notices\n")},
		{"oarfish", 0o755, elfStub("oarfish " + version)},
		{"oarfish.service", 0o644, baseServiceUnit},
	}
	if components {
		fs = append(fs,
			tf{"oarfish-update-helper", 0o755, baseComponents["oarfish-update-helper"]},
			tf{"oarfish-update-helper.service", 0o644, baseComponents["oarfish-update-helper.service"]},
			tf{"oarfish-update-helper.socket", 0o644, baseComponents["oarfish-update-helper.socket"]},
			tf{"oarfish-update-recover.service", 0o644, baseComponents["oarfish-update-recover.service"]},
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
}

func makeTar(ents []tarEnt) []byte {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, e := range ents {
		typ := e.typ
		if typ == 0 {
			typ = tar.TypeReg
		}
		h := &tar.Header{Typeflag: typ, Name: e.name, Mode: e.mode, Linkname: e.link}
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

// release is a release under construction.
type release struct {
	t          testing.TB
	key        testKey
	version    string
	components bool
	root       string
	files      []tf
	bundle     []byte
	man        map[string]any
	extra      map[string][]byte
}

func newRelease(t testing.TB, key testKey, version string, components bool) *release {
	t.Helper()
	r := &release{t: t, key: key, version: version, components: components,
		root: "oarfish_" + version + "_linux_arm64", files: testFiles(version, components),
		extra: map[string][]byte{"install.sh": []byte("#!/bin/sh\n"), "sbom.json": []byte("{}\n")}}
	r.setBundle(defaultEnts(r.root, r.files))
	return r
}

func (r *release) bundleName() string { return r.root + ".tar.gz" }

// setFile replaces the bytes of one file (and rebuilds the bundle).
func (r *release) setFile(name string, data []byte) {
	for i := range r.files {
		if r.files[i].name == name {
			r.files[i].data = data
		}
	}
	r.setBundle(defaultEnts(r.root, r.files))
}

func (r *release) setBundle(ents []tarEnt) {
	r.bundle = gz(makeTar(ents))
	r.man = r.defaultManifest()
}

func (r *release) defaultManifest() map[string]any {
	var contents []any
	for _, f := range r.files {
		contents = append(contents, map[string]any{
			"path": r.root + "/" + f.name, "mode": fmt.Sprintf("%04o", f.mode), "size": len(f.data), "sha256": relverify.SumHex(f.data),
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
		"bundle":   map[string]any{"file": r.bundleName(), "sha256": relverify.SumHex(r.bundle), "size": len(r.bundle)},
		"contents": contents,
	}
	if r.components {
		m["components"] = map[string]any{"key_id": "11f1c52777090bfc", "protocol": 1}
	}
	return m
}

// built is a release as the four files that reach the cache.
type built struct {
	Checksums, Sig, Manifest, Bundle []byte
	BundleName                       string
}

func (r *release) build() built {
	r.t.Helper()
	mj, err := json.MarshalIndent(r.man, "", "  ")
	if err != nil {
		r.t.Fatal(err)
	}
	return r.buildWith(append(mj, '\n'))
}

// buildWith lists the given manifest bytes and signs the result.
func (r *release) buildWith(manifest []byte) built {
	files := map[string][]byte{relverify.ManifestName: manifest, r.bundleName(): r.bundle}
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
		fmt.Fprintf(&sb, "%s  %s\n", relverify.SumHex(files[n]), n)
	}
	cs := []byte(sb.String())
	return built{Checksums: cs, Sig: r.key.sign(cs), Manifest: manifest, Bundle: r.bundle, BundleName: r.bundleName()}
}

// incompressible returns n pseudo-random bytes (a gzip of them is not smaller).
func incompressible(n int) []byte {
	rng := rand.New(rand.NewPCG(1, 2))
	b := make([]byte, n)
	for i := 0; i+8 <= n; i += 8 {
		v := rng.Uint64()
		for j := 0; j < 8; j++ {
			b[i+j] = byte(v >> (8 * j))
		}
	}
	return b
}
