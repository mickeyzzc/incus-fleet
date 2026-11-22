package agentops

import (
	"context"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/mickeyzzc/incus-fleet/internal/agentpb"
	"github.com/mickeyzzc/incus-fleet/internal/hostops"
	"github.com/mickeyzzc/incus-fleet/internal/nodenet"
	"github.com/mickeyzzc/incus-fleet/internal/store"
)

func newOverlayOperator(t *testing.T) (hostops.Operator, *fakeDispatcher) {
	t.Helper()

	dispatcher := &fakeDispatcher{}
	resolver := New(dispatcher, fakeLookup{agent: &store.Agent{ID: "agent-1", Status: store.AgentOnline}})
	operator, ok := resolver.Resolve(context.Background(), "c1", "node-a")
	if !ok {
		t.Fatal("Resolve() did not find online agent")
	}
	return operator, dispatcher
}

func decodeSpec(t *testing.T, payload []byte) *agentpb.NodenetApplySpec {
	t.Helper()

	var spec agentpb.NodenetApplySpec
	if err := proto.Unmarshal(payload, &spec); err != nil {
		t.Fatalf("unmarshal spec: %v", err)
	}
	return &spec
}

func TestOverlayMethodsCarryTheirFields(t *testing.T) {
	operator, dispatcher := newOverlayOperator(t)

	if err := operator.AddBridge(nodenet.BridgeConfig{Name: "fleetbr0", MTU: 1450, Address: "198.51.100.1/24"}); err != nil {
		t.Fatalf("AddBridge() error = %v", err)
	}
	spec := decodeSpec(t, dispatcher.payload)
	if spec.Action != "bridge-add" || spec.Interface != "fleetbr0" || spec.Mtu != 1450 || spec.Address != "198.51.100.1/24" {
		t.Fatalf("bridge spec = %#v", spec)
	}

	if err := operator.DeleteBridge("fleetbr0"); err != nil {
		t.Fatalf("DeleteBridge() error = %v", err)
	}
	if spec := decodeSpec(t, dispatcher.payload); spec.Action != "bridge-del" || spec.Interface != "fleetbr0" {
		t.Fatalf("bridge delete spec = %#v", spec)
	}

	link := nodenet.VXLANLink{
		Name: "vx1", Bridge: "fleetbr0", VNI: 6200, LocalIP: "192.0.2.11",
		RemoteIP: "192.0.2.12", Underlay: "enp1s0", Port: 4789, MTU: 1450,
	}
	if err := operator.AddVXLAN(link); err != nil {
		t.Fatalf("AddVXLAN() error = %v", err)
	}
	spec = decodeSpec(t, dispatcher.payload)
	if spec.Action != "vxlan-add" || spec.Interface != "vx1" || spec.Bridge != "fleetbr0" ||
		spec.Vni != 6200 || spec.LocalIp != "192.0.2.11" || spec.RemoteIp != "192.0.2.12" ||
		spec.Device != "enp1s0" || spec.Port != 4789 || spec.Mtu != 1450 {
		t.Fatalf("vxlan spec = %#v", spec)
	}

	if err := operator.DeleteVXLAN("vx1"); err != nil {
		t.Fatalf("DeleteVXLAN() error = %v", err)
	}
	if spec := decodeSpec(t, dispatcher.payload); spec.Action != "vxlan-del" || spec.Interface != "vx1" {
		t.Fatalf("vxlan delete spec = %#v", spec)
	}

	dhcp := nodenet.DHCPConfig{
		Name: "c62", Interface: "fleetbr0", RangeStart: "198.51.100.10", RangeEnd: "198.51.100.200",
		Gateway: "198.51.100.1", Netmask: "255.255.255.0", DNS: "1.1.1.1",
	}
	if err := operator.WriteDHCPConfig(dhcp); err != nil {
		t.Fatalf("WriteDHCPConfig() error = %v", err)
	}
	spec = decodeSpec(t, dispatcher.payload)
	if spec.Action != "dhcp-config" || spec.Comment != "c62" || spec.RangeStart != "198.51.100.10" ||
		spec.RangeEnd != "198.51.100.200" || spec.Gateway != "198.51.100.1" ||
		spec.Netmask != "255.255.255.0" || spec.Dns != "1.1.1.1" {
		t.Fatalf("dhcp spec = %#v", spec)
	}

	if err := operator.StartDHCP("c62"); err != nil {
		t.Fatalf("StartDHCP() error = %v", err)
	}
	if spec := decodeSpec(t, dispatcher.payload); spec.Action != "dhcp-start" || spec.Comment != "c62" {
		t.Fatalf("dhcp start spec = %#v", spec)
	}

	if err := operator.StopDHCP("c62"); err != nil {
		t.Fatalf("StopDHCP() error = %v", err)
	}
	if spec := decodeSpec(t, dispatcher.payload); spec.Action != "dhcp-stop" || spec.Comment != "c62" {
		t.Fatalf("dhcp stop spec = %#v", spec)
	}

	if err := operator.AddNAT(nodenet.NATConfig{Source: "198.51.100.0/24", OutDevice: "enp1s0", Comment: "c62"}); err != nil {
		t.Fatalf("AddNAT() error = %v", err)
	}
	spec = decodeSpec(t, dispatcher.payload)
	if spec.Action != "nat-add" || spec.Source != "198.51.100.0/24" || spec.OutDevice != "enp1s0" || spec.Comment != "c62" {
		t.Fatalf("nat spec = %#v", spec)
	}

	if err := operator.DeleteNAT("c62"); err != nil {
		t.Fatalf("DeleteNAT() error = %v", err)
	}
	if spec := decodeSpec(t, dispatcher.payload); spec.Action != "nat-del" || spec.Comment != "c62" {
		t.Fatalf("nat delete spec = %#v", spec)
	}
}

func TestNodenetActionAllowlist(t *testing.T) {
	for _, action := range []string{
		"link-state", "address", "route-add", "route-delete", "firewall-add", "firewall-delete", "wifi-connect",
		"bridge-add", "bridge-del", "vxlan-add", "vxlan-del",
		"dhcp-config", "dhcp-start", "dhcp-stop", "nat-add", "nat-del",
	} {
		if !IsNodenetAction(action) {
			t.Fatalf("IsNodenetAction(%q) = false", action)
		}
	}
	for _, action := range []string{"", "confirm", "rollback", "incus-bootstrap", "rm -rf"} {
		if IsNodenetAction(action) {
			t.Fatalf("IsNodenetAction(%q) = true", action)
		}
	}
	if len(NodenetActions()) < 16 {
		t.Fatalf("allowlist = %#v", NodenetActions())
	}
}

func TestOverlayMethodFailureSurfaces(t *testing.T) {
	operator := &Operator{
		dispatcher: failingDispatcher{state: agentpb.TaskState_TASK_STATE_FAILED},
		timeout:    time.Second,
	}

	if err := operator.AddBridge(nodenet.BridgeConfig{Name: "fleetbr0"}); err == nil {
		t.Fatal("expected AddBridge to fail")
	}
	if err := operator.AddVXLAN(nodenet.VXLANLink{Name: "vx1"}); err == nil {
		t.Fatal("expected AddVXLAN to fail")
	}
	if err := operator.StartDHCP("c62"); err == nil {
		t.Fatal("expected StartDHCP to fail")
	}
}
