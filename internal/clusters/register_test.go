package clusters

import (
	"context"
	"errors"
	"testing"

	"github.com/mickeyzzc/incus-fleet/internal/config"
	"github.com/mickeyzzc/incus-fleet/internal/testutil/incusfake"
)

// newRegisterTestManager wires a manager with no initial clusters and a dialer
// serving the given fake servers by name.
func newRegisterTestManager(t *testing.T, servers map[string]InstanceServer) *Manager {
	t.Helper()

	manager, err := NewWithDialer(context.Background(), nil, fakeDialer(servers, nil), testLogger())
	if err != nil {
		t.Fatalf("NewWithDialer() error = %v", err)
	}
	t.Cleanup(manager.Close)

	return manager
}

func TestRegisterAndUnregister(t *testing.T) {
	manager := newRegisterTestManager(t, map[string]InstanceServer{"c64": &incusfake.Server{}})

	summary, err := manager.Register(context.Background(), config.Cluster{
		Name:        "c64",
		DisplayName: "Cluster 64",
		Endpoint:    "https://192.0.2.10:8443",
		Project:     "default",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if summary.Name != "c64" || !summary.Online {
		t.Fatalf("Register() summary = %#v", summary)
	}

	if _, err := manager.Get("c64"); err != nil {
		t.Fatalf("Get() after register error = %v", err)
	}

	if _, err := manager.Register(context.Background(), config.Cluster{Name: "c64"}); err == nil {
		t.Fatalf("Register() duplicate error = nil")
	}

	if err := manager.Unregister("c64"); err != nil {
		t.Fatalf("Unregister() error = %v", err)
	}
	if _, err := manager.Get("c64"); !errors.Is(err, ErrUnknownCluster) {
		t.Fatalf("Get() after unregister error = %v, want ErrUnknownCluster", err)
	}
	if err := manager.Unregister("c64"); !errors.Is(err, ErrUnknownCluster) {
		t.Fatalf("Unregister() unknown error = %v, want ErrUnknownCluster", err)
	}
}

func TestRegisterValidationAndDialFailure(t *testing.T) {
	manager := newRegisterTestManager(t, map[string]InstanceServer{})

	if _, err := manager.Register(context.Background(), config.Cluster{}); err == nil {
		t.Fatalf("Register() with empty name error = nil")
	}

	summary, err := manager.Register(context.Background(), config.Cluster{
		Name:     "down",
		Endpoint: "https://198.51.100.99:8443",
	})
	if err == nil {
		t.Fatalf("Register() dial failure error = nil")
	}
	if summary.Online {
		t.Fatalf("Register() dial failure summary marked online")
	}

	// The failed cluster stays in the manager so the UI can retry it.
	if _, err := manager.Get("down"); err != nil {
		t.Fatalf("Get() for failed cluster error = %v", err)
	}
}
