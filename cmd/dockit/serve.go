package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pdutton/DockIt/internal/api"
	"github.com/pdutton/DockIt/internal/service"
	"github.com/pdutton/DockIt/internal/store"
)

// shutdownTimeout is how long in-flight requests get to finish on shutdown.
const shutdownTimeout = 10 * time.Second

func runServe(e *env, args []string) error {
	flags := newFlagSet(e, "serve", "<dir>")
	listen := flags.String("listen", envOr(e, "DOCKIT_LISTEN", "localhost:8080"),
		"address to listen on ($DOCKIT_LISTEN)")
	certFile := flags.String("tls-cert", e.getenv("DOCKIT_TLS_CERT"),
		"TLS certificate file, to serve HTTPS directly ($DOCKIT_TLS_CERT)")
	keyFile := flags.String("tls-key", e.getenv("DOCKIT_TLS_KEY"),
		"TLS key file ($DOCKIT_TLS_KEY)")
	devUser := flags.String("dev-insecure-user", "",
		"development only: treat every request as this user, with no authentication; localhost only")
	if err := flags.Parse(args); err != nil {
		return err
	}
	dir, err := dataDir(e, flags)
	if err != nil {
		return err
	}
	if (*certFile == "") != (*keyFile == "") {
		return errors.New("-tls-cert and -tls-key must be given together")
	}
	if *devUser != "" && !isLoopback(*listen) {
		return fmt.Errorf("-dev-insecure-user requires a localhost -listen address, not %q", *listen)
	}

	log := slog.New(slog.NewTextHandler(e.stderr, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return serve(ctx, log, dir, *listen, *certFile, *keyFile, *devUser, nil)
}

// serve runs the server until ctx is done.  If ready is not nil, the bound
// address is sent on it once the server is listening.
func serve(ctx context.Context, log *slog.Logger, dir, listen, certFile, keyFile, devUser string, ready chan<- string) error {
	svc, report, err := service.Open(dir)
	var le *store.LockedError
	switch {
	case errors.As(err, &le):
		msg := "the dataset is locked; another instance may be running.\n" +
			"If it is not, run `dockit unlock` and start again"
		if le.Info != nil {
			msg = fmt.Sprintf("the dataset is locked by host %s, pid %d, since %s.\n"+
				"If that instance is not running, run `dockit unlock` and start again",
				le.Info.Host, le.Info.PID, le.Info.Started.Format(time.RFC3339))
		}
		return errors.New(msg)
	case err != nil:
		if report != nil {
			for _, p := range report.Problems {
				if !p.Warning {
					log.Error("dataset problem", "problem", p.String())
				}
			}
		}
		return err
	}
	defer func() {
		if err := svc.Close(); err != nil {
			log.Error("releasing the dataset lock", "err", err)
		}
	}()
	for _, p := range report.Problems {
		log.Warn("dataset problem", "problem", p.String())
	}

	if devUser != "" {
		if _, err := svc.User(devUser, devUser); err != nil {
			return fmt.Errorf("-dev-insecure-user %q is not an active user", devUser)
		}
		log.Warn("INSECURE: authentication is disabled; every request acts as " + devUser)
	}

	mux := http.NewServeMux()
	mux.Handle(api.Prefix+"/", api.New(svc, api.Options{DevUser: devUser, Logger: log}))

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       2 * time.Minute,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		return err
	}
	scheme := "http"
	if certFile != "" {
		scheme = "https"
	}
	log.Info("DockIt serving", "dataset", dir, "url", scheme+"://"+ln.Addr().String()+api.Prefix+"/")
	if ready != nil {
		ready <- ln.Addr().String()
	}

	errc := make(chan error, 1)
	go func() {
		if certFile != "" {
			errc <- srv.ServeTLS(ln, certFile, keyFile)
		} else {
			errc <- srv.Serve(ln)
		}
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down")
	sctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil {
		return err
	}
	return nil
}

func envOr(e *env, key, def string) string {
	if v := e.getenv(key); v != "" {
		return v
	}
	return def
}

// isLoopback reports whether a listen address binds only to loopback.  An
// empty host (":8080") binds every interface, so it is not.
func isLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
