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

	"github.com/HasanMal1k/coder/internal/assistant"
	"github.com/HasanMal1k/coder/internal/auth"
	"github.com/HasanMal1k/coder/internal/config"
	"github.com/HasanMal1k/coder/internal/httpapi"
	"github.com/HasanMal1k/coder/internal/instance"
	"github.com/HasanMal1k/coder/internal/link"
	"github.com/HasanMal1k/coder/internal/linkgw"
	"github.com/HasanMal1k/coder/internal/llmgateway"
	"github.com/HasanMal1k/coder/internal/portproxy"
	"github.com/HasanMal1k/coder/internal/sandbox/docker"
	"github.com/HasanMal1k/coder/internal/store"
	"github.com/HasanMal1k/coder/internal/teamapi"
	"github.com/HasanMal1k/coder/internal/tunnel"
	"github.com/HasanMal1k/coder/internal/wsgateway"
)

// shutdownTimeout is how long in-flight requests get to finish after we are
// asked to stop.
const shutdownTimeout = 30 * time.Second

// modelTimeout bounds one request to the model for the assistant's next step.
const modelTimeout = 5 * time.Minute

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
	chatRepo := store.NewChatRepository(pool)
	agentRepo := store.NewAgentRepository(pool)

	authSvc := auth.NewService(userRepo, []byte(cfg.JWTSecret), cfg.AccessTokenTTL)
	if cfg.AccessTokenTTL > 0 {
		log.Printf("access tokens last %s", cfg.AccessTokenTTL)
	} else {
		log.Printf("access tokens never expire")
	}

	hub := tunnel.NewHub()
	instanceSvc := instance.NewService(instanceRepo, provisioner, hub, instance.Config{
		Provider:        "docker",
		Image:           cfg.WorkspaceImage,
		GuestConnectURL: cfg.GuestConnectURL,
		LinkConnectURL:  cfg.LinkConnectURL,
		IdleStopAfter:   cfg.IdleStopAfter,
		ModelContext:    cfg.ModelContextWindow,
		ModelOutput:     cfg.ModelMaxOutput,
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

	if cfg.IdleStopAfter > 0 {
		log.Printf("machines stop after %s with nobody connected", cfg.IdleStopAfter)
	} else {
		log.Printf("machines are never stopped for being idle")
	}
	go instanceSvc.RunIdleStop(ctx)

	// The assistant: a model that directs the coding agents inside machines.
	// Without a key for the model it is switched off, and everything else
	// works as before.
	var model assistant.LLM
	if cfg.ModelAPIKey != "" {
		model = &assistant.ChatCompletions{
			BaseURL: cfg.ModelBaseURL,
			APIKey:  cfg.ModelAPIKey,
			Model:   cfg.ModelName,
			Client:  &http.Client{Timeout: modelTimeout},
		}
		log.Printf("assistant is on, using %s at %s", cfg.ModelName, cfg.ModelBaseURL)
	} else {
		log.Printf("assistant is off: DEEPSEEK_API_KEY is not set")
	}

	fleet := assistant.NewFleet(instanceSvc, agentRepo)
	assistantSvc := assistant.NewService(assistant.Stores{
		Chats:       chatRepo,
		Agents:      agentRepo,
		Attachments: store.NewAttachmentRepository(pool),
		Designs:     store.NewArchitectureRepository(pool),
		Services:    store.NewServiceRepository(pool),
	}, model, fleet)

	// As with instances: a chat or an agent still marked as working was cut
	// off.
	interrupted, err := assistantSvc.RecoverInterrupted(ctx)
	if err != nil {
		return err
	}
	if interrupted > 0 {
		log.Printf("freed %d chat(s) the assistant was working on when the server stopped", interrupted)
	}

	gateway := wsgateway.New(authSvc, instanceSvc, cfg.AllowedOrigins)
	proxy := portproxy.New(portproxy.Config{
		Host:   cfg.WorkspaceHost,
		Scheme: cfg.WorkspaceScheme,
		Secret: []byte(cfg.JWTSecret),
		// A design can give a path on one machine's address to another
		// machine; the assistant knows the designs.
		Router: assistantSvc,
	}, instanceSvc)
	models := llmgateway.New(llmgateway.Config{
		BaseURL:         cfg.ModelBaseURL,
		APIKey:          cfg.ModelAPIKey,
		Model:           cfg.ModelName,
		ReasoningEffort: cfg.AgentReasoningEffort,
	}, instanceSvc, fleet)

	router := httpapi.NewRouter(httpapi.Deps{
		Auth:           authSvc,
		Instances:      instanceSvc,
		Assistant:      assistantSvc,
		AssistantModel: cfg.ModelName,
		Gateway:        gateway,
		Proxy:          proxy,
		Models:         models,
		Team:           teamapi.New(instanceSvc, assistantSvc),
		AllowedOrigins: cfg.AllowedOrigins,
	})

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

	// The distributed harness: every machine's link daemon holds a stream
	// open to this, on a port of its own because it speaks gRPC.
	links := link.NewHub()
	assistantSvc.UseLinks(links)
	linkSrv := linkgw.New(instanceSvc, links, assistantSvc).GRPC()
	linkListener, err := net.Listen("tcp", cfg.LinkAddr)
	if err != nil {
		return err
	}
	// Streams last as long as machines do, so there is nothing to wait for:
	// stopping ends them, and the daemons dial again when a server is back.
	defer linkSrv.Stop()

	serveErr := make(chan error, 2)
	go func() {
		log.Printf("listening on :%s", cfg.Port)
		serveErr <- srv.ListenAndServe()
	}()
	go func() {
		log.Printf("listening for machines' links on %s", cfg.LinkAddr)
		serveErr <- linkSrv.Serve(linkListener)
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
