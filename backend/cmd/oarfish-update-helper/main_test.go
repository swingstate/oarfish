package main

import (
	"bytes"
	"os"
	"runtime/debug"
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

// In the stub, recover touches nothing: no output, no environment, no user
// lookup, no socket, no file.
func TestRecoverTouchesNothing(t *testing.T) {
	dir := shortDir(t)
	wd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)

	o := testOptions(dir)
	o.getenv = func(string) string { t.Fatal("read the environment"); return "" }
	o.lookupUID = func(string) (uint32, error) { t.Fatal("looked up a user"); return 0, nil }
	o.peerUID = nil
	var stderr bytes.Buffer
	if code := run([]string{"recover"}, &stderr, o); code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr %q", stderr.String())
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("recover created %d entries", len(entries))
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
