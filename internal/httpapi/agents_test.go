package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	"github.com/mickeyzzc/incus-fleet/internal/agentgw"
	"github.com/mickeyzzc/incus-fleet/internal/agentpb"
	"github.com/mickeyzzc/incus-fleet/internal/store"
	"github.com/mickeyzzc/incus-fleet/internal/testutil/incusfake"
)

type fakeAgentDispatcher struct {
	result *agentpb.TaskResult
	err    error
	calls  int
}

func (f *fakeAgentDispatcher) Dispatch(_ context.Context, _ string, _ agentpb.TaskType, _ []byte, _ time.Duration) (*agentpb.TaskResult, error) {
	f.calls++
	return f.result, f.err
}

func TestAgentEnrollmentTokenAndList(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	cookie := f.login(t)

	rec := f.request(t, http.MethodPost, "/api/v1/agents/enrollment-tokens", map[string]any{
		"cluster": "c1",
		"member":  "node-a",
	}, cookie, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("token status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Token     string    `json:"token"`
		Cluster   string    `json:"cluster"`
		Member    string    `json:"member"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Token == "" || payload.Cluster != "c1" || payload.Member != "node-a" || payload.ExpiresAt.IsZero() {
		t.Fatalf("token payload = %#v", payload)
	}
	if _, err := f.store.ConsumeEnrollmentToken(context.Background(), agentgw.HashToken(payload.Token), "c1", "node-a", time.Now()); err != nil {
		t.Fatalf("stored token not consumable: %v", err)
	}

	bad := f.request(t, http.MethodPost, "/api/v1/agents/enrollment-tokens", map[string]any{
		"cluster": "missing",
		"member":  "node-a",
	}, cookie, "")
	if bad.Code != http.StatusNotFound {
		t.Fatalf("bad cluster status = %d body=%s", bad.Code, bad.Body.String())
	}

	ttl := f.request(t, http.MethodPost, "/api/v1/agents/enrollment-tokens", map[string]any{
		"cluster": "c1", "member": "node-a", "ttl_seconds": 7200,
	}, cookie, "")
	if ttl.Code != http.StatusBadRequest {
		t.Fatalf("bad ttl status = %d body=%s", ttl.Code, ttl.Body.String())
	}

	agent := &store.Agent{
		ID: "agent-1", Cluster: "c1", Member: "node-a", Status: store.AgentOnline,
		CertFingerprint: "fp", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := f.store.UpsertAgent(context.Background(), agent); err != nil {
		t.Fatal(err)
	}
	list := f.request(t, http.MethodGet, "/api/v1/agents", nil, cookie, "")
	if list.Code != http.StatusOK || !containsAll(list.Body.String(), "agent-1", "node-a") {
		t.Fatalf("list agents = %d body=%s", list.Code, list.Body.String())
	}
}

func TestAgentSnapshotEndpoint(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	snapshot := &agentpb.NodenetSnapshot{
		Host: "node-a",
		Nics: []*agentpb.NIC{{Name: "eth0", State: "up", Kind: "ethernet"}},
	}
	payload, err := protojson.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	dispatcher := &fakeAgentDispatcher{result: &agentpb.TaskResult{
		TaskId: "task-1", State: agentpb.TaskState_TASK_STATE_SUCCEEDED, Payload: payload,
	}}
	f.handler = New(Deps{
		Clusters: f.manager, Store: f.store, Agent: dispatcher, Auth: f.auth, Version: "test",
	}).Handler()
	cookie := f.login(t)

	rec := f.request(t, http.MethodGet, "/api/v1/agents/agent-1/snapshot", nil, cookie, "")
	if rec.Code != http.StatusOK || !containsAll(rec.Body.String(), `"host":"node-a"`, `"eth0"`) {
		t.Fatalf("snapshot = %d body=%s", rec.Code, rec.Body.String())
	}
	if dispatcher.calls != 1 {
		t.Fatalf("dispatch calls = %d", dispatcher.calls)
	}

	dispatcher.err = agentgw.ErrAgentOffline
	offline := f.request(t, http.MethodGet, "/api/v1/agents/agent-1/snapshot", nil, cookie, "")
	if offline.Code != http.StatusServiceUnavailable {
		t.Fatalf("offline status = %d body=%s", offline.Code, offline.Body.String())
	}

	dispatcher.err = nil
	dispatcher.result = &agentpb.TaskResult{State: agentpb.TaskState_TASK_STATE_FAILED, Error: "boom"}
	failed := f.request(t, http.MethodGet, "/api/v1/agents/agent-1/snapshot", nil, cookie, "")
	if failed.Code != http.StatusBadGateway {
		t.Fatalf("failed status = %d body=%s", failed.Code, failed.Body.String())
	}
}

func containsAll(value string, needles ...string) bool {
	for _, needle := range needles {
		if !strings.Contains(value, needle) {
			return false
		}
	}
	return true
}

func TestAgentEndpointsDisabledAndValidation(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	f.handler = New(Deps{Auth: f.auth, Version: "test"}).Handler()
	cookie := f.login(t)

	for _, path := range []string{"/api/v1/agents", "/api/v1/agents/agent-1/snapshot"} {
		rec := f.request(t, http.MethodGet, path, nil, cookie, "")
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s status = %d body=%s", path, rec.Code, rec.Body.String())
		}
	}
	token := f.request(t, http.MethodPost, "/api/v1/agents/enrollment-tokens", map[string]string{"cluster": "c1", "member": "node-a"}, cookie, "")
	if token.Code != http.StatusServiceUnavailable {
		t.Fatalf("token disabled status = %d body=%s", token.Code, token.Body.String())
	}

	f = newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	cookie = f.login(t)
	bad := f.request(t, http.MethodPost, "/api/v1/agents/enrollment-tokens", "{bad", cookie, "")
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("bad token body status = %d", bad.Code)
	}
	missing := f.request(t, http.MethodPost, "/api/v1/agents/enrollment-tokens", map[string]string{"cluster": "c1"}, cookie, "")
	if missing.Code != http.StatusBadRequest {
		t.Fatalf("missing member status = %d body=%s", missing.Code, missing.Body.String())
	}
}

func TestAgentSnapshotInvalidPayloadAndTimeout(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	dispatcher := &fakeAgentDispatcher{result: &agentpb.TaskResult{
		State: agentpb.TaskState_TASK_STATE_SUCCEEDED, Payload: []byte("not-json"),
	}}
	f.handler = New(Deps{
		Clusters: f.manager, Store: f.store, Agent: dispatcher, Auth: f.auth, Version: "test",
	}).Handler()
	cookie := f.login(t)

	bad := f.request(t, http.MethodGet, "/api/v1/agents/agent-1/snapshot", nil, cookie, "")
	if bad.Code != http.StatusBadGateway {
		t.Fatalf("invalid payload status = %d body=%s", bad.Code, bad.Body.String())
	}

	dispatcher.err = context.DeadlineExceeded
	timeout := f.request(t, http.MethodGet, "/api/v1/agents/agent-1/snapshot", nil, cookie, "")
	if timeout.Code != http.StatusGatewayTimeout {
		t.Fatalf("timeout status = %d body=%s", timeout.Code, timeout.Body.String())
	}
}

func TestAgentEndpointsStoreErrors(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	cookie := f.login(t)
	if err := f.store.Close(); err != nil {
		t.Fatal(err)
	}

	token := f.request(t, http.MethodPost, "/api/v1/agents/enrollment-tokens", map[string]string{"cluster": "c1", "member": "node-a"}, cookie, "")
	if token.Code != http.StatusInternalServerError {
		t.Fatalf("token store error status = %d body=%s", token.Code, token.Body.String())
	}
	list := f.request(t, http.MethodGet, "/api/v1/agents", nil, cookie, "")
	if list.Code != http.StatusInternalServerError {
		t.Fatalf("list store error status = %d body=%s", list.Code, list.Body.String())
	}
}
