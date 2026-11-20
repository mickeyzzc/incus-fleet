package clusters

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/pkg/sftp"
)

// pipeSFTPClient serves the real filesystem over an in-process SFTP channel.
func pipeSFTPClient(t *testing.T) *sftp.Client {
	t.Helper()

	serverConn, clientConn := net.Pipe()
	server, err := sftp.NewServer(serverConn)
	if err != nil {
		t.Fatalf("sftp.NewServer() error = %v", err)
	}

	go func() {
		_ = server.Serve()
	}()
	t.Cleanup(func() {
		_ = server.Close()
	})

	client, err := sftp.NewClientPipe(clientConn, clientConn)
	if err != nil {
		t.Fatalf("sftp.NewClientPipe() error = %v", err)
	}
	t.Cleanup(func() {
		_ = client.Close()
	})

	return client
}

func TestAugmentedClientSFTP(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app.log"), []byte("hello"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o700); err != nil {
		t.Fatalf("mkdir fixture: %v", err)
	}

	wrapped := &augmentedClient{
		sftpFor: func(string) (*sftp.Client, error) {
			return pipeSFTPClient(t), nil
		},
	}

	entries, err := wrapped.ListInstanceFiles("web", dir)
	if err != nil {
		t.Fatalf("ListInstanceFiles() error = %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %#v", entries)
	}
	if !entries[0].IsDir || entries[0].Name != "sub" {
		t.Fatalf("entries[0] = %#v, want directory first", entries[0])
	}
	if entries[1].Name != "app.log" || entries[1].Size != 5 {
		t.Fatalf("entries[1] = %#v", entries[1])
	}

	nested := filepath.Join(dir, "created")
	if err := wrapped.MkdirInstanceDir("web", nested); err != nil {
		t.Fatalf("MkdirInstanceDir() error = %v", err)
	}
	info, err := os.Stat(nested)
	if err != nil || !info.IsDir() {
		t.Fatalf("created dir stat = %v", err)
	}

	// Failure paths surface the SFTP errors wrapped with context.
	if _, err := wrapped.ListInstanceFiles("web", filepath.Join(dir, "missing")); err == nil {
		t.Fatal("ListInstanceFiles() expected missing dir error")
	}
}

func TestSFTPErrorPaths(t *testing.T) {
	failing := &augmentedClient{
		sftpFor: func(string) (*sftp.Client, error) {
			return nil, os.ErrPermission
		},
	}

	if _, err := failing.ListInstanceFiles("web", "/"); err == nil {
		t.Fatal("ListInstanceFiles() with a broken client returned no error")
	}
	if err := failing.MkdirInstanceDir("web", "/tmp"); err == nil {
		t.Fatal("MkdirInstanceDir() with a broken client returned no error")
	}

	// A real client pointed at a missing directory exercises the read failure.
	wrapped := &augmentedClient{
		sftpFor: func(string) (*sftp.Client, error) {
			return pipeSFTPClient(t), nil
		},
	}
	if _, err := wrapped.ListInstanceFiles("web", "/definitely/not/here"); err == nil {
		t.Fatal("ListInstanceFiles() on a missing directory returned no error")
	}
}
