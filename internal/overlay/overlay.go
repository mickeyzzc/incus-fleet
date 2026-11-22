// Package overlay orchestrates the fleet-managed private network that connects
// instances across the members of one Incus cluster. The design is deliberately
// host-side: Incus requires identical network configuration on every cluster
// member, so a managed bridge has the same subnet (and gateway address) on all
// members. Stretching it would duplicate the gateway IP and run competing
// DHCP servers. Instead fleet owns a bridge per host, joins the members with
// point-to-point VXLAN tunnels, and runs DHCP from exactly one gateway member.
//
// Instances still attach with a legacy `nictype=bridged` NIC pointing at the
// fleet bridge, so nothing inside a guest changes: it keeps using DHCP from
// its usual default gateway.
package overlay

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"

	"github.com/mickeyzzc/incus-fleet/internal/clusters"
	"github.com/mickeyzzc/incus-fleet/internal/hostops"
	"github.com/mickeyzzc/incus-fleet/internal/nodenet"
	"github.com/mickeyzzc/incus-fleet/internal/remote"
	"github.com/mickeyzzc/incus-fleet/internal/store"
)

// ErrInvalidRequest marks a rejected overlay definition.
var ErrInvalidRequest = errors.New("overlay: invalid request")

// ErrNotFound marks an overlay that is not configured.
var ErrNotFound = errors.New("overlay: not found")

// DefaultBridge is the host bridge fleet creates for an overlay.
const DefaultBridge = "fleetbr0"

// DefaultMTU leaves room for the VXLAN header on a 1500-byte underlay.
const DefaultMTU = 1450

// MinVNI and MaxVNI bound the VXLAN network identifier.
const (
	MinVNI = 1
	MaxVNI = 16777215
)

// ClusterLookup resolves the live member list of one cluster. Keeping this
// narrow makes the orchestrator testable without an Incus server.
type ClusterLookup interface {
	Members(ctx context.Context, cluster string) ([]clusters.Member, error)
}

// NodeResolver picks a host operator for one member.
type NodeResolver interface {
	Resolve(ctx context.Context, cluster, member string, kind hostops.Kind) (hostops.Operator, hostops.Backend, func(), error)
}

// Store persists overlay definitions.
type Store interface {
	SaveOverlay(ctx context.Context, overlay *store.OverlayNetwork) error
	GetOverlay(ctx context.Context, cluster, name string) (*store.OverlayNetwork, error)
	ListOverlays(ctx context.Context, cluster string) ([]*store.OverlayNetwork, error)
	DeleteOverlay(ctx context.Context, cluster, name string) error
}

// MemberStatus is the observed state of one member's overlay participation.
type MemberStatus struct {
	Member     string `json:"member"`
	UnderlayIP string `json:"underlay_ip,omitempty"`
	BridgeIP   string `json:"bridge_ip,omitempty"`
	Links      int    `json:"links"`
	Gateway    bool   `json:"gateway"`
	Error      string `json:"error,omitempty"`
}

// Status is the overlay as reported to the API.
type Status struct {
	Cluster       string         `json:"cluster"`
	Name          string         `json:"name"`
	Bridge        string         `json:"bridge"`
	VNI           int            `json:"vni"`
	Subnet        string         `json:"subnet"`
	GatewayMember string         `json:"gateway_member"`
	MTU           int            `json:"mtu"`
	DHCPStart     string         `json:"dhcp_start,omitempty"`
	DHCPEnd       string         `json:"dhcp_end,omitempty"`
	Enabled       bool           `json:"enabled"`
	Members       []MemberStatus `json:"members"`
	Tunnels       [][2]string    `json:"tunnels"`
	Steps         []string       `json:"steps,omitempty"`
	Detail        map[string]any `json:"detail,omitempty"`
}

// Request is one create or update of an overlay.
type Request struct {
	Name          string            `json:"name"`
	Bridge        string            `json:"bridge,omitempty"`
	VNI           int               `json:"vni,omitempty"`
	Subnet        string            `json:"subnet,omitempty"`
	GatewayMember string            `json:"gateway_member,omitempty"`
	MTU           int               `json:"mtu,omitempty"`
	DHCPStart     string            `json:"dhcp_start,omitempty"`
	DHCPEnd       string            `json:"dhcp_end,omitempty"`
	UnderlayNIC   string            `json:"underlay_nic,omitempty"`
	UnderlayIPs   map[string]string `json:"underlay_ips,omitempty"`
}

// Service orchestrates overlay networks.
type Service struct {
	clusters ClusterLookup
	nodes    NodeResolver
	store    Store
}

// New builds an overlay service. The store may be nil for a stateless run.
func New(clustersLookup ClusterLookup, nodes NodeResolver, st Store) *Service {
	return &Service{clusters: clustersLookup, nodes: nodes, store: st}
}

// normalize fills defaults and validates the request. It returns the parsed
// subnet so callers do not parse twice.
func normalize(req Request, previous *store.OverlayNetwork) (*store.OverlayNetwork, *net.IPNet, error) {
	overlay := &store.OverlayNetwork{
		Name:          strings.TrimSpace(req.Name),
		Bridge:        strings.TrimSpace(req.Bridge),
		VNI:           req.VNI,
		Subnet:        strings.TrimSpace(req.Subnet),
		GatewayMember: strings.TrimSpace(req.GatewayMember),
		MTU:           req.MTU,
		DHCPStart:     strings.TrimSpace(req.DHCPStart),
		DHCPEnd:       strings.TrimSpace(req.DHCPEnd),
		Enabled:       true,
	}

	if previous != nil {
		if overlay.Name == "" {
			overlay.Name = previous.Name
		}
		if overlay.Bridge == "" {
			overlay.Bridge = previous.Bridge
		}
		if overlay.VNI == 0 {
			overlay.VNI = previous.VNI
		}
		if overlay.Subnet == "" {
			overlay.Subnet = previous.Subnet
		}
		if overlay.GatewayMember == "" {
			overlay.GatewayMember = previous.GatewayMember
		}
		if overlay.MTU == 0 {
			overlay.MTU = previous.MTU
		}
		if overlay.DHCPStart == "" {
			overlay.DHCPStart = previous.DHCPStart
		}
		if overlay.DHCPEnd == "" {
			overlay.DHCPEnd = previous.DHCPEnd
		}
	}

	if overlay.Bridge == "" {
		overlay.Bridge = DefaultBridge
	}
	if overlay.MTU == 0 {
		overlay.MTU = DefaultMTU
	}

	if err := nodenet.ValidateOverlayName(overlay.Name); err != nil {
		return nil, nil, fmt.Errorf("%w: %s", ErrInvalidRequest, err)
	}
	if err := nodenet.ValidateName(overlay.Bridge); err != nil {
		return nil, nil, fmt.Errorf("%w: %s", ErrInvalidRequest, err)
	}
	if overlay.VNI < MinVNI || overlay.VNI > MaxVNI {
		return nil, nil, fmt.Errorf("%w: VNI %d is out of range", ErrInvalidRequest, overlay.VNI)
	}
	if overlay.MTU < 576 || overlay.MTU > 9000 {
		return nil, nil, fmt.Errorf("%w: MTU %d is out of range", ErrInvalidRequest, overlay.MTU)
	}
	if overlay.GatewayMember == "" {
		return nil, nil, fmt.Errorf("%w: gateway member is required", ErrInvalidRequest)
	}

	ip, network, err := net.ParseCIDR(overlay.Subnet)
	if err != nil || network == nil {
		return nil, nil, fmt.Errorf("%w: subnet %q is invalid", ErrInvalidRequest, overlay.Subnet)
	}
	if ip.To4() == nil {
		return nil, nil, fmt.Errorf("%w: only IPv4 overlay subnets are supported", ErrInvalidRequest)
	}

	gateway := networkGateway(network)
	if overlay.DHCPStart == "" {
		overlay.DHCPStart = firstHost(network, 10)
	}
	if overlay.DHCPEnd == "" {
		overlay.DHCPEnd = lastHost(network, 10)
	}
	if err := validateDHCPRange(overlay, network, gateway); err != nil {
		return nil, nil, err
	}

	return overlay, network, nil
}

// validateDHCPRange checks that the pool stays inside the subnet and does not
// include the gateway.
func validateDHCPRange(overlay *store.OverlayNetwork, network *net.IPNet, gateway net.IP) error {
	start := net.ParseIP(overlay.DHCPStart)
	end := net.ParseIP(overlay.DHCPEnd)
	if start == nil || end == nil {
		return fmt.Errorf("%w: DHCP range %q-%q is invalid", ErrInvalidRequest, overlay.DHCPStart, overlay.DHCPEnd)
	}
	if start.To4() == nil || end.To4() == nil {
		return fmt.Errorf("%w: DHCP range must be IPv4", ErrInvalidRequest)
	}
	if !network.Contains(start) || !network.Contains(end) {
		return fmt.Errorf("%w: DHCP range %s-%s is outside %s", ErrInvalidRequest, overlay.DHCPStart, overlay.DHCPEnd, overlay.Subnet)
	}
	if compareIP(start, end) > 0 {
		return fmt.Errorf("%w: DHCP range start %s is after end %s", ErrInvalidRequest, overlay.DHCPStart, overlay.DHCPEnd)
	}
	if containsIP(start, end, gateway) {
		return fmt.Errorf("%w: DHCP range must not include the gateway %s", ErrInvalidRequest, gateway)
	}
	return nil
}

// networkGateway returns the conventional first host of a subnet.
func networkGateway(network *net.IPNet) net.IP {
	base := network.IP.To4()
	gateway := make(net.IP, len(base))
	copy(gateway, base)
	gateway[3]++
	return gateway
}

// firstHost returns the subnet base plus offset, bounded to usable hosts.
func firstHost(network *net.IPNet, offset int) string {
	base := ipToUint32(network.IP)
	value := uint32ToIP(base + uint32(offset)) //nolint:gosec // offset is bounded by the member count
	if !network.Contains(value) {
		return networkGateway(network).String()
	}
	return value.String()
}

// lastHost returns the last usable host minus offset.
func lastHost(network *net.IPNet, offset int) string {
	base := ipToUint32(network.IP)
	ones, _ := network.Mask.Size()
	size := uint32(1) << (32 - ones)
	// Broadcast is base+size-1; step back one more for the last host.
	last := base + size - 2 - uint32(offset) //nolint:gosec // offset is small and bounded
	value := uint32ToIP(last)
	if !network.Contains(value) {
		return networkGateway(network).String()
	}
	return value.String()
}

// ipToUint32 converts an IPv4 address to its big-endian integer value.
func ipToUint32(ip net.IP) uint32 {
	v4 := ip.To4()
	if v4 == nil {
		return 0
	}
	return uint32(v4[0])<<24 | uint32(v4[1])<<16 | uint32(v4[2])<<8 | uint32(v4[3])
}

// uint32ToIP converts a big-endian integer back into an IPv4 address.
func uint32ToIP(value uint32) net.IP {
	return net.IPv4(byte(value>>24), byte(value>>16), byte(value>>8), byte(value)).To4() //nolint:gosec // masked by construction
}

// containsIP reports whether target falls inside [start, end].
func containsIP(start, end, target net.IP) bool {
	return compareIP(start, target) <= 0 && compareIP(target, end) <= 0
}

// compareIP orders two IPv4 addresses.
func compareIP(a, b net.IP) int {
	a4 := a.To4()
	b4 := b.To4()
	for i := 0; i < 4; i++ {
		if a4[i] != b4[i] {
			if a4[i] < b4[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

// Tunnel is one undirected VXLAN link between two members.
type Tunnel struct {
	Left  string
	Right string
}

// planTunnels returns every unordered member pair. Two members produce one
// tunnel; N members produce N*(N-1)/2, which is the full mesh a bridge needs
// to behave like one segment.
func planTunnels(members []string) []Tunnel {
	sorted := append([]string(nil), members...)
	sort.Strings(sorted)

	tunnels := make([]Tunnel, 0, len(sorted)*(len(sorted)-1)/2)
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			tunnels = append(tunnels, Tunnel{Left: sorted[i], Right: sorted[j]})
		}
	}
	return tunnels
}

// tunnelName derives a stable, length-bounded interface name for one link.
// The pair is sorted so both endpoints compute the same name regardless of
// which side calls first. Linux limits interface names to 15 characters.
func tunnelName(overlayName, left, right string) string {
	low, high := left, right
	if high < low {
		low, high = high, low
	}
	digest := sha256.Sum256([]byte(overlayName + "/" + low + "/" + high))
	return "vx" + hex.EncodeToString(digest[:])[:8]
}

// Enable validates, plans, and applies a new overlay across every member.
func (s *Service) Enable(ctx context.Context, clusterName string, req Request) (*Status, error) {
	if s.store == nil {
		return nil, fmt.Errorf("%w: overlay storage is not configured", ErrInvalidRequest)
	}
	if _, err := s.store.GetOverlay(ctx, clusterName, strings.TrimSpace(req.Name)); err == nil {
		return nil, fmt.Errorf("%w: overlay %q already exists", ErrInvalidRequest, req.Name)
	} else if !errors.Is(err, ErrNotFound) && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}

	overlay, network, err := normalize(req, nil)
	if err != nil {
		return nil, err
	}
	overlay.Cluster = clusterName

	members, err := s.memberAddresses(ctx, clusterName, overlay.GatewayMember, req.UnderlayIPs, req.UnderlayNIC)
	if err != nil {
		return nil, err
	}
	if len(members) < 2 {
		return nil, fmt.Errorf("%w: a private overlay needs at least two members", ErrInvalidRequest)
	}
	if len(members) > 254 {
		return nil, fmt.Errorf("%w: too many members for one /24 overlay", ErrInvalidRequest)
	}
	assignBridgeIPs(overlay, network, members)

	status, err := s.apply(ctx, clusterName, overlay, members, req.UnderlayNIC)
	if err != nil {
		return nil, err
	}

	if err := s.store.SaveOverlay(ctx, overlay); err != nil {
		return nil, err
	}

	return status, nil
}

// Reconcile re-applies the stored overlay against the current member list.
func (s *Service) Reconcile(ctx context.Context, clusterName, name string) (*Status, error) {
	if s.store == nil {
		return nil, fmt.Errorf("%w: overlay storage is not configured", ErrInvalidRequest)
	}
	stored, err := s.store.GetOverlay(ctx, clusterName, name)
	if err != nil {
		return nil, mapStoreError(err)
	}

	underlay := map[string]string{}
	for _, member := range stored.Members {
		underlay[member.Member] = member.UnderlayIP
	}

	overlay := *stored
	overlay.Members = nil
	members, err := s.memberAddresses(ctx, clusterName, overlay.GatewayMember, underlay, "")
	if err != nil {
		return nil, err
	}
	if len(members) < 2 {
		return nil, fmt.Errorf("%w: a private overlay needs at least two members", ErrInvalidRequest)
	}
	_, network, err := net.ParseCIDR(overlay.Subnet)
	if err != nil {
		return nil, fmt.Errorf("%w: stored subnet %q is invalid", ErrInvalidRequest, overlay.Subnet)
	}
	assignBridgeIPs(&overlay, network, members)

	status, err := s.apply(ctx, clusterName, &overlay, members, "")
	if err != nil {
		return nil, err
	}
	overlay.Enabled = true
	if err := s.store.SaveOverlay(ctx, &overlay); err != nil {
		return nil, err
	}

	return status, nil
}

// Disable removes the overlay from every member and deletes its definition.
func (s *Service) Disable(ctx context.Context, clusterName, name string) error {
	if s.store == nil {
		return fmt.Errorf("%w: overlay storage is not configured", ErrInvalidRequest)
	}
	stored, err := s.store.GetOverlay(ctx, clusterName, name)
	if err != nil {
		return mapStoreError(err)
	}

	members := make([]string, 0, len(stored.Members))
	for _, member := range stored.Members {
		members = append(members, member.Member)
	}

	if err := s.teardown(ctx, clusterName, stored, members); err != nil {
		return err
	}

	return mapStoreError(s.store.DeleteOverlay(ctx, clusterName, name))
}

// Get returns the stored overlay definition.
func (s *Service) Get(ctx context.Context, clusterName, name string) (*Status, error) {
	if s.store == nil {
		return nil, fmt.Errorf("%w: overlay storage is not configured", ErrInvalidRequest)
	}
	stored, err := s.store.GetOverlay(ctx, clusterName, name)
	if err != nil {
		return nil, mapStoreError(err)
	}

	status := statusFromStore(stored)
	for _, member := range stored.Members {
		status.Members = append(status.Members, MemberStatus{
			Member:     member.Member,
			UnderlayIP: member.UnderlayIP,
			BridgeIP:   member.BridgeIP,
			Gateway:    member.Member == stored.GatewayMember,
		})
	}

	return status, nil
}

// List returns every overlay of a cluster.
func (s *Service) List(ctx context.Context, clusterName string) ([]*Status, error) {
	if s.store == nil {
		return nil, fmt.Errorf("%w: overlay storage is not configured", ErrInvalidRequest)
	}
	stored, err := s.store.ListOverlays(ctx, clusterName)
	if err != nil {
		return nil, err
	}

	out := make([]*Status, 0, len(stored))
	for _, item := range stored {
		out = append(out, statusFromStore(item))
	}

	return out, nil
}

func statusFromStore(stored *store.OverlayNetwork) *Status {
	status := &Status{
		Cluster: stored.Cluster, Name: stored.Name, Bridge: stored.Bridge,
		VNI: stored.VNI, Subnet: stored.Subnet, GatewayMember: stored.GatewayMember,
		MTU: stored.MTU, DHCPStart: stored.DHCPStart, DHCPEnd: stored.DHCPEnd,
		Enabled: stored.Enabled, Members: []MemberStatus{}, Tunnels: [][2]string{},
	}
	members := make([]string, 0, len(stored.Members))
	for _, member := range stored.Members {
		members = append(members, member.Member)
	}
	for _, tunnel := range planTunnels(members) {
		status.Tunnels = append(status.Tunnels, [2]string{tunnel.Left, tunnel.Right})
	}

	return status
}

// memberAddresses builds the member list with bridge and underlay addresses.
// Underlay addresses come from explicit overrides, then the stored record,
// then the member URL reported by Incus.
func (s *Service) memberAddresses(ctx context.Context, clusterName, gatewayMember string, overrides map[string]string, underlayNIC string) ([]store.OverlayMember, error) {
	raw, err := s.clusters.Members(ctx, clusterName)
	if err != nil {
		return nil, err
	}

	members := make([]store.OverlayMember, 0, len(raw))
	seenGateway := false
	for _, member := range raw {
		underlay := strings.TrimSpace(overrides[member.Name])
		if underlay == "" {
			host, err := remote.HostFromURL(member.URL)
			if err != nil {
				return nil, fmt.Errorf("%w: member %s has no usable underlay address: %v", ErrInvalidRequest, member.Name, err)
			}
			underlay = host
		}
		if net.ParseIP(underlay) == nil {
			return nil, fmt.Errorf("%w: member %s underlay %q is not an IP address", ErrInvalidRequest, member.Name, underlay)
		}
		if member.Name == gatewayMember {
			seenGateway = true
		}
		members = append(members, store.OverlayMember{
			Member: member.Name, UnderlayIP: underlay, UnderlayNIC: underlayNIC,
		})
	}
	if !seenGateway {
		return nil, fmt.Errorf("%w: gateway member %q is not a cluster member", ErrInvalidRequest, gatewayMember)
	}

	sort.Slice(members, func(i, j int) bool { return members[i].Member < members[j].Member })
	return members, nil
}

// assignBridgeIPs gives each member a distinct host address on the overlay
// bridge so no two members claim the same gateway address.
func assignBridgeIPs(overlay *store.OverlayNetwork, network *net.IPNet, members []store.OverlayMember) {
	ones, _ := network.Mask.Size()
	for i := range members {
		host := firstHost(network, i+1)
		members[i].BridgeIP = host + "/" + strconv.Itoa(ones)
	}
	overlay.Members = members
}

// apply drives the per-member and per-tunnel host operations.
func (s *Service) apply(ctx context.Context, clusterName string, overlay *store.OverlayNetwork, members []store.OverlayMember, underlayNIC string) (*Status, error) {
	status := statusFromStore(overlay)
	status.Steps = []string{}

	byName := map[string]store.OverlayMember{}
	for _, member := range members {
		byName[member.Member] = member
	}

	// Each member hosts the bridge, its host address, and every tunnel that
	// terminates on it.
	for _, tunnel := range planTunnels(memberNames(members)) {
		for _, endpoint := range []string{tunnel.Left, tunnel.Right} {
			peer := tunnel.Right
			if endpoint == tunnel.Right {
				peer = tunnel.Left
			}
			local := byName[endpoint]
			remote := byName[peer]
			if err := s.applyMember(ctx, clusterName, overlay, local, remote, underlayNIC); err != nil {
				return nil, err
			}
			status.Steps = append(status.Steps, fmt.Sprintf("%s: vxlan %s", endpoint, tunnelName(overlay.Name, tunnel.Left, tunnel.Right)))
		}
	}

	gateway := byName[overlay.GatewayMember]
	if err := s.applyGateway(ctx, clusterName, overlay, gateway); err != nil {
		return nil, err
	}
	status.Steps = append(status.Steps, fmt.Sprintf("%s: dhcp %s-%s", gateway.Member, overlay.DHCPStart, overlay.DHCPEnd))

	status.Members = memberStatuses(members, overlay.GatewayMember)
	return status, nil
}

// applyMember creates the bridge, host address, and one tunnel endpoint on a
// single member.
func (s *Service) applyMember(ctx context.Context, clusterName string, overlay *store.OverlayNetwork, local, remote store.OverlayMember, underlayNIC string) error {
	operator, _, closeNode, err := s.nodes.Resolve(ctx, clusterName, local.Member, hostops.Write)
	if err != nil {
		return err
	}
	defer closeNode()

	cfg := nodenet.BridgeConfig{Name: overlay.Bridge, MTU: overlay.MTU, Address: local.BridgeIP}
	if err := operator.AddBridge(cfg); err != nil {
		return memberError(local.Member, "add bridge", err)
	}
	link := nodenet.VXLANLink{
		Name: tunnelName(overlay.Name, local.Member, remote.Member), Bridge: overlay.Bridge,
		VNI: overlay.VNI, LocalIP: local.UnderlayIP, RemoteIP: remote.UnderlayIP,
		Underlay: underlayNIC, MTU: overlay.MTU,
	}
	if err := operator.AddVXLAN(link); err != nil {
		return memberError(local.Member, "add vxlan", err)
	}

	return nil
}

// applyGateway configures DHCP and NAT on the gateway member.
func (s *Service) applyGateway(ctx context.Context, clusterName string, overlay *store.OverlayNetwork, gateway store.OverlayMember) error {
	operator, _, closeNode, err := s.nodes.Resolve(ctx, clusterName, gateway.Member, hostops.Write)
	if err != nil {
		return err
	}
	defer closeNode()

	gatewayIP := strings.SplitN(gateway.BridgeIP, "/", 2)[0]
	_, network, err := net.ParseCIDR(overlay.Subnet)
	if err != nil {
		return fmt.Errorf("%w: subnet %q is invalid", ErrInvalidRequest, overlay.Subnet)
	}
	mask := net.IP(network.Mask).String()

	cfg := nodenet.DHCPConfig{
		Name: overlay.Name, Interface: overlay.Bridge, RangeStart: overlay.DHCPStart,
		RangeEnd: overlay.DHCPEnd, Gateway: gatewayIP, Netmask: mask,
	}
	if err := operator.WriteDHCPConfig(cfg); err != nil {
		return memberError(gateway.Member, "write dhcp", err)
	}
	if err := operator.StartDHCP(overlay.Name); err != nil {
		return memberError(gateway.Member, "start dhcp", err)
	}
	if err := operator.AddNAT(nodenet.NATConfig{Source: overlay.Subnet, Comment: overlay.Name}); err != nil {
		return memberError(gateway.Member, "add nat", err)
	}

	return nil
}

// teardown removes tunnels, DHCP, NAT, and bridges from the given members.
func (s *Service) teardown(ctx context.Context, clusterName string, overlay *store.OverlayNetwork, members []string) error {
	sorted := append([]string(nil), members...)
	sort.Strings(sorted)

	for _, tunnel := range planTunnels(sorted) {
		for _, endpoint := range []string{tunnel.Left, tunnel.Right} {
			operator, _, closeNode, err := s.nodes.Resolve(ctx, clusterName, endpoint, hostops.Write)
			if err != nil {
				if errors.Is(err, hostops.ErrAgentUnavailable) {
					continue
				}
				return err
			}
			if err := operator.DeleteVXLAN(tunnelName(overlay.Name, tunnel.Left, tunnel.Right)); err != nil {
				closeNode()
				return memberError(endpoint, "delete vxlan", err)
			}
			closeNode()
		}
	}

	// The gateway cleanup must succeed before the bridge goes away.
	if operator, _, closeNode, err := s.nodes.Resolve(ctx, clusterName, overlay.GatewayMember, hostops.Write); err == nil {
		if err := operator.StopDHCP(overlay.Name); err != nil {
			closeNode()
			return memberError(overlay.GatewayMember, "stop dhcp", err)
		}
		if err := operator.DeleteNAT(overlay.Name); err != nil {
			closeNode()
			return memberError(overlay.GatewayMember, "delete nat", err)
		}
		closeNode()
	} else if !errors.Is(err, hostops.ErrAgentUnavailable) {
		return err
	}

	for _, member := range sorted {
		operator, _, closeNode, err := s.nodes.Resolve(ctx, clusterName, member, hostops.Write)
		if err != nil {
			if errors.Is(err, hostops.ErrAgentUnavailable) {
				continue
			}
			return err
		}
		err = operator.DeleteBridge(overlay.Bridge)
		closeNode()
		if err != nil {
			return memberError(member, "delete bridge", err)
		}
	}

	return nil
}

func memberNames(members []store.OverlayMember) []string {
	out := make([]string, 0, len(members))
	for _, member := range members {
		out = append(out, member.Member)
	}
	return out
}

func memberStatuses(members []store.OverlayMember, gateway string) []MemberStatus {
	out := make([]MemberStatus, 0, len(members))
	for _, member := range members {
		out = append(out, MemberStatus{
			Member: member.Member, UnderlayIP: member.UnderlayIP,
			BridgeIP: member.BridgeIP, Gateway: member.Member == gateway,
		})
	}
	return out
}

// memberError annotates an operation failure with the member name.
func memberError(member, operation string, err error) error {
	return fmt.Errorf("overlay: %s: %s: %w", member, operation, err)
}

// mapStoreError turns a missing store row into the overlay sentinel.
func mapStoreError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("%w: %s", ErrNotFound, err)
	}
	return err
}
