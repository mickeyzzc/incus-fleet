package store

import (
	"context"
	"errors"
	"testing"
)

func TestRegisteredClusterLifecycle(t *testing.T) {
	ctx := context.Background()
	s := openSQLite(t)

	cluster := &RegisteredCluster{
		Name:          "c64",
		DisplayName:   "Cluster 64",
		Endpoint:      "https://192.0.2.10:8443",
		Project:       "default",
		TLSCert:       "cert-pem",
		TLSKey:        "key-pem",
		TLSServerCert: "server-pem",
	}
	if err := s.InsertCluster(ctx, cluster); err != nil {
		t.Fatalf("InsertCluster() error = %v", err)
	}
	if cluster.CreatedAt.IsZero() {
		t.Fatalf("InsertCluster() did not stamp CreatedAt")
	}

	list, err := s.ListClusters(ctx)
	if err != nil {
		t.Fatalf("ListClusters() error = %v", err)
	}
	if len(list) != 1 || list[0].Name != "c64" || list[0].TLSCert != "cert-pem" {
		t.Fatalf("list = %#v", list)
	}

	got, err := s.GetCluster(ctx, "c64")
	if err != nil {
		t.Fatalf("GetCluster() error = %v", err)
	}
	if got.Endpoint != cluster.Endpoint || got.TLSServerCert != "server-pem" {
		t.Fatalf("got = %#v", got)
	}

	if err := s.DeleteCluster(ctx, "c64"); err != nil {
		t.Fatalf("DeleteCluster() error = %v", err)
	}

	if _, err := s.GetCluster(ctx, "c64"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetCluster() after delete error = %v, want ErrNotFound", err)
	}
	if err := s.DeleteCluster(ctx, "c64"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeleteCluster() missing error = %v, want ErrNotFound", err)
	}

	list, err = s.ListClusters(ctx)
	if err != nil {
		t.Fatalf("ListClusters() after delete error = %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("list after delete = %#v", list)
	}
}

func TestRegisteredClusterDuplicate(t *testing.T) {
	ctx := context.Background()
	s := openSQLite(t)

	cluster := &RegisteredCluster{Name: "dup", Endpoint: "https://x:8443", TLSCert: "c", TLSKey: "k"}
	if err := s.InsertCluster(ctx, cluster); err != nil {
		t.Fatalf("InsertCluster() error = %v", err)
	}
	if err := s.InsertCluster(ctx, cluster); err == nil {
		t.Fatalf("InsertCluster() duplicate error = nil, want failure")
	}
}

func TestRegisteredClusterClosedStore(t *testing.T) {
	ctx := context.Background()
	s := openSQLite(t)
	if err := s.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if err := s.InsertCluster(ctx, &RegisteredCluster{Name: "x"}); err == nil {
		t.Fatalf("InsertCluster() on closed store error = nil")
	}
	if _, err := s.ListClusters(ctx); err == nil {
		t.Fatalf("ListClusters() on closed store error = nil")
	}
	if _, err := s.GetCluster(ctx, "x"); err == nil {
		t.Fatalf("GetCluster() on closed store error = nil")
	}
	if err := s.DeleteCluster(ctx, "x"); err == nil {
		t.Fatalf("DeleteCluster() on closed store error = nil")
	}
}
