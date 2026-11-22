package nodenet

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBridgeNetdevAndNetwork(t *testing.T) {
	netdev := BridgeNetdev(BridgeConfig{Name: "fleetbr0", MTU: 1450})
	for _, want := range []string{"Name=fleetbr0", "Kind=bridge", "MTUBytes=1450"} {
		if !strings.Contains(netdev, want) {
			t.Fatalf("netdev missing %q:\n%s", want, netdev)
		}
	}

	network := BridgeNetwork(BridgeConfig{Name: "fleetbr0", MTU: 1450, Address: "198.51.100.1/24"})
	for _, want := range []string{"Name=fleetbr0", "Address=198.51.100.1/24", "ConfigureWithoutCarrier=yes"} {
		if !strings.Contains(network, want) {
			t.Fatalf("network missing %q:\n%s", want, network)
		}
	}

	// A bridge without MTU or address emits no optional sections.
	bare := BridgeNetdev(BridgeConfig{Name: "fleetbr0"})
	if strings.Contains(bare, "[Bridge]") {
		t.Fatalf("bare bridge should not emit [Bridge]:\n%s", bare)
	}
}

func TestVXLANNetdevAndNetwork(t *testing.T) {
	link := VXLANLink{Name: "vxlan100", Bridge: "fleetbr0", VNI: 100, LocalIP: "192.0.2.1", RemoteIP: "192.0.2.2", Underlay: "eth0", MTU: 1450}
	netdev := VXLANNetdev(link)
	for _, want := range []string{
		"Name=vxlan100", "Kind=vxlan", "VNI=100", "Local=192.0.2.1", "Remote=192.0.2.2",
		"DestinationPort=4789", "Parent=eth0",
	} {
		if !strings.Contains(netdev, want) {
			t.Fatalf("netdev missing %q:\n%s", want, netdev)
		}
	}

	network := VXLANNetwork(link)
	for _, want := range []string{"Name=vxlan100", "MTUBytes=1450", "Bridge=fleetbr0"} {
		if !strings.Contains(network, want) {
			t.Fatalf("network missing %q:\n%s", want, network)
		}
	}

	// Custom port is honored; no underlay omits Parent.
	custom := VXLANNetdev(VXLANLink{Name: "vx", Bridge: "fleetbr0", VNI: 5, LocalIP: "192.0.2.1", RemoteIP: "192.0.2.2", Port: 5555})
	if !strings.Contains(custom, "DestinationPort=5555") {
		t.Fatalf("custom port missing:\n%s", custom)
	}
	if strings.Contains(custom, "Parent=") {
		t.Fatalf("Parent must be omitted without underlay:\n%s", custom)
	}
}

func TestPersistBridgeWritesUnits(t *testing.T) {
	root := t.TempDir()
	run := &persistRunner{}
	cfg := BridgeConfig{Name: "fleetbr0", MTU: 1450, Address: "198.51.100.1/24"}

	if err := PersistBridge(run, root, ManagerNetworkd, cfg); err != nil {
		t.Fatalf("PersistBridge() error = %v", err)
	}
	base := filepath.Join(root, "etc/systemd/network", "10-fleet-fleetbr0")
	for _, path := range []string{base + ".netdev", base + ".network"} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("missing %s: %v", path, err)
		}
	}
	if !strings.Contains(strings.Join(run.calls, "\n"), "networkctl reload") {
		t.Fatalf("calls = %#v", run.calls)
	}

	if err := PersistBridge(run, root, ManagerIfupdown, cfg); !errors.Is(err, ErrUnsupportedManager) {
		t.Fatalf("expected unsupported manager, got %v", err)
	}
	if err := PersistBridge(run, root, ManagerNetworkd, BridgeConfig{Name: "bad name"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected validation error, got %v", err)
	}
}

func TestPersistVXLANWritesUnits(t *testing.T) {
	root := t.TempDir()
	run := &persistRunner{}
	link := VXLANLink{Name: "vxlan100", Bridge: "fleetbr0", VNI: 100, LocalIP: "192.0.2.1", RemoteIP: "192.0.2.2", MTU: 1450}

	if err := PersistVXLAN(run, root, ManagerNetworkd, link); err != nil {
		t.Fatalf("PersistVXLAN() error = %v", err)
	}
	path := filepath.Join(root, "etc/systemd/network", "10-fleet-vxlan100.netdev")
	body, err := os.ReadFile(path) //nolint:gosec // test fixture
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !strings.Contains(string(body), "VNI=100") {
		t.Fatalf("netdev = %s", body)
	}

	if err := PersistVXLAN(run, root, ManagerNetplan, link); !errors.Is(err, ErrUnsupportedManager) {
		t.Fatalf("expected unsupported manager, got %v", err)
	}
}

func TestRemoveOverlayPersistence(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "etc/systemd/network")
	if err := os.MkdirAll(base, 0o750); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(base, "10-fleet-fleetbr0.netdev")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}

	if err := RemoveOverlayPersistence(root, "fleetbr0"); err != nil {
		t.Fatalf("RemoveOverlayPersistence() error = %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected removal, got %v", err)
	}
	// Idempotent when nothing is left.
	if err := RemoveOverlayPersistence(root, "fleetbr0"); err != nil {
		t.Fatalf("second RemoveOverlayPersistence() error = %v", err)
	}
	if err := RemoveOverlayPersistence(root, "bad name"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected validation error, got %v", err)
	}
}

func TestOverlayPersistencePath(t *testing.T) {
	if got := OverlayPersistencePath("/root", ManagerNetworkd, "br0"); got != "/root/etc/systemd/network/10-fleet-br0.netdev" {
		t.Fatalf("path = %q", got)
	}
	if got := OverlayPersistencePath("/root", ManagerNetplan, "br0"); got != "" {
		t.Fatalf("non-networkd path = %q", got)
	}
}
