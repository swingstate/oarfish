// Command oarfish is the Oarfish core process.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/swingstate/oarfish/backend/internal/audit"
	"github.com/swingstate/oarfish/backend/internal/auth"
	"github.com/swingstate/oarfish/backend/internal/clock"
	"github.com/swingstate/oarfish/backend/internal/metrics"
	"github.com/swingstate/oarfish/backend/internal/platform/detector"
	"github.com/swingstate/oarfish/backend/internal/platform/hardware"
	"github.com/swingstate/oarfish/backend/internal/platform/profiles"
	"github.com/swingstate/oarfish/backend/internal/platform/source"
	"github.com/swingstate/oarfish/backend/internal/server"
	"github.com/swingstate/oarfish/backend/internal/version"
	"github.com/swingstate/oarfish/backend/internal/web"
)

// defaultListen is loopback only. The binary accepts no other address until
// LAN access and HTTPS are designed (D-C29, D-C48, open item D-D10;
// server.ValidateListen).
const defaultListen = "127.0.0.1:8484"

func main() {
	listen := flag.String("listen", defaultListen, "loopback IP address and port to listen on (127.0.0.0/8 or [::1])")
	stateDir := flag.String("state-dir", "", "directory for the account, setup token and audit file (default: $STATE_DIRECTORY, set by systemd)")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("oarfish %s (%s)\n", version.Version, version.Commit)
		return
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	addr, err := server.ValidateListen(*listen)
	if err != nil {
		log.Error("oarfish refuses to start", "error", err)
		os.Exit(1)
	}
	dir := resolveStateDir(*stateDir, os.Getenv("STATE_DIRECTORY"))
	if err := run(addr, dir, log); err != nil {
		log.Error("oarfish stopped", "error", err)
		os.Exit(1)
	}
}

// resolveStateDir prefers the flag; otherwise it takes the first path of
// STATE_DIRECTORY, which systemd sets from StateDirectory= (a colon-separated
// list when there are several).
func resolveStateDir(flagValue, env string) string {
	if flagValue != "" {
		return flagValue
	}
	first, _, _ := strings.Cut(env, ":")
	return first
}

func run(addr, stateDir string, log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Access control (ADR 0009). An unusable state directory or account file
	// stops the process here, before it listens (ORB-SEC-005).
	am, err := auth.Open(auth.Config{StateDir: stateDir})
	if err != nil {
		return fmt.Errorf("access control: %w", err)
	}
	auditLog, err := audit.Open(stateDir, clock.System(), func(msg string) { log.Warn(msg) })
	if err != nil {
		return fmt.Errorf("audit: %w", err)
	}
	// The audit buffer is written on every way out of run, including SIGTERM
	// and SIGINT (systemctl stop), which end the select below.
	defer auditLog.Close()
	if am.SetupRequired() {
		// The path only; the token itself never goes to the log (AT-023).
		log.Info("no administrator account yet; the GUI asks for the setup token", "token_file", am.SetupTokenPath())
	}

	// Detection runs once, now. It only reads files and cannot fail; the
	// profiles are embedded, so an error here is a defect in the build.
	reg, err := profiles.Load()
	if err != nil {
		return fmt.Errorf("embedded profiles: %w", err)
	}
	dev := detector.New(source.Host(), hardware.HostEnv(), reg)
	res := dev.Result()
	log.Info("platform detected",
		"arch", res.Hardware.Arch, "os_adapter", res.OS.Adapter, "profile", res.Profile.ID,
		"tier", res.Support.Tier, "reason", res.Support.Reason)

	// Metrics are read only while a browser watches (ORB-DASH-006): the sampler
	// has no goroutine, timer or file read until the first client connects.
	tier := res.Support.Tier
	sampler := metrics.NewSampler(metrics.Config{
		Collector:    metrics.NewCollector(source.Host(), metrics.HostStatfs),
		AlertContext: func() metrics.AlertContext { return metrics.AlertContext{SupportTier: tier} },
	})

	srv := server.New(addr, server.NewHandler(web.Assets(),
		server.WithDevice(dev), server.WithMetrics(sampler), server.WithAuth(am), server.WithAudit(auditLog)))

	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	log.Info("oarfish listening", "addr", addr, "version", version.Version, "commit", version.Commit)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}

	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	if err := <-errc; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
