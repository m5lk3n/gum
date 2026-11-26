// Command gum is a small MCP server for a limited set of Linux admin tasks.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"lttl.dev/gum/internal/apt"
	"lttl.dev/gum/internal/server"
)

// Set through -ldflags, see Makefile.
var (
	version = "dev"
	builtBy = "unknown"
)

type config struct {
	listen            string
	tokenFile         string
	updateMinInterval time.Duration
	jobTimeout        time.Duration
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "gum:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("gum", flag.ContinueOnError)
	var cfg config
	fs.StringVar(&cfg.listen, "listen", envOr("GUM_LISTEN", "127.0.0.1:8765"), "address to listen on (env GUM_LISTEN)")
	fs.StringVar(&cfg.tokenFile, "token-file", os.Getenv("GUM_TOKEN_FILE"), "file containing the bearer token clients must send (env GUM_TOKEN_FILE)")
	updateMin := fs.String("update-min-interval", envOr("GUM_UPDATE_MIN_INTERVAL", "15m"), "minimum time between package index refreshes (env GUM_UPDATE_MIN_INTERVAL)")
	jobTimeout := fs.String("job-timeout", envOr("GUM_JOB_TIMEOUT", "60m"), "maximum duration of a single apt job (env GUM_JOB_TIMEOUT)")
	showVersion := fs.Bool("version", false, "print version and exit")
	printSudoers := fs.String("print-sudoers", "", "print the sudoers rule for the given user and exit")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *showVersion {
		fmt.Printf("gum %s (built by %s)\n", version, builtBy)
		return nil
	}
	if *printSudoers != "" {
		fmt.Print(apt.SudoersRule(*printSudoers))
		return nil
	}

	var err error
	if cfg.updateMinInterval, err = time.ParseDuration(*updateMin); err != nil {
		return fmt.Errorf("invalid update min interval: %w", err)
	}
	if cfg.jobTimeout, err = time.ParseDuration(*jobTimeout); err != nil || cfg.jobTimeout <= 0 {
		return fmt.Errorf("invalid job timeout %q", *jobTimeout)
	}
	return serve(cfg)
}

func serve(cfg config) error {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	token, err := readToken(cfg.tokenFile)
	if err != nil {
		return err
	}
	loopback, err := isLoopback(cfg.listen)
	if err != nil {
		return err
	}
	if !loopback && token == "" {
		return fmt.Errorf("refusing to listen on non-loopback address %q without a token (set GUM_TOKEN_FILE)", cfg.listen)
	}

	mgr := apt.NewManager(apt.Config{
		Runner:            apt.ExecRunner{},
		UpdateMinInterval: cfg.updateMinInterval,
		JobTimeout:        cfg.jobTimeout,
		Logger:            log,
	})

	mux := http.NewServeMux()
	mux.Handle("/mcp", server.NewHandler(server.NewMCPServer(mgr, server.BuildInfo{Version: version, BuiltBy: builtBy}), token))
	srv := &http.Server{
		Addr:              cfg.listen,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		log.Info("gum listening", "version", version, "addr", cfg.listen, "endpoint", "/mcp", "auth", token != "")
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	log.Info("shutting down; waiting for running apt jobs")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		log.Warn("http shutdown", "error", err)
	}
	// Never abandon dpkg mid-run: give a running job its full timeout to finish.
	jobsCtx, cancelJobs := context.WithTimeout(context.Background(), cfg.jobTimeout)
	defer cancelJobs()
	return mgr.Wait(jobsCtx)
}

func envOr(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func readToken(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	b, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("read token file: %w", err)
	}
	token := strings.TrimSpace(string(b))
	if token == "" {
		return "", fmt.Errorf("token file %q is empty", path)
	}
	return token, nil
}

func isLoopback(addr string) (bool, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false, fmt.Errorf("invalid listen address %q: %w", addr, err)
	}
	if host == "localhost" {
		return true, nil
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback(), nil
}
