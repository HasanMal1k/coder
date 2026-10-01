// guestd runs inside every user machine. It dials out to the control plane
// and serves terminals over that connection, so machines need no open ports.
package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"github.com/HasanMal1k/coder/internal/guest"
)

func main() {
	log.SetPrefix("guestd: ")

	cfg, err := guest.ConfigFromEnv()
	if err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	guest.Run(ctx, cfg, guest.NewServer())

	log.Printf("shutting down")
}
