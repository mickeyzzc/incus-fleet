package nodenet

import (
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// BridgeConfig describes a fleet-managed host bridge used by a private
// overlay network. Fleet owns the bridge outright, so it does not collide
// with the cluster-wide managed networks that Incus replicates on every
// member.
type BridgeConfig struct {
	Name    string `json:"name"`
	MTU     int    `json:"mtu,omitempty"`
	Address string `json:"address,omitempty"` // optional CIDR for the host side
}

// VXLANLink describes one point-to-point VXLAN interface joining a fleet
// bridge. local/remote are the underlay addresses of the two members; the
// VNI and UDP port are shared by every member of one overlay.
type VXLANLink struct {
	Name     string `json:"name"`
	Bridge   string `json:"bridge"`
	VNI      int    `json:"vni"`
	LocalIP  string `json:"local_ip"`
	RemoteIP string `json:"remote_ip"`
	Underlay string `json:"underlay,omitempty"` // parent device; empty lets the kernel route
	Port     int    `json:"port"`               // UDP destination port, 0 uses IANA 4789
	MTU      int    `json:"mtu,omitempty"`
}

// DHCPConfig describes the dnsmasq instance that serves one overlay from its
// gateway member. The service is managed through a generated systemd unit so
// start/stop and reboot survival follow the same path.
type DHCPConfig struct {
	Name       string `json:"name"` // overlay identifier; becomes the unit name
	Interface  string `json:"interface"`
	RangeStart string `json:"range_start"`
	RangeEnd   string `json:"range_end"`
	Gateway    string `json:"gateway"`
	Netmask    string `json:"netmask"`
	DNS        string `json:"dns,omitempty"`
}

// NATConfig describes an outbound masquerade rule for an overlay subnet.
type NATConfig struct {
	Source    string `json:"source"` // CIDR to masquerade
	OutDevice string `json:"out_device,omitempty"`
	Comment   string `json:"comment"` // fleet-owned handle, e.g. overlay name
}

const defaultVXLANPort = 4789

// ValidateBridge checks a bridge configuration before any command runs.
func ValidateBridge(cfg BridgeConfig) error {
	if err := ValidateName(cfg.Name); err != nil {
		return err
	}
	if cfg.MTU != 0 && (cfg.MTU < 576 || cfg.MTU > 9000) {
		return fmt.Errorf("%w: bridge MTU %d is out of range", ErrInvalidRequest, cfg.MTU)
	}
	if cfg.Address != "" {
		ip, _, err := net.ParseCIDR(cfg.Address)
		if err != nil || ip == nil {
			return fmt.Errorf("%w: bridge address %q is invalid", ErrInvalidRequest, cfg.Address)
		}
	}
	return nil
}

// ValidateVXLAN checks a VXLAN link before any command runs.
func ValidateVXLAN(link VXLANLink) error {
	if err := ValidateName(link.Name); err != nil {
		return err
	}
	if err := ValidateName(link.Bridge); err != nil {
		return err
	}
	if link.VNI < 1 || link.VNI > 16777215 {
		return fmt.Errorf("%w: VXLAN VNI %d is out of range", ErrInvalidRequest, link.VNI)
	}
	if ip := net.ParseIP(link.LocalIP); ip == nil {
		return fmt.Errorf("%w: VXLAN local address %q is invalid", ErrInvalidRequest, link.LocalIP)
	}
	if ip := net.ParseIP(link.RemoteIP); ip == nil {
		return fmt.Errorf("%w: VXLAN remote address %q is invalid", ErrInvalidRequest, link.RemoteIP)
	}
	if link.Underlay != "" {
		if err := ValidateName(link.Underlay); err != nil {
			return err
		}
	}
	if link.Port != 0 && (link.Port < 1 || link.Port > 65535) {
		return fmt.Errorf("%w: VXLAN port %d is out of range", ErrInvalidRequest, link.Port)
	}
	if link.MTU != 0 && (link.MTU < 576 || link.MTU > 9000) {
		return fmt.Errorf("%w: VXLAN MTU %d is out of range", ErrInvalidRequest, link.MTU)
	}
	return nil
}

// LinkExists reports whether a link with the given name is present. A missing
// link is not an error so callers can use this for idempotent apply.
func (s *Service) LinkExists(name string) (bool, error) {
	if err := ValidateName(name); err != nil {
		return false, err
	}
	out, err := s.run.Run("ip", "link", "show", name)
	if err != nil {
		if strings.Contains(strings.ToLower(out), "does not exist") {
			return false, nil
		}
		return false, fmt.Errorf("%w: ip link show %s: %v", ErrUnavailable, name, err)
	}
	return out != "", nil
}

// AddBridge creates a fleet bridge and brings it up. Re-running it on an
// existing bridge only reconciles MTU, so apply is idempotent.
func (s *Service) AddBridge(cfg BridgeConfig) error {
	if err := ValidateBridge(cfg); err != nil {
		return err
	}
	exists, err := s.LinkExists(cfg.Name)
	if err != nil {
		return err
	}
	if !exists {
		if _, err := s.run.Run("ip", "link", "add", cfg.Name, "type", "bridge"); err != nil {
			return fmt.Errorf("%w: add bridge %s: %v", ErrUnavailable, cfg.Name, err)
		}
	}
	if cfg.MTU != 0 {
		if _, err := s.run.Run("ip", "link", "set", cfg.Name, "mtu", strconv.Itoa(cfg.MTU)); err != nil {
			return fmt.Errorf("%w: set bridge %s mtu: %v", ErrUnavailable, cfg.Name, err)
		}
	}
	if _, err := s.run.Run("ip", "link", "set", cfg.Name, "up"); err != nil {
		return fmt.Errorf("%w: bring bridge %s up: %v", ErrUnavailable, cfg.Name, err)
	}
	if cfg.Address != "" {
		return s.ensureBridgeAddress(cfg.Name, cfg.Address)
	}
	return nil
}

// DeleteBridge removes a fleet bridge. The bridge must be empty; callers
// delete VXLAN links and addresses first.
func (s *Service) DeleteBridge(name string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	exists, err := s.LinkExists(name)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	if _, err := s.run.Run("ip", "link", "del", name); err != nil {
		return fmt.Errorf("%w: delete bridge %s: %v", ErrUnavailable, name, err)
	}
	return nil
}

func (s *Service) ensureBridgeAddress(bridge, cidr string) error {
	out, err := s.run.Run("ip", "-j", "addr", "show", "dev", bridge)
	if err != nil {
		return fmt.Errorf("%w: ip addr show %s: %v", ErrUnavailable, bridge, err)
	}
	var addrs []struct {
		AddrInfo []ipAddr `json:"addr_info"`
	}
	if err := json.Unmarshal([]byte(out), &addrs); err == nil {
		for _, item := range addrs {
			for _, addr := range item.AddrInfo {
				if net.JoinHostPort(addr.Address, strconv.Itoa(addr.Prefixlen)) == cidr ||
					addr.Address+"/"+strconv.Itoa(addr.Prefixlen) == cidr {
					return nil
				}
			}
		}
	}
	if _, err := s.run.Run("ip", "addr", "replace", cidr, "dev", bridge); err != nil {
		return fmt.Errorf("%w: add address %s to %s: %v", ErrUnavailable, cidr, bridge, err)
	}
	return nil
}

// AddVXLAN creates a point-to-point VXLAN link and attaches it to a bridge.
// An existing link with the same name is left unchanged so repeated applies
// do not duplicate tunnels.
func (s *Service) AddVXLAN(link VXLANLink) error {
	if err := ValidateVXLAN(link); err != nil {
		return err
	}
	exists, err := s.LinkExists(link.Name)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	port := link.Port
	if port == 0 {
		port = defaultVXLANPort
	}
	args := []string{"link", "add", link.Name, "type", "vxlan", "id", strconv.Itoa(link.VNI),
		"local", link.LocalIP, "remote", link.RemoteIP, "dstport", strconv.Itoa(port)}
	if link.Underlay != "" {
		args = append(args, "dev", link.Underlay)
	}
	if _, err := s.run.Run("ip", args...); err != nil {
		return fmt.Errorf("%w: add vxlan %s: %v", ErrUnavailable, link.Name, err)
	}
	if link.MTU != 0 {
		if _, err := s.run.Run("ip", "link", "set", link.Name, "mtu", strconv.Itoa(link.MTU)); err != nil {
			return fmt.Errorf("%w: set vxlan %s mtu: %v", ErrUnavailable, link.Name, err)
		}
	}
	if _, err := s.run.Run("ip", "link", "set", link.Name, "master", link.Bridge); err != nil {
		return fmt.Errorf("%w: attach vxlan %s to %s: %v", ErrUnavailable, link.Name, link.Bridge, err)
	}
	if _, err := s.run.Run("ip", "link", "set", link.Name, "up"); err != nil {
		return fmt.Errorf("%w: bring vxlan %s up: %v", ErrUnavailable, link.Name, err)
	}
	return nil
}

// DeleteVXLAN removes a VXLAN link if it exists.
func (s *Service) DeleteVXLAN(name string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	exists, err := s.LinkExists(name)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	if _, err := s.run.Run("ip", "link", "del", name); err != nil {
		return fmt.Errorf("%w: delete vxlan %s: %v", ErrUnavailable, name, err)
	}
	return nil
}
