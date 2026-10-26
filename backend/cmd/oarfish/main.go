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
	"syscall"
	"time"

	"github.com/swingstate/oarfish/backend/internal/platform/detector"
	"github.com/swingstate/oarfish/backend/internal/platform/hardware"
	"github.com/swingstate/oarfish/backend/internal/platform/profiles"
	"github.com/swingstate/oarfish/backend/internal/platform/source"
	"github.com/swingstate/oarfish/backend/internal/server"
	"github.com/swingstate/oarfish/backend/internal/version"
	"github.com/swingstate/oarfish/backend/internal/web"
)

// defaultListen is loopback only; a public bind needs an explicit -listen value.
const defaultListen = "127.0.0.1:8484"

func main() {
	listen := flag.String("listen", defaultListen, "address to listen on")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("oarfish %s (%s)\n", version.Version, version.Commit)
		return
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(*listen, log); err != nil {
		log.Error("oarfish stopped", "error", err)
		os.Exit(1)
	}
}

func run(addr string, log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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

	srv := server.New(addr, server.NewHandler(web.Assets(), server.WithDevice(dev)))

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
