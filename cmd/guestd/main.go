// guestd runs inside every user machine. It dials out to the control plane
// and serves terminals and port forwards over that connection, so machines
// need no open ports. It also keeps the editor running.
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

	ideDone := make(chan struct{})
	go func() {
		defer close(ideDone)
		guest.RunIDE(ctx)
	}()

	guest.Run(ctx, cfg, guest.NewServer())

	log.Printf("shutting down")
	<-ideDone
}
