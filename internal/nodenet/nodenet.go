// Package nodenet manages the physical network configuration of the node
// fleet itself runs on: NICs and addresses, routes, WiFi, and the nftables
// firewall. Host operations are privileged, so they run through `sudo -n`
// against a whitelist of network binaries installed by the operator
// (see deploy/nodesudoers).
package nodenet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ErrInvalidRequest marks a rejected configuration value.
var ErrInvalidRequest = errors.New("nodenet: invalid request")

// ErrUnavailable marks host operations that cannot run (missing sudo rules
// or binaries).
var ErrUnavailable = errors.New("nodenet: host operations unavailable")

// Runner executes one whitelisted binary with arguments and returns stdout.
type Runner interface {
	Run(name string, args ...string) (string, error)
	RunStdin(name string, args []string, stdin string) error
}

// DirectRunner runs commands directly. The helper uses it after sudo has
// already elevated the process.
type DirectRunner struct{}

// Run implements Runner.
func (DirectRunner) Run(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, resolveBinary(name), args...).CombinedOutput() //nolint:gosec // allowlisted network binary
	if err != nil {
		return string(out), err
	}

	return string(out), nil
}

// RunStdin implements Runner.
func (DirectRunner) RunStdin(name string, args []string, stdin string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, resolveBinary(name), args...) //nolint:gosec // allowlisted network binary
	cmd.Stdin = strings.NewReader(stdin)

	return cmd.Run()
}

// SudoRunner runs commands through `sudo -n`.
type SudoRunner struct{}

// Run implements Runner.
func (SudoRunner) Run(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	full := append([]string{"-n", resolveBinary(name)}, args...)
	// #nosec G204 -- args are validated against whitelist before execution
	// CombinedOutput keeps stderr: nft reports a missing table there and
	// ListFirewall turns that into an empty rule set.
	out, err := exec.CommandContext(ctx, "sudo", full...).CombinedOutput()
	if err != nil {
		return string(out), err
	}

	return string(out), nil
}

// RunStdin implements Runner for content-writing commands such as tee.
func (SudoRunner) RunStdin(name string, args []string, stdin string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	full := append([]string{"-n", resolveBinary(name)}, args...)
	// #nosec G204 -- args are validated against whitelist before execution
	cmd := exec.CommandContext(ctx, "sudo", full...)
	cmd.Stdin = strings.NewReader(stdin)

	return cmd.Run()
}

// resolveBinary turns a binary name into its absolute path. sudo matches
// commands against the sudoers whitelist by path, and some sudo builds fail to
// match bare names resolved through PATH, so nodenet always passes the
// resolved path. The search order mirrors deploy/nodesudoers (usr/bin before
// usr/sbin) because systemd's service PATH puts usr/sbin first; without this
// preference a whitelisted /usr/bin/ip would resolve to an unlisted
// /usr/sbin/ip and sudo would demand a password.
func resolveBinary(name string) string {
	for _, dir := range []string{"/usr/bin", "/usr/sbin", "/bin", "/sbin", "/usr/local/bin", "/usr/local/sbin"} {
		candidate := filepath.Join(dir, name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return candidate
		}
	}

	if path, err := exec.LookPath(name); err == nil {
		return path
	}

	return name
}

// NewService builds a node network service.
func NewService(run Runner) *Service {
	if run == nil {
		run = SudoRunner{}
	}

	return &Service{run: run}
}

// Service operates on the node's physical network configuration.
type Service struct {
	run Runner
}

var namePattern = regexp.MustCompile(`^[a-zA-Z0-9._@-]{1,15}$`)

// ValidateName rejects interface names that could smuggle arguments.
func ValidateName(name string) error {
	if !namePattern.MatchString(name) || strings.HasPrefix(name, "-") {
		return fmt.Errorf("%w: interface name %q is invalid", ErrInvalidRequest, name)
	}

	return nil
}

// Address is one address assigned to a NIC as the API renders it.
type Address struct {
	Family    string `json:"family"`
	Address   string `json:"address"`
	Prefixlen int    `json:"prefixlen"`
}

// ipAddr mirrors the iproute2 `ip -j addr` field names for parsing.
type ipAddr struct {
	Family    string `json:"family"`
	Address   string `json:"local"`
	Prefixlen int    `json:"prefixlen"`
}

// WiFiLink carries the current Wi-Fi association of a NIC.
type WiFiLink struct {
	Connected bool   `json:"connected"`
	SSID      string `json:"ssid,omitempty"`
	Signal    int    `json:"signal,omitempty"`
	Frequency int    `json:"frequency,omitempty"`
}

// NIC is one physical or virtual network interface on the node.
type NIC struct {
	Name      string    `json:"name"`
	State     string    `json:"state"`
	Kind      string    `json:"kind"`
	MAC       string    `json:"mac,omitempty"`
	MTU       int       `json:"mtu,omitempty"`
	Addresses []Address `json:"addresses"`
	WiFi      *WiFiLink `json:"wifi,omitempty"`
}

// Route is one entry of the node routing table.
type Route struct {
	Destination string `json:"destination"`
	Gateway     string `json:"gateway,omitempty"`
	Device      string `json:"device,omitempty"`
	Metric      string `json:"metric,omitempty"`
	Protocol    string `json:"protocol,omitempty"`
}

// FirewallRule is one nftables rule managed by fleet.
type FirewallRule struct {
	Handle string `json:"handle"`
	Rule   string `json:"rule"`
}

// Snapshot is everything the node network page renders.
type Snapshot struct {
	Host         string         `json:"host"`
	NICs         []NIC          `json:"nics"`
	Routes       []Route        `json:"routes"`
	Firewall     []FirewallRule `json:"firewall"`
	WifiScan     []WiFiNetwork  `json:"wifi_scan,omitempty"`
	WifiDevice   string         `json:"wifi_device,omitempty"`
	Capabilities []string       `json:"capabilities"`
	Backend      string         `json:"backend,omitempty"`
}

// isWireless reports whether the kernel exposes the interface as a Wi-Fi
// device. iproute2 often labels wireless links as "ether", so nodenet cannot
// rely on link_type alone.
func isWireless(ifname string) bool {
	_, err := os.Stat(filepath.Join("/sys/class/net", ifname, "wireless"))
	return err == nil
}

// ListNICs returns every interface with its state and addresses.
func (s *Service) ListNICs(_ context.Context) ([]NIC, error) {
	out, err := s.run.Run("ip", "-j", "addr", "show")
	if err != nil {
		return nil, fmt.Errorf("%w: ip addr show: %v", ErrUnavailable, err)
	}

	var raw []struct {
		Ifname    string   `json:"ifname"`
		Operstate string   `json:"operstate"`
		Flags     []string `json:"flags"`
		MTU       int      `json:"mtu"`
		Address   string   `json:"address"`
		LinkType  string   `json:"link_type"`
		AddrInfo  []ipAddr `json:"addr_info"`
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return nil, fmt.Errorf("%w: parse ip output: %v", ErrUnavailable, err)
	}

	nics := make([]NIC, 0, len(raw))
	for _, item := range raw {
		kind := "ethernet"
		switch {
		case item.LinkType == "loopback":
			kind = "loopback"
		case item.LinkType == "bridge":
			kind = "bridge"
		case item.LinkType == "wlan", isWireless(item.Ifname):
			kind = "wifi"
		}

		addresses := make([]Address, 0, len(item.AddrInfo))
		for _, addr := range item.AddrInfo {
			addresses = append(addresses, Address(addr))
		}

		// Admin state from IFF_UP when flags are present: an enabled Wi-Fi
		// interface without association reports operstate "down" while it is
		// administratively up. Fall back to operstate for callers that only
		// provide it.
		state := strings.ToLower(item.Operstate)
		if len(item.Flags) > 0 {
			state = "down"
			for _, flag := range item.Flags {
				if flag == "UP" {
					state = "up"
					break
				}
			}
		}

		nic := NIC{
			Name:      item.Ifname,
			State:     state,
			Kind:      kind,
			MAC:       item.Address,
			MTU:       item.MTU,
			Addresses: addresses,
		}
		if kind == "wifi" {
			nic.WiFi = s.wifiLink(item.Ifname)
		}

		nics = append(nics, nic)
	}

	sort.Slice(nics, func(i, j int) bool { return nics[i].Name < nics[j].Name })
	return nics, nil
}

// wifiLink reads the Wi-Fi association for one interface; missing output
// means the link is down or the interface is not a station.
func (s *Service) wifiLink(dev string) *WiFiLink {
	out, err := s.run.Run("iw", "dev", dev, "link")
	trimmed := strings.TrimSpace(out)
	if err != nil || trimmed == "" || strings.HasPrefix(trimmed, "Not connected") {
		return &WiFiLink{Connected: false}
	}

	link := &WiFiLink{Connected: true}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "Connected to "):
			link.SSID = ""
		case strings.HasPrefix(line, "signal:"):
			dbm := strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(line, "signal:")), " dBm")
			link.Signal, _ = strconv.Atoi(strings.Split(dbm, ".")[0])
		case strings.HasPrefix(line, "freq:"):
			mhz := strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(line, "freq:")), " MHz")
			m, _ := strconv.Atoi(strings.Split(mhz, ".")[0])
			link.Frequency = m
		}
	}

	// SSID lives on the station dump of the interface itself.
	ssidOut, err := s.run.Run("iw", "dev", dev, "info")
	if err == nil {
		for _, line := range strings.Split(ssidOut, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "ssid ") {
				link.SSID = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "ssid "))
			}
		}
	}

	return link
}

// WiFiNetwork is one scanned Wi-Fi network.
type WiFiNetwork struct {
	SSID      string `json:"ssid"`
	Signal    int    `json:"signal"`
	Frequency int    `json:"frequency"`
	Secure    bool   `json:"secure"`
}

// ScanWiFi lists visible Wi-Fi networks for one interface.
func (s *Service) ScanWiFi(dev string) ([]WiFiNetwork, error) {
	if err := ValidateName(dev); err != nil {
		return nil, err
	}

	out, err := s.run.Run("iw", "dev", dev, "scan")
	if err != nil {
		return nil, fmt.Errorf("%w: iw scan (root rules required): %v", ErrUnavailable, err)
	}

	networks := dedupeWiFiNetworks(parseWiFiScan(out))
	sort.Slice(networks, func(i, j int) bool { return networks[i].Signal > networks[j].Signal })
	return networks, nil
}

// dedupeWiFiNetworks keeps one entry per SSID (the strongest BSS) so the page
// lists visible networks rather than every access point radio.
func dedupeWiFiNetworks(networks []WiFiNetwork) []WiFiNetwork {
	best := make(map[string]WiFiNetwork, len(networks))
	for _, network := range networks {
		current, exists := best[network.SSID]
		if !exists || network.Signal > current.Signal {
			best[network.SSID] = network
		}
	}

	out := make([]WiFiNetwork, 0, len(best))
	for _, network := range best {
		out = append(out, network)
	}

	return out
}

func parseWiFiScan(out string) []WiFiNetwork {
	var networks []WiFiNetwork
	var current *WiFiNetwork

	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "BSS "):
			if current != nil && current.SSID != "" {
				networks = append(networks, *current)
			}
			current = &WiFiNetwork{}
			if strings.Contains(line, "capability:") {
				current.Secure = true
			}
		case current == nil:
			continue
		case strings.HasPrefix(line, "SSID:"):
			current.SSID = strings.TrimSpace(strings.TrimPrefix(line, "SSID:"))
		case strings.HasPrefix(line, "signal:"):
			dbm := strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(line, "signal:")), " dBm")
			current.Signal, _ = strconv.Atoi(strings.Split(dbm, ".")[0])
		case strings.HasPrefix(line, "freq:"):
			mhz := strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(line, "freq:")), " MHz")
			current.Frequency, _ = strconv.Atoi(strings.Split(mhz, ".")[0])
		case strings.HasPrefix(line, "WPA:") || strings.HasPrefix(line, "RSN:"):
			current.Secure = true
		}
	}
	if current != nil && current.SSID != "" {
		networks = append(networks, *current)
	}

	return networks
}

// SetNICState brings one interface up or down.
func (s *Service) SetNICState(name string, up bool) error {
	if err := ValidateName(name); err != nil {
		return err
	}

	state := "down"
	if up {
		state = "up"
	}

	if _, err := s.run.Run("ip", "link", "set", "dev", name, state); err != nil {
		return fmt.Errorf("%w: set %s %s: %v", ErrUnavailable, name, state, err)
	}

	return nil
}

// AddressRequest configures one interface address.
type AddressRequest struct {
	DHCP    bool   `json:"dhcp"`
	Address string `json:"address,omitempty"` // CIDR, e.g. 10.0.0.50/24
	Gateway string `json:"gateway,omitempty"`
	Persist bool   `json:"persist"`
}

// ApplyAddress switches one interface between DHCP and a static address.
// Static addresses are applied at runtime; persist writes the ifupdown
// snippet used at boot.
func (s *Service) ApplyAddress(_ context.Context, name string, req AddressRequest) error {
	if err := ValidateName(name); err != nil {
		return err
	}

	if req.DHCP {
		if _, err := s.run.Run("dhcpcd", "-1", "--nohook", "resolv.conf", name); err != nil {
			return fmt.Errorf("%w: dhcp on %s: %v", ErrUnavailable, name, err)
		}
	} else {
		if req.Address == "" {
			return fmt.Errorf("%w: static configuration requires an address", ErrInvalidRequest)
		}
		if !strings.Contains(req.Address, "/") {
			ip := net.ParseIP(strings.TrimSpace(req.Address))
			if ip == nil {
				return fmt.Errorf("%w: address %q is invalid", ErrInvalidRequest, req.Address)
			}
			req.Address += "/24"
		}
		if _, _, err := net.ParseCIDR(req.Address); err != nil {
			return fmt.Errorf("%w: address %q is invalid", ErrInvalidRequest, req.Address)
		}
		if req.Gateway != "" && net.ParseIP(req.Gateway) == nil {
			return fmt.Errorf("%w: gateway %q is invalid", ErrInvalidRequest, req.Gateway)
		}

		if _, err := s.run.Run("ip", "addr", "add", req.Address, "dev", name); err != nil {
			return fmt.Errorf("%w: add %s on %s: %v", ErrUnavailable, req.Address, name, err)
		}
		if req.Gateway != "" {
			if _, err := s.run.Run("ip", "route", "replace", "default", "via", req.Gateway, "dev", name); err != nil {
				return fmt.Errorf("%w: gateway via %s on %s: %v", ErrUnavailable, req.Gateway, name, err)
			}
		}
	}

	if req.Persist {
		return s.persistAddress(name, req)
	}

	return nil
}

func (s *Service) persistAddress(name string, req AddressRequest) error {
	var snippet string
	if req.DHCP {
		snippet = fmt.Sprintf("allow-hotplug %s\niface %s inet dhcp\n", name, name)
	} else {
		mask := "255.255.255.0"
		body := fmt.Sprintf("allow-hotplug %s\niface %s inet static\n  address %s\n  netmask %s\n", name, name, strings.SplitN(req.Address, "/", 2)[0], mask)
		if req.Gateway != "" {
			body += fmt.Sprintf("  gateway %s\n", req.Gateway)
		}
		snippet = body
	}

	// tee as root writes the ifupdown snippet into interfaces.d.
	if err := s.run.RunStdin("tee", []string{"/etc/network/interfaces.d/" + name}, snippet); err != nil {
		return fmt.Errorf("%w: persist %s: %v", ErrUnavailable, name, err)
	}

	return nil
}

// ListRoutes returns the node routing table.
func (s *Service) ListRoutes() ([]Route, error) {
	out, err := s.run.Run("ip", "-j", "route", "show")
	if err != nil {
		return nil, fmt.Errorf("%w: ip route show: %v", ErrUnavailable, err)
	}

	var raw []struct {
		Dst      string `json:"dst"`
		Gateway  string `json:"gateway,omitempty"`
		Dev      string `json:"dev"`
		Metric   int    `json:"metric,omitempty"`
		Protocol string `json:"protocol,omitempty"`
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return nil, fmt.Errorf("%w: parse route output: %v", ErrUnavailable, err)
	}

	routes := make([]Route, 0, len(raw))
	for _, item := range raw {
		route := Route{
			Destination: item.Dst,
			Gateway:     item.Gateway,
			Device:      item.Dev,
			Protocol:    item.Protocol,
		}
		if item.Metric > 0 {
			route.Metric = strconv.Itoa(item.Metric)
		}
		routes = append(routes, route)
	}

	return routes, nil
}

// AddRoute adds one static route at runtime.
func (s *Service) AddRoute(destination, gateway, device string) error {
	if err := ValidateName(device); err != nil {
		return err
	}
	if net.ParseIP(strings.SplitN(destination, "/", 2)[0]) == nil {
		return fmt.Errorf("%w: destination %q is invalid", ErrInvalidRequest, destination)
	}
	if net.ParseIP(gateway) == nil {
		return fmt.Errorf("%w: gateway %q is invalid", ErrInvalidRequest, gateway)
	}

	if _, err := s.run.Run("ip", "route", "add", destination, "via", gateway, "dev", device); err != nil {
		return fmt.Errorf("%w: add route %s via %s: %v", ErrUnavailable, destination, gateway, err)
	}

	return nil
}

// DeleteRoute removes one runtime route.
func (s *Service) DeleteRoute(destination string) error {
	if net.ParseIP(strings.SplitN(destination, "/", 2)[0]) == nil {
		return fmt.Errorf("%w: destination %q is invalid", ErrInvalidRequest, destination)
	}

	if _, err := s.run.Run("ip", "route", "del", destination); err != nil {
		return fmt.Errorf("%w: delete route %s: %v", ErrUnavailable, destination, err)
	}

	return nil
}

const fleetTable = "inet fleet_filter"

// ListFirewall returns the fleet-managed nftables rules.
func (s *Service) ListFirewall() ([]FirewallRule, error) {
	out, err := s.run.Run("nft", "-a", "list", "table", fleetTable)
	if err != nil {
		// A missing table means the firewall section is empty, not broken.
		if strings.Contains(out, "No such file or directory") || strings.Contains(out, "has no table") {
			return []FirewallRule{}, nil
		}
		return nil, fmt.Errorf("%w: nft list: %v", ErrUnavailable, err)
	}

	rules := []FirewallRule{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		// nft -a appends "# handle N" to table, chain, and rule lines alike;
		// only the rules are editable by handle for the UI.
		if !strings.Contains(line, "handle ") || strings.HasPrefix(line, "table ") ||
			strings.HasPrefix(line, "chain ") || strings.HasPrefix(line, "}") ||
			strings.Contains(line, "hook input") || strings.Contains(line, "policy") {
			continue
		}

		idx := strings.LastIndex(line, "handle ")
		handle := strings.TrimSpace(line[idx+len("handle "):])
		rule := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(line[:idx]), "#"))
		if rule == "" || handle == "" {
			continue
		}
		rules = append(rules, FirewallRule{Handle: handle, Rule: rule})
	}

	return rules, nil
}

// FirewallRequest adds one input rule to the fleet-managed table.
type FirewallRequest struct {
	Port    int    `json:"port"`
	Proto   string `json:"proto"`
	Action  string `json:"action"` // accept or drop
	Comment string `json:"comment,omitempty"`
}

// AddFirewallRule adds one input rule to the fleet-managed nftables table.
func (s *Service) AddFirewallRule(req FirewallRequest) error {
	if req.Port < 1 || req.Port > 65535 {
		return fmt.Errorf("%w: port %d is invalid", ErrInvalidRequest, req.Port)
	}
	proto := strings.ToLower(strings.TrimSpace(req.Proto))
	if proto != "tcp" && proto != "udp" {
		return fmt.Errorf("%w: protocol must be tcp or udp", ErrInvalidRequest)
	}
	action := strings.ToLower(strings.TrimSpace(req.Action))
	if action != "accept" && action != "drop" {
		return fmt.Errorf("%w: action must be accept or drop", ErrInvalidRequest)
	}
	comment := "fleet_rule"
	if req.Comment != "" {
		comment = strings.Map(func(r rune) rune {
			if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
				return r
			}
			return '_'
		}, req.Comment)
		if comment == "" {
			comment = "fleet_rule"
		}
	}

	if err := s.ensureFleetTable(); err != nil {
		return err
	}

	args := []string{"add", "rule", fleetTable, "input", proto, "dport", strconv.Itoa(req.Port), action, "comment", fmt.Sprintf("%q", comment)}
	if _, err := s.run.Run("nft", args...); err != nil {
		return fmt.Errorf("%w: add rule: %v", ErrUnavailable, err)
	}

	return nil
}

// DeleteFirewallRule removes one fleet-managed rule by handle.
func (s *Service) DeleteFirewallRule(handle string) error {
	if !regexp.MustCompile(`^\d+$`).MatchString(handle) {
		return fmt.Errorf("%w: handle %q is invalid", ErrInvalidRequest, handle)
	}

	if _, err := s.run.Run("nft", "delete", "rule", fleetTable, "input", "handle", handle); err != nil {
		return fmt.Errorf("%w: delete rule handle %s: %v", ErrUnavailable, handle, err)
	}

	return nil
}

func (s *Service) ensureFleetTable() error {
	out, err := s.run.Run("nft", "list", "tables")
	if err != nil {
		return fmt.Errorf("%w: nft list tables: %v", ErrUnavailable, err)
	}
	if strings.Contains(out, "table inet fleet_filter") {
		return nil
	}

	if _, err := s.run.Run("nft", "add", "table", "inet", "fleet_filter"); err != nil {
		return fmt.Errorf("%w: create table: %v", ErrUnavailable, err)
	}
	if _, err := s.run.Run("nft", "add", "chain", "inet", "fleet_filter", "input", "{", "type", "filter", "hook", "input", "priority", "-10", ";", "policy", "accept", ";", "}"); err != nil {
		return fmt.Errorf("%w: create chain: %v", ErrUnavailable, err)
	}

	return nil
}

// Snapshot collects everything the node network page renders.
func (s *Service) Snapshot(ctx context.Context, wifiDevice string) (*Snapshot, error) {
	nics, err := s.ListNICs(ctx)
	if err != nil {
		return nil, err
	}

	snapshot := &Snapshot{
		NICs:         nics,
		Routes:       []Route{},
		Firewall:     []FirewallRule{},
		WifiScan:     []WiFiNetwork{},
		Capabilities: []string{"nics", "routes", "firewall"},
	}

	if routes, err := s.ListRoutes(); err == nil {
		snapshot.Routes = routes
	}
	if rules, err := s.ListFirewall(); err == nil {
		snapshot.Firewall = rules
	}
	if wifiDevice == "" {
		// Default to the first wireless interface so the page can show the
		// scan section without the caller knowing interface names.
		for _, nic := range nics {
			if nic.Kind == "wifi" {
				wifiDevice = nic.Name
				break
			}
		}
	}
	if wifiDevice != "" {
		snapshot.WifiDevice = wifiDevice
		if scan, err := s.ScanWiFi(wifiDevice); err == nil {
			snapshot.WifiScan = scan
		}
	}

	if host, err := s.run.Run("hostname"); err == nil {
		snapshot.Host = strings.TrimSpace(host)
	}

	return snapshot, nil
}

// WifiConnect associates one Wi-Fi interface with an access point and
// requests a lease. Requires wpasupplicant and dhcpcd on the node; the
// returned error names the missing piece otherwise.
func (s *Service) WifiConnect(dev, ssid, password string) error {
	if err := ValidateName(dev); err != nil {
		return err
	}
	if strings.TrimSpace(ssid) == "" {
		return fmt.Errorf("%w: ssid is required", ErrInvalidRequest)
	}
	if strings.ContainsAny(ssid, "\"\n\\") || strings.ContainsAny(password, "\"\n\\") {
		return fmt.Errorf("%w: ssid and password must not contain quotes or newlines", ErrInvalidRequest)
	}

	// wpa_supplicant is invoked by absolute path so the sudo whitelist and
	// the exec here agree on the binary.
	if _, err := s.run.Run("wpa_supplicant", "-v"); err != nil {
		return fmt.Errorf("%w: wpa_supplicant is not installed on this node (apt-get install -y wpasupplicant)", ErrUnavailable)
	}

	config := fmt.Sprintf("network={\n  ssid=\"%s\"\n  psk=\"%s\"\n}\n", ssid, password)
	if err := s.run.RunStdin("tee", []string{"/etc/wpa_supplicant/fleet.conf"}, config); err != nil {
		return fmt.Errorf("%w: write wifi config: %v", ErrUnavailable, err)
	}

	args := []string{"-B", "-i", dev, "-c", "/etc/wpa_supplicant/fleet.conf"}
	if _, err := s.run.Run("wpa_supplicant", args...); err != nil {
		return fmt.Errorf("%w: wpa_supplicant on %s: %v", ErrUnavailable, dev, err)
	}

	// Bring the link up and lease an address.
	if _, err := s.run.Run("ip", "link", "set", "dev", dev, "up"); err != nil {
		return fmt.Errorf("%w: bring %s up: %v", ErrUnavailable, dev, err)
	}
	if _, err := s.run.Run("dhcpcd", "-1", "--nohook", "resolv.conf", dev); err != nil {
		return fmt.Errorf("%w: dhcp on %s: %v", ErrUnavailable, dev, err)
	}

	return nil
}

// TrafficSample is the per-instance network byte counter of one cluster.
type TrafficSample struct {
	Instance string            `json:"instance"`
	Location string            `json:"location,omitempty"`
	NICs     map[string]uint64 `json:"nics"` // nic -> rx bytes
	TxBytes  map[string]uint64 `json:"tx"`   // nic -> tx bytes
}

// AddFleetRoute adds a route owned by fleet (protocol 99). It allows a
// directly connected route when gateway is empty.
func (s *Service) AddFleetRoute(destination, gateway, device string) error {
	if net.ParseIP(strings.SplitN(destination, "/", 2)[0]) == nil {
		return fmt.Errorf("%w: destination %q is invalid", ErrInvalidRequest, destination)
	}
	if device != "" {
		if err := ValidateName(device); err != nil {
			return err
		}
	}
	if routes, err := s.ListRoutes(); err == nil {
		for _, route := range routes {
			if route.Destination == destination && route.Protocol != "" && route.Protocol != "99" {
				return fmt.Errorf("%w: route %s is owned by protocol %s", ErrInvalidRequest, destination, route.Protocol)
			}
		}
	} else {
		return err
	}
	args := []string{"route", "replace", destination}
	if gateway != "" {
		if net.ParseIP(gateway) == nil {
			return fmt.Errorf("%w: gateway %q is invalid", ErrInvalidRequest, gateway)
		}
		args = append(args, "via", gateway)
	}
	if device != "" {
		args = append(args, "dev", device)
	}
	args = append(args, "proto", "99")
	if _, err := s.run.Run("ip", args...); err != nil {
		return fmt.Errorf("%w: add fleet route %s: %v", ErrUnavailable, destination, err)
	}

	return nil
}

// DeleteFleetRoute removes only a route owned by fleet (protocol 99).
func (s *Service) DeleteFleetRoute(destination string) error {
	if net.ParseIP(strings.SplitN(destination, "/", 2)[0]) == nil {
		return fmt.Errorf("%w: destination %q is invalid", ErrInvalidRequest, destination)
	}
	if _, err := s.run.Run("ip", "route", "del", destination, "proto", "99"); err != nil {
		return fmt.Errorf("%w: delete fleet route %s: %v", ErrUnavailable, destination, err)
	}

	return nil
}
