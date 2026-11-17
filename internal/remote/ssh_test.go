package remote

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestShellQuote(t *testing.T) {
	cases := map[string]string{
		"ip":    "'ip'",
		"a b":   "'a b'",
		"it's":  `'it'\''s'`,
		"$HOME": "'$HOME'",
		"a\nb":  "'a\nb'",
		"":      "''",
	}
	for input, want := range cases {
		if got := ShellQuote(input); got != want {
			t.Fatalf("ShellQuote(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestHostFromURL(t *testing.T) {
	cases := []struct {
		url  string
		host string
		err  bool
	}{
		{"https://198.51.100.10:8443", "198.51.100.10", false},
		{"http://node.local:8443", "node.local", false},
		{"https://node.local", "node.local", false},
		{"", "", true},
		{"https://bad/path", "", true},
	}
	for _, tc := range cases {
		host, err := HostFromURL(tc.url)
		if tc.err {
			if err == nil {
				t.Fatalf("HostFromURL(%q) error = nil", tc.url)
			}
			continue
		}
		if err != nil || host != tc.host {
			t.Fatalf("HostFromURL(%q) = %q, %v", tc.url, host, err)
		}
	}
}

func TestJoinHostPort(t *testing.T) {
	if got := JoinHostPort("192.0.2.1", 22); got != "192.0.2.1:22" {
		t.Fatalf("JoinHostPort() = %q", got)
	}
	if got := JoinHostPort("2001:db8::1", 2222); got != "[2001:db8::1]:2222" {
		t.Fatalf("JoinHostPort(ipv6) = %q", got)
	}
	if got := JoinHostPort("node", 0); got != "node:22" {
		t.Fatalf("JoinHostPort(default port) = %q", got)
	}
}

func TestWithDefaultPort(t *testing.T) {
	if got := withDefaultPort("node"); got != "node:22" {
		t.Fatalf("withDefaultPort(node) = %q", got)
	}
	if got := withDefaultPort("node:2222"); got != "node:2222" {
		t.Fatalf("withDefaultPort(port) = %q", got)
	}
	if got := withDefaultPort("2001:db8::1"); got != "[2001:db8::1]:22" {
		t.Fatalf("withDefaultPort(ipv6) = %q", got)
	}
}

func TestAuthAndHostKeyErrors(t *testing.T) {
	if _, err := authMethods(Options{}); err == nil {
		t.Fatal("authMethods() without credentials error = nil")
	}
	if _, err := authMethods(Options{KeyPath: "/does/not/exist"}); err == nil {
		t.Fatal("authMethods() missing key error = nil")
	}

	dir := t.TempDir()
	badKey := filepath.Join(dir, "bad")
	if err := os.WriteFile(badKey, []byte("not a key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := authMethods(Options{KeyPath: badKey}); err == nil {
		t.Fatal("authMethods() invalid key error = nil")
	}

	if _, err := hostKeyCallback(Options{}); err == nil {
		t.Fatal("hostKeyCallback() without known_hosts error = nil")
	}
	if _, err := hostKeyCallback(Options{InsecureIgnoreHostKey: true}); err != nil {
		t.Fatalf("hostKeyCallback(insecure) error = %v", err)
	}
	if _, err := hostKeyCallback(Options{KnownHostsPath: filepath.Join(dir, "missing")}); err == nil {
		t.Fatal("hostKeyCallback() missing known_hosts error = nil")
	}
}

func TestSSHDialerValidation(t *testing.T) {
	dialer := SSHDialer{}
	if _, err := dialer.Dial(context.Background(), Options{}); err == nil {
		t.Fatal("Dial() without address error = nil")
	}
	if _, err := dialer.Dial(context.Background(), Options{Address: "node"}); err == nil {
		t.Fatal("Dial() without user error = nil")
	}
	if _, err := dialer.Dial(context.Background(), Options{
		Address:  "127.0.0.1:1",
		User:     "node-admin",
		Password: "secret",
	}); err == nil {
		t.Fatal("Dial() without host key policy error = nil")
	}

	// Connection refused surfaces as a dial error.
	clientKey := writeClientKey(t, t.TempDir())
	_, err := dialer.Dial(context.Background(), Options{
		Address:               "127.0.0.1:1",
		User:                  "node-admin",
		KeyPath:               clientKey,
		InsecureIgnoreHostKey: true,
		Timeout:               2 * time.Second,
	})
	if err == nil {
		t.Fatal("Dial() refused connection error = nil")
	}
}

func TestSSHRunAndStdin(t *testing.T) {
	server := newTestSSHServer(t, "secret", true)
	defer server.Close()

	client, err := SSHDialer{}.Dial(context.Background(), Options{
		Address:               server.Addr(),
		User:                  "node-admin",
		Password:              "secret",
		InsecureIgnoreHostKey: true,
		Timeout:               5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer func() { _ = client.Close() }()

	out, err := client.Run(context.Background(), "printf hello")
	if err != nil || out != "hello" {
		t.Fatalf("Run() = %q, %v", out, err)
	}

	if err := client.RunStdin(context.Background(), "grep needle", "needle\nhaystack\n"); err != nil {
		t.Fatalf("RunStdin() error = %v", err)
	}

	if _, err := client.Run(context.Background(), "exit 7"); err == nil {
		t.Fatal("Run(exit 7) error = nil")
	}
	if err := client.RunStdin(context.Background(), "exit 4", "data"); err == nil {
		t.Fatal("RunStdin(exit 4) error = nil")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := client.Run(ctx, "sleep 5"); err == nil {
		t.Fatal("Run() cancelled command error = nil")
	}
}

func TestSSHKeyAuthenticationAndKnownHosts(t *testing.T) {
	server := newTestSSHServer(t, "", false)
	defer server.Close()

	dir := t.TempDir()
	keyPath := writeClientKey(t, dir)
	knownHosts := filepath.Join(dir, "known_hosts")
	if err := os.WriteFile(knownHosts, []byte(knownHostsLine(server.Addr(), server.HostKey())), 0o600); err != nil {
		t.Fatal(err)
	}

	client, err := SSHDialer{}.Dial(context.Background(), Options{
		Address:        server.Addr(),
		User:           "node-admin",
		KeyPath:        keyPath,
		KnownHostsPath: knownHosts,
		Timeout:        5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Dial(key+known_hosts) error = %v", err)
	}
	_ = client.Close()

	// A different host key must be rejected.
	wrong := filepath.Join(dir, "wrong_known_hosts")
	otherPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	otherSigner, err := ssh.NewPublicKey(otherPub)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(wrong, []byte(knownHostsLine(server.Addr(), otherSigner)), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := (SSHDialer{}).Dial(context.Background(), Options{
		Address:        server.Addr(),
		User:           "node-admin",
		KeyPath:        keyPath,
		KnownHostsPath: wrong,
		Timeout:        5 * time.Second,
	}); err == nil {
		t.Fatal("Dial() host key mismatch error = nil")
	}
}

func writeClientKey(t *testing.T, dir string) string {
	t.Helper()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_ = pub

	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "id_ed25519")
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

func knownHostsLine(addr string, key ssh.PublicKey) string {
	host := addr
	if h, port, err := net.SplitHostPort(addr); err == nil {
		host = "[" + h + "]:" + port
	}

	authorized := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))

	return host + " " + authorized + "\n"
}

// testSSHServer is a minimal in-process SSH server that executes commands
// through the local shell.
type testSSHServer struct {
	listener net.Listener
	config   *ssh.ServerConfig
	hostKey  ssh.PublicKey
	done     chan struct{}
}

func newTestSSHServer(t *testing.T, password string, passwordAuth bool) *testSSHServer {
	t.Helper()

	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}

	config := &ssh.ServerConfig{}
	if passwordAuth {
		config.PasswordCallback = func(_ ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if string(pass) != password {
				return nil, fmt.Errorf("invalid password")
			}
			return nil, nil
		}
	} else {
		config.PublicKeyCallback = func(_ ssh.ConnMetadata, _ ssh.PublicKey) (*ssh.Permissions, error) {
			return nil, nil
		}
	}
	config.AddHostKey(signer)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	server := &testSSHServer{
		listener: listener,
		config:   config,
		hostKey:  signer.PublicKey(),
		done:     make(chan struct{}),
	}

	go server.serve()

	return server
}

func (s *testSSHServer) Addr() string { return s.listener.Addr().String() }

func (s *testSSHServer) HostKey() ssh.PublicKey { return s.hostKey }

func (s *testSSHServer) Close() {
	close(s.done)
	_ = s.listener.Close()
}

func (s *testSSHServer) serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}

		go s.handle(conn)
	}
}

func (s *testSSHServer) handle(conn net.Conn) {
	defer func() { _ = conn.Close() }()

	_, chans, reqs, err := ssh.NewServerConn(conn, s.config)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(reqs)

	for newChannel := range chans {
		if newChannel.ChannelType() != "session" {
			_ = newChannel.Reject(ssh.UnknownChannelType, "unsupported")
			continue
		}

		channel, requests, err := newChannel.Accept()
		if err != nil {
			continue
		}

		go s.session(channel, requests)
	}
}

func (s *testSSHServer) session(channel ssh.Channel, requests <-chan *ssh.Request) {
	defer func() { _ = channel.Close() }()

	for request := range requests {
		if request.Type != "exec" {
			_ = request.Reply(false, nil)
			continue
		}

		command := decodeExecPayload(request.Payload)
		_ = request.Reply(true, nil)

		// #nosec G204 -- the test SSH server intentionally executes the
		// scripted commands used by the runner tests.
		cmd := exec.Command("sh", "-c", command)
		cmd.Stdin = channel
		cmd.Stdout = channel
		cmd.Stderr = channel

		status := uint32(0)
		if err := cmd.Run(); err != nil {
			status = 1
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				status = uint32(exitErr.ExitCode()) //nolint:gosec // process exit codes fit in uint32
			}
		}

		_, _ = channel.SendRequest("exit-status", false, exitStatusPayload(status))
		return
	}
}

func decodeExecPayload(payload []byte) string {
	if len(payload) < 4 {
		return ""
	}
	length := binary.BigEndian.Uint32(payload[:4])
	if len(payload)-4 < int(length) {
		return ""
	}

	return string(payload[4 : 4+length])
}

func exitStatusPayload(status uint32) []byte {
	payload := make([]byte, 4)
	binary.BigEndian.PutUint32(payload, status)

	return payload
}
