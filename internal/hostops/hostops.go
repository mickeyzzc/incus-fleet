// Package hostops defines the typed host operations shared by the local,
// SSH, and future agent backends. Callers resolve one operator per cluster
// member and never choose a transport themselves.
package hostops

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/mickeyzzc/incus-fleet/internal/nodenet"
)

// Backend identifies how a host operation reached its target.
type Backend string

const (
	// BackendLocal runs on the fleet app host.
	BackendLocal Backend = "local"
	// BackendSSH runs on a remote member over SSH.
	BackendSSH Backend = "ssh"
	// BackendAgent runs through the host agent.
	BackendAgent Backend = "agent"
)

// Kind separates read-only operations from mutating operations. Mutating
// operations must not silently fall back to SSH unless explicitly enabled.
type Kind int

const (
	// Read is a read-only host operation.
	Read Kind = iota
	// Write is a mutating host operation.
	Write
)

// Mode selects the preferred host operation backend.
type Mode string

const (
	// ModeAuto prefers an online agent, then local, then SSH.
	ModeAuto Mode = "auto"
	// ModeAgent requires the agent backend.
	ModeAgent Mode = "agent"
	// ModeSSH requires the SSH backend for remote members.
	ModeSSH Mode = "ssh"
	// ModeLocal only permits the fleet app host.
	ModeLocal Mode = "local"
)

// ParseMode validates a configured host operation mode.
func ParseMode(value string) (Mode, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", string(ModeAuto):
		return ModeAuto, nil
	case string(ModeAgent):
		return ModeAgent, nil
	case string(ModeSSH):
		return ModeSSH, nil
	case string(ModeLocal):
		return ModeLocal, nil
	default:
		return "", fmt.Errorf("hostops: invalid mode %q", value)
	}
}

// String returns the stable mode name.
func (m Mode) String() string { return string(m) }

// ErrAgentUnavailable marks an operation that needs an agent but has none.
var ErrAgentUnavailable = errors.New("hostops: agent backend is unavailable")

// ErrSSHWriteDisabled marks a write rejected because SSH write fallback is off.
var ErrSSHWriteDisabled = errors.New("hostops: ssh write fallback is disabled")

// Operator is the host operation surface exposed by every backend.
type Operator interface {
	Snapshot(ctx context.Context, wifiDevice string) (*nodenet.Snapshot, error)
	ScanWiFi(dev string) ([]nodenet.WiFiNetwork, error)
	SetNICState(name string, up bool) error
	ApplyAddress(ctx context.Context, name string, req nodenet.AddressRequest) error
	ListRoutes() ([]nodenet.Route, error)
	AddRoute(destination, gateway, device string) error
	DeleteRoute(destination string) error
	ListFirewall() ([]nodenet.FirewallRule, error)
	AddFirewallRule(req nodenet.FirewallRequest) error
	DeleteFirewallRule(handle string) error
	WifiConnect(dev, ssid, password string) error

	// Overlay network operations drive the fleet-managed bridge, VXLAN links,
	// DHCP instance, and NAT rules that make up a cluster private network.
	AddBridge(cfg nodenet.BridgeConfig) error
	DeleteBridge(name string) error
	AddVXLAN(link nodenet.VXLANLink) error
	DeleteVXLAN(name string) error
	WriteDHCPConfig(cfg nodenet.DHCPConfig) error
	StartDHCP(name string) error
	StopDHCP(name string) error
	AddNAT(cfg nodenet.NATConfig) error
	DeleteNAT(name string) error
}

// Resolver picks one operator and backend for a cluster member.
type Resolver interface {
	Resolve(ctx context.Context, cluster, member string, kind Kind) (Operator, Backend, func(), error)
}
