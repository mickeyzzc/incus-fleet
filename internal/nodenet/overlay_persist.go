package nodenet

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// overlayBase returns the fleet-owned unit prefix for one link under root.
func overlayBase(root, name string) string {
	return filepath.Join(root, "etc/systemd/network", "10-fleet-"+name)
}

// OverlayPersistencePath returns the fleet-owned persistence file for an
// overlay bridge or VXLAN link on the selected manager. systemd-networkd is
// the only manager that can express both a bridge (.netdev) and a VXLAN
// (.netdev), so it is the supported target; other managers report
// ErrUnsupportedManager.
func OverlayPersistencePath(root string, manager Manager, name string) string {
	if manager != ManagerNetworkd {
		return ""
	}
	return overlayBase(root, name) + ".netdev"
}

// BridgeNetdev renders a systemd-networkd .netdev unit for a fleet bridge.
func BridgeNetdev(cfg BridgeConfig) string {
	var b strings.Builder
	b.WriteString("# Managed by incus-fleet.\n")
	b.WriteString("[NetDev]\n")
	fmt.Fprintf(&b, "Name=%s\n", cfg.Name)
	b.WriteString("Kind=bridge\n")
	if cfg.MTU != 0 {
		b.WriteString("\n[Bridge]\n")
		fmt.Fprintf(&b, "MTUBytes=%d\n", cfg.MTU)
	}
	return b.String()
}

// BridgeNetwork renders the .network unit that assigns the host address and
// brings the fleet bridge up at boot. An empty address keeps the bridge
// unnumbered, which is valid for a pure transit bridge.
func BridgeNetwork(cfg BridgeConfig) string {
	var b strings.Builder
	b.WriteString("# Managed by incus-fleet.\n")
	b.WriteString("[Match]\n")
	fmt.Fprintf(&b, "Name=%s\n", cfg.Name)
	b.WriteString("\n[Network]\n")
	if cfg.MTU != 0 {
		fmt.Fprintf(&b, "MTUBytes=%d\n", cfg.MTU)
	}
	if cfg.Address != "" {
		fmt.Fprintf(&b, "Address=%s\n", cfg.Address)
	}
	b.WriteString("ConfigureWithoutCarrier=yes\n")
	return b.String()
}

// VXLANNetdev renders a .netdev unit for one point-to-point VXLAN link.
func VXLANNetdev(link VXLANLink) string {
	port := link.Port
	if port == 0 {
		port = defaultVXLANPort
	}
	var b strings.Builder
	b.WriteString("# Managed by incus-fleet.\n")
	b.WriteString("[NetDev]\n")
	fmt.Fprintf(&b, "Name=%s\n", link.Name)
	b.WriteString("Kind=vxlan\n")
	b.WriteString("\n[VXLAN]\n")
	fmt.Fprintf(&b, "VNI=%d\n", link.VNI)
	fmt.Fprintf(&b, "Local=%s\n", link.LocalIP)
	fmt.Fprintf(&b, "Remote=%s\n", link.RemoteIP)
	fmt.Fprintf(&b, "DestinationPort=%d\n", port)
	if link.Underlay != "" {
		fmt.Fprintf(&b, "Parent=%s\n", link.Underlay)
	}
	return b.String()
}

// VXLANNetwork renders the .network unit attaching a VXLAN link to its
// bridge.
func VXLANNetwork(link VXLANLink) string {
	var b strings.Builder
	b.WriteString("# Managed by incus-fleet.\n")
	b.WriteString("[Match]\n")
	fmt.Fprintf(&b, "Name=%s\n", link.Name)
	b.WriteString("\n[Network]\n")
	if link.MTU != 0 {
		fmt.Fprintf(&b, "MTUBytes=%d\n", link.MTU)
	}
	fmt.Fprintf(&b, "Bridge=%s\n", link.Bridge)
	return b.String()
}

// PersistBridge writes the bridge persistence units and reloads networkd.
func PersistBridge(run Runner, root string, manager Manager, cfg BridgeConfig) error {
	if err := ValidateBridge(cfg); err != nil {
		return err
	}
	if manager != ManagerNetworkd {
		return fmt.Errorf("%w: overlay persistence requires systemd-networkd", ErrUnsupportedManager)
	}
	if run == nil {
		run = DirectRunner{}
	}
	base := overlayBase(root, cfg.Name)
	if err := writeFleetFile(base+".netdev", BridgeNetdev(cfg)); err != nil {
		return err
	}
	if err := writeFleetFile(base+".network", BridgeNetwork(cfg)); err != nil {
		return err
	}
	_, err := run.Run("networkctl", "reload")
	return err
}

// PersistVXLAN writes the VXLAN persistence units and reloads networkd.
func PersistVXLAN(run Runner, root string, manager Manager, link VXLANLink) error {
	if err := ValidateVXLAN(link); err != nil {
		return err
	}
	if manager != ManagerNetworkd {
		return fmt.Errorf("%w: overlay persistence requires systemd-networkd", ErrUnsupportedManager)
	}
	if run == nil {
		run = DirectRunner{}
	}
	base := overlayBase(root, link.Name)
	if err := writeFleetFile(base+".netdev", VXLANNetdev(link)); err != nil {
		return err
	}
	if err := writeFleetFile(base+".network", VXLANNetwork(link)); err != nil {
		return err
	}
	_, err := run.Run("networkctl", "reload")
	return err
}

// RemoveOverlayPersistence deletes the fleet-owned units for a link. It is
// used on teardown and tolerates absent files.
func RemoveOverlayPersistence(root string, name string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	base := overlayBase(root, name)
	for _, path := range []string{base + ".netdev", base + ".network"} {
		if err := removeFleetFile(path); err != nil {
			return err
		}
	}
	return nil
}

func removeFleetFile(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("%w: remove %s: %v", ErrUnavailable, path, err)
	}
	return nil
}
