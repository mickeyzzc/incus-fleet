package store

import (
	"context"
	"testing"
	"time"
)

func TestPluginDetailTables(t *testing.T) {
	ctx := context.Background()
	s := openSQLite(t)

	version, err := s.SchemaVersion(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if version < 16 {
		t.Fatalf("schema version = %d, want >= 16", version)
	}

	if err := s.UpsertPluginConfig(ctx, "vm", "listen_port", "8428"); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertPluginConfig(ctx, "vm", "listen_port", "8429"); err != nil {
		t.Fatal(err)
	}
	config, err := s.ListPluginConfig(ctx, "vm")
	if err != nil || config["listen_port"] != "8429" {
		t.Fatalf("config = %#v err=%v", config, err)
	}
	if err := s.DeletePluginConfig(ctx, "vm"); err != nil {
		t.Fatal(err)
	}
	config, err = s.ListPluginConfig(ctx, "vm")
	if err != nil || len(config) != 0 {
		t.Fatalf("deleted config = %#v err=%v", config, err)
	}

	sample := &PluginHealthSample{
		PluginID: "vm", At: time.Now().UTC(), Status: "healthy", Detail: "probe ok",
	}
	if err := s.RecordPluginHealth(ctx, sample); err != nil {
		t.Fatal(err)
	}
	health, err := s.ListPluginHealth(ctx, "vm", 10)
	if err != nil || len(health) != 1 || health[0].Status != "healthy" {
		t.Fatalf("health = %#v err=%v", health, err)
	}

	event := &PluginEvent{
		PluginID: "vm", At: time.Now().UTC(), Kind: "delivery", Message: "agent.connected delivered",
	}
	if err := s.RecordPluginEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	event.ID = ""
	event.At = time.Time{}
	event.Kind = "error"
	event.Message = "webhook timeout"
	if err := s.RecordPluginEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	events, err := s.ListPluginEvents(ctx, "vm", 1)
	if err != nil || len(events) != 1 || events[0].Kind != "error" {
		t.Fatalf("events = %#v err=%v", events, err)
	}
}

func TestPluginDetailValidationAndStoreErrors(t *testing.T) {
	ctx := context.Background()
	s := openSQLite(t)
	if err := s.UpsertPluginConfig(ctx, "", "k", "v"); err == nil {
		t.Fatal("expected missing plugin id error")
	}
	if err := s.RecordPluginHealth(ctx, nil); err == nil {
		t.Fatal("expected nil health sample error")
	}
	if err := s.RecordPluginEvent(ctx, nil); err == nil {
		t.Fatal("expected nil event error")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertPluginConfig(ctx, "vm", "k", "v"); err == nil {
		t.Fatal("expected closed store error")
	}
	if _, err := s.ListPluginConfig(ctx, "vm"); err == nil {
		t.Fatal("expected closed store error")
	}
	if err := s.DeletePluginConfig(ctx, "vm"); err == nil {
		t.Fatal("expected closed store error")
	}
	if err := s.RecordPluginHealth(ctx, &PluginHealthSample{PluginID: "vm"}); err == nil {
		t.Fatal("expected closed store error")
	}
	if _, err := s.ListPluginHealth(ctx, "vm", 0); err == nil {
		t.Fatal("expected closed store error")
	}
	if err := s.RecordPluginEvent(ctx, &PluginEvent{PluginID: "vm"}); err == nil {
		t.Fatal("expected closed store error")
	}
	if _, err := s.ListPluginEvents(ctx, "vm", 0); err == nil {
		t.Fatal("expected closed store error")
	}
}
