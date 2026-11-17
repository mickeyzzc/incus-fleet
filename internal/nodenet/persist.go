package nodenet

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
)

// Manager identifies the host network configuration manager.
type Manager string

// Supported network configuration managers.
const (
	ManagerIfupdown   Manager = "ifupdown"
	ManagerIfupdownNG Manager = "ifupdown-ng"
	ManagerNetworkd   Manager = "systemd-networkd"
	ManagerNetplan    Manager = "netplan"
	ManagerWicked     Manager = "wicked"
	ManagerNetworkMgr Manager = "NetworkManager"
	ManagerUnknown    Manager = "unknown"
)

// DetectManager inspects a filesystem root for a supported manager.
func DetectManager(root string) Manager {
	if dirExists(filepath.Join(root, "etc/netplan")) {
		return ManagerNetplan
	}
	if fileExists(filepath.Join(root, "etc/NetworkManager/NetworkManager.conf")) {
		return ManagerNetworkMgr
	}
	if dirExists(filepath.Join(root, "etc/systemd/network")) {
		return ManagerNetworkd
	}
	if fileExists(filepath.Join(root, "etc/sysconfig/network/config")) {
		return ManagerWicked
	}
	if fileExists(filepath.Join(root, "etc/network/interfaces")) {
		return ManagerIfupdown
	}

	return ManagerUnknown
}

// PersistAddress writes a fleet-owned persistence file for the selected
// manager. The root parameter makes the operation testable; production uses
// "/".
func PersistAddress(run Runner, root string, manager Manager, name string, req AddressRequest) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	req, err := normalizeAddressRequest(req)
	if err != nil {
		return err
	}
	if run == nil {
		run = DirectRunner{}
	}

	switch manager {
	case ManagerIfupdown, ManagerIfupdownNG:
		return writeIfupdown(root, name, req)
	case ManagerNetworkd:
		if err := writeNetworkd(root, name, req); err != nil {
			return err
		}
		_, err := run.Run("networkctl", "reload")
		return err
	case ManagerNetplan:
		if err := writeNetplan(root, name, req); err != nil {
			return err
		}
		_, err := run.Run("netplan", "apply")
		return err
	case ManagerWicked:
		if err := writeWicked(root, name, req); err != nil {
			return err
		}
		_, err := run.Run("wicked", "ifreload", name)
		return err
	case ManagerNetworkMgr:
		return persistNetworkManager(run, name, req)
	default:
		return fmt.Errorf("%w: %q", ErrUnsupportedManager, manager)
	}
}

// PersistencePath returns the fleet-owned file for a manager, or "" when the
// manager persists through a command/API instead of a file.
func PersistencePath(root string, manager Manager, name string) string {
	switch manager {
	case ManagerIfupdown, ManagerIfupdownNG:
		return filepath.Join(root, "etc/network/interfaces.d", "fleet-"+name)
	case ManagerNetworkd:
		return filepath.Join(root, "etc/systemd/network", "10-fleet-"+name+".network")
	case ManagerNetplan:
		return filepath.Join(root, "etc/netplan", "90-fleet-"+name+".yaml")
	case ManagerWicked:
		return filepath.Join(root, "etc/sysconfig/network", "ifcfg-"+name)
	default:
		return ""
	}
}

func normalizeAddressRequest(req AddressRequest) (AddressRequest, error) {
	if req.DHCP {
		return req, nil
	}
	if req.Address == "" {
		return req, fmt.Errorf("%w: static configuration requires an address", ErrInvalidRequest)
	}
	if !strings.Contains(req.Address, "/") {
		ip := net.ParseIP(strings.TrimSpace(req.Address))
		if ip == nil {
			return req, fmt.Errorf("%w: address %q is invalid", ErrInvalidRequest, req.Address)
		}
		req.Address += "/24"
	}
	if _, _, err := net.ParseCIDR(req.Address); err != nil {
		return req, fmt.Errorf("%w: address %q is invalid", ErrInvalidRequest, req.Address)
	}
	if req.Gateway != "" && net.ParseIP(req.Gateway) == nil {
		return req, fmt.Errorf("%w: gateway %q is invalid", ErrInvalidRequest, req.Gateway)
	}

	return req, nil
}

func writeIfupdown(root, name string, req AddressRequest) error {
	var body string
	if req.DHCP {
		body = fmt.Sprintf("# Managed by incus-fleet.\nallow-hotplug %s\niface %s inet dhcp\n", name, name)
	} else {
		ip, network, _ := net.ParseCIDR(req.Address)
		mask := network.Mask
		body = fmt.Sprintf("# Managed by incus-fleet.\nallow-hotplug %s\niface %s inet static\n  address %s\n  netmask %s\n",
			name, name, ip.String(), net.IP(mask).String())
		if req.Gateway != "" {
			body += fmt.Sprintf("  gateway %s\n", req.Gateway)
		}
	}

	return writeFleetFile(PersistencePath(root, ManagerIfupdown, name), body)
}

func writeNetworkd(root, name string, req AddressRequest) error {
	var body strings.Builder
	fmt.Fprintf(&body, "# Managed by incus-fleet.\n[Match]\nName=%s\n\n[Network]\n", name)
	if req.DHCP {
		body.WriteString("DHCP=yes\n")
	} else {
		fmt.Fprintf(&body, "Address=%s\n", req.Address)
		if req.Gateway != "" {
			fmt.Fprintf(&body, "Gateway=%s\n", req.Gateway)
		}
	}

	return writeFleetFile(PersistencePath(root, ManagerNetworkd, name), body.String())
}

func writeNetplan(root, name string, req AddressRequest) error {
	var body strings.Builder
	body.WriteString("# Managed by incus-fleet.\nnetwork:\n  version: 2\n  ethernets:\n")
	fmt.Fprintf(&body, "    %s:\n", name)
	if req.DHCP {
		body.WriteString("      dhcp4: true\n")
	} else {
		fmt.Fprintf(&body, "      addresses:\n        - %s\n", req.Address)
		if req.Gateway != "" {
			fmt.Fprintf(&body, "      routes:\n        - to: default\n          via: %s\n", req.Gateway)
		}
	}

	return writeFleetFile(PersistencePath(root, ManagerNetplan, name), body.String())
}

func writeWicked(root, name string, req AddressRequest) error {
	var body strings.Builder
	body.WriteString("# Managed by incus-fleet.\n")
	if req.DHCP {
		body.WriteString("BOOTPROTO='dhcp'\n")
	} else {
		fmt.Fprintf(&body, "BOOTPROTO='static'\nIPADDR='%s'\n", req.Address)
		if req.Gateway != "" {
			fmt.Fprintf(&body, "GATEWAY='%s'\n", req.Gateway)
		}
	}

	return writeFleetFile(PersistencePath(root, ManagerWicked, name), body.String())
}

func persistNetworkManager(run Runner, name string, req AddressRequest) error {
	profile := "fleet-" + name
	_, err := run.Run("nmcli", "-g", "NAME", "connection", "show", profile)
	if err != nil {
		args := []string{"connection", "add", "type", "ethernet", "ifname", name, "con-name", profile}
		if req.DHCP {
			args = append(args, "ipv4.method", "auto")
		} else {
			args = append(args, "ipv4.method", "manual", "ipv4.addresses", req.Address)
			if req.Gateway != "" {
				args = append(args, "ipv4.gateway", req.Gateway)
			}
		}
		if _, err := run.Run("nmcli", args...); err != nil {
			return err
		}
		return nil
	}

	args := []string{"connection", "modify", profile}
	if req.DHCP {
		args = append(args, "ipv4.method", "auto", "ipv4.addresses", "", "ipv4.gateway", "")
	} else {
		args = append(args, "ipv4.method", "manual", "ipv4.addresses", req.Address)
		if req.Gateway != "" {
			args = append(args, "ipv4.gateway", req.Gateway)
		}
	}
	if _, err := run.Run("nmcli", args...); err != nil {
		return err
	}
	_, err = run.Run("nmcli", "connection", "up", profile)

	return err
}

func writeFleetFile(path, body string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("%w: create config directory: %v", ErrUnavailable, err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(body), 0o644); err != nil { //nolint:gosec // manager config files are world-readable
		return fmt.Errorf("%w: write config: %v", ErrUnavailable, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("%w: install config: %v", ErrUnavailable, err)
	}

	return nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// ErrUnsupportedManager marks a detected manager without an adapter.
var ErrUnsupportedManager = errors.New("nodenet: unsupported network manager")
