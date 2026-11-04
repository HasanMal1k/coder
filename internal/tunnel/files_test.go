package tunnel_test

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/HasanMal1k/coder/internal/guestapi"
	"github.com/HasanMal1k/coder/internal/tunnel"
)

func TestFilesGoBothWays(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	hub := tunnel.NewHub()
	connect(t, hub, "inst-1")
	ctx := context.Background()

	// Larger than gRPC's own limit on a message, which both ends raise.
	data := bytes.Repeat([]byte("mockup "), (5<<20)/7)
	written, err := hub.PutFile(ctx, "inst-1", ".coder/attachments/x/big.bin", data)
	if err != nil {
		t.Fatalf("PutFile: %v", err)
	}
	if want := filepath.Join(home, ".coder/attachments/x/big.bin"); written != want {
		t.Errorf("written to %q, want %q", written, want)
	}

	read, where, err := hub.GetFile(ctx, "inst-1", written, guestapi.MaxFileSize)
	if err != nil || !bytes.Equal(read, data) || where != written {
		t.Fatalf("GetFile: %d bytes from %q, %v", len(read), where, err)
	}

	if _, _, err := hub.GetFile(ctx, "inst-1", "missing.png", 1024); !errors.Is(err, tunnel.ErrFileNotFound) {
		t.Errorf("a missing file: %v", err)
	}
	var refused *tunnel.FileError
	if _, _, err := hub.GetFile(ctx, "inst-1", written, 1024); !errors.As(err, &refused) {
		t.Errorf("a file over the limit: %v", err)
	}
}
