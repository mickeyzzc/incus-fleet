package incusbootstrap

import (
	"strings"
	"testing"
)

func TestJoinPreseed(t *testing.T) {
	doc, err := Join(JoinOptions{Member: "node-b", Address: "198.51.100.11", Token: "token", StorageSource: "/srv/incus"})
	if err != nil {
		t.Fatalf("Join() error = %v", err)
	}
	for _, want := range []string{"cluster:", "server_address: 198.51.100.11:8443", "cluster_token: token", "storage-pool", "/srv/incus"} {
		if !strings.Contains(doc, want) {
			t.Fatalf("join preseed missing %q:\n%s", want, doc)
		}
	}
	if _, err := Join(JoinOptions{Member: "node-b", Address: "bad", Token: "token"}); err == nil {
		t.Fatal("Join(bad address) expected error")
	}
	if _, err := Join(JoinOptions{Member: "node-b", Address: "198.51.100.11"}); err == nil {
		t.Fatal("Join(no token) expected error")
	}
}

func TestInitPreseed(t *testing.T) {
	doc, err := Init(InitOptions{Member: "node-a", Address: "192.0.2.10", NetworkCIDR: "10.0.0.1/24"})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	for _, want := range []string{"core.https_address: 192.0.2.10:8443", "server_name: node-a", "incusbr0", "10.0.0.1/24"} {
		if !strings.Contains(doc, want) {
			t.Fatalf("init preseed missing %q:\n%s", want, doc)
		}
	}
	if _, err := Init(InitOptions{Member: "node-a", Address: "192.0.2.10", NetworkCIDR: "bad"}); err == nil {
		t.Fatal("Init(bad CIDR) expected error")
	}
}

func TestJoinPreseedMemberConfig(t *testing.T) {
	doc, err := Join(JoinOptions{
		Member: "node-b", Address: "198.51.100.11", Token: "token",
		StorageSource: "/data/incus",
		MemberConfig:  []MemberConfig{{Entity: "storage-pool", Name: "default", Key: "source", Value: "/old"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, "/data/incus") || strings.Contains(doc, "/old") {
		t.Fatalf("member config not replaced:\n%s", doc)
	}
}

func TestInitPreseedDefaults(t *testing.T) {
	doc, err := Init(InitOptions{Member: "node-a", Address: "192.0.2.10"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, "/srv/incus") || !strings.Contains(doc, "10.0.0.1/24") {
		t.Fatalf("defaults missing:\n%s", doc)
	}
	if _, err := Init(InitOptions{Member: "", Address: "192.0.2.10"}); err == nil {
		t.Fatal("Init(no member) expected error")
	}
}

func TestBuilderRejectsIncompleteOptions(t *testing.T) {
	if _, err := Join(JoinOptions{Address: "198.51.100.11", Token: "t"}); err == nil {
		t.Fatal("Join(no member) expected error")
	}
	if _, err := Join(JoinOptions{Member: "node-b", Token: "t"}); err == nil {
		t.Fatal("Join(no address) expected error")
	}
	if _, err := Init(InitOptions{Member: "node-a"}); err == nil {
		t.Fatal("Init(no address) expected error")
	}
	if _, err := Init(InitOptions{Member: "node-a", Address: "bad"}); err == nil {
		t.Fatal("Init(bad address) expected error")
	}
}
