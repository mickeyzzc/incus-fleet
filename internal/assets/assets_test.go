package assets

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/lxc/incus/v7/shared/api"

	"github.com/mickeyzzc/incus-fleet/internal/clusters"
	"github.com/mickeyzzc/incus-fleet/internal/config"
	"github.com/mickeyzzc/incus-fleet/internal/instances"
	"github.com/mickeyzzc/incus-fleet/internal/store"
	"github.com/mickeyzzc/incus-fleet/internal/testutil/incusfake"
)

func newFixture(t *testing.T, servers map[string]*incusfake.Server) (*Service, *store.SQLStore) {
	t.Helper()

	cfgs := []config.Cluster{
		{Name: "c1", DisplayName: "c1", Endpoint: "https://192.0.2.10:8443"},
		{Name: "c2", DisplayName: "c2", Endpoint: "https://198.51.100.11:8443"},
	}
	dial := func(_ context.Context, cfg config.Cluster) (clusters.InstanceServer, error) {
		srv, ok := servers[cfg.Name]
		if !ok {
			return nil, clusters.ErrUnavailable
		}

		return srv, nil
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	manager, err := clusters.NewWithDialer(context.Background(), cfgs, dial, logger)
	if err != nil {
		t.Fatalf("clusters.NewWithDialer() error = %v", err)
	}

	st, err := store.Open(context.Background(), "sqlite", ":memory:")
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	instanceService := instances.New(manager)

	return New(st, instanceService), st
}

func TestKeyValidationAndCRUD(t *testing.T) {
	svc, _ := newFixture(t, map[string]*incusfake.Server{"c1": {}})
	ctx := context.Background()

	if err := svc.CreateKey(ctx, &Keystone{Name: "", PublicKey: "ssh-ed25519 AAA"}); err == nil {
		t.Fatal("CreateKey() missing name accepted")
	}
	if err := svc.CreateKey(ctx, &Keystone{Name: "k", PublicKey: "garbage-no-space"}); err == nil {
		t.Fatal("CreateKey() malformed key accepted")
	}
	if err := svc.CreateKey(ctx, &Keystone{Name: "k", PublicKey: "bad\rreturn"}); err == nil {
		t.Fatal("CreateKey() carriage return accepted")
	}

	key := &Keystone{Name: "desk", PublicKey: "  ssh-ed25519 AAA desk  \nssh-ed25519 BBB lab  "}
	if err := svc.CreateKey(ctx, key); err != nil {
		t.Fatalf("CreateKey() error = %v", err)
	}
	if key.PublicKey != "ssh-ed25519 AAA desk\nssh-ed25519 BBB lab" {
		t.Fatalf("public key trimmed = %q", key.PublicKey)
	}
	if key.ID == "" {
		t.Fatal("id not derived")
	}

	list, err := svc.Keys(ctx)
	if err != nil {
		t.Fatalf("Keys() error = %v", err)
	}
	if len(list) != 1 || list[0].Name != "desk" {
		t.Fatalf("list = %#v", list)
	}

	// Identical material maps to the same id and replaces the entry.
	dup := &Keystone{Name: "again", PublicKey: "ssh-ed25519 AAA desk\nssh-ed25519 BBB lab"}
	if err := svc.CreateKey(ctx, dup); err != nil {
		t.Fatalf("CreateKey() duplicate error = %v", err)
	}
	if dup.ID != key.ID {
		t.Fatalf("duplicate id = %q want %q", dup.ID, key.ID)
	}
	list, err = svc.Keys(ctx)
	if err != nil {
		t.Fatalf("Keys() after upsert error = %v", err)
	}
	if len(list) != 1 || list[0].Name != "again" {
		t.Fatalf("list after upsert = %#v", list)
	}

	if err := svc.UpdateKey(ctx, &Keystone{ID: key.ID, PublicKey: "ssh-ed25519 CCC updated"}); err != nil {
		t.Fatalf("UpdateKey() error = %v", err)
	}
	got, err := svc.Key(ctx, key.ID)
	if err != nil {
		t.Fatalf("Key() error = %v", err)
	}
	if !strings.Contains(got.PublicKey, "CCC") {
		t.Fatalf("updated key = %q", got.PublicKey)
	}

	if err := svc.DeleteKey(ctx, key.ID); err != nil {
		t.Fatalf("DeleteKey() error = %v", err)
	}
}

func TestScriptValidationAndCRUD(t *testing.T) {
	svc, _ := newFixture(t, map[string]*incusfake.Server{"c1": {}})
	ctx := context.Background()

	if err := svc.CreateScript(ctx, &Script{Name: "", Body: "x"}); err == nil {
		t.Fatal("CreateScript() missing name accepted")
	}
	if err := svc.CreateScript(ctx, &Script{Name: "s", Body: "   "}); err == nil {
		t.Fatal("CreateScript() empty body accepted")
	}

	script := &Script{Name: "init", Description: "bootstrap", Body: "#!/bin/sh\necho ok\n"}
	if err := svc.CreateScript(ctx, script); err != nil {
		t.Fatalf("CreateScript() error = %v", err)
	}
	if script.ID == "" {
		t.Fatal("id not derived")
	}

	if err := svc.UpdateScript(ctx, &Script{ID: script.ID, Name: "init", Body: "#!/bin/sh\necho v2\n"}); err != nil {
		t.Fatalf("UpdateScript() error = %v", err)
	}
	got, err := svc.Script(ctx, script.ID)
	if err != nil {
		t.Fatalf("Script() error = %v", err)
	}
	if !strings.Contains(got.Body, "v2") {
		t.Fatalf("body = %q", got.Body)
	}

	if err := svc.DeleteScript(ctx, script.ID); err != nil {
		t.Fatalf("DeleteScript() error = %v", err)
	}
}

func TestBatchInjectKeys(t *testing.T) {
	web := &incusfake.Server{
		Instance:  &api.Instance{Name: "web", Type: "container", Status: "Running", StatusCode: api.Running},
		Operation: &incusfake.Operation{Metadata: map[string]any{"return": float64(0)}},
	}
	db := &incusfake.Server{
		Instance: &api.Instance{Name: "db", Type: "container", Status: "Running", StatusCode: api.Running},
	}
	svc, _ := newFixture(t, map[string]*incusfake.Server{"c1": web, "c2": db})
	ctx := context.Background()

	keyA := &Keystone{Name: "desk", PublicKey: "ssh-ed25519 AAA desk"}
	keyB := &Keystone{Name: "lab", PublicKey: "ssh-ed25519 BBB lab"}
	if err := svc.CreateKey(ctx, keyA); err != nil {
		t.Fatalf("CreateKey() error = %v", err)
	}
	if err := svc.CreateKey(ctx, keyB); err != nil {
		t.Fatalf("CreateKey() error = %v", err)
	}

	items := svc.BatchInjectKeys(ctx, []Target{
		{Cluster: "c1", Instance: "web"},
		{Cluster: "c2", Instance: "db", User: "deploy"},
	}, []string{keyA.ID, keyB.ID})

	if len(items) != 2 || !items[0].OK || !items[1].OK {
		t.Fatalf("items = %#v", items)
	}

	// Both instances received both keys on the right account.
	webExec := web.ExecArgs()
	if len(webExec) != 1 || webExec[0].Command[0] != "sh" || webExec[0].Command[4] != "root" {
		t.Fatalf("web exec = %#v", webExec)
	}
	dbExec := db.ExecArgs()
	if len(dbExec) != 1 || dbExec[0].Command[4] != "deploy" {
		t.Fatalf("db exec = %#v", dbExec)
	}

	// Failures are reported per instance without aborting the rest.
	items = svc.BatchInjectKeys(ctx, []Target{
		{Cluster: "c1", Instance: "web"},
		{Cluster: "c2", Instance: "db"},
	}, []string{"key-missing"})
	if len(items) != 1 || items[0].Error == "" {
		t.Fatalf("missing key items = %#v", items)
	}

	items = svc.BatchInjectKeys(ctx, []Target{}, []string{keyA.ID})
	if len(items) != 1 || items[0].Error == "" {
		t.Fatalf("empty targets items = %#v", items)
	}
	items = svc.BatchInjectKeys(ctx, []Target{{Cluster: "c1", Instance: "web"}}, nil)
	if len(items) != 1 || items[0].Error == "" {
		t.Fatalf("no keys items = %#v", items)
	}
}

func TestBatchRunScript(t *testing.T) {
	web := &incusfake.Server{
		Instance:   &api.Instance{Name: "web", Type: "container", Status: "Running", StatusCode: api.Running},
		ExecOutput: "ok",
		Operation:  &incusfake.Operation{Metadata: map[string]any{"return": float64(0)}},
	}
	svc, _ := newFixture(t, map[string]*incusfake.Server{"c1": web})
	ctx := context.Background()

	script := &Script{Name: "init", Body: "#!/bin/sh\necho ok\n"}
	if err := svc.CreateScript(ctx, script); err != nil {
		t.Fatalf("CreateScript() error = %v", err)
	}

	items := svc.BatchRunScript(ctx, []Target{{Cluster: "c1", Instance: "web"}}, script.ID)
	if len(items) != 1 || !items[0].OK || items[0].Stdout != "ok" {
		t.Fatalf("items = %#v", items)
	}
	webExec := web.ExecArgs()
	if len(webExec) != 1 || len(webExec[0].Command) < 2 || webExec[0].Command[0] != "sh" {
		t.Fatalf("exec = %#v", webExec)
	}

	items = svc.BatchRunScript(ctx, []Target{{Cluster: "c1", Instance: "web"}}, "script-none")
	if len(items) != 1 || items[0].Error == "" {
		t.Fatalf("missing script items = %#v", items)
	}
}

func TestBatchPower(t *testing.T) {
	web := &incusfake.Server{
		Instance: &api.Instance{Name: "web", Type: "container", Status: "Running", StatusCode: api.Running},
	}
	svc, _ := newFixture(t, map[string]*incusfake.Server{"c1": web})
	ctx := context.Background()

	items := svc.BatchPower(ctx, []Target{{Cluster: "c1", Instance: "web"}}, "stop", true)
	if len(items) != 1 || !items[0].OK {
		t.Fatalf("items = %#v", items)
	}
	// An unknown action is reported per request, not as a panic.
	bad := svc.BatchPower(ctx, []Target{{Cluster: "c1", Instance: "web"}}, "pause", false)
	if len(bad) != 1 || bad[0].Error == "" {
		t.Fatalf("bad action items = %#v", bad)
	}
	empty := svc.BatchPower(ctx, nil, "start", false)
	if len(empty) != 1 || empty[0].Error == "" {
		t.Fatalf("empty targets items = %#v", empty)
	}
}

func TestServiceLoadErrorsSurface(t *testing.T) {
	svc, st := newFixture(t, map[string]*incusfake.Server{"c1": {}})
	ctx := context.Background()

	if err := st.CreateScript(ctx, &Script{ID: "s1", Name: "s", Body: "b"}); err != nil {
		t.Fatalf("seed script: %v", err)
	}
	got, err := svc.Script(ctx, "s1")
	if err != nil {
		t.Fatalf("Script() error = %v", err)
	}
	if got.Body != "b" {
		t.Fatalf("script = %#v", got)
	}
	if _, err := svc.Script(ctx, "missing"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing script error = %v", err)
	}
}

func TestUpdateScriptRequiresBody(t *testing.T) {
	ctx := context.Background()
	svc, st := newFixture(t, map[string]*incusfake.Server{"c1": {}})

	script := &Script{Name: "s", Body: "echo hi"}
	if err := svc.CreateScript(ctx, script); err != nil {
		t.Fatalf("CreateScript() = %v", err)
	}

	script.Body = "   "
	if err := svc.UpdateScript(ctx, script); !errors.Is(err, instances.ErrInvalidRequest) {
		t.Fatalf("UpdateScript(empty body) = %v, want ErrInvalidRequest", err)
	}

	script.Body = "echo updated"
	if err := svc.UpdateScript(ctx, script); err != nil {
		t.Fatalf("UpdateScript() = %v", err)
	}

	got, err := st.GetScript(ctx, script.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Body != "echo updated" {
		t.Fatalf("body = %q", got.Body)
	}
}
