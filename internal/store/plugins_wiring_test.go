package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPluginWiringPersistence(t *testing.T) {
	ctx := context.Background()
	s := openSQLite(t)

	version, err := s.SchemaVersion(ctx)
	if err != nil {
		t.Fatalf("SchemaVersion() error = %v", err)
	}
	if version < 15 {
		t.Fatalf("schema version = %d, want >= 15", version)
	}

	w := &PluginWiring{
		PluginID:     "vm",
		Key:          "metrics.query_url",
		Value:        "http://192.0.2.10:8428",
		DefaultValue: "http://192.0.2.1:8428",
		Enabled:      true,
		Approved:     true,
	}
	if err := s.UpsertPluginWiring(ctx, w); err != nil {
		t.Fatalf("UpsertPluginWiring() error = %v", err)
	}

	got, err := s.GetPluginWiring(ctx, "vm", "metrics.query_url")
	if err != nil {
		t.Fatalf("GetPluginWiring() error = %v", err)
	}
	if got.Value != w.Value || got.DefaultValue != w.DefaultValue || !got.Enabled || !got.Approved {
		t.Fatalf("wiring = %#v", got)
	}
	if got.UpdatedAt.IsZero() {
		t.Fatal("UpdatedAt was not populated")
	}

	// Re-inserting the same key updates the row rather than duplicating it.
	w.Value = "http://192.0.2.11:8428"
	if err := s.UpsertPluginWiring(ctx, w); err != nil {
		t.Fatalf("UpsertPluginWiring() update error = %v", err)
	}
	all, err := s.ListPluginWiring(ctx, "vm")
	if err != nil {
		t.Fatalf("ListPluginWiring() error = %v", err)
	}
	if len(all) != 1 || all[0].Value != w.Value {
		t.Fatalf("wiring rows = %#v", all)
	}

	if err := s.SetPluginWiringEnabled(ctx, "vm", "metrics.query_url", false); err != nil {
		t.Fatalf("SetPluginWiringEnabled() error = %v", err)
	}
	got, err = s.GetPluginWiring(ctx, "vm", "metrics.query_url")
	if err != nil || got.Enabled {
		t.Fatalf("disabled wiring = %#v err=%v", got, err)
	}

	if err := s.RestorePluginWiringDefaults(ctx, "vm"); err != nil {
		t.Fatalf("RestorePluginWiringDefaults() error = %v", err)
	}
	got, err = s.GetPluginWiring(ctx, "vm", "metrics.query_url")
	if err != nil {
		t.Fatalf("GetPluginWiring() error = %v", err)
	}
	if got.Value != w.DefaultValue || got.Enabled {
		t.Fatalf("restored wiring = %#v", got)
	}

	if err := s.DeletePluginWiring(ctx, "vm", "metrics.query_url"); err != nil {
		t.Fatalf("DeletePluginWiring() error = %v", err)
	}
	if _, err := s.GetPluginWiring(ctx, "vm", "metrics.query_url"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted wiring error = %v, want ErrNotFound", err)
	}
	if err := s.SetPluginWiringEnabled(ctx, "vm", "missing", true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("enable missing error = %v, want ErrNotFound", err)
	}
	if err := s.DeletePluginWiring(ctx, "vm", "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete missing error = %v, want ErrNotFound", err)
	}

	if err := s.UpsertPluginWiring(ctx, &PluginWiring{Key: "k", Value: "v"}); err == nil {
		t.Fatal("expected missing plugin id error")
	}

	// Timestamps persist in UTC even when the caller supplies a local time.
	local := time.Now().In(time.FixedZone("fixture", 3600))
	w2 := &PluginWiring{PluginID: "bell", Key: "notifications.sink_url", Value: "http://192.0.2.12", UpdatedAt: local}
	if err := s.UpsertPluginWiring(ctx, w2); err != nil {
		t.Fatalf("UpsertPluginWiring() error = %v", err)
	}
	got, err = s.GetPluginWiring(ctx, "bell", "notifications.sink_url")
	if err != nil {
		t.Fatalf("GetPluginWiring() error = %v", err)
	}
	if got.UpdatedAt.Location() != time.UTC {
		t.Fatalf("timestamp location = %v, want UTC", got.UpdatedAt.Location())
	}
}
