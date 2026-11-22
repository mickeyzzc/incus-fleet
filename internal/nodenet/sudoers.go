package nodenet

import "strings"

// sudoBinaryPaths lists the binaries the node network whitelist grants.
//
// The teardown path uses rm for fleet-owned dnsmasq/unit files and systemctl
// for the overlay DHCP unit. Both are limited to fleet-owned paths by the
// stricter per-path rules appended below; the bare entries here cover
// systemctl verbs used with fleet-created units.
var sudoBinaryPaths = []string{
	"/usr/bin/ip", "/usr/sbin/ip",
	"/usr/bin/nft", "/usr/sbin/nft",
	"/usr/bin/iw", "/usr/sbin/iw",
	"/usr/bin/dhcpcd", "/usr/sbin/dhcpcd",
	"/usr/bin/wpa_supplicant", "/usr/sbin/wpa_supplicant",
	"/usr/bin/hostname", "/usr/sbin/hostname",
	"/usr/bin/systemctl", "/bin/systemctl",
	"/usr/bin/rm", "/bin/rm",
}

// sudoFileRules lists the file-writing entries. Only fleet-owned paths are
// writable so a compromised service cannot rewrite arbitrary host config.
var sudoFileRules = []string{
	"/usr/bin/tee /etc/network/interfaces.d/*",
	"/usr/bin/tee /etc/wpa_supplicant/fleet.conf",
	"/usr/bin/tee /etc/dnsmasq.d/fleet-overlay-*.conf",
	"/usr/bin/tee /etc/systemd/system/fleet-overlay-dhcp-*.service",
}

// SudoersContent renders the node network whitelist for one account. The
// provisioner and the checked-in deploy/nodesudoers file both derive from
// this so the two cannot drift.
func SudoersContent(user string) string {
	rules := make([]string, 0, len(sudoBinaryPaths)+len(sudoFileRules))
	rules = append(rules, sudoBinaryPaths...)
	rules = append(rules, sudoFileRules...)

	for i, rule := range rules {
		suffix := ", \\"
		if i == len(rules)-1 {
			suffix = ""
		}
		rules[i] = "    " + rule + suffix
	}

	return strings.Join([]string{
		"# Managed by incus-fleet.",
		"# Minimal host network operations used by the fleet node page and",
		"# the cluster private overlay network.",
		user + " ALL=(root) NOPASSWD: \\",
		strings.Join(rules, "\n"),
		"",
	}, "\n")
}
