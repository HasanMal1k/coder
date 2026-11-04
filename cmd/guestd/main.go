// guestd runs inside every user machine. It dials out to the control plane
// and serves terminals and port forwards over that connection, so machines
// need no open ports. It also keeps the editor, the coding agent and the
// link daemon running, and a copy on disk of what the owner's other agents
// are doing.
package main

import (
	"context"
	"log"
	"os/signal"
	"sync"
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

	var programs sync.WaitGroup
	programs.Go(func() { guest.RunIDE(ctx) })
	programs.Go(func() { guest.RunAgent(ctx, cfg) })
	programs.Go(func() { guest.RunTeamSync(ctx, cfg) })
	programs.Go(func() { guest.RunLink(ctx, cfg) })

	guest.Run(ctx, cfg, guest.NewServer())

	log.Printf("shutting down")
	programs.Wait()
}
