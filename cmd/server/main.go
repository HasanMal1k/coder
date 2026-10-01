package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/HasanMal1k/coder/internal/auth"
	"github.com/HasanMal1k/coder/internal/config"
	"github.com/HasanMal1k/coder/internal/httpapi"
	"github.com/HasanMal1k/coder/internal/instance"
	"github.com/HasanMal1k/coder/internal/portproxy"
	"github.com/HasanMal1k/coder/internal/sandbox/docker"
	"github.com/HasanMal1k/coder/internal/store"
	"github.com/HasanMal1k/coder/internal/tunnel"
	"github.com/HasanMal1k/coder/internal/wsgateway"
)

// shutdownTimeout is how long in-flight requests get to finish after we are
// asked to stop.
const shutdownTimeout = 30 * time.Second

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// ctx is cancelled on Ctrl+C or SIGTERM, which starts a graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := store.NewPostgresPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	log.Printf("connected to database")

	provisioner, err := docker.New(ctx)
	if err != nil {
		return err
	}
	defer provisioner.Close()

	userRepo := store.NewUserRepository(pool)
	instanceRepo := store.NewInstanceRepository(pool)

	authSvc := auth.NewService(userRepo, []byte(cfg.JWTSecret), 15*time.Minute)

	hub := tunnel.NewHub()
	instanceSvc := instance.NewService(instanceRepo, provisioner, hub, instance.Config{
		Provider:        "docker",
		Image:           cfg.WorkspaceImage,
		GuestConnectURL: cfg.GuestConnectURL,
	})

	// Nothing is in flight yet, so any instance still mid-operation was cut
	// off when the previous process stopped.
	recovered, err := instanceSvc.RecoverInterrupted(ctx)
	if err != nil {
		return err
	}
	if recovered > 0 {
		log.Printf("marked %d interrupted instance(s) as errored", recovered)
	}

	gateway := wsgateway.New(authSvc, instanceSvc, cfg.AllowedOrigins)
	proxy := portproxy.New(portproxy.Config{
		Host:   cfg.WorkspaceHost,
		Scheme: cfg.WorkspaceScheme,
		Secret: []byte(cfg.JWTSecret),
	}, instanceSvc)
	router := httpapi.NewRouter(authSvc, instanceSvc, gateway, proxy, cfg.AllowedOrigins)

	srv := &http.Server{
		Addr: ":" + cfg.Port,
		// Requests for a machine's own hostname go to the proxy; everything
		// else is the API.
		Handler:           proxy.Wrap(router),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		// Every request's context descends from ctx. Shutdown doesn't close
		// WebSockets by itself; cancelling their context is what ends them.
		BaseContext: func(net.Listener) context.Context {
			return ctx
		},
	}

	serveErr := make(chan error, 1)
	go func() {
		log.Printf("listening on :%s", cfg.Port)
		serveErr <- srv.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
	}

	log.Printf("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	return nil
}
