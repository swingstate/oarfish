// Command orbit is the Orbit core process.
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

	"github.com/swingstate/orbit-linux/backend/internal/server"
	"github.com/swingstate/orbit-linux/backend/internal/version"
	"github.com/swingstate/orbit-linux/backend/internal/web"
)

// defaultListen is loopback only; a public bind needs an explicit -listen value.
const defaultListen = "127.0.0.1:8484"

func main() {
	listen := flag.String("listen", defaultListen, "address to listen on")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("orbit %s (%s)\n", version.Version, version.Commit)
		return
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(*listen, log); err != nil {
		log.Error("orbit stopped", "error", err)
		os.Exit(1)
	}
}

func run(addr string, log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := server.New(addr, server.NewHandler(web.Assets()))

	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	log.Info("orbit listening", "addr", addr, "version", version.Version, "commit", version.Commit)

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
