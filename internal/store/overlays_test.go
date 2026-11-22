package store

import (
	"context"
	"errors"
	"testing"
)

func TestOverlayLifecycle(t *testing.T) {
	ctx := context.Background()
	s := openSQLite(t)

	overlay := &OverlayNetwork{
		Cluster:       "c62",
		Name:          "c62",
		Bridge:        "fleetbr0",
		VNI:           6200,
		Subnet:        "198.51.100.0/24",
		GatewayMember: "node-a",
		MTU:           1450,
		DHCPStart:     "198.51.100.10",
		DHCPEnd:       "198.51.100.200",
		Enabled:       true,
		Members: []OverlayMember{
			{Member: "node-a", BridgeIP: "198.51.100.1/24", UnderlayIP: "192.0.2.11"},
			{Member: "node-b", BridgeIP: "198.51.100.2/24", UnderlayIP: "192.0.2.12"},
		},
	}
	if err := s.SaveOverlay(ctx, overlay); err != nil {
		t.Fatalf("SaveOverlay() error = %v", err)
	}
	if overlay.CreatedAt.IsZero() || overlay.UpdatedAt.IsZero() {
		t.Fatalf("SaveOverlay() did not stamp timestamps: %#v", overlay)
	}

	got, err := s.GetOverlay(ctx, "c62", "c62")
	if err != nil {
		t.Fatalf("GetOverlay() error = %v", err)
	}
	if got.Bridge != "fleetbr0" || got.VNI != 6200 || !got.Enabled || got.MTU != 1450 {
		t.Fatalf("overlay = %#v", got)
	}
	if len(got.Members) != 2 || got.Members[1].UnderlayIP != "192.0.2.12" {
		t.Fatalf("members = %#v", got.Members)
	}

	list, err := s.ListOverlays(ctx, "c62")
	if err != nil {
		t.Fatalf("ListOverlays() error = %v", err)
	}
	if len(list) != 1 || list[0].Name != "c62" {
		t.Fatalf("list = %#v", list)
	}

	// Upsert replaces the row and keeps the original CreatedAt.
	created := got.CreatedAt
	overlay.Enabled = false
	overlay.Members = []OverlayMember{{Member: "node-a", BridgeIP: "198.51.100.1/24", UnderlayIP: "192.0.2.11"}}
	if err := s.SaveOverlay(ctx, overlay); err != nil {
		t.Fatalf("SaveOverlay(update) error = %v", err)
	}
	updated, err := s.GetOverlay(ctx, "c62", "c62")
	if err != nil {
		t.Fatalf("GetOverlay(updated) error = %v", err)
	}
	if updated.Enabled || len(updated.Members) != 1 {
		t.Fatalf("updated = %#v", updated)
	}
	if !updated.CreatedAt.Equal(created) {
		t.Fatalf("CreatedAt changed: %v -> %v", created, updated.CreatedAt)
	}

	if err := s.DeleteOverlay(ctx, "c62", "c62"); err != nil {
		t.Fatalf("DeleteOverlay() error = %v", err)
	}
	if _, err := s.GetOverlay(ctx, "c62", "c62"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if err := s.DeleteOverlay(ctx, "c62", "c62"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound on second delete, got %v", err)
	}
}

func TestOverlayIsolatedPerCluster(t *testing.T) {
	ctx := context.Background()
	s := openSQLite(t)

	for _, cluster := range []string{"c62", "c63"} {
		if err := s.SaveOverlay(ctx, &OverlayNetwork{
			Cluster: cluster, Name: "overlay", Bridge: "fleetbr0", VNI: 100,
			Subnet: "10.99.0.0/24", GatewayMember: "node-a", MTU: 1450,
		}); err != nil {
			t.Fatalf("SaveOverlay(%s) error = %v", cluster, err)
		}
	}

	got, err := s.GetOverlay(ctx, "c63", "overlay")
	if err != nil {
		t.Fatalf("GetOverlay() error = %v", err)
	}
	if got.Cluster != "c63" {
		t.Fatalf("cluster = %q", got.Cluster)
	}

	list, err := s.ListOverlays(ctx, "c62")
	if err != nil {
		t.Fatalf("ListOverlays() error = %v", err)
	}
	if len(list) != 1 || list[0].Cluster != "c62" {
		t.Fatalf("list = %#v", list)
	}

	if _, err := s.GetOverlay(ctx, "c64", "overlay"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
