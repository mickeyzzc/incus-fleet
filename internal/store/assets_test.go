package store

import (
	"context"
	"errors"
	"testing"
)

func TestKeystoneLifecycle(t *testing.T) {
	ctx := context.Background()
	s := openSQLite(t)

	key := &Keystone{ID: "k1", Name: "user@example", PublicKey: "ssh-ed25519 AAA user@example", Comment: "daily driver"}
	if err := s.CreateKeystone(ctx, key); err != nil {
		t.Fatalf("CreateKeystone() error = %v", err)
	}

	list, err := s.ListKeystones(ctx)
	if err != nil {
		t.Fatalf("ListKeystones() error = %v", err)
	}
	if len(list) != 1 || list[0].ID != "k1" || list[0].CreatedAt.IsZero() {
		t.Fatalf("list = %#v", list)
	}

	got, err := s.GetKeystone(ctx, "k1")
	if err != nil {
		t.Fatalf("GetKeystone() error = %v", err)
	}
	if got.PublicKey != key.PublicKey {
		t.Fatalf("got = %#v", got)
	}

	key.Name = "user@example-lab"
	key.PublicKey = "ssh-ed25519 BBB user@example-lab"
	if err := s.UpdateKeystone(ctx, key); err != nil {
		t.Fatalf("UpdateKeystone() error = %v", err)
	}
	got, err = s.GetKeystone(ctx, "k1")
	if err != nil {
		t.Fatalf("GetKeystone() after update error = %v", err)
	}
	if got.Name != "user@example-lab" || got.PublicKey != "ssh-ed25519 BBB user@example-lab" {
		t.Fatalf("after update = %#v", got)
	}

	if err := s.DeleteKeystone(ctx, "k1"); err != nil {
		t.Fatalf("DeleteKeystone() error = %v", err)
	}
	if _, err = s.GetKeystone(ctx, "k1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing key error = %v", err)
	}
	if err := s.DeleteKeystone(ctx, "k1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("double delete error = %v", err)
	}
}

func TestScriptLifecycle(t *testing.T) {
	ctx := context.Background()
	s := openSQLite(t)

	script := &Script{ID: "s1", Name: "init", Description: "bootstrap", Body: "#!/bin/sh\napt-get update\n"}
	if err := s.CreateScript(ctx, script); err != nil {
		t.Fatalf("CreateScript() error = %v", err)
	}

	list, err := s.ListScripts(ctx)
	if err != nil {
		t.Fatalf("ListScripts() error = %v", err)
	}
	if len(list) != 1 || list[0].ID != "s1" || list[0].UpdatedAt.IsZero() {
		t.Fatalf("list = %#v", list)
	}

	script.Body = "#!/bin/sh\napt-get update && apt-get upgrade -y\n"
	if err := s.UpdateScript(ctx, script); err != nil {
		t.Fatalf("UpdateScript() error = %v", err)
	}
	got, err := s.GetScript(ctx, "s1")
	if err != nil {
		t.Fatalf("GetScript() error = %v", err)
	}
	if !got.UpdatedAt.After(got.CreatedAt) {
		t.Fatalf("updated_at must move: %#v", got)
	}

	if err := s.DeleteScript(ctx, "s1"); err != nil {
		t.Fatalf("DeleteScript() error = %v", err)
	}
	if _, err = s.GetScript(ctx, "s1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing script error = %v", err)
	}
}
