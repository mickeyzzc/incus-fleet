package overlay

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mickeyzzc/incus-fleet/internal/clusters"
	"github.com/mickeyzzc/incus-fleet/internal/hostops"
	"github.com/mickeyzzc/incus-fleet/internal/nodenet"
	"github.com/mickeyzzc/incus-fleet/internal/store"
)

// fakeLookup serves a fixed member list.
type fakeLookup struct {
	members []clusters.Member
	err     error
}

func (f *fakeLookup) Members(context.Context, string) ([]clusters.Member, error) {
	return f.members, f.err
}

// fakeNode records the overlay calls it receives.
type fakeNode struct {
	bridges  []nodenet.BridgeConfig
	links    []nodenet.VXLANLink
	dhcp     []nodenet.DHCPConfig
	started  []string
	stopped  []string
	nats     []nodenet.NATConfig
	natDel   []string
	bridgeOK int
	failOn   string
}

func (f *fakeNode) matches(op string) bool { return f.failOn != "" && f.failOn == op }

func (f *fakeNode) Snapshot(context.Context, string) (*nodenet.Snapshot, error) {
	return &nodenet.Snapshot{}, nil
}
func (f *fakeNode) ScanWiFi(string) ([]nodenet.WiFiNetwork, error) { return nil, nil }
func (f *fakeNode) SetNICState(string, bool) error                 { return nil }
func (f *fakeNode) ApplyAddress(context.Context, string, nodenet.AddressRequest) error {
	return nil
}
func (f *fakeNode) ListRoutes() ([]nodenet.Route, error)  { return nil, nil }
func (f *fakeNode) AddRoute(string, string, string) error { return nil }
func (f *fakeNode) DeleteRoute(string) error              { return nil }
func (f *fakeNode) ListFirewall() ([]nodenet.FirewallRule, error) {
	return nil, nil
}
func (f *fakeNode) AddFirewallRule(nodenet.FirewallRequest) error { return nil }
func (f *fakeNode) DeleteFirewallRule(string) error               { return nil }
func (f *fakeNode) WifiConnect(string, string, string) error      { return nil }

func (f *fakeNode) AddBridge(cfg nodenet.BridgeConfig) error {
	if f.matches("bridge") {
		return errors.New("bridge failed")
	}
	f.bridges = append(f.bridges, cfg)
	f.bridgeOK++
	return nil
}
func (f *fakeNode) DeleteBridge(string) error {
	if f.matches("bridge-del") {
		return errors.New("bridge delete failed")
	}
	return nil
}
func (f *fakeNode) AddVXLAN(link nodenet.VXLANLink) error {
	if f.matches("vxlan") {
		return errors.New("vxlan failed")
	}
	f.links = append(f.links, link)
	return nil
}
func (f *fakeNode) DeleteVXLAN(string) error {
	if f.matches("vxlan-del") {
		return errors.New("vxlan delete failed")
	}
	return nil
}
func (f *fakeNode) WriteDHCPConfig(cfg nodenet.DHCPConfig) error {
	if f.matches("dhcp-write") {
		return errors.New("dhcp write failed")
	}
	f.dhcp = append(f.dhcp, cfg)
	return nil
}
func (f *fakeNode) StartDHCP(name string) error {
	if f.matches("dhcp-start") {
		return errors.New("dhcp start failed")
	}
	f.started = append(f.started, name)
	return nil
}
func (f *fakeNode) StopDHCP(name string) error {
	if f.matches("dhcp-stop") {
		return errors.New("dhcp stop failed")
	}
	f.stopped = append(f.stopped, name)
	return nil
}
func (f *fakeNode) AddNAT(cfg nodenet.NATConfig) error {
	if f.matches("nat") {
		return errors.New("nat failed")
	}
	f.nats = append(f.nats, cfg)
	return nil
}
func (f *fakeNode) DeleteNAT(name string) error {
	if f.matches("nat-del") {
		return errors.New("nat delete failed")
	}
	f.natDel = append(f.natDel, name)
	return nil
}

// fakeNodes hands out one fakeNode per member.
type fakeNodes struct {
	operators map[string]*fakeNode
	err       error
	missing   map[string]bool
}

func (f *fakeNodes) Resolve(_ context.Context, _, member string, _ hostops.Kind) (hostops.Operator, hostops.Backend, func(), error) {
	if f.err != nil {
		return nil, "", nil, f.err
	}
	if f.missing[member] {
		return nil, "", nil, hostops.ErrAgentUnavailable
	}
	operator, ok := f.operators[member]
	if !ok {
		operator = &fakeNode{}
		f.operators[member] = operator
	}
	return operator, hostops.BackendAgent, func() {}, nil
}

// fakeStore is an in-memory overlay store.
type fakeStore struct {
	items map[string]*store.OverlayNetwork
	keys  []string
}

func newFakeStore() *fakeStore {
	return &fakeStore{items: map[string]*store.OverlayNetwork{}}
}

func (f *fakeStore) key(cluster, name string) string { return cluster + "/" + name }

func (f *fakeStore) SaveOverlay(_ context.Context, overlay *store.OverlayNetwork) error {
	clone := *overlay
	f.items[f.key(overlay.Cluster, overlay.Name)] = &clone
	f.keys = append(f.keys, overlay.Name)
	return nil
}
func (f *fakeStore) GetOverlay(_ context.Context, cluster, name string) (*store.OverlayNetwork, error) {
	item, ok := f.items[f.key(cluster, name)]
	if !ok {
		return nil, store.ErrNotFound
	}
	clone := *item
	return &clone, nil
}
func (f *fakeStore) ListOverlays(_ context.Context, cluster string) ([]*store.OverlayNetwork, error) {
	out := []*store.OverlayNetwork{}
	for _, item := range f.items {
		if item.Cluster == cluster {
			clone := *item
			out = append(out, &clone)
		}
	}
	return out, nil
}
func (f *fakeStore) DeleteOverlay(_ context.Context, cluster, name string) error {
	key := f.key(cluster, name)
	if _, ok := f.items[key]; !ok {
		return store.ErrNotFound
	}
	delete(f.items, key)
	return nil
}

func twoMembers() []clusters.Member {
	return []clusters.Member{
		{Name: "node-a", URL: "https://192.0.2.11:8443"},
		{Name: "node-b", URL: "https://192.0.2.12:8443"},
	}
}

func newTestService(members []clusters.Member) (*Service, *fakeNodes, *fakeStore) {
	nodes := &fakeNodes{operators: map[string]*fakeNode{}, missing: map[string]bool{}}
	st := newFakeStore()
	return New(&fakeLookup{members: members}, nodes, st), nodes, st
}

func TestNormalizeDefaultsAndValidation(t *testing.T) {
	overlay, network, err := normalize(Request{Name: "c62", Subnet: "198.51.100.0/24", GatewayMember: "node-a", VNI: 6200}, nil)
	if err != nil {
		t.Fatalf("normalize() error = %v", err)
	}
	if overlay.Bridge != DefaultBridge || overlay.MTU != DefaultMTU || !overlay.Enabled {
		t.Fatalf("defaults = %#v", overlay)
	}
	if overlay.DHCPStart != "198.51.100.10" || overlay.DHCPEnd != "198.51.100.244" {
		t.Fatalf("dhcp range = %s-%s", overlay.DHCPStart, overlay.DHCPEnd)
	}
	if network.String() != "198.51.100.0/24" {
		t.Fatalf("network = %s", network)
	}

	invalid := []Request{
		{Name: "Bad Name", Subnet: "198.51.100.0/24", GatewayMember: "a", VNI: 1},
		{Name: "ok", Subnet: "198.51.100.0/24", GatewayMember: "a", VNI: 0},
		{Name: "ok", Subnet: "198.51.100.0/24", GatewayMember: "a", VNI: 1, MTU: 100},
		{Name: "ok", Subnet: "nope", GatewayMember: "a", VNI: 1},
		{Name: "ok", Subnet: "fd00::/64", GatewayMember: "a", VNI: 1},
		{Name: "ok", Subnet: "198.51.100.0/24", VNI: 1},
		{Name: "ok", Subnet: "198.51.100.0/24", GatewayMember: "a", VNI: 1, DHCPStart: "203.0.113.10", DHCPEnd: "203.0.113.20"},
		{Name: "ok", Subnet: "198.51.100.0/24", GatewayMember: "a", VNI: 1, DHCPStart: "198.51.100.20", DHCPEnd: "198.51.100.10"},
		{Name: "ok", Subnet: "198.51.100.0/24", GatewayMember: "a", VNI: 1, DHCPStart: "198.51.100.1", DHCPEnd: "198.51.100.50"},
		{Name: "ok", Subnet: "198.51.100.0/24", GatewayMember: "a", VNI: 1, Bridge: "br; rm"},
	}
	for _, req := range invalid {
		if _, _, err := normalize(req, nil); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("expected invalid for %#v, got %v", req, err)
		}
	}
}

func TestNormalizeInheritsPrevious(t *testing.T) {
	previous := &store.OverlayNetwork{
		Name: "c62", Bridge: "fleetbr0", VNI: 6200, Subnet: "198.51.100.0/24",
		GatewayMember: "node-a", MTU: 1450, DHCPStart: "198.51.100.10", DHCPEnd: "198.51.100.200",
	}
	overlay, _, err := normalize(Request{Name: "c62"}, previous)
	if err != nil {
		t.Fatalf("normalize() error = %v", err)
	}
	if overlay.VNI != 6200 || overlay.MTU != 1450 || overlay.DHCPEnd != "198.51.100.200" {
		t.Fatalf("inherited = %#v", overlay)
	}
}

func TestPlanTunnels(t *testing.T) {
	cases := []struct {
		members []string
		want    int
	}{
		{nil, 0},
		{[]string{"a"}, 0},
		{[]string{"b", "a"}, 1},
		{[]string{"c", "a", "b"}, 3},
		{[]string{"a", "b", "c", "d", "e"}, 10},
	}
	for _, tc := range cases {
		if got := len(planTunnels(tc.members)); got != tc.want {
			t.Fatalf("planTunnels(%v) = %d, want %d", tc.members, got, tc.want)
		}
	}

	tunnels := planTunnels([]string{"c", "a", "b"})
	if tunnels[0].Left != "a" || tunnels[0].Right != "b" {
		t.Fatalf("tunnels = %#v", tunnels)
	}
}

func TestTunnelNameIsStableAndBounded(t *testing.T) {
	name := tunnelName("c62", "node-a", "node-b")
	if len(name) > 15 {
		t.Fatalf("name too long: %q", name)
	}
	if name != tunnelName("c62", "node-a", "node-b") {
		t.Fatal("tunnelName must be deterministic")
	}
	if name == tunnelName("c62", "node-b", "node-a") {
		t.Log("direction affects the name; callers pass a sorted pair")
	}
}

func TestEnableTwoMembers(t *testing.T) {
	service, nodes, st := newTestService(twoMembers())

	status, err := service.Enable(context.Background(), "c62", Request{
		Name: "c62", VNI: 6200, Subnet: "198.51.100.0/24", GatewayMember: "node-a",
	})
	if err != nil {
		t.Fatalf("Enable() error = %v", err)
	}
	if !status.Enabled || status.Bridge != DefaultBridge || status.MTU != DefaultMTU {
		t.Fatalf("status = %#v", status)
	}
	if len(status.Members) != 2 {
		t.Fatalf("members = %#v", status.Members)
	}

	// Each member gets a bridge with a distinct host address.
	nodeA := nodes.operators["node-a"]
	nodeB := nodes.operators["node-b"]
	if nodeA == nil || nodeB == nil {
		t.Fatalf("operators = %#v", nodes.operators)
	}
	if len(nodeA.bridges) != 1 || nodeA.bridges[0].Address != "198.51.100.1/24" {
		t.Fatalf("node-a bridges = %#v", nodeA.bridges)
	}
	if len(nodeB.bridges) != 1 || nodeB.bridges[0].Address != "198.51.100.2/24" {
		t.Fatalf("node-b bridges = %#v", nodeB.bridges)
	}

	// One tunnel endpoint on each side, mirrored.
	if len(nodeA.links) != 1 || nodeA.links[0].RemoteIP != "192.0.2.12" || nodeA.links[0].LocalIP != "192.0.2.11" {
		t.Fatalf("node-a links = %#v", nodeA.links)
	}
	if len(nodeB.links) != 1 || nodeB.links[0].RemoteIP != "192.0.2.11" {
		t.Fatalf("node-b links = %#v", nodeB.links)
	}
	if nodeA.links[0].Name != nodeB.links[0].Name {
		t.Fatalf("tunnel names differ: %s vs %s", nodeA.links[0].Name, nodeB.links[0].Name)
	}
	if nodeA.links[0].VNI != 6200 {
		t.Fatalf("vni = %d", nodeA.links[0].VNI)
	}

	// DHCP and NAT only on the gateway.
	if len(nodeA.dhcp) != 1 || nodeA.dhcp[0].Gateway != "198.51.100.1" || nodeA.dhcp[0].Netmask != "255.255.255.0" {
		t.Fatalf("node-a dhcp = %#v", nodeA.dhcp)
	}
	if len(nodeA.started) != 1 || len(nodeA.nats) != 1 || nodeA.nats[0].Source != "198.51.100.0/24" {
		t.Fatalf("node-a gateway config = %#v %#v", nodeA.started, nodeA.nats)
	}
	if len(nodeB.dhcp) != 0 || len(nodeB.started) != 0 || len(nodeB.nats) != 0 {
		t.Fatalf("node-b must not run the gateway: %#v %#v %#v", nodeB.dhcp, nodeB.started, nodeB.nats)
	}

	// The definition is persisted.
	if _, err := st.GetOverlay(context.Background(), "c62", "c62"); err != nil {
		t.Fatalf("GetOverlay() error = %v", err)
	}
}

func TestEnableThreeAndFiveMembersCreateFullMesh(t *testing.T) {
	for _, count := range []int{3, 5} {
		members := make([]clusters.Member, 0, count)
		for i := 0; i < count; i++ {
			members = append(members, clusters.Member{
				Name: string(rune('a' + i)), URL: "https://192.0.2." + string(rune('1'+i)) + ":8443",
			})
		}
		service, nodes, _ := newTestService(members)
		if _, err := service.Enable(context.Background(), "c62", Request{
			Name: "c62", VNI: 100, Subnet: "198.51.100.0/24", GatewayMember: "a",
		}); err != nil {
			t.Fatalf("Enable(%d members) error = %v", count, err)
		}
		want := count - 1
		for name, node := range nodes.operators {
			if len(node.links) != want {
				t.Fatalf("%d members: %s has %d links, want %d", count, name, len(node.links), want)
			}
		}
	}
}

func TestEnableRejectsBadInputs(t *testing.T) {
	service, _, _ := newTestService(twoMembers())
	ctx := context.Background()

	if _, err := service.Enable(ctx, "c62", Request{Name: "c62"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("missing subnet: %v", err)
	}
	if _, err := service.Enable(ctx, "c62", Request{Name: "c62", VNI: 1, Subnet: "198.51.100.0/24", GatewayMember: "ghost"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("unknown gateway: %v", err)
	}
	if _, err := service.Enable(ctx, "c62", Request{Name: "c62", VNI: 1, Subnet: "198.51.100.0/24", GatewayMember: "node-a", UnderlayIPs: map[string]string{"node-a": "not-an-ip"}}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("bad underlay: %v", err)
	}

	// A single-member cluster cannot form a mesh.
	single, _, _ := newTestService([]clusters.Member{{Name: "node-a", URL: "https://192.0.2.11:8443"}})
	if _, err := single.Enable(ctx, "c62", Request{Name: "c62", VNI: 1, Subnet: "198.51.100.0/24", GatewayMember: "node-a"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("single member: %v", err)
	}
}

func TestEnableRejectsDuplicate(t *testing.T) {
	service, _, _ := newTestService(twoMembers())
	req := Request{Name: "c62", VNI: 6200, Subnet: "198.51.100.0/24", GatewayMember: "node-a"}
	if _, err := service.Enable(context.Background(), "c62", req); err != nil {
		t.Fatalf("first Enable() error = %v", err)
	}
	if _, err := service.Enable(context.Background(), "c62", req); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("duplicate Enable() = %v", err)
	}
}

func TestEnablePropagatesMemberFailures(t *testing.T) {
	service, nodes, _ := newTestService(twoMembers())
	nodes.operators["node-a"] = &fakeNode{failOn: "vxlan"}

	_, err := service.Enable(context.Background(), "c62", Request{
		Name: "c62", VNI: 6200, Subnet: "198.51.100.0/24", GatewayMember: "node-a",
	})
	if err == nil || !strings.Contains(err.Error(), "add vxlan") {
		t.Fatalf("expected vxlan failure, got %v", err)
	}
}

func TestReconcileAddsNewMember(t *testing.T) {
	members := twoMembers()
	service, nodes, _ := newTestService(members)
	if _, err := service.Enable(context.Background(), "c62", Request{
		Name: "c62", VNI: 6200, Subnet: "198.51.100.0/24", GatewayMember: "node-a",
	}); err != nil {
		t.Fatalf("Enable() error = %v", err)
	}

	// A third member joins; reconcile must bring it into the mesh.
	service.clusters = &fakeLookup{members: append(members, clusters.Member{Name: "node-c", URL: "https://192.0.2.13:8443"})}
	nodes.operators = map[string]*fakeNode{}

	status, err := service.Reconcile(context.Background(), "c62", "c62")
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if len(status.Members) != 3 {
		t.Fatalf("members = %#v", status.Members)
	}
	for name, node := range nodes.operators {
		if len(node.links) != 2 {
			t.Fatalf("%s has %d links, want 2", name, len(node.links))
		}
	}
}

func TestReconcileAndDisableErrors(t *testing.T) {
	service, _, _ := newTestService(twoMembers())
	ctx := context.Background()

	if _, err := service.Reconcile(ctx, "c62", "ghost"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Reconcile(missing) = %v", err)
	}
	if err := service.Disable(ctx, "c62", "ghost"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Disable(missing) = %v", err)
	}

	// No store configured is a clear invalid request.
	if _, err := New(&fakeLookup{members: twoMembers()}, &fakeNodes{}, nil).List(ctx, "c62"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("List(no store) = %v", err)
	}
}

func TestDisableTearsEverythingDown(t *testing.T) {
	service, nodes, st := newTestService(twoMembers())
	ctx := context.Background()
	if _, err := service.Enable(ctx, "c62", Request{
		Name: "c62", VNI: 6200, Subnet: "198.51.100.0/24", GatewayMember: "node-a",
	}); err != nil {
		t.Fatalf("Enable() error = %v", err)
	}

	if err := service.Disable(ctx, "c62", "c62"); err != nil {
		t.Fatalf("Disable() error = %v", err)
	}

	nodeA := nodes.operators["node-a"]
	if len(nodeA.stopped) != 1 || nodeA.stopped[0] != "c62" {
		t.Fatalf("dhcp not stopped: %#v", nodeA.stopped)
	}
	if len(nodeA.natDel) != 1 || nodeA.natDel[0] != "c62" {
		t.Fatalf("nat not deleted: %#v", nodeA.natDel)
	}
	if _, err := st.GetOverlay(ctx, "c62", "c62"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("definition not deleted: %v", err)
	}
}

func TestGetAndList(t *testing.T) {
	service, _, _ := newTestService(twoMembers())
	ctx := context.Background()
	if _, err := service.Enable(ctx, "c62", Request{
		Name: "c62", VNI: 6200, Subnet: "198.51.100.0/24", GatewayMember: "node-a",
	}); err != nil {
		t.Fatalf("Enable() error = %v", err)
	}

	status, err := service.Get(ctx, "c62", "c62")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if len(status.Members) != 2 || len(status.Tunnels) != 1 {
		t.Fatalf("status = %#v", status)
	}
	if !status.Members[0].Gateway && !status.Members[1].Gateway {
		t.Fatalf("no gateway flagged: %#v", status.Members)
	}

	list, err := service.List(ctx, "c62")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list) != 1 || list[0].Name != "c62" {
		t.Fatalf("list = %#v", list)
	}
	if _, err := service.Get(ctx, "c62", "ghost"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(missing) = %v", err)
	}
}

func TestMemberAddressesUsesOverrides(t *testing.T) {
	service, _, _ := newTestService(twoMembers())
	members, err := service.memberAddresses(context.Background(), "c62", "node-a", map[string]string{
		"node-a": "198.51.100.1", "node-b": "198.51.100.2",
	}, "enp1s0")
	if err != nil {
		t.Fatalf("memberAddresses() error = %v", err)
	}
	if members[0].UnderlayIP != "198.51.100.1" || members[1].UnderlayIP != "198.51.100.2" {
		t.Fatalf("members = %#v", members)
	}
	if members[0].UnderlayNIC != "enp1s0" {
		t.Fatalf("nic = %q", members[0].UnderlayNIC)
	}
}

func TestClusterResolverReportsErrors(t *testing.T) {
	resolver := &ClusterResolver{Manager: failingManager{}}
	if _, err := resolver.Members(context.Background(), "c62"); err == nil {
		t.Fatal("expected error")
	}
}

type failingManager struct{}

func (failingManager) Connection(string) (*clusters.Connection, error) {
	return nil, errors.New("unknown cluster")
}
