package instances

import (
	"bytes"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	incus "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"

	"github.com/mickeyzzc/incus-fleet/internal/apitypes"
)

const (
	uploadFileMode    = 0o644
	consoleRetryDelay = 700 * time.Millisecond
)

// ExecRequest starts an interactive command session.
type ExecRequest struct {
	Command     []string          `json:"command"`
	Environment map[string]string `json:"environment,omitempty"`
	Cwd         string            `json:"cwd,omitempty"`
	Width       int               `json:"width,omitempty"`
	Height      int               `json:"height,omitempty"`
}

// ConsoleOp opens an interactive console operation for an instance. The
// relay reads the data and control channels from the operation metadata
// secrets and waits for completion.
//
// Force is set so a reconnecting session always takes over: when the
// browser closes a terminal the previous Incus console operation takes a
// moment to release, and without force the immediate reconnect fails with
// "This console is already connected". The takeover itself can still race
// the teardown ("attaching to the container failed"), so the open is
// retried briefly before giving up.
func (s *Service) ConsoleOp(clusterName, name string) (incus.Operation, error) {
	client, err := s.client(clusterName)
	if err != nil {
		return nil, err
	}

	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			time.Sleep(consoleRetryDelay)
		}

		// The relay reads the data and control channels from the operation
		// metadata secrets directly; the callback only satisfies the SDK check.
		op, _, err := client.ConsoleInstanceDynamic(name, api.InstanceConsolePost{
			Type:   "console",
			Force:  true,
			Width:  80,
			Height: 25,
		}, &incus.InstanceConsoleArgs{
			Control: func(*websocket.Conn) {},
		})
		if err == nil {
			return op, nil
		}

		lastErr = err
	}

	return nil, fmt.Errorf("instances: console %s:%s: %w", clusterName, name, lastErr)
}

// VgaOp opens the graphical console of a running virtual machine. The data
// channel carries the raw SPICE protocol, which the browser client speaks.
// Force mirrors ConsoleOp so SPICE reconnects never race a releasing
// session.
func (s *Service) VgaOp(clusterName, name string) (incus.Operation, error) {
	client, err := s.client(clusterName)
	if err != nil {
		return nil, err
	}

	op, _, err := client.ConsoleInstanceDynamic(name, api.InstanceConsolePost{
		Type:  "vga",
		Force: true,
	}, &incus.InstanceConsoleArgs{
		Control: func(*websocket.Conn) {},
	})
	if err != nil {
		return nil, fmt.Errorf("instances: vga console %s:%s: %w", clusterName, name, err)
	}

	return op, nil
}

// ExecOp starts an interactive command operation for an instance.
func (s *Service) ExecOp(clusterName, name string, req ExecRequest) (incus.Operation, error) {
	if len(req.Command) == 0 {
		return nil, fmt.Errorf("%w: command is required", ErrInvalidRequest)
	}

	client, err := s.client(clusterName)
	if err != nil {
		return nil, err
	}

	post := api.InstanceExecPost{
		Command:     req.Command,
		Environment: req.Environment,
		Cwd:         req.Cwd,
		Width:       req.Width,
		Height:      req.Height,
		Interactive: true,
		WaitForWS:   true,
	}

	op, err := client.ExecInstance(name, post, &incus.InstanceExecArgs{})
	if err != nil {
		return nil, fmt.Errorf("instances: exec %s:%s: %w", clusterName, name, err)
	}

	return op, nil
}

// ListFiles lists one directory inside an instance.
func (s *Service) ListFiles(clusterName, name, dir string) ([]apitypes.FileEntry, error) {
	if err := validateDirPath(dir); err != nil {
		return nil, err
	}

	client, err := s.client(clusterName)
	if err != nil {
		return nil, err
	}

	entries, err := client.ListInstanceFiles(name, path.Clean(dir))
	if err != nil {
		return nil, fmt.Errorf("instances: list files %s:%s%s: %w", clusterName, name, dir, err)
	}

	return entries, nil
}

// DownloadFile opens one file inside an instance for streaming.
func (s *Service) DownloadFile(clusterName, name, filePath string) (io.ReadCloser, error) {
	if err := validateFilePath(filePath); err != nil {
		return nil, err
	}

	client, err := s.client(clusterName)
	if err != nil {
		return nil, err
	}

	reader, _, err := client.GetInstanceFile(name, path.Clean(filePath))
	if err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("%w: %s:%s%s", ErrNotFound, clusterName, name, filePath)
		}

		return nil, fmt.Errorf("instances: get file %s:%s%s: %w", clusterName, name, filePath, err)
	}

	return reader, nil
}

// UploadFile writes one file inside an instance.
func (s *Service) UploadFile(clusterName, name, filePath string, content []byte) error {
	if err := validateFilePath(filePath); err != nil {
		return err
	}

	client, err := s.client(clusterName)
	if err != nil {
		return err
	}

	if err := client.CreateInstanceFile(name, path.Clean(filePath), incus.InstanceFileArgs{
		Type:    "file",
		Mode:    uploadFileMode,
		Content: bytes.NewReader(content),
	}); err != nil {
		return fmt.Errorf("instances: write file %s:%s%s: %w", clusterName, name, filePath, err)
	}

	return nil
}

// MkdirDir creates a directory inside an instance.
func (s *Service) MkdirDir(clusterName, name, dir string) error {
	if err := validateDirPath(dir); err != nil {
		return err
	}

	client, err := s.client(clusterName)
	if err != nil {
		return err
	}

	if err := client.MkdirInstanceDir(name, path.Clean(dir)); err != nil {
		return fmt.Errorf("instances: mkdir %s:%s%s: %w", clusterName, name, dir, err)
	}

	return nil
}

// DeleteFile removes one file inside an instance.
func (s *Service) DeleteFile(clusterName, name, filePath string) error {
	if err := validateFilePath(filePath); err != nil {
		return err
	}

	client, err := s.client(clusterName)
	if err != nil {
		return err
	}

	if err := client.DeleteInstanceFile(name, path.Clean(filePath)); err != nil {
		return fmt.Errorf("instances: delete file %s:%s%s: %w", clusterName, name, filePath, err)
	}

	return nil
}

// validateFilePath requires an absolute file path without traversal.
func validateFilePath(filePath string) error {
	if !strings.HasPrefix(filePath, "/") {
		return fmt.Errorf("%w: file path must be absolute", ErrInvalidRequest)
	}

	return validateNoTraversal(filePath)
}

// validateDirPath requires an absolute directory path without traversal.
func validateDirPath(dir string) error {
	return validateFilePath(dir)
}

func validateNoTraversal(value string) error {
	cleaned := path.Clean(value)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return fmt.Errorf("%w: path traversal is not allowed", ErrInvalidRequest)
	}

	return nil
}
