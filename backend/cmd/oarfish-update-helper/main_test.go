package main

import (
	"bytes"
	"os"
	"runtime/debug"
	"strconv"
	"testing"
)

func TestUsage(t *testing.T) {
	for _, args := range [][]string{
		nil, {}, {"status"}, {"stage"}, {"SERVE"}, {"Recover"}, {"--help"}, {"-h"}, {""},
		{"serve", "extra"}, {"recover", "extra"}, {"serve", "recover"},
	} {
		var stderr bytes.Buffer
		o := testOptions(shortDir(t))
		o.getenv = func(string) string { t.Fatal("read the environment"); return "" }
		if code := run(args, &stderr, o); code != 2 {
			t.Errorf("run(%q) = %d, want 2", args, code)
		}
		if got := stderr.String(); got != "usage: oarfish-update-helper serve|recover\n" {
			t.Errorf("run(%q) stderr %q", args, got)
		}
	}
}

// recover needs no environment, no user lookup and no socket; on a state
// directory that does not exist it fails with a fixed message and creates
// nothing.
func TestRecoverWithoutAStateDirectory(t *testing.T) {
	dir := shortDir(t)
	o := testOptions(dir)
	o.getenv = func(string) string { t.Fatal("read the environment"); return "" }
	o.lookupUID = func(string) (uint32, error) { t.Fatal("looked up a user"); return 0, nil }
	o.peerUID = nil
	var stderr bytes.Buffer
	if code := run([]string{"recover"}, &stderr, o); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if got := stderr.String(); got != "oarfish-update-helper: recovery did not run: A storage error occurred.\n" {
		t.Fatalf("stderr %q", got)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("recover changed the release root: %d entries", len(entries))
	}
}

func TestSoftMemoryLimit(t *testing.T) {
	old := debug.SetMemoryLimit(-1)
	defer debug.SetMemoryLimit(old)
	configureRuntime()
	if got := debug.SetMemoryLimit(-1); got != softMemoryLimit || softMemoryLimit != 16<<20 {
		t.Fatalf("memory limit %d, want %d", got, 16<<20)
	}
}

func TestProductionOptions(t *testing.T) {
	o := productionOptions()
	if o.root != "/opt/oarfish" || o.idle.Seconds() != 30 || o.requestDeadline.Seconds() != 5 ||
		o.maxConnections != 4 || o.maxResponse != 8192 || o.pid != os.Getpid() {
		t.Fatalf("production options: %+v", o)
	}
	if serviceUser != "oarfish" {
		t.Fatalf("service user %q", serviceUser)
	}
}

// helper_version is a constant of the helper, named after the highest protocol
// it implements, and never a release version (D-C80, ADR 0010 A13).
func TestHelperID(t *testing.T) {
	if helperID != "protocol-"+strconv.Itoa(protocolMax) {
		t.Fatalf("helperID %q does not name protocol %d", helperID, protocolMax)
	}
	if !okHelperV(helperID) || validVersion(helperID) {
		t.Fatalf("helperID %q: valid helper_version %v, release version %v", helperID, okHelperV(helperID), validVersion(helperID))
	}
	if o := productionOptions(); o.helperVersion != helperID {
		t.Fatalf("production helper version %q, want %q", o.helperVersion, helperID)
	}
}

func TestProductionOptionsG65(t *testing.T) {
	o := productionOptions()
	if o.stateDir != "/var/lib/oarfish-update" || o.cacheDir != "/var/cache/oarfish" || o.arch == "" || o.journal != os.Stderr {
		t.Fatalf("production options: %+v", o)
	}
}

// The production handler has no test seam set and uses the real clocks.
func TestProductionHandlerHasNoSeams(t *testing.T) {
	key := newTestKey(t, "k")
	h := newHandler(productionOptions(), key.key)
	if h.wrapFS != nil || h.onChunk != nil {
		t.Fatal("a test seam is set in production")
	}
	if h.deadline.Seconds() != 180 || h.uptime == nil || h.monotonic == nil || h.wall == nil || h.newID == nil || h.freeBytes == nil {
		t.Fatalf("handler: %+v", h)
	}
	if h.keyID != key.key.ID() || h.root != "/opt/oarfish" {
		t.Fatalf("handler: %+v", h)
	}
	a, b := h.monotonic(), h.monotonic()
	if b < a {
		t.Fatal("the deadline clock goes backwards")
	}
	id, err := h.newID()
	id2, _ := h.newID()
	if err != nil || len(id) != 16 || id == id2 || !isHex(16)(id) {
		t.Fatalf("ids %q %q: %v", id, id2, err)
	}
}
