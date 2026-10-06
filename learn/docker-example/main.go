// Command hits counts visits in Postgres. It is tiny on purpose: it is here
// to be put in a container. What it does have is what any program needs to
// behave well in one: its settings come from the environment, it can say
// whether it is healthy, and it shuts down cleanly when asked to stop.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// shutdownTimeout is how long requests in flight get to finish after the
// program is asked to stop. It has to be shorter than the time Docker waits
// before killing a container, which is ten seconds unless told otherwise.
const shutdownTimeout = 5 * time.Second

func main() {
	// The final image has no shell and no curl, so the program is its own
	// health check: `hits -healthcheck` asks the running server and exits 0
	// or 1. The HEALTHCHECK line in the Dockerfile runs exactly that.
	healthcheck := flag.Bool("healthcheck", false, "ask the running server whether it is healthy, then exit 0 or 1")
	flag.Parse()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	if *healthcheck {
		if err := checkHealth(port); err != nil {
			fmt.Fprintln(os.Stderr, "unhealthy:", err)
			os.Exit(1)
		}
		return
	}

	if err := run(port); err != nil {
		log.Fatal(err)
	}
}

func run(port string) error {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return errors.New("DATABASE_URL is required")
	}

	// `docker stop` sends SIGTERM. Left alone, a Go program dies on the spot
	// and any request it was answering is cut off. Cancelling ctx instead is
	// what starts a clean shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("create database pool: %w", err)
	}
	defer pool.Close()

	// Fail at startup, where it is seen, if the database can't be reached.
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("reach database: %w", err)
	}

	// Inside a container this is the container's ID, which shows which
	// container answered a request.
	hostname, _ := os.Hostname()

	mux := http.NewServeMux()

	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		// One statement adds the visit and reads the total, so two requests
		// arriving together can't both be given the same number.
		var visits int64
		err := pool.QueryRow(r.Context(),
			`INSERT INTO counters (name, value) VALUES ('visits', 1)
			 ON CONFLICT (name) DO UPDATE SET value = counters.value + 1
			 RETURNING value`,
		).Scan(&visits)
		if err != nil {
			log.Printf("count a visit: %v", err)
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"visits":    visits,
			"served_by": hostname,
		})
	})

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		if err := pool.Ping(ctx); err != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext: func(net.Listener) context.Context {
			return ctx
		},
	}

	serveErr := make(chan error, 1)
	go func() {
		log.Printf("listening on :%s", port)
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

// checkHealth asks the server running in this same container whether it is
// healthy.
func checkHealth(port string) error {
	client := &http.Client{Timeout: 2 * time.Second}

	resp, err := client.Get("http://127.0.0.1:" + port + "/healthz")
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("server answered %s", resp.Status)
	}

	return nil
}
