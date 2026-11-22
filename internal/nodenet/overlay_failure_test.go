package nodenet

import (
	"errors"
	"strings"
	"testing"
)

// TestOverlayFailurePaths exercises the error branches of the overlay host
// primitives so a broken command path cannot silently report success.
func TestOverlayFailurePaths(t *testing.T) {
	t.Run("link exists query fails", func(t *testing.T) {
		svc := NewService(&fakeRunner{failOn: "ip link show"})
		if _, err := svc.LinkExists("fleetbr0"); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("expected unavailable, got %v", err)
		}
	})

	t.Run("bridge create fails", func(t *testing.T) {
		svc := NewService(&fakeRunner{failOn: "ip link add"})
		if err := svc.AddBridge(BridgeConfig{Name: "fleetbr0"}); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("expected unavailable, got %v", err)
		}
	})

	t.Run("bridge mtu fails", func(t *testing.T) {
		svc := NewService(&fakeRunner{failOn: "mtu"})
		if err := svc.AddBridge(BridgeConfig{Name: "fleetbr0", MTU: 1450}); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("expected unavailable, got %v", err)
		}
	})

	t.Run("bridge address query fails", func(t *testing.T) {
		svc := NewService(&fakeRunner{failOn: "ip -j addr show"})
		if err := svc.AddBridge(BridgeConfig{Name: "fleetbr0", Address: "10.0.0.1/24"}); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("expected unavailable, got %v", err)
		}
	})

	t.Run("bridge address replace fails", func(t *testing.T) {
		svc := NewService(&fakeRunner{failOn: "ip addr replace"})
		if err := svc.AddBridge(BridgeConfig{Name: "fleetbr0", Address: "10.0.0.1/24"}); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("expected unavailable, got %v", err)
		}
	})

	t.Run("bridge delete link check fails", func(t *testing.T) {
		svc := NewService(&fakeRunner{failOn: "ip link show"})
		if err := svc.DeleteBridge("fleetbr0"); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("expected unavailable, got %v", err)
		}
	})

	t.Run("bridge delete fails", func(t *testing.T) {
		r := &fakeRunner{
			outputs: map[string]string{"ip link show fleetbr0": "3: fleetbr0:"},
			failOn:  "ip link del",
		}
		if err := NewService(r).DeleteBridge("fleetbr0"); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("expected unavailable, got %v", err)
		}
	})

	t.Run("vxlan create fails", func(t *testing.T) {
		svc := NewService(&fakeRunner{failOn: "type vxlan"})
		link := VXLANLink{Name: "vx1", Bridge: "fleetbr0", VNI: 1, LocalIP: "192.0.2.1", RemoteIP: "192.0.2.2"}
		if err := svc.AddVXLAN(link); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("expected unavailable, got %v", err)
		}
	})

	t.Run("vxlan mtu fails", func(t *testing.T) {
		svc := NewService(&fakeRunner{failOn: "mtu"})
		link := VXLANLink{Name: "vx1", Bridge: "fleetbr0", VNI: 1, LocalIP: "192.0.2.1", RemoteIP: "192.0.2.2", MTU: 1450}
		if err := svc.AddVXLAN(link); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("expected unavailable, got %v", err)
		}
	})

	t.Run("vxlan master fails", func(t *testing.T) {
		svc := NewService(&fakeRunner{failOn: "master"})
		link := VXLANLink{Name: "vx1", Bridge: "fleetbr0", VNI: 1, LocalIP: "192.0.2.1", RemoteIP: "192.0.2.2"}
		if err := svc.AddVXLAN(link); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("expected unavailable, got %v", err)
		}
	})

	t.Run("vxlan up fails", func(t *testing.T) {
		svc := NewService(&fakeRunner{failOn: "vx1 up"})
		link := VXLANLink{Name: "vx1", Bridge: "fleetbr0", VNI: 1, LocalIP: "192.0.2.1", RemoteIP: "192.0.2.2"}
		if err := svc.AddVXLAN(link); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("expected unavailable, got %v", err)
		}
	})

	t.Run("vxlan delete fails", func(t *testing.T) {
		r := &fakeRunner{
			outputs: map[string]string{"ip link show vx1": "4: vx1:"},
			failOn:  "ip link del",
		}
		if err := NewService(r).DeleteVXLAN("vx1"); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("expected unavailable, got %v", err)
		}
	})

	t.Run("dhcp write fails", func(t *testing.T) {
		r := &failingStdinRunner{}
		cfg := DHCPConfig{Name: "c62", Interface: "fleetbr0", RangeStart: "198.51.100.10", RangeEnd: "198.51.100.20", Gateway: "198.51.100.1"}
		if err := NewService(r).WriteDHCPConfig(cfg); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("expected unavailable, got %v", err)
		}
	})

	t.Run("dhcp start reload fails", func(t *testing.T) {
		svc := NewService(&fakeRunner{failOn: "daemon-reload"})
		if err := svc.StartDHCP("c62"); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("expected unavailable, got %v", err)
		}
	})

	t.Run("dhcp start enable fails", func(t *testing.T) {
		svc := NewService(&fakeRunner{failOn: "systemctl enable"})
		if err := svc.StartDHCP("c62"); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("expected unavailable, got %v", err)
		}
	})

	t.Run("dhcp stop rm fails", func(t *testing.T) {
		svc := NewService(&fakeRunner{failOn: "rm -f"})
		if err := svc.StopDHCP("c62"); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("expected unavailable, got %v", err)
		}
	})

	t.Run("nat table create fails", func(t *testing.T) {
		svc := NewService(&fakeRunner{failOn: "add table"})
		if err := svc.AddNAT(NATConfig{Source: "10.0.0.0/24", Comment: "c62"}); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("expected unavailable, got %v", err)
		}
	})

	t.Run("nat chain create fails", func(t *testing.T) {
		svc := NewService(&fakeRunner{failOn: "add chain"})
		if err := svc.AddNAT(NATConfig{Source: "10.0.0.0/24", Comment: "c62"}); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("expected unavailable, got %v", err)
		}
	})

	t.Run("nat rule create fails", func(t *testing.T) {
		svc := NewService(&fakeRunner{failOn: "add rule"})
		if err := svc.AddNAT(NATConfig{Source: "10.0.0.0/24", Comment: "c62"}); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("expected unavailable, got %v", err)
		}
	})

	t.Run("nat delete fails", func(t *testing.T) {
		svc := NewService(&fakeRunner{failOn: "delete table"})
		if err := svc.DeleteNAT("c62"); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("expected unavailable, got %v", err)
		}
	})
}

// failingStdinRunner always fails the stdin write path.
type failingStdinRunner struct{}

func (failingStdinRunner) Run(string, ...string) (string, error) { return "", nil }
func (failingStdinRunner) RunStdin(string, []string, string) error {
	return errors.New("write failed")
}

// TestNATIdempotentBranches covers the "already exists" tolerance.
func TestNATIdempotentBranches(t *testing.T) {
	r := &existingErrRunner{}
	if err := NewService(r).AddNAT(NATConfig{Source: "10.0.0.0/24", Comment: "c62"}); err != nil {
		t.Fatalf("AddNAT() must tolerate existing objects: %v", err)
	}
}

// existingErrRunner reports "File exists" on every command.
type existingErrRunner struct{}

func (existingErrRunner) Run(name string, args ...string) (string, error) {
	if name == "nft" && strings.Contains(strings.Join(args, " "), "delete") {
		return "No such file or directory", errors.New("no such file")
	}
	return "File exists", errors.New("file exists")
}
func (existingErrRunner) RunStdin(string, []string, string) error { return nil }

// TestEnsureFleetTableFailureBranches covers table discovery and creation
// failures, which must surface rather than silently leaving the firewall
// unmanaged.
func TestEnsureFleetTableFailureBranches(t *testing.T) {
	t.Run("list fails", func(t *testing.T) {
		svc := NewService(&fakeRunner{failOn: "nft list tables"})
		if err := svc.AddFirewallRule(FirewallRequest{Port: 22, Proto: "tcp", Action: "accept"}); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("expected unavailable, got %v", err)
		}
	})

	t.Run("create table fails", func(t *testing.T) {
		r := &fakeRunner{outputs: map[string]string{"nft list tables": ""}, failOn: "add table"}
		svc := NewService(r)
		if err := svc.AddFirewallRule(FirewallRequest{Port: 22, Proto: "tcp", Action: "accept"}); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("expected unavailable, got %v", err)
		}
	})

	t.Run("create chain fails", func(t *testing.T) {
		r := &fakeRunner{outputs: map[string]string{"nft list tables": ""}, failOn: "add chain"}
		svc := NewService(r)
		if err := svc.AddFirewallRule(FirewallRequest{Port: 22, Proto: "tcp", Action: "accept"}); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("expected unavailable, got %v", err)
		}
	})
}
