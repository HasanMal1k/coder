package guest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/HasanMal1k/coder/internal/guestapi"
)

// PutFile writes a file the control plane sends: something the owner gave an
// agent, such as an image or a document. guestd runs as the machine's user,
// so the file is that user's.
func (s *Server) PutFile(ctx context.Context, req *guestapi.PutFileRequest) (*guestapi.PutFileResponse, error) {
	path, err := resolve(req.GetPath())
	if err != nil {
		return nil, err
	}
	if len(req.GetData()) > guestapi.MaxFileSize {
		return nil, status.Errorf(codes.InvalidArgument, "the file is larger than %d bytes", guestapi.MaxFileSize)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "create the folder: %v", err)
	}

	// Written beside it and renamed, so nothing ever reads half a file.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".put-*")
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "write the file: %v", err)
	}
	defer os.Remove(tmp.Name())

	_, err = tmp.Write(req.GetData())
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Chmod(tmp.Name(), 0o644)
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "write the file: %v", err)
	}

	return &guestapi.PutFileResponse{Path: path}, nil
}

// GetFile reads a file for the control plane: something an agent wants to
// show the owner, such as a mockup it drew.
func (s *Server) GetFile(ctx context.Context, req *guestapi.GetFileRequest) (*guestapi.GetFileResponse, error) {
	path, err := resolve(req.GetPath())
	if err != nil {
		return nil, err
	}

	limit := req.GetMaxSize()
	if limit == 0 || limit > guestapi.MaxFileSize {
		limit = guestapi.MaxFileSize
	}

	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, status.Errorf(codes.NotFound, "there is no file at %s", path)
		}
		return nil, status.Errorf(codes.FailedPrecondition, "open %s: %v", path, err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "open %s: %v", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, status.Errorf(codes.FailedPrecondition, "%s is not a file", path)
	}
	if uint64(info.Size()) > limit {
		return nil, status.Errorf(codes.FailedPrecondition, "%s is %d bytes, more than the %d allowed", path, info.Size(), limit)
	}

	// Read to the limit and one byte past it, in case it grew since.
	data, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "read %s: %v", path, err)
	}
	if uint64(len(data)) > limit {
		return nil, status.Errorf(codes.FailedPrecondition, "%s is more than the %d bytes allowed", path, limit)
	}

	return &guestapi.GetFileResponse{Data: data, Path: path}, nil
}

// resolve makes a path absolute, taking a relative one, or one starting with
// "~/", to be under the home folder.
func resolve(path string) (string, error) {
	if path == "" {
		return "", status.Error(codes.InvalidArgument, "the path is empty")
	}

	home := homeDir()
	switch {
	case path == "~":
		path = home
	case len(path) > 1 && path[:2] == "~/":
		path = filepath.Join(home, path[2:])
	case !filepath.IsAbs(path):
		path = filepath.Join(home, path)
	}

	clean := filepath.Clean(path)
	if clean == "/" {
		return "", status.Error(codes.InvalidArgument, fmt.Sprintf("%q is not a file", path))
	}

	return clean, nil
}
