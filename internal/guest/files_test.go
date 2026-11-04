package guest

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/HasanMal1k/coder/internal/guestapi"
)

func TestPutAndGetFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	srv := NewServer()
	ctx := context.Background()

	data := []byte("\x89PNG not really")
	put, err := srv.PutFile(ctx, &guestapi.PutFileRequest{Path: "~/.coder/attachments/abc/logo.png", Data: data})
	if err != nil {
		t.Fatalf("PutFile: %v", err)
	}
	want := filepath.Join(home, ".coder/attachments/abc/logo.png")
	if put.GetPath() != want {
		t.Errorf("written to %q, want %q", put.GetPath(), want)
	}
	if on, _ := os.ReadFile(want); !bytes.Equal(on, data) {
		t.Errorf("on disk: %q, want %q", on, data)
	}

	// Replacing it leaves no temporary file behind.
	if _, err := srv.PutFile(ctx, &guestapi.PutFileRequest{Path: want, Data: []byte("v2")}); err != nil {
		t.Fatalf("PutFile again: %v", err)
	}
	entries, _ := os.ReadDir(filepath.Dir(want))
	if len(entries) != 1 {
		t.Errorf("folder holds %d entries, want 1", len(entries))
	}

	got, err := srv.GetFile(ctx, &guestapi.GetFileRequest{Path: ".coder/attachments/abc/logo.png"})
	if err != nil {
		t.Fatalf("GetFile: %v", err)
	}
	if string(got.GetData()) != "v2" || got.GetPath() != want {
		t.Errorf("GetFile = %q at %q", got.GetData(), got.GetPath())
	}
}

func TestGetFileRefuses(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	srv := NewServer()
	ctx := context.Background()

	os.WriteFile(filepath.Join(home, "big"), make([]byte, 100), 0o644)
	os.Mkdir(filepath.Join(home, "dir"), 0o755)

	cases := []struct {
		req  *guestapi.GetFileRequest
		code codes.Code
	}{
		{&guestapi.GetFileRequest{Path: "missing"}, codes.NotFound},
		{&guestapi.GetFileRequest{Path: "big", MaxSize: 99}, codes.FailedPrecondition},
		{&guestapi.GetFileRequest{Path: "dir"}, codes.FailedPrecondition},
		{&guestapi.GetFileRequest{Path: ""}, codes.InvalidArgument},
		{&guestapi.GetFileRequest{Path: "/"}, codes.InvalidArgument},
	}
	for _, c := range cases {
		_, err := srv.GetFile(ctx, c.req)
		if status.Code(err) != c.code {
			t.Errorf("GetFile(%q, max %d) = %v, want %s", c.req.Path, c.req.MaxSize, err, c.code)
		}
	}

	if _, err := srv.GetFile(ctx, &guestapi.GetFileRequest{Path: "big", MaxSize: 100}); err != nil {
		t.Errorf("a file exactly at the limit: %v", err)
	}
}
