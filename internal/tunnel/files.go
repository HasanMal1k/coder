package tunnel

import (
	"context"
	"errors"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/HasanMal1k/coder/internal/guestapi"
)

var (
	// ErrFilesUnsupported is returned when the machine's guest daemon is too
	// old to pass files.
	ErrFilesUnsupported = errors.New("the machine's guest daemon does not support passing files")
	// ErrFileNotFound is returned when there is no file at the path asked for.
	ErrFileNotFound = errors.New("no such file")
)

// FileError is the machine's reason for not reading or writing a file, in
// its own words: the file is too large, or is a folder.
type FileError struct{ Message string }

func (e *FileError) Error() string { return e.Message }

// PutFile writes data to path on instanceID's machine and returns the
// absolute path it was written to. A relative path is under the machine's
// home folder.
func (h *Hub) PutFile(ctx context.Context, instanceID, path string, data []byte) (string, error) {
	g, err := h.wait(ctx, instanceID)
	if err != nil {
		return "", err
	}

	resp, err := g.client.PutFile(ctx, &guestapi.PutFileRequest{Path: path, Data: data},
		grpc.MaxCallSendMsgSize(guestapi.MaxMessageSize))
	if err != nil {
		return "", fileError("write", err)
	}

	return resp.GetPath(), nil
}

// GetFile reads the file at path on instanceID's machine, refusing one larger
// than maxSize, and returns it with its absolute path.
func (h *Hub) GetFile(ctx context.Context, instanceID, path string, maxSize int) ([]byte, string, error) {
	g, err := h.wait(ctx, instanceID)
	if err != nil {
		return nil, "", err
	}

	resp, err := g.client.GetFile(ctx, &guestapi.GetFileRequest{Path: path, MaxSize: uint64(maxSize)},
		grpc.MaxCallRecvMsgSize(guestapi.MaxMessageSize))
	if err != nil {
		return nil, "", fileError("read", err)
	}

	return resp.GetData(), resp.GetPath(), nil
}

func fileError(doing string, err error) error {
	st, ok := status.FromError(err)
	if !ok {
		return fmt.Errorf("%s file: %w", doing, err)
	}

	switch st.Code() {
	case codes.Unimplemented:
		return ErrFilesUnsupported
	case codes.NotFound:
		return ErrFileNotFound
	case codes.InvalidArgument, codes.FailedPrecondition:
		return &FileError{Message: st.Message()}
	}

	return fmt.Errorf("%s file: %w", doing, err)
}
