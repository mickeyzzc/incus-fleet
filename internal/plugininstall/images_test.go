package plugininstall

import (
	"context"
	"testing"

	"github.com/lxc/incus/v7/shared/api"

	"github.com/mickeyzzc/incus-fleet/internal/clusters"
	"github.com/mickeyzzc/incus-fleet/internal/config"
	"github.com/mickeyzzc/incus-fleet/internal/testutil/incusfake"
)

func TestClusterImageResolver(t *testing.T) {
	cfg := []config.Cluster{{Name: "c1", Endpoint: "https://192.0.2.10:8443"}}
	srv := &incusfake.Server{Images: []api.Image{{
		Fingerprint: "fp-1", Aliases: []api.ImageAlias{{Name: "example-image"}},
	}}}
	dial := func(_ context.Context, _ config.Cluster) (clusters.InstanceServer, error) { return srv, nil }
	manager, err := clusters.NewWithDialer(context.Background(), cfg, dial, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	resolver := ClusterImageResolver{Manager: manager}
	if got, err := resolver.Resolve(context.Background(), "c1", "example-image"); err != nil || got != "fp-1" {
		t.Fatalf("alias resolve = %q err=%v", got, err)
	}
	if got, err := resolver.Resolve(context.Background(), "c1", "fp-1"); err != nil || got != "fp-1" {
		t.Fatalf("fingerprint resolve = %q err=%v", got, err)
	}
	if _, err := resolver.Resolve(context.Background(), "c1", "missing"); err == nil {
		t.Fatal("expected missing image error")
	}
	if _, err := (ClusterImageResolver{}).Resolve(context.Background(), "c1", "x"); err == nil {
		t.Fatal("expected missing manager error")
	}
}
