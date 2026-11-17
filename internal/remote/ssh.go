// Package remote runs commands on node hosts that are not the machine
// incus-fleet itself runs on. The fleet service is unprivileged; remote node
// operations therefore connect over SSH and escalate through the sudo
// whitelist installed by deploy/nodesudoers (or passwordless sudo when the
// operator already provides it).
package remote

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// Options describes one SSH connection to a node host.
type Options struct {
	// Address is the host or host:port to connect to.
	Address string
	// User is the SSH login account.
	User string
	// Password enables password authentication when non-empty.
	Password string
	// KeyPath points at an unencrypted private key (OpenSSH format).
	KeyPath string
	// KnownHostsPath enables host key verification against a known_hosts
	// file. Leave empty only when InsecureIgnoreHostKey is explicitly set.
	KnownHostsPath string
	// InsecureIgnoreHostKey disables host key verification. This is a
	// deliberate opt-out for lab environments with stable private networks.
	InsecureIgnoreHostKey bool
	// Timeout bounds dialing and authentication.
	Timeout time.Duration
}

// Dialer opens SSH connections. Tests supply a fake dialer.
type Dialer interface {
	Dial(ctx context.Context, opts Options) (Client, error)
}

// Client runs commands over one SSH connection.
type Client interface {
	Run(ctx context.Context, command string) (string, error)
	RunStdin(ctx context.Context, command string, stdin string) error
	Close() error
}

// SSHDialer dials real SSH servers.
type SSHDialer struct{}

// Dial implements Dialer.
func (SSHDialer) Dial(ctx context.Context, opts Options) (Client, error) {
	if strings.TrimSpace(opts.Address) == "" {
		return nil, errors.New("remote: ssh address is required")
	}
	if strings.TrimSpace(opts.User) == "" {
		return nil, errors.New("remote: ssh user is required")
	}

	auth, err := authMethods(opts)
	if err != nil {
		return nil, err
	}

	hostKey, err := hostKeyCallback(opts)
	if err != nil {
		return nil, err
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}

	config := &ssh.ClientConfig{
		User:            opts.User,
		Auth:            auth,
		HostKeyCallback: hostKey,
		Timeout:         timeout,
	}

	address := withDefaultPort(opts.Address)

	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("remote: dial %s: %w", address, err)
	}

	clientConn, chans, reqs, err := ssh.NewClientConn(conn, address, config)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("remote: ssh handshake %s: %w", address, err)
	}

	return &sshClient{client: ssh.NewClient(clientConn, chans, reqs)}, nil
}

func authMethods(opts Options) ([]ssh.AuthMethod, error) {
	methods := make([]ssh.AuthMethod, 0, 2)

	if opts.KeyPath != "" {
		key, err := os.ReadFile(opts.KeyPath) //nolint:gosec // operator-supplied key path
		if err != nil {
			return nil, fmt.Errorf("remote: read ssh key: %w", err)
		}

		signer, err := ssh.ParsePrivateKey(key)
		if err != nil {
			return nil, fmt.Errorf("remote: parse ssh key: %w", err)
		}

		methods = append(methods, ssh.PublicKeys(signer))
	}

	if opts.Password != "" {
		methods = append(methods, ssh.Password(opts.Password))
	}

	if len(methods) == 0 {
		return nil, errors.New("remote: ssh key or password is required")
	}

	return methods, nil
}

func hostKeyCallback(opts Options) (ssh.HostKeyCallback, error) {
	if opts.InsecureIgnoreHostKey {
		return ssh.InsecureIgnoreHostKey(), nil //nolint:gosec // explicit operator opt-out
	}

	if opts.KnownHostsPath == "" {
		return nil, errors.New("remote: known_hosts path or insecure_ignore_host_key is required")
	}

	callback, err := knownhosts.New(opts.KnownHostsPath)
	if err != nil {
		return nil, fmt.Errorf("remote: load known_hosts: %w", err)
	}

	return callback, nil
}

func withDefaultPort(address string) string {
	if _, _, err := net.SplitHostPort(address); err == nil {
		return address
	}

	if strings.Contains(address, ":") && strings.Count(address, ":") > 1 {
		// Bare IPv6 address.
		return net.JoinHostPort(address, "22")
	}

	return net.JoinHostPort(address, "22")
}

// sshClient implements Client.
type sshClient struct {
	client *ssh.Client
}

// Run implements Client.
func (c *sshClient) Run(ctx context.Context, command string) (string, error) {
	session, err := c.client.NewSession()
	if err != nil {
		return "", fmt.Errorf("remote: new session: %w", err)
	}
	defer session.Close() //nolint:errcheck // best-effort cleanup

	stop := closeOnDone(ctx, session)
	defer stop()

	out, err := session.CombinedOutput(command)
	if err != nil {
		return string(out), fmt.Errorf("remote: command failed: %w: %s", err, strings.TrimSpace(string(out)))
	}

	return string(out), nil
}

// RunStdin implements Client.
func (c *sshClient) RunStdin(ctx context.Context, command string, stdin string) error {
	session, err := c.client.NewSession()
	if err != nil {
		return fmt.Errorf("remote: new session: %w", err)
	}
	defer session.Close() //nolint:errcheck // best-effort cleanup

	session.Stdin = strings.NewReader(stdin)

	stop := closeOnDone(ctx, session)
	defer stop()

	out, err := session.CombinedOutput(command)
	if err != nil {
		return fmt.Errorf("remote: command failed: %w: %s", err, strings.TrimSpace(string(out)))
	}

	return nil
}

// Close implements Client.
func (c *sshClient) Close() error {
	return c.client.Close()
}

// closeOnDone closes the session when the context is cancelled. The returned
// function stops the watcher.
func closeOnDone(ctx context.Context, session *ssh.Session) func() {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = session.Close()
		case <-done:
		}
	}()

	return func() { close(done) }
}

// ShellQuote quotes one argument for safe use in a POSIX shell command.
func ShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// HostFromURL extracts the host from an Incus member URL such as
// https://198.51.100.10:8443.
func HostFromURL(raw string) (string, error) {
	if raw == "" {
		return "", errors.New("remote: empty member URL")
	}

	trimmed := strings.TrimPrefix(strings.TrimPrefix(raw, "https://"), "http://")
	host, _, err := net.SplitHostPort(trimmed)
	if err == nil {
		return host, nil
	}

	// No port in the URL.
	host = strings.TrimSuffix(trimmed, "/")
	if host == "" || strings.Contains(host, "/") {
		return "", fmt.Errorf("remote: invalid member URL %q", raw)
	}

	return host, nil
}

// JoinHostPort builds host:port while accepting bare IPv6 addresses.
func JoinHostPort(host string, port int) string {
	if port <= 0 {
		port = 22
	}

	return net.JoinHostPort(host, strconv.Itoa(port))
}
