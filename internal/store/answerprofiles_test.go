package store

import (
	"context"
	"errors"
	"testing"
)

func TestAnswerProfileLifecycle(t *testing.T) {
	ctx := context.Background()
	s := openSQLite(t)

	p := &AnswerProfile{
		ID: "ap1", Name: "debian-lab", Profile: "debian-preseed",
		Hostname: "lab-node", Username: "ops", Password: "secret",
		Timezone: "Asia/Shanghai", Disk: "/dev/vda",
		Locale: "zh_CN", Keyboard: "us",
	}
	if err := s.CreateAnswerProfile(ctx, p); err != nil {
		t.Fatalf("CreateAnswerProfile() error = %v", err)
	}

	list, err := s.ListAnswerProfiles(ctx)
	if err != nil {
		t.Fatalf("ListAnswerProfiles() error = %v", err)
	}
	if len(list) != 1 || list[0].ID != "ap1" || list[0].CreatedAt.IsZero() {
		t.Fatalf("list = %#v", list)
	}
	if list[0].Password != "secret" || list[0].Disk != "/dev/vda" || list[0].Locale != "zh_CN" {
		t.Fatalf("round-trip lost fields: %#v", list[0])
	}

	got, err := s.GetAnswerProfile(ctx, "ap1")
	if err != nil {
		t.Fatalf("GetAnswerProfile() error = %v", err)
	}
	if got.Hostname != "lab-node" || got.Keyboard != "us" {
		t.Fatalf("got = %#v", got)
	}

	p.Timezone = "UTC"
	if err := s.UpdateAnswerProfile(ctx, p); err != nil {
		t.Fatalf("UpdateAnswerProfile() error = %v", err)
	}
	got, err = s.GetAnswerProfile(ctx, "ap1")
	if err != nil {
		t.Fatalf("GetAnswerProfile() after update error = %v", err)
	}
	if got.Timezone != "UTC" || !got.UpdatedAt.After(got.CreatedAt) {
		t.Fatalf("after update = %#v", got)
	}

	if err := s.DeleteAnswerProfile(ctx, "ap1"); err != nil {
		t.Fatalf("DeleteAnswerProfile() error = %v", err)
	}
	if _, err = s.GetAnswerProfile(ctx, "ap1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing profile error = %v", err)
	}
	if err := s.DeleteAnswerProfile(ctx, "ap1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("double delete error = %v", err)
	}
}

func TestAnswerProfileDefaultPerProfile(t *testing.T) {
	ctx := context.Background()
	s := openSQLite(t)

	// Two defaults in different installer families coexist.
	deb := &AnswerProfile{ID: "d1", Name: "debian-default", Profile: "debian-preseed", Username: "fleet", IsDefault: true}
	if err := s.CreateAnswerProfile(ctx, deb); err != nil {
		t.Fatalf("CreateAnswerProfile(debian) error = %v", err)
	}
	deb2 := &AnswerProfile{ID: "d2", Name: "debian-alt", Profile: "debian-preseed", Username: "alt"}
	if err := s.CreateAnswerProfile(ctx, deb2); err != nil {
		t.Fatalf("CreateAnswerProfile(debian-alt) error = %v", err)
	}
	kick := &AnswerProfile{ID: "k1", Name: "kickstart-default", Profile: "kickstart", IsDefault: true}
	if err := s.CreateAnswerProfile(ctx, kick); err != nil {
		t.Fatalf("CreateAnswerProfile(kickstart) error = %v", err)
	}

	got, err := s.GetDefaultAnswerProfile(ctx, "debian-preseed")
	if err != nil {
		t.Fatalf("GetDefaultAnswerProfile(debian) error = %v", err)
	}
	if got.ID != "d1" {
		t.Fatalf("debian default = %q, want d1", got.ID)
	}
	got, err = s.GetDefaultAnswerProfile(ctx, "kickstart")
	if err != nil || got.ID != "k1" {
		t.Fatalf("kickstart default = %#v, err = %v", got, err)
	}
	if _, err := s.GetDefaultAnswerProfile(ctx, "nocloud"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing default error = %v", err)
	}

	// Promoting another debian profile clears the old default and keeps the
	// kickstart default untouched.
	deb2.IsDefault = true
	if err := s.UpdateAnswerProfile(ctx, deb2); err != nil {
		t.Fatalf("UpdateAnswerProfile(promote) error = %v", err)
	}
	got, err = s.GetDefaultAnswerProfile(ctx, "debian-preseed")
	if err != nil || got.ID != "d2" {
		t.Fatalf("debian default after promote = %#v, err = %v", got, err)
	}
	old, err := s.GetAnswerProfile(ctx, "d1")
	if err != nil || old.IsDefault {
		t.Fatalf("old default still flagged: %#v, err = %v", old, err)
	}
	got, err = s.GetDefaultAnswerProfile(ctx, "kickstart")
	if err != nil || got.ID != "k1" {
		t.Fatalf("kickstart default changed unexpectedly: %#v, err = %v", got, err)
	}
}

func TestAnswerProfileDuplicateNameConflict(t *testing.T) {
	ctx := context.Background()
	s := openSQLite(t)

	if err := s.CreateAnswerProfile(ctx, &AnswerProfile{ID: "ap1", Name: "same", Profile: "nocloud"}); err != nil {
		t.Fatalf("CreateAnswerProfile() error = %v", err)
	}
	err := s.CreateAnswerProfile(ctx, &AnswerProfile{ID: "ap2", Name: "same", Profile: "nocloud"})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate name error = %v, want ErrConflict", err)
	}
}
