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
	return o.apply(context.Background(), &agentpb.NodenetApplySpec{
		Action: "firewall-add", Port: int32(req.Port), //nolint:gosec // validated by the helper and UI range Proto: req.Proto,
		FirewallAction: req.Action, Comment: req.Comment,
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
