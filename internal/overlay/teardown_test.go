package overlay

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/lxc/incus/v7/shared/api"

	"github.com/mickeyzzc/incus-fleet/internal/clusters"
	"github.com/mickeyzzc/incus-fleet/internal/config"
	"github.com/mickeyzzc/incus-fleet/internal/testutil/incusfake"

	"github.com/mickeyzzc/incus-fleet/internal/store"
)

// TestTeardownSurfacesFailures covers the cleanup error branches so a partial
// teardown cannot be mistaken for success.
func TestTeardownSurfacesFailures(t *testing.T) {
	base := func() (*Service, *fakeNodes) {
		nodes := &fakeNodes{operators: map[string]*fakeNode{}, missing: map[string]bool{}}
		return New(&fakeLookup{members: twoMembers()}, nodes, newFakeStore()), nodes
	}

	stored := &store.OverlayNetwork{
		Cluster: "c62", Name: "c62", Bridge: "fleetbr0", VNI: 6200,
		Subnet: "198.51.100.0/24", GatewayMember: "node-a", MTU: 1450,
		Members: []store.OverlayMember{
			{Member: "node-a", UnderlayIP: "192.0.2.11", BridgeIP: "198.51.100.1/24"},
			{Member: "node-b", UnderlayIP: "192.0.2.12", BridgeIP: "198.51.100.2/24"},
		},
	}

	t.Run("vxlan delete fails", func(t *testing.T) {
		service, nodes := base()
		nodes.operators["node-a"] = &fakeNode{failOn: "vxlan-del"}
		if err := service.teardown(context.Background(), "c62", stored, []string{"node-a", "node-b"}); err == nil {
			t.Fatal("expected teardown error")
		}
	})

	t.Run("dhcp stop fails", func(t *testing.T) {
		service, nodes := base()
		nodes.operators["node-a"] = &fakeNode{failOn: "dhcp-stop"}
		if err := service.teardown(context.Background(), "c62", stored, []string{"node-a", "node-b"}); err == nil {
			t.Fatal("expected teardown error")
		}
	})

	t.Run("nat delete fails", func(t *testing.T) {
		service, nodes := base()
		nodes.operators["node-a"] = &fakeNode{failOn: "nat-del"}
		if err := service.teardown(context.Background(), "c62", stored, []string{"node-a", "node-b"}); err == nil {
			t.Fatal("expected teardown error")
		}
	})

	t.Run("bridge delete fails", func(t *testing.T) {
		service, nodes := base()
		nodes.operators["node-b"] = &fakeNode{failOn: "bridge-del"}
		if err := service.teardown(context.Background(), "c62", stored, []string{"node-a", "node-b"}); err == nil {
			t.Fatal("expected teardown error")
		}
	})

	t.Run("unavailable members are skipped", func(t *testing.T) {
		service, nodes := base()
		nodes.missing["node-a"] = true
		nodes.missing["node-b"] = true
		if err := service.teardown(context.Background(), "c62", stored, []string{"node-a", "node-b"}); err != nil {
			t.Fatalf("teardown() should skip missing agents: %v", err)
		}
	})

	t.Run("resolve error surfaces", func(t *testing.T) {
		service, nodes := base()
		nodes.err = errors.New("resolver down")
		if err := service.teardown(context.Background(), "c62", stored, []string{"node-a"}); err == nil {
			t.Fatal("expected resolver error")
		}
	})
}

// TestApplyFailureBranches covers applyMember and applyGateway failures.
func TestApplyFailureBranches(t *testing.T) {
	stored := &store.OverlayNetwork{
		Cluster: "c62", Name: "c62", Bridge: "fleetbr0", VNI: 6200,
		Subnet: "198.51.100.0/24", GatewayMember: "node-a", MTU: 1450,
		DHCPStart: "198.51.100.10", DHCPEnd: "198.51.100.200",
		Members: []store.OverlayMember{
			{Member: "node-a", UnderlayIP: "192.0.2.11", BridgeIP: "198.51.100.1/24"},
			{Member: "node-b", UnderlayIP: "192.0.2.12", BridgeIP: "198.51.100.2/24"},
		},
	}

	cases := []struct {
		name   string
		member string
		failOn string
	}{
		{"bridge", "node-a", "bridge"},
		{"vxlan", "node-b", "vxlan"},
		{"dhcp write", "node-a", "dhcp-write"},
		{"dhcp start", "node-a", "dhcp-start"},
		{"nat", "node-a", "nat"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			nodes := &fakeNodes{
				operators: map[string]*fakeNode{tc.member: {failOn: tc.failOn}},
				missing:   map[string]bool{},
			}
			service := New(&fakeLookup{members: twoMembers()}, nodes, newFakeStore())
			members := stored.Members
			if _, err := service.apply(context.Background(), "c62", stored, members, ""); err == nil {
				t.Fatalf("expected apply error for %s", tc.name)
			}
		})
	}
}

// TestReconcileWithoutStoreAndResolverErrors covers defensive branches.
func TestReconcileWithoutStoreAndResolverErrors(t *testing.T) {
	ctx := context.Background()

	nodes := &fakeNodes{operators: map[string]*fakeNode{}, missing: map[string]bool{}}
	service := New(&fakeLookup{members: twoMembers()}, nodes, nil)
	if _, err := service.Reconcile(ctx, "c62", "c62"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("Reconcile(no store) = %v", err)
	}
	if err := service.Disable(ctx, "c62", "c62"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("Disable(no store) = %v", err)
	}

	// A resolver failure inside memberAddresses surfaces.
	failingLookup := &fakeLookup{err: errors.New("lookup down")}
	withStore := New(failingLookup, nodes, newFakeStore())
	if _, err := withStore.Enable(ctx, "c62", Request{Name: "c62", VNI: 1, Subnet: "198.51.100.0/24", GatewayMember: "node-a"}); err == nil {
		t.Fatal("expected lookup error")
	}
}

// TestMapStoreError covers the sentinel translation.
func TestMapStoreError(t *testing.T) {
	if got := mapStoreError(nil); got != nil {
		t.Fatalf("nil = %v", got)
	}
	if got := mapStoreError(store.ErrNotFound); !errors.Is(got, ErrNotFound) {
		t.Fatalf("ErrNotFound = %v", got)
	}
	plain := errors.New("boom")
	if got := mapStoreError(plain); !errors.Is(got, plain) {
		t.Fatalf("plain = %v", got)
	}
}

// TestApplyToleratesGatewayResolveFailureForTeardown documents that teardown
// only requires the gateway cleanup when it can be resolved.
func TestApplyToleratesGatewayResolveFailureForTeardown(t *testing.T) {
	nodes := &fakeNodes{operators: map[string]*fakeNode{}, missing: map[string]bool{"node-a": true}}
	service := New(&fakeLookup{members: twoMembers()}, nodes, newFakeStore())

	stored := &store.OverlayNetwork{
		Cluster: "c62", Name: "c62", Bridge: "fleetbr0", VNI: 6200,
		Subnet: "198.51.100.0/24", GatewayMember: "node-a", MTU: 1450,
		Members: []store.OverlayMember{
			{Member: "node-a", UnderlayIP: "192.0.2.11", BridgeIP: "198.51.100.1/24"},
			{Member: "node-b", UnderlayIP: "192.0.2.12", BridgeIP: "198.51.100.2/24"},
		},
	}
	if err := service.teardown(context.Background(), "c62", stored, []string{"node-a", "node-b"}); err != nil {
		t.Fatalf("teardown() error = %v", err)
	}
}

// TestClusterResolverSuccess covers the adapter's happy path with a real
// cluster manager backed by a fake Incus server.
func TestClusterResolverSuccess(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := &incusfake.Server{
		Clustered: true,
		Server: &api.Server{Environment: api.ServerEnvironment{
			ServerName: "node-a", ServerVersion: "7.0.2", ServerClustered: true,
		}},
		Members: []api.ClusterMember{
			{ServerName: "node-a", Status: "Online", URL: "https://192.0.2.11:8443"},
			{ServerName: "node-b", Status: "Online", URL: "https://192.0.2.12:8443"},
		},
	}
	manager, err := clusters.NewWithDialer(ctx,
		[]config.Cluster{{Name: "c62", Endpoint: "https://192.0.2.11:8443"}},
		func(context.Context, config.Cluster) (clusters.InstanceServer, error) { return server, nil },
		logger)
	if err != nil {
		t.Fatalf("NewWithDialer() error = %v", err)
	}
	t.Cleanup(manager.Close)

	resolver := &ClusterResolver{Manager: manager}
	members, err := resolver.Members(ctx, "c62")
	if err != nil {
		t.Fatalf("Members() error = %v", err)
	}
	if len(members) != 2 || members[0].Name != "node-a" {
		t.Fatalf("members = %#v", members)
	}

	// An unknown cluster surfaces the manager's error.
	if _, err := resolver.Members(ctx, "missing"); err == nil {
		t.Fatal("Members(unknown) should fail")
	}
}
