package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lxc/incus/v7/shared/api"

	"github.com/mickeyzzc/incus-fleet/internal/config"
	"github.com/mickeyzzc/incus-fleet/internal/nodejoin"
	"github.com/mickeyzzc/incus-fleet/internal/remote"
	"github.com/mickeyzzc/incus-fleet/internal/testutil/incusfake"
)

type fakeJoinClient struct {
	mu       sync.Mutex
	calls    []string
	stdins   []string
	versions []string
	failOn   string
	joined   *atomic.Bool
}

func (c *fakeJoinClient) Run(_ context.Context, command string) (string, error) {
	c.mu.Lock()
	c.calls = append(c.calls, command)
	failOn := c.failOn
	c.mu.Unlock()

	if failOn != "" && strings.Contains(command, failOn) {
		return "", context.DeadlineExceeded
	}

	if strings.Contains(command, "incus --version") {
		c.mu.Lock()
		defer c.mu.Unlock()
		if len(c.versions) == 0 {
			return "", nil
		}
		version := c.versions[0]
		c.versions = c.versions[1:]
		return version, nil
	}

	switch {
	case strings.Contains(command, "hostname -s"):
		return "new-node", nil
	case strings.Contains(command, "/etc/os-release"):
		return "trixie", nil
	case strings.Contains(command, "dpkg --print-architecture"):
		return "amd64", nil
	case strings.Contains(command, "query -X PUT"):
		if c.joined != nil {
			c.joined.Store(true)
		}
		return "", nil
	}

	return "", nil
}

func (c *fakeJoinClient) RunStdin(_ context.Context, command string, stdin string) error {
	c.mu.Lock()
	c.calls = append(c.calls, command)
	c.stdins = append(c.stdins, stdin)
	failOn := c.failOn
	c.mu.Unlock()

	if strings.Contains(command, "query -X PUT") && c.joined != nil {
		c.joined.Store(true)
	}

	if failOn != "" && strings.Contains(command, failOn) {
		return context.DeadlineExceeded
	}

	return nil
}

func (c *fakeJoinClient) Close() error { return nil }

type fakeJoinDialer struct {
	client *fakeJoinClient
	err    error
}

func (d *fakeJoinDialer) Dial(_ context.Context, _ remote.Options) (remote.Client, error) {
	if d.err != nil {
		return nil, d.err
	}

	return d.client, nil
}

// newJoinHandler rebuilds the API with a node join service backed by fakes.
func newJoinHandler(t *testing.T, f *apiFixture, dialer remote.Dialer) {
	t.Helper()

	service := nodejoin.New(f.manager, config.NodeAccess{
		SSHUser:                  "node-admin",
		SSHInsecureIgnoreHostKey: true,
	}, slog.Default(),
		nodejoin.WithDialer(dialer),
		nodejoin.WithFetch(func(context.Context, string) ([]byte, error) { return []byte("KEY"), nil }),
		nodejoin.WithWait(100*time.Millisecond, 5*time.Millisecond),
		nodejoin.WithReadyTimeout(20*time.Millisecond),
	)

	f.handler = New(Deps{
		Clusters: f.manager,
		NodeNet:  nil,
		NodeJoin: service,
		Auth:     f.auth,
		Version:  "test",
	}).Handler()
}

func TestAddClusterMemberSuccess(t *testing.T) {
	joined := &atomic.Bool{}
	source := &incusfake.Server{
		Clustered: true,
		Server: &api.Server{Environment: api.ServerEnvironment{
			ServerVersion:   "7.0.2",
			ServerClustered: true,
		}},
		Members: []api.ClusterMember{{ServerName: "test-node", Status: "Online", URL: "https://192.0.2.10:8443"}},
	}
	source.MembersFunc = func() ([]api.ClusterMember, error) {
		members := []api.ClusterMember{{ServerName: "test-node", Status: "Online", URL: "https://192.0.2.10:8443"}}
		if joined.Load() {
			members = append(members, api.ClusterMember{ServerName: "new-node", Status: "Online", URL: "https://192.0.2.55:8443"})
		}
		return members, nil
	}

	f := newAPIFixture(t, source, &incusfake.Server{})
	client := &fakeJoinClient{versions: []string{"", "7.0.2"}, joined: joined}
	newJoinHandler(t, f, &fakeJoinDialer{client: client})
	cookie := f.login(t)

	rec := f.request(t, "POST", "/api/v1/clusters/c1/members", map[string]any{
		"name":            "new-node",
		"address":         "192.0.2.55",
		"ssh_user":        "node-admin",
		"ssh_password":    "ssh-secret",
		"sudo_password":   "sudo-secret",
		"install_incus":   true,
		"ignore_host_key": true,
	}, cookie, "")
	if rec.Code != 201 {
		t.Fatalf("add member status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "new-node") || !strings.Contains(rec.Body.String(), "steps") {
		t.Fatalf("add member body = %s", rec.Body.String())
	}

	commands := strings.Join(client.calls, "\n")
	if !strings.Contains(commands, "apt-get install -y 'incus'") {
		t.Fatalf("install command missing:\n%s", commands)
	}
	if !strings.Contains(commands, "query -X PUT") {
		t.Fatalf("join command missing:\n%s", commands)
	}
}

func TestRenameClusterMember(t *testing.T) {
	source := &incusfake.Server{}
	f := newAPIFixture(t, source, &incusfake.Server{})
	cookie := f.login(t)

	rec := f.request(t, "POST", "/api/v1/clusters/c1/members/test-node/rename",
		map[string]string{"name": "renamed-node"}, cookie, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("rename member status = %d body=%s", rec.Code, rec.Body.String())
	}
	args := source.RenameMemberArgs()
	if len(args) != 1 || args[0].Name != "test-node" || args[0].Post.ServerName != "renamed-node" {
		t.Fatalf("rename args = %#v", args)
	}

	badJSON := f.request(t, "POST", "/api/v1/clusters/c1/members/test-node/rename", "{not json", cookie, "")
	if badJSON.Code != http.StatusBadRequest {
		t.Fatalf("bad JSON status = %d", badJSON.Code)
	}

	empty := f.request(t, "POST", "/api/v1/clusters/c1/members/test-node/rename",
		map[string]string{"name": " "}, cookie, "")
	if empty.Code != http.StatusBadRequest {
		t.Fatalf("empty name status = %d body=%s", empty.Code, empty.Body.String())
	}

	source.RenameMemberErr = errors.New("name exists")
	failed := f.request(t, "POST", "/api/v1/clusters/c1/members/test-node/rename",
		map[string]string{"name": "renamed-node"}, cookie, "")
	if failed.Code != http.StatusInternalServerError {
		t.Fatalf("rename failure status = %d body=%s", failed.Code, failed.Body.String())
	}
}

func TestAddClusterMemberErrors(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{Clustered: true}, &incusfake.Server{})
	cookie := f.login(t)

	// Node join is not configured on the default fixture.
	disabled := f.request(t, "POST", "/api/v1/clusters/c1/members", map[string]any{"address": "192.0.2.55"}, cookie, "")
	if disabled.Code != 503 || !strings.Contains(disabled.Body.String(), "node_unavailable") {
		t.Fatalf("disabled status = %d body=%s", disabled.Code, disabled.Body.String())
	}

	newJoinHandler(t, f, &fakeJoinDialer{err: context.DeadlineExceeded})

	badJSON := f.request(t, "POST", "/api/v1/clusters/c1/members", "{not json", cookie, "")
	if badJSON.Code != 400 {
		t.Fatalf("bad JSON status = %d", badJSON.Code)
	}

	missingAddress := f.request(t, "POST", "/api/v1/clusters/c1/members", map[string]string{"ssh_user": "node-admin"}, cookie, "")
	if missingAddress.Code != 400 {
		t.Fatalf("missing address status = %d body=%s", missingAddress.Code, missingAddress.Body.String())
	}

	conflict := f.request(t, "POST", "/api/v1/clusters/c1/members", map[string]string{
		"name":     "test-node",
		"address":  "192.0.2.55",
		"ssh_user": "node-admin",
	}, cookie, "")
	if conflict.Code != 409 {
		t.Fatalf("conflict status = %d body=%s", conflict.Code, conflict.Body.String())
	}

	unknown := f.request(t, "POST", "/api/v1/clusters/nope/members", map[string]string{
		"address":  "192.0.2.55",
		"ssh_user": "node-admin",
	}, cookie, "")
	if unknown.Code != 404 {
		t.Fatalf("unknown cluster status = %d body=%s", unknown.Code, unknown.Body.String())
	}

	dialFailure := f.request(t, "POST", "/api/v1/clusters/c1/members", map[string]string{
		"address":  "192.0.2.55",
		"ssh_user": "node-admin",
	}, cookie, "")
	if dialFailure.Code != 400 {
		t.Fatalf("dial failure status = %d body=%s", dialFailure.Code, dialFailure.Body.String())
	}
}

func TestAddClusterMemberJSONShape(t *testing.T) {
	// Guard the request decoder against silently ignoring unknown fields.
	f := newAPIFixture(t, &incusfake.Server{Clustered: true}, &incusfake.Server{})
	newJoinHandler(t, f, &fakeJoinDialer{err: context.DeadlineExceeded})
	cookie := f.login(t)

	body, err := json.Marshal(map[string]string{"address": "192.0.2.55", "unknown": "x"})
	if err != nil {
		t.Fatal(err)
	}
	rec := f.request(t, "POST", "/api/v1/clusters/c1/members", string(body), cookie, "")
	if rec.Code != 400 {
		t.Fatalf("unknown field status = %d body=%s", rec.Code, rec.Body.String())
	}
}
