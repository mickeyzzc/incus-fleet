package nodenet

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateBridge(t *testing.T) {
	valid := BridgeConfig{Name: "fleetbr0", MTU: 1450, Address: "198.51.100.1/24"}
	if err := ValidateBridge(valid); err != nil {
		t.Fatalf("ValidateBridge() error = %v", err)
	}
	for _, tc := range []struct {
		name string
		cfg  BridgeConfig
	}{
		{"bad name", BridgeConfig{Name: "br0; rm", MTU: 1450}},
		{"mtu too small", BridgeConfig{Name: "fleetbr0", MTU: 100}},
		{"mtu too large", BridgeConfig{Name: "fleetbr0", MTU: 10000}},
		{"bad address", BridgeConfig{Name: "fleetbr0", Address: "nope"}},
	} {
		if err := ValidateBridge(tc.cfg); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("%s: expected invalid request, got %v", tc.name, err)
		}
	}
}

func TestValidateVXLAN(t *testing.T) {
	valid := VXLANLink{Name: "vxlan0", Bridge: "fleetbr0", VNI: 100, LocalIP: "192.0.2.1", RemoteIP: "192.0.2.2", Port: 4789, MTU: 1450}
	if err := ValidateVXLAN(valid); err != nil {
		t.Fatalf("ValidateVXLAN() error = %v", err)
	}
	for _, tc := range []struct {
		name string
		link VXLANLink
	}{
		{"bad name", VXLANLink{Name: "vx;rm", Bridge: "fleetbr0", VNI: 1, LocalIP: "192.0.2.1", RemoteIP: "192.0.2.2"}},
		{"vni zero", VXLANLink{Name: "vx0", Bridge: "fleetbr0", VNI: 0, LocalIP: "192.0.2.1", RemoteIP: "192.0.2.2"}},
		{"vni too big", VXLANLink{Name: "vx0", Bridge: "fleetbr0", VNI: 16777216, LocalIP: "192.0.2.1", RemoteIP: "192.0.2.2"}},
		{"bad local", VXLANLink{Name: "vx0", Bridge: "fleetbr0", VNI: 1, LocalIP: "", RemoteIP: "192.0.2.2"}},
		{"bad remote", VXLANLink{Name: "vx0", Bridge: "fleetbr0", VNI: 1, LocalIP: "192.0.2.1", RemoteIP: "x"}},
		{"bad underlay", VXLANLink{Name: "vx0", Bridge: "fleetbr0", VNI: 1, LocalIP: "192.0.2.1", RemoteIP: "192.0.2.2", Underlay: "eth0; rm"}},
		{"bad port", VXLANLink{Name: "vx0", Bridge: "fleetbr0", VNI: 1, LocalIP: "192.0.2.1", RemoteIP: "192.0.2.2", Port: 70000}},
		{"bad mtu", VXLANLink{Name: "vx0", Bridge: "fleetbr0", VNI: 1, LocalIP: "192.0.2.1", RemoteIP: "192.0.2.2", MTU: 100}},
	} {
		if err := ValidateVXLAN(tc.link); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("%s: expected invalid request, got %v", tc.name, err)
		}
	}
}

func TestAddBridgeCreatesAndIsIdempotent(t *testing.T) {
	r := &fakeRunner{}
	svc := NewService(r)
	cfg := BridgeConfig{Name: "fleetbr0", MTU: 1450, Address: "198.51.100.1/24"}

	if err := svc.AddBridge(cfg); err != nil {
		t.Fatalf("AddBridge() error = %v", err)
	}
	joined := strings.Join(r.calls, "\n")
	for _, want := range []string{
		"ip link add fleetbr0 type bridge",
		"ip link set fleetbr0 mtu 1450",
		"ip link set fleetbr0 up",
		"ip -j addr show dev fleetbr0",
		"ip addr replace 198.51.100.1/24 dev fleetbr0",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in calls:\n%s", want, joined)
		}
	}

	// Simulate an existing bridge with the address already applied.
	r2 := &fakeRunner{outputs: map[string]string{
		"ip link show fleetbr0":       "3: fleetbr0: <BROADCAST> mtu 1450",
		"ip -j addr show dev fleetbr0": `[{"addr_info":[{"local":"198.51.100.1","prefixlen":24}]}]`,
	}}
	svc2 := NewService(r2)
	if err := svc2.AddBridge(cfg); err != nil {
		t.Fatalf("AddBridge(existing) error = %v", err)
	}
	joined2 := strings.Join(r2.calls, "\n")
	if strings.Contains(joined2, "ip link add") || strings.Contains(joined2, "addr replace") {
		t.Fatalf("existing bridge should not be recreated:\n%s", joined2)
	}
}

func TestDeleteBridge(t *testing.T) {
	r := &fakeRunner{}
	svc := NewService(r)
	if err := svc.DeleteBridge("bad name"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected validation error, got %v", err)
	}
	// No link present -> no delete command.
	if err := svc.DeleteBridge("fleetbr0"); err != nil {
		t.Fatalf("DeleteBridge(missing) error = %v", err)
	}
	r2 := &fakeRunner{outputs: map[string]string{"ip link show fleetbr0": "3: fleetbr0:"}}
	if err := NewService(r2).DeleteBridge("fleetbr0"); err != nil {
		t.Fatalf("DeleteBridge() error = %v", err)
	}
	if !strings.Contains(strings.Join(r2.calls, "\n"), "ip link del fleetbr0") {
		t.Fatalf("calls = %#v", r2.calls)
	}
}

func TestAddVXLANBuildsTunnelAndIsIdempotent(t *testing.T) {
	r := &fakeRunner{}
	svc := NewService(r)
	link := VXLANLink{Name: "vxlan100", Bridge: "fleetbr0", VNI: 100, LocalIP: "192.0.2.1", RemoteIP: "192.0.2.2", Underlay: "eth0", MTU: 1450}

	if err := svc.AddVXLAN(link); err != nil {
		t.Fatalf("AddVXLAN() error = %v", err)
	}
	joined := strings.Join(r.calls, "\n")
	for _, want := range []string{
		"ip link add vxlan100 type vxlan id 100 local 192.0.2.1 remote 192.0.2.2 dstport 4789 dev eth0",
		"ip link set vxlan100 mtu 1450",
		"ip link set vxlan100 master fleetbr0",
		"ip link set vxlan100 up",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in calls:\n%s", want, joined)
		}
	}

	// Port 0 uses the IANA default; no underlay leaves the dev clause out.
	r2 := &fakeRunner{}
	link2 := VXLANLink{Name: "vxlan200", Bridge: "fleetbr0", VNI: 200, LocalIP: "192.0.2.1", RemoteIP: "192.0.2.2"}
	if err := NewService(r2).AddVXLAN(link2); err != nil {
		t.Fatalf("AddVXLAN(no underlay) error = %v", err)
	}
	if !strings.Contains(strings.Join(r2.calls, "\n"), "dstport 4789") {
		t.Fatalf("calls = %#v", r2.calls)
	}

	// Existing link is untouched.
	r3 := &fakeRunner{outputs: map[string]string{"ip link show vxlan100": "4: vxlan100:"}}
	if err := NewService(r3).AddVXLAN(link); err != nil {
		t.Fatalf("AddVXLAN(existing) error = %v", err)
	}
	if strings.Contains(strings.Join(r3.calls, "\n"), "ip link add") {
		t.Fatalf("existing vxlan should not be recreated: %#v", r3.calls)
	}
}

func TestDeleteVXLAN(t *testing.T) {
	r := &fakeRunner{}
	svc := NewService(r)
	if err := svc.DeleteVXLAN("vxlan100"); err != nil {
		t.Fatalf("DeleteVXLAN(missing) error = %v", err)
	}
	r2 := &fakeRunner{outputs: map[string]string{"ip link show vxlan100": "4: vxlan100:"}}
	if err := NewService(r2).DeleteVXLAN("vxlan100"); err != nil {
		t.Fatalf("DeleteVXLAN() error = %v", err)
	}
	if !strings.Contains(strings.Join(r2.calls, "\n"), "ip link del vxlan100") {
		t.Fatalf("calls = %#v", r2.calls)
	}
}
