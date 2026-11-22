// Package agentops adapts the agent gateway to the hostops.Operator interface.
package agentops

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/mickeyzzc/incus-fleet/internal/agentpb"
	"github.com/mickeyzzc/incus-fleet/internal/hostops"
	"github.com/mickeyzzc/incus-fleet/internal/nodenet"
	"github.com/mickeyzzc/incus-fleet/internal/store"
)

// Dispatcher is the agent task transport.
type Dispatcher interface {
	Dispatch(ctx context.Context, agentID string, typ agentpb.TaskType, payload []byte, timeout time.Duration) (*agentpb.TaskResult, error)
}

// Lookup resolves an enrolled agent by member.
type Lookup interface {
	GetAgentByMember(ctx context.Context, cluster, member string) (*store.Agent, error)
}

// Resolver resolves online agents to host operators.
type Resolver struct {
	dispatcher Dispatcher
	lookup     Lookup
	timeout    time.Duration
}

// New builds a resolver.
func New(dispatcher Dispatcher, lookup Lookup) *Resolver {
	return &Resolver{dispatcher: dispatcher, lookup: lookup, timeout: 30 * time.Second}
}

// Resolve returns an operator when an online agent exists for the member.
func (r *Resolver) Resolve(ctx context.Context, cluster, member string) (hostops.Operator, bool) {
	agent, err := r.lookup.GetAgentByMember(ctx, cluster, member)
	if err != nil || agent.Status != store.AgentOnline {
		return nil, false
	}
	return &Operator{
		dispatcher: r.dispatcher, timeout: r.timeout,
		agentID: agent.ID, cluster: cluster, member: member,
	}, true
}

// Operator executes host operations through one agent.
type Operator struct {
	dispatcher Dispatcher
	agentID    string
	cluster    string
	member     string
	timeout    time.Duration
}

// Snapshot returns the agent network snapshot.
func (o *Operator) Snapshot(ctx context.Context, wifiDevice string) (*nodenet.Snapshot, error) {
	result, err := o.dispatcher.Dispatch(ctx, o.agentID, agentpb.TaskType_TASK_TYPE_NODENET_SNAPSHOT, nil, 15*time.Second)
	if err != nil {
		return nil, err
	}
	if result.State != agentpb.TaskState_TASK_STATE_SUCCEEDED {
		return nil, fmt.Errorf("agentops: snapshot failed: %s", result.Error)
	}
	var snapshot agentpb.NodenetSnapshot
	if err := proto.Unmarshal(result.Payload, &snapshot); err != nil {
		return nil, fmt.Errorf("agentops: decode snapshot: %w", err)
	}
	return convertSnapshot(&snapshot, wifiDevice), nil
}

// ScanWiFi returns the agent Wi-Fi scan.
func (o *Operator) ScanWiFi(dev string) ([]nodenet.WiFiNetwork, error) {
	snapshot, err := o.Snapshot(context.Background(), dev)
	if err != nil {
		return nil, err
	}
	return snapshot.WifiScan, nil
}

// SetNICState changes one link state through the agent.
func (o *Operator) SetNICState(name string, up bool) error {
	return o.apply(context.Background(), &agentpb.NodenetApplySpec{
		Action: "link-state", Interface: name, Up: up,
	})
}

// ApplyAddress configures one address through the agent.
func (o *Operator) ApplyAddress(ctx context.Context, name string, req nodenet.AddressRequest) error {
	return o.apply(ctx, &agentpb.NodenetApplySpec{
		Action: "address", Interface: name, Dhcp: req.DHCP,
		Address: req.Address, Gateway: req.Gateway, Persist: req.Persist,
	})
}

// ListRoutes returns the agent routing table.
func (o *Operator) ListRoutes() ([]nodenet.Route, error) {
	snapshot, err := o.Snapshot(context.Background(), "")
	if err != nil {
		return nil, err
	}
	return snapshot.Routes, nil
}

// AddRoute adds one fleet route through the agent.
func (o *Operator) AddRoute(destination, gateway, device string) error {
	return o.apply(context.Background(), &agentpb.NodenetApplySpec{
		Action: "route-add", Destination: destination, Gateway: gateway, Device: device,
	})
}

// DeleteRoute removes one fleet route.
func (o *Operator) DeleteRoute(destination string) error {
	return o.apply(context.Background(), &agentpb.NodenetApplySpec{
		Action: "route-delete", Destination: destination,
	})
}

// ListFirewall returns the fleet nftables rules.
func (o *Operator) ListFirewall() ([]nodenet.FirewallRule, error) {
	snapshot, err := o.Snapshot(context.Background(), "")
	if err != nil {
		return nil, err
	}
	return snapshot.Firewall, nil
}

// AddFirewallRule adds one rule through the agent.
func (o *Operator) AddFirewallRule(req nodenet.FirewallRequest) error {
	//nolint:gosec // port is range-validated by the helper and the UI
	return o.apply(context.Background(), &agentpb.NodenetApplySpec{
		Action:         "firewall-add",
		Port:           int32(req.Port),
		Proto:          req.Proto,
		FirewallAction: req.Action,
		Comment:        req.Comment,
	})
}

// DeleteFirewallRule removes one rule by handle.
func (o *Operator) DeleteFirewallRule(handle string) error {
	return o.apply(context.Background(), &agentpb.NodenetApplySpec{
		Action: "firewall-delete", Handle: handle,
	})
}

// WifiConnect associates one Wi-Fi interface.
func (o *Operator) WifiConnect(dev, ssid, password string) error {
	return o.apply(context.Background(), &agentpb.NodenetApplySpec{
		Action: "wifi-connect", Interface: dev, Ssid: ssid, Password: password,
	})
}

// overlayActions lists the fleet-managed overlay host mutations. The center
// repeats this allowlist in the HTTP layer; keeping one exported slice makes
// the two sides impossible to drift apart.
var overlayActions = []string{
	"bridge-add", "bridge-del",
	"vxlan-add", "vxlan-del",
	"dhcp-config", "dhcp-start", "dhcp-stop",
	"nat-add", "nat-del",
}

// NodenetActions returns the allowlisted nodenet mutation actions.
func NodenetActions() []string {
	base := []string{"link-state", "address", "route-add", "route-delete", "firewall-add", "firewall-delete", "wifi-connect"}
	return append(base, overlayActions...)
}

// IsNodenetAction reports whether action is an allowlisted mutation.
func IsNodenetAction(action string) bool {
	for _, candidate := range NodenetActions() {
		if candidate == action {
			return true
		}
	}
	return false
}

// AddBridge creates the fleet bridge for a private overlay.
func (o *Operator) AddBridge(cfg nodenet.BridgeConfig) error {
	return o.apply(context.Background(), &agentpb.NodenetApplySpec{
		Action: "bridge-add", Interface: cfg.Name, Address: cfg.Address,
		Mtu: int32(cfg.MTU), //nolint:gosec // MTU is range-validated
	})
}

// DeleteBridge removes the fleet bridge.
func (o *Operator) DeleteBridge(name string) error {
	return o.apply(context.Background(), &agentpb.NodenetApplySpec{
		Action: "bridge-del", Interface: name,
	})
}

// AddVXLAN creates one point-to-point overlay tunnel.
func (o *Operator) AddVXLAN(link nodenet.VXLANLink) error {
	return o.apply(context.Background(), &agentpb.NodenetApplySpec{
		Action: "vxlan-add", Interface: link.Name, Bridge: link.Bridge,
		Vni: int32(link.VNI), LocalIp: link.LocalIP, RemoteIp: link.RemoteIP, //nolint:gosec // VNI is range-validated
		Device: link.Underlay, Mtu: int32(link.MTU), Port: int32(link.Port), //nolint:gosec // MTU/port are range-validated
	})
}

// DeleteVXLAN removes one overlay tunnel.
func (o *Operator) DeleteVXLAN(name string) error {
	return o.apply(context.Background(), &agentpb.NodenetApplySpec{
		Action: "vxlan-del", Interface: name,
	})
}

// WriteDHCPConfig writes the overlay dnsmasq config and unit.
func (o *Operator) WriteDHCPConfig(cfg nodenet.DHCPConfig) error {
	return o.apply(context.Background(), &agentpb.NodenetApplySpec{
		Action: "dhcp-config", Comment: cfg.Name, Interface: cfg.Interface,
		RangeStart: cfg.RangeStart, RangeEnd: cfg.RangeEnd, Gateway: cfg.Gateway,
		Netmask: cfg.Netmask, Dns: cfg.DNS,
	})
}

// StartDHCP starts the overlay DHCP unit.
func (o *Operator) StartDHCP(name string) error {
	return o.apply(context.Background(), &agentpb.NodenetApplySpec{
		Action: "dhcp-start", Comment: name,
	})
}

// StopDHCP stops and removes the overlay DHCP unit.
func (o *Operator) StopDHCP(name string) error {
	return o.apply(context.Background(), &agentpb.NodenetApplySpec{
		Action: "dhcp-stop", Comment: name,
	})
}

// AddNAT installs the overlay masquerade rule.
func (o *Operator) AddNAT(cfg nodenet.NATConfig) error {
	return o.apply(context.Background(), &agentpb.NodenetApplySpec{
		Action: "nat-add", Source: cfg.Source, OutDevice: cfg.OutDevice, Comment: cfg.Comment,
	})
}

// DeleteNAT removes the overlay NAT table.
func (o *Operator) DeleteNAT(name string) error {
	return o.apply(context.Background(), &agentpb.NodenetApplySpec{
		Action: "nat-del", Comment: name,
	})
}

func (o *Operator) apply(ctx context.Context, spec *agentpb.NodenetApplySpec) error {
	payload, err := proto.Marshal(spec)
	if err != nil {
		return err
	}
	result, err := o.dispatcher.Dispatch(ctx, o.agentID, agentpb.TaskType_TASK_TYPE_NODENET_APPLY, payload, o.timeout)
	if err != nil {
		return err
	}
	if result.State != agentpb.TaskState_TASK_STATE_SUCCEEDED {
		return fmt.Errorf("agentops: %s failed: %s", spec.Action, result.Error)
	}
	var applied agentpb.NodenetApplyResult
	if err := proto.Unmarshal(result.Payload, &applied); err != nil {
		return fmt.Errorf("agentops: decode apply result: %w", err)
	}
	if !applied.Ok {
		return fmt.Errorf("agentops: %s failed: %s", spec.Action, applied.Error)
	}
	return nil
}

func convertSnapshot(snapshot *agentpb.NodenetSnapshot, wifiDevice string) *nodenet.Snapshot {
	out := &nodenet.Snapshot{
		Host: snapshot.Host, Backend: "agent", Capabilities: append([]string(nil), snapshot.Capabilities...),
		WifiDevice: snapshot.WifiDevice,
	}
	if wifiDevice != "" {
		out.WifiDevice = wifiDevice
	}
	for _, nic := range snapshot.Nics {
		item := nodenet.NIC{
			Name: nic.Name, State: nic.State, Kind: nic.Kind, MAC: nic.Mac, MTU: int(nic.Mtu),
		}
		for _, addr := range nic.Addresses {
			item.Addresses = append(item.Addresses, nodenet.Address{
				Family: addr.Family, Address: addr.Address, Prefixlen: int(addr.Prefixlen),
			})
		}
		if nic.Wifi != nil {
			item.WiFi = &nodenet.WiFiLink{
				Connected: nic.Wifi.Connected, SSID: nic.Wifi.Ssid,
				Signal: int(nic.Wifi.Signal), Frequency: int(nic.Wifi.Frequency),
			}
		}
		out.NICs = append(out.NICs, item)
	}
	for _, route := range snapshot.Routes {
		out.Routes = append(out.Routes, nodenet.Route{
			Destination: route.Destination, Gateway: route.Gateway,
			Device: route.Device, Metric: route.Metric,
		})
	}
	for _, rule := range snapshot.Firewall {
		out.Firewall = append(out.Firewall, nodenet.FirewallRule{Handle: rule.Handle, Rule: rule.Rule})
	}
	for _, network := range snapshot.WifiScan {
		out.WifiScan = append(out.WifiScan, nodenet.WiFiNetwork{
			SSID: network.Ssid, Signal: int(network.Signal),
			Frequency: int(network.Frequency), Secure: network.Secure,
		})
	}
	return out
}
