package answerprofiles

import (
	"context"
	"errors"
	"testing"

	"github.com/mickeyzzc/incus-fleet/internal/instances"
	"github.com/mickeyzzc/incus-fleet/internal/store"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	st, err := store.Open(context.Background(), "sqlite", ":memory:")
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	return New(st)
}

func TestServiceCRUD(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)

	p := &AnswerProfile{
		Name: " debian-lab ", Profile: "debian-preseed", Username: "ops",
		Hostname: "lab", Password: "pw", Timezone: "UTC", Disk: "/dev/vda",
	}
	if err := svc.Create(ctx, p); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if p.ID == "" || p.Name != "debian-lab" {
		t.Fatalf("Create did not normalize/assign id: %#v", p)
	}

	list, err := svc.List(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("List() = %#v, err = %v", list, err)
	}
	got, err := svc.Get(ctx, p.ID)
	if err != nil || got.Username != "ops" || got.Disk != "/dev/vda" {
		t.Fatalf("Get() = %#v, err = %v", got, err)
	}

	got.Timezone = "Asia/Shanghai"
	if err := svc.Update(ctx, got); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	got, err = svc.Get(ctx, p.ID)
	if err != nil || got.Timezone != "Asia/Shanghai" {
		t.Fatalf("after update = %#v, err = %v", got, err)
	}

	if err := svc.Delete(ctx, p.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := svc.Get(ctx, p.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing profile error = %v", err)
	}
}

func TestServiceValidation(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)

	cases := []struct {
		name string
		p    AnswerProfile
		ok   bool
	}{
		{"no name", AnswerProfile{Profile: "debian-preseed", Username: "ops"}, false},
		{"unknown profile", AnswerProfile{Name: "x", Profile: "bogus", Username: "ops"}, false},
		{"template no username", AnswerProfile{Name: "x", Profile: "debian-preseed"}, false},
		{"bad hostname", AnswerProfile{Name: "x", Profile: "debian-preseed", Username: "ops", Hostname: "bad host"}, false},
		{"valid template", AnswerProfile{Name: "x", Profile: "debian-preseed", Username: "ops"}, true},
		{"custom answer no username", AnswerProfile{Name: "y", Profile: "nocloud", AnswerFile: "#cloud-config\n"}, true},
		{"custom answer with NUL", AnswerProfile{Name: "z", Profile: "nocloud", AnswerFile: "a\x00b"}, false},
	}
	for _, tc := range cases {
		err := svc.Create(ctx, &tc.p)
		if (err == nil) != tc.ok {
			t.Errorf("%s: Create() error = %v, want ok=%v", tc.name, err, tc.ok)
		}
	}
}

func TestServiceResolveAndToSpec(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)

	def := &AnswerProfile{
		Name: "default", Profile: "debian-preseed", Username: "fleet",
		Hostname: "default-host", Timezone: "Europe/London", IsDefault: true,
	}
	if err := svc.Create(ctx, def); err != nil {
		t.Fatalf("Create(default) error = %v", err)
	}
	other := &AnswerProfile{Name: "other", Profile: "debian-preseed", Username: "ops"}
	if err := svc.Create(ctx, other); err != nil {
		t.Fatalf("Create(other) error = %v", err)
	}

	// No explicit id -> per-profile default.
	got, err := svc.Resolve(ctx, "", "debian-preseed")
	if err != nil || got.ID != def.ID {
		t.Fatalf("Resolve(default) = %#v, err = %v", got, err)
	}
	// Explicit id beats the default.
	got, err = svc.Resolve(ctx, other.ID, "debian-preseed")
	if err != nil || got.ID != other.ID {
		t.Fatalf("Resolve(explicit) = %#v, err = %v", got, err)
	}
	// Missing explicit id surfaces not-found (caller decides fallback).
	if _, err := svc.Resolve(ctx, "missing", "debian-preseed"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Resolve(missing) error = %v", err)
	}
	// Profile without a default surfaces not-found.
	if _, err := svc.Resolve(ctx, "", "kickstart"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Resolve(no default) error = %v", err)
	}

	spec := ToSpec(def)
	if spec.Username != "fleet" || spec.Hostname != "default-host" || spec.Timezone != "Europe/London" {
		t.Fatalf("ToSpec() = %#v", spec)
	}
}

func TestValidationUsesInvalidRequestSentinel(t *testing.T) {
	svc := newTestService(t)
	err := svc.Create(context.Background(), &AnswerProfile{Profile: "debian-preseed"})
	if !errors.Is(err, instances.ErrInvalidRequest) {
		t.Fatalf("validation error = %v, want ErrInvalidRequest", err)
	}
}
