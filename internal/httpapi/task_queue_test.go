package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/mickeyzzc/incus-fleet/internal/store"
	"github.com/mickeyzzc/incus-fleet/internal/testutil/incusfake"
)

func TestEnqueueHostTask(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	cookie := f.login(t)

	rec := f.request(t, http.MethodPost, "/api/v1/tasks", map[string]any{
		"cluster": "c1",
		"member":  "test-node",
		"plan":    map[string]any{"action": "link-state", "interface": "eth0", "up": false},
	}, cookie, "")
	if rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), `"state":"queued"`) {
		t.Fatalf("enqueue = %d body=%s", rec.Code, rec.Body.String())
	}
	tasks, err := f.store.ListHostTasks(context.Background(), "c1", "test-node", 10)
	if err != nil || len(tasks) != 1 || tasks[0].State != store.HostTaskQueued || tasks[0].Type != "agent.network" {
		t.Fatalf("tasks = %#v, %v", tasks, err)
	}
	if tasks[0].PlanHash == "" {
		t.Fatal("plan hash is empty")
	}

	cases := []struct {
		name string
		body any
		code int
	}{
		{"missing plan", map[string]any{"cluster": "c1", "member": "test-node"}, http.StatusBadRequest},
		{"bad type", map[string]any{"cluster": "c1", "member": "test-node", "type": "shell",
			"plan": map[string]any{"action": "link-state"}}, http.StatusBadRequest},
		{"bad action", map[string]any{"cluster": "c1", "member": "test-node",
			"plan": map[string]any{"action": "shell"}}, http.StatusBadRequest},
		{"unknown member", map[string]any{"cluster": "c1", "member": "missing",
			"plan": map[string]any{"action": "link-state"}}, http.StatusNotFound},
		{"unknown cluster", map[string]any{"cluster": "missing", "member": "test-node",
			"plan": map[string]any{"action": "link-state"}}, http.StatusNotFound},
	}
	for _, tc := range cases {
		rec := f.request(t, http.MethodPost, "/api/v1/tasks", tc.body, cookie, "")
		if rec.Code != tc.code {
			t.Fatalf("%s = %d body=%s", tc.name, rec.Code, rec.Body.String())
		}
	}
}

func TestEnqueueAdditionalAgentTasks(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	cookie := f.login(t)

	snapshot := f.request(t, http.MethodPost, "/api/v1/tasks", map[string]any{
		"cluster": "c1", "member": "test-node", "type": "agent.snapshot",
	}, cookie, "")
	if snapshot.Code != http.StatusAccepted {
		t.Fatalf("snapshot = %d body=%s", snapshot.Code, snapshot.Body.String())
	}

	unsigned := f.request(t, http.MethodPost, "/api/v1/tasks", map[string]any{
		"cluster": "c1", "member": "test-node", "type": "agent.bootstrap",
		"plan": map[string]any{"task_id": "b1"},
	}, cookie, "")
	if unsigned.Code != http.StatusBadRequest {
		t.Fatalf("unsigned bootstrap = %d body=%s", unsigned.Code, unsigned.Body.String())
	}

	signed := f.request(t, http.MethodPost, "/api/v1/tasks", map[string]any{
		"cluster": "c1", "member": "test-node", "type": "agent.bootstrap",
		"plan": map[string]any{"task_id": "b1", "signature": "signed"},
	}, cookie, "")
	if signed.Code != http.StatusAccepted {
		t.Fatalf("signed bootstrap = %d body=%s", signed.Code, signed.Body.String())
	}
}

func TestEnqueueHostTaskDisabled(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	cookie := f.login(t)
	f.handler = New(Deps{Auth: f.auth, Version: "test"}).Handler()
	rec := f.request(t, http.MethodPost, "/api/v1/tasks", map[string]any{"cluster": "c1", "member": "test-node"}, cookie, "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("disabled = %d", rec.Code)
	}
}

// Without a dispatcher (and without a shared database another instance could
// claim from) the endpoint must refuse the work instead of accepting it into
// a queue no one drains.
func TestEnqueueHostTaskWithoutWorker(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	cookie := f.login(t)
	deps := Deps{Auth: f.auth, Store: f.store, Clusters: f.manager, Version: "test"}
	f.handler = New(deps).Handler()

	rec := f.request(t, http.MethodPost, "/api/v1/tasks", map[string]any{
		"cluster": "c1", "member": "test-node",
		"plan": map[string]any{"action": "link-state", "interface": "eth0", "up": false},
	}, cookie, "")
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "no_worker") {
		t.Fatalf("no worker = %d body=%s", rec.Code, rec.Body.String())
	}
	tasks, err := f.store.ListHostTasks(context.Background(), "c1", "test-node", 10)
	if err != nil || len(tasks) != 0 {
		t.Fatalf("tasks = %#v, %v", tasks, err)
	}
}

func TestEnqueueHostTaskValidation(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	cookie := f.login(t)
	badJSON := f.request(t, http.MethodPost, "/api/v1/tasks", "{bad", cookie, "")
	if badJSON.Code != http.StatusBadRequest {
		t.Fatalf("bad json = %d", badJSON.Code)
	}
	missing := f.request(t, http.MethodPost, "/api/v1/tasks", map[string]any{"cluster": "c1"}, cookie, "")
	if missing.Code != http.StatusBadRequest {
		t.Fatalf("missing = %d", missing.Code)
	}
	f.handler = New(Deps{Auth: f.auth, Store: f.store, Version: "test"}).Handler()
	noClusters := f.request(t, http.MethodPost, "/api/v1/tasks", map[string]any{
		"cluster": "c1", "member": "test-node", "plan": map[string]any{"action": "link-state"},
	}, cookie, "")
	if noClusters.Code != http.StatusServiceUnavailable {
		t.Fatalf("no clusters = %d body=%s", noClusters.Code, noClusters.Body.String())
	}
}
