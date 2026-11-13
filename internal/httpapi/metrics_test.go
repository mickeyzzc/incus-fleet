package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/lxc/incus/v7/shared/api"

	"github.com/mickeyzzc/incus-fleet/internal/testutil/incusfake"
)

func TestInstanceMetricsEndpoint(t *testing.T) {
	state := &api.InstanceState{
		Status:     "Running",
		StatusCode: api.Running,
		Pid:        99,
		Processes:  42,
		CPU:        api.InstanceStateCPU{Usage: 1_500_000_000},
		Memory: api.InstanceStateMemory{
			Usage: 1024, UsagePeak: 2048, Total: 8192,
		},
		Disk: map[string]api.InstanceStateDisk{
			"/": {Usage: 100, Total: 200},
		},
		Network: map[string]api.InstanceStateNetwork{
			"eth0": {
				State: "up",
				Counters: api.InstanceStateNetworkCounters{
					BytesReceived: 10, BytesSent: 20,
				},
			},
		},
	}
	source := &incusfake.Server{
		StateFunc: func(name string) (*api.InstanceState, error) {
			if name == "gone" {
				return nil, api.StatusErrorf(http.StatusNotFound, "Instance %q not found", name)
			}

			return state, nil
		},
	}
	f := newAPIFixture(t, source, &incusfake.Server{})
	cookie := f.login(t)

	rec := f.request(t, http.MethodGet, "/api/v1/clusters/c1/instances/web/metrics", nil, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("metrics status = %d body = %s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Metrics map[string]any `json:"metrics"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode metrics: %v", err)
	}
	if payload.Metrics["status"] != "Running" || payload.Metrics["pid"] != float64(99) {
		t.Fatalf("metrics payload = %#v", payload.Metrics)
	}
	if payload.Metrics["cpu"] == nil || payload.Metrics["memory"] == nil {
		t.Fatalf("metrics cpu/memory missing: %#v", payload.Metrics)
	}

	// Unknown instances surface as 404.
	rec = f.request(t, http.MethodGet, "/api/v1/clusters/c1/instances/gone/metrics", nil, cookie, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing instance status = %d body = %s", rec.Code, rec.Body.String())
	}

	// Unauthenticated requests are rejected.
	rec = f.request(t, http.MethodGet, "/api/v1/clusters/c1/instances/web/metrics", nil, nil, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d", rec.Code)
	}
}
