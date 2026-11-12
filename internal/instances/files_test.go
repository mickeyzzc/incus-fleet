package instances

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/lxc/incus/v7/shared/api"

	"github.com/mickeyzzc/incus-fleet/internal/testutil/incusfake"
)

func TestExecOpValidation(t *testing.T) {
	srv := &incusfake.Server{}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})

	if _, err := svc.ExecOp("c1", "web", ExecRequest{}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("ExecOp() empty command = %v", err)
	}
	if _, err := svc.ExecOp("missing", "web", ExecRequest{Command: []string{"bash"}}); err == nil {
		t.Fatal("ExecOp() expected unknown cluster error")
	}

	srv.ExecErr = errors.New("not running")
	if _, err := svc.ExecOp("c1", "web", ExecRequest{Command: []string{"bash"}}); err == nil {
		t.Fatal("ExecOp() expected propagated error")
	}
}

func TestConsoleOpErrors(t *testing.T) {
	srv := &incusfake.Server{}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})

	if _, _, err := svc.ConsoleOp("missing", "web"); err == nil {
		t.Fatal("ConsoleOp() expected unknown cluster error")
	}

	srv.ConsoleErr = errors.New("console unavailable")
	if _, _, err := svc.ConsoleOp("c1", "web"); err == nil {
		t.Fatal("ConsoleOp() expected propagated error")
	}
}

func TestFilePathValidation(t *testing.T) {
	srv := &incusfake.Server{}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})

	if _, err := svc.ListFiles("c1", "web", "relative"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("ListFiles() relative path = %v", err)
	}
	if err := svc.UploadFile("c1", "web", "relative", nil); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("UploadFile() relative path = %v", err)
	}
	if err := svc.DeleteFile("c1", "web", ""); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("DeleteFile() empty path = %v", err)
	}
	if err := svc.MkdirDir("c1", "web", ""); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("MkdirDir() empty path = %v", err)
	}
	if _, err := svc.DownloadFile("c1", "web", ""); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("DownloadFile() empty path = %v", err)
	}
	if _, err := svc.ListFiles("missing", "web", "/opt"); err == nil {
		t.Fatal("ListFiles() expected unknown cluster error")
	}
}

func TestFileOperations(t *testing.T) {
	srv := &incusfake.Server{
		Files: map[string][]byte{"/etc/hostname": []byte("web")},
	}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})

	if err := svc.UploadFile("c1", "web", "/etc/motd", []byte("welcome")); err != nil {
		t.Fatalf("UploadFile() error = %v", err)
	}
	args := srv.CreateFileArgs()
	if len(args) != 1 || args[0].Path != "/etc/motd" || string(args[0].Content) != "welcome" || args[0].Mode != 0o644 {
		t.Fatalf("upload args = %#v", args)
	}

	reader, err := svc.DownloadFile("c1", "web", "/etc/hostname")
	if err != nil {
		t.Fatalf("DownloadFile() error = %v", err)
	}
	buf := make([]byte, 16)
	n, _ := reader.Read(buf)
	if strings.TrimSpace(string(buf[:n])) != "web" {
		t.Fatalf("downloaded = %q", string(buf[:n]))
	}

	if err := svc.MkdirDir("c1", "web", "/opt/app"); err != nil {
		t.Fatalf("MkdirDir() error = %v", err)
	}
	if err := svc.DeleteFile("c1", "web", "/etc/motd"); err != nil {
		t.Fatalf("DeleteFile() error = %v", err)
	}

	srv.GetFileErr = api.StatusErrorf(http.StatusNotFound, "missing")
	if _, err := svc.DownloadFile("c1", "web", "/missing"); err == nil {
		t.Fatal("DownloadFile() expected propagated error")
	}
	srv.CreateFileErr = errors.New("read only")
	if err := svc.UploadFile("c1", "web", "/etc/motd", []byte("x")); err == nil {
		t.Fatal("UploadFile() expected propagated error")
	}
	srv.CreateFileErr = nil
	srv.MkdirErr = errors.New("no space")
	if err := svc.MkdirDir("c1", "web", "/opt/app"); err == nil {
		t.Fatal("MkdirDir() expected propagated error")
	}
	srv.MkdirErr = nil
	srv.DeleteFileErr = errors.New("locked")
	if err := svc.DeleteFile("c1", "web", "/etc/motd"); err == nil {
		t.Fatal("DeleteFile() expected propagated error")
	}
	srv.DeleteFileErr = nil
	srv.ListFilesErr = errors.New("sftp down")
	if _, err := svc.ListFiles("c1", "web", "/opt"); err == nil {
		t.Fatal("ListFiles() expected propagated error")
	}
}
