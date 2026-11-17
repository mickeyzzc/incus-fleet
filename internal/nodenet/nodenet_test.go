package nodenet

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeRunner struct {
	calls    []string
	outputs  map[string]string
	failOn   string
	stdinLog []string
}

func (f *fakeRunner) Run(name string, args ...string) (string, error) {
	call := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, call)
	if f.failOn != "" && strings.Contains(call, f.failOn) {
		return "", errors.New("boom: " + call)
	}
	if out, ok := f.outputs[call]; ok {
		return out, nil
	}
	return "", nil
}

func (f *fakeRunner) RunStdin(name string, args []string, stdin string) error {
	call := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, call+" <stdin")
	f.stdinLog = append(f.stdinLog, stdin)
	return nil
}

func TestListNICsParsesIpdOutput(t *testing.T) {
	r := &fakeRunner{
		outputs: map[string]string{
			"ip -j addr show":  `[{"ifname":"enp1s0","operstate":"UP","mtu":1500,"address":"aa:bb","link_type":"ether","addr_info":[{"family":"inet","local":"10.62.0.1","prefixlen":24}]},{"ifname":"wlo1","operstate":"DOWN","link_type":"wlan","addr_info":[]}]`,
			"iw dev wlo1 link": "",
		},
	}
	svc := NewService(r)

	nics, err := svc.ListNICs(context.Background())
	if err != nil {
		t.Fatalf("ListNICs() error = %v", err)
	}
	if len(nics) != 2 {
		t.Fatalf("nics = %#v", nics)
	}
	if nics[0].Name != "enp1s0" || nics[0].State != "up" || nics[0].Kind != "ethernet" {
		t.Fatalf("nic0 = %#v", nics[0])
	}
	if len(nics[0].Addresses) != 1 || nics[0].Addresses[0].Address != "10.62.0.1" {
		t.Fatalf("addresses = %#v", nics[0].Addresses)
	}
	if nics[1].Kind != "wifi" || nics[1].WiFi == nil || nics[1].WiFi.Connected {
		t.Fatalf("wifi nic = %#v", nics[1])
	}
}

func TestSetNICStateValidatesAndRuns(t *testing.T) {
	r := &fakeRunner{}
	svc := NewService(r)

	if err := svc.SetNICState("bad name; rm", true); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected name validation error, got %v", err)
	}
	if err := svc.SetNICState("enp1s0", true); err != nil {
		t.Fatalf("SetNICState() error = %v", err)
	}
	if len(r.calls) != 1 || !strings.Contains(r.calls[0], "ip link set dev enp1s0 up") {
		t.Fatalf("calls = %#v", r.calls)
	}
}

func TestApplyAddressStaticAndPersisted(t *testing.T) {
	r := &fakeRunner{}
	svc := NewService(r)

	err := svc.ApplyAddress(context.Background(), "enp1s0", AddressRequest{
		Address: "10.62.0.50",
		Gateway: "10.62.0.1",
		Persist: true,
	})
	if err != nil {
		t.Fatalf("ApplyAddress() error = %v", err)
	}

	joined := strings.Join(r.calls, "\n")
	if !strings.Contains(joined, "ip addr add 10.62.0.50/24 dev enp1s0") {
		t.Fatalf("missing addr add: %s", joined)
	}
	if !strings.Contains(joined, "ip route replace default via 10.62.0.1 dev enp1s0") {
		t.Fatalf("missing gateway: %s", joined)
	}
	if len(r.stdinLog) != 1 || !strings.Contains(r.stdinLog[0], "iface enp1s0 inet static") {
		t.Fatalf("persisted snippet = %#v", r.stdinLog)
	}
}

func TestApplyAddressRejectsInvalidValues(t *testing.T) {
	svc := NewService(&fakeRunner{})

	if err := svc.ApplyAddress(context.Background(), "enp1s0", AddressRequest{Address: "not-an-ip"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected address validation error, got %v", err)
	}
	if err := svc.ApplyAddress(context.Background(), "enp1s0", AddressRequest{Address: "10.0.0.1", Gateway: "nope"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected gateway validation error, got %v", err)
	}
	if err := svc.ApplyAddress(context.Background(), "enp1s0", AddressRequest{}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected empty request error, got %v", err)
	}
}

func TestApplyAddressDHCP(t *testing.T) {
	r := &fakeRunner{}
	svc := NewService(r)

	if err := svc.ApplyAddress(context.Background(), "enp2s0", AddressRequest{DHCP: true, Persist: true}); err != nil {
		t.Fatalf("ApplyAddress() error = %v", err)
	}

	joined := strings.Join(r.calls, "\n")
	if !strings.Contains(joined, "dhcpcd -1 --nohook resolv.conf enp2s0") {
		t.Fatalf("missing dhcp: %s", joined)
	}
	if len(r.stdinLog) != 1 || !strings.Contains(r.stdinLog[0], "iface enp2s0 inet dhcp") {
		t.Fatalf("persisted snippet = %#v", r.stdinLog)
	}
}

func TestRoutes(t *testing.T) {
	r := &fakeRunner{
		outputs: map[string]string{
			"ip -j route show": `[{"dst":"default","gateway":"10.62.0.1","dev":"enp1s0"},{"dst":"10.63.0.0/24","dev":"enp2s0","metric":50}]`,
		},
	}
	svc := NewService(r)

	routes, err := svc.ListRoutes()
	if err != nil {
		t.Fatalf("ListRoutes() error = %v", err)
	}
	if len(routes) != 2 || routes[0].Gateway != "10.62.0.1" || routes[1].Metric != "50" {
		t.Fatalf("routes = %#v", routes)
	}

	if err := svc.AddRoute("bad_dst", "10.0.0.1", "enp1s0"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected destination validation error, got %v", err)
	}
	if err := svc.AddRoute("10.63.0.0/24", "10.62.0.1", "enp2s0"); err != nil {
		t.Fatalf("AddRoute() error = %v", err)
	}
	if !strings.Contains(r.calls[len(r.calls)-1], "ip route add 10.63.0.0/24 via 10.62.0.1 dev enp2s0") {
		t.Fatalf("calls = %#v", r.calls)
	}

	if err := svc.DeleteRoute("10.63.0.0/24"); err != nil {
		t.Fatalf("DeleteRoute() error = %v", err)
	}
}

func TestFirewallRules(t *testing.T) {
	r := &fakeRunner{
		outputs: map[string]string{
			"nft list tables":                     "table inet fleet_filter",
			"nft -a list table inet fleet_filter": "table inet fleet_filter {\n\tchain input {\n\t\ttype filter hook input priority -10; policy accept;\n\t\ttcp dport 22 accept comment \"fleet_rule\" handle 5\n\t\tudp dport 53 drop comment \"fleet_rule\" handle 7\n\t}\n}",
		},
	}
	svc := NewService(r)

	rules, err := svc.ListFirewall()
	if err != nil {
		t.Fatalf("ListFirewall() error = %v", err)
	}
	if len(rules) != 2 || rules[0].Handle != "5" || rules[1].Handle != "7" {
		t.Fatalf("rules = %#v", rules)
	}

	if err := svc.AddFirewallRule(FirewallRequest{Port: 0, Proto: "tcp", Action: "accept"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected port validation error, got %v", err)
	}
	if err := svc.AddFirewallRule(FirewallRequest{Port: 443, Proto: "sctp", Action: "accept"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected proto validation error, got %v", err)
	}
	if err := svc.AddFirewallRule(FirewallRequest{Port: 443, Proto: "TCP", Action: "Accept", Comment: "web-ui"}); err != nil {
		t.Fatalf("AddFirewallRule() error = %v", err)
	}
	if !strings.Contains(r.calls[len(r.calls)-1], `tcp dport 443 accept comment "web-ui"`) {
		t.Fatalf("calls = %#v", r.calls)
	}

	if err := svc.DeleteFirewallRule("abc"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected handle validation error, got %v", err)
	}
	if err := svc.DeleteFirewallRule("7"); err != nil {
		t.Fatalf("DeleteFirewallRule() error = %v", err)
	}
}

func TestScanWiFiParsesIwOutput(t *testing.T) {
	r := &fakeRunner{
		outputs: map[string]string{
			"iw dev wlo1 scan": "BSS ab:cd:ef:01:capability: ESS\n\tSSID: Home-Wifi\n\tfreq: 2437\n\tsignal: -42.00 dBm\nBSS ab:cd:ef:02\n\tSSID: Open-Net\n\tfreq: 5180\n\tsignal: -55.00 dBm",
		},
	}
	svc := NewService(r)

	if _, err := svc.ScanWiFi("wlo1; rm"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected name validation error, got %v", err)
	}

	networks, err := svc.ScanWiFi("wlo1")
	if err != nil {
		t.Fatalf("ScanWiFi() error = %v", err)
	}
	if len(networks) != 2 {
		t.Fatalf("networks = %#v", networks)
	}
	if networks[0].SSID != "Home-Wifi" || !networks[0].Secure || networks[0].Signal != -42 {
		t.Fatalf("network0 = %#v", networks[0])
	}
	if networks[1].Secure {
		t.Fatalf("open network marked secure: %#v", networks[1])
	}
}

func TestSnapshotCollectsEverything(t *testing.T) {
	r := &fakeRunner{
		outputs: map[string]string{
			"ip -j addr show":                     `[{"ifname":"enp1s0","operstate":"UP","link_type":"ether","addr_info":[{"family":"inet","local":"10.62.0.50","prefixlen":24}]}]`,
			"ip -j route show":                    `[{"dst":"default","gateway":"10.62.0.1","dev":"enp1s0"},{"dst":"10.63.0.0/24","gateway":"10.62.0.1","dev":"enp1s0","metric":100}]`,
			"nft -a list table inet fleet_filter": "table inet fleet_filter { # handle 3\n\tchain input { # handle 1\n\t\ttype filter hook input priority filter; policy accept;\n\t\ttcp dport 22 accept comment \"fleet_rule\" # handle 7\n\t}\n}",
			"iw dev wlo1 scan":                    "BSS aa:bb(driver=iwlwifi)\n\tfreq: 2437\n\tsignal: -40.00 dBm\n\tSSID: Office\n\tRSN: IEEE 802.11i\n",
			"hostname ":                           "n100-server",
		},
	}
	svc := NewService(r)

	snapshot, err := svc.Snapshot(context.Background(), "wlo1")
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if snapshot.Host != "n100-server" || snapshot.WifiDevice != "wlo1" {
		t.Fatalf("snapshot identity = %+v", snapshot)
	}
	if len(snapshot.NICs) != 1 || len(snapshot.Routes) != 2 {
		t.Fatalf("snapshot nics/routes = %#v / %#v", snapshot.NICs, snapshot.Routes)
	}
	if len(snapshot.Firewall) != 1 || snapshot.Firewall[0].Handle != "7" {
		t.Fatalf("snapshot firewall = %#v", snapshot.Firewall)
	}
	if len(snapshot.WifiScan) != 1 || snapshot.WifiScan[0].SSID != "Office" {
		t.Fatalf("snapshot wifi scan = %#v", snapshot.WifiScan)
	}
	if len(snapshot.Capabilities) == 0 {
		t.Fatalf("snapshot capabilities empty")
	}
}

func TestSnapshotToleratesOptionalFailures(t *testing.T) {
	r := &fakeRunner{
		outputs: map[string]string{
			"ip -j addr show": `[{"ifname":"enp1s0","operstate":"UP","link_type":"ether","addr_info":[]}]`,
		},
		failOn: "route show",
	}
	svc := NewService(r)

	snapshot, err := svc.Snapshot(context.Background(), "wlo1")
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	// Routes and firewall errors are tolerated; wifi scan failure too.
	if len(snapshot.Routes) != 0 || len(snapshot.Firewall) != 0 || len(snapshot.WifiScan) != 0 {
		t.Fatalf("snapshot should be partially empty: %#v", snapshot)
	}
}

func TestSnapshotPropagatesNICFailure(t *testing.T) {
	svc := NewService(&fakeRunner{failOn: "ip -j addr show"})

	if _, err := svc.Snapshot(context.Background(), ""); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Snapshot() error = %v, want ErrUnavailable", err)
	}
}

func TestWifiConnectWritesConfigAndLeases(t *testing.T) {
	r := &fakeRunner{
		outputs: map[string]string{
			"wpa_supplicant -v": "wpa_supplicant v2.10",
		},
	}
	svc := NewService(r)

	if err := svc.WifiConnect("wlo1", "OfficeWiFi", "secret"); err != nil {
		t.Fatalf("WifiConnect() error = %v", err)
	}

	if len(r.stdinLog) != 1 || !strings.Contains(r.stdinLog[0], `ssid="OfficeWiFi"`) {
		t.Fatalf("stdin = %#v", r.stdinLog)
	}
	joined := strings.Join(r.calls, "\n")
	for _, want := range []string{
		"wpa_supplicant -B -i wlo1 -c /etc/wpa_supplicant/fleet.conf",
		"ip link set dev wlo1 up",
		"dhcpcd -1 --nohook resolv.conf wlo1",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("calls missing %q: %s", want, joined)
		}
	}
}

func TestWifiConnectValidation(t *testing.T) {
	svc := NewService(&fakeRunner{})

	if err := svc.WifiConnect("bad name", "ssid", ""); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("bad device error = %v", err)
	}
	if err := svc.WifiConnect("wlo1", " ", ""); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("empty ssid error = %v", err)
	}
	if err := svc.WifiConnect("wlo1", "ssid\"", ""); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("quote injection error = %v", err)
	}
}

func TestWifiConnectRequiresSupplicant(t *testing.T) {
	svc := NewService(&fakeRunner{failOn: "wpa_supplicant -v"})

	if err := svc.WifiConnect("wlo1", "ssid", ""); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("WifiConnect() error = %v, want ErrUnavailable", err)
	}
}

func TestWifiConnectPropagatesRunFailures(t *testing.T) {
	svc := NewService(&fakeRunner{
		outputs: map[string]string{"wpa_supplicant -v": "ok"},
		failOn:  "wpa_supplicant -B",
	})
	if err := svc.WifiConnect("wlo1", "ssid", ""); err == nil {
		t.Fatalf("WifiConnect() wpa failure error = nil")
	}

	svc = NewService(&fakeRunner{
		outputs: map[string]string{"wpa_supplicant -v": "ok"},
		failOn:  "ip link set",
	})
	if err := svc.WifiConnect("wlo1", "ssid", ""); err == nil {
		t.Fatalf("WifiConnect() link failure error = nil")
	}

	svc = NewService(&fakeRunner{
		outputs: map[string]string{"wpa_supplicant -v": "ok"},
		failOn:  "dhcpcd",
	})
	if err := svc.WifiConnect("wlo1", "ssid", ""); err == nil {
		t.Fatalf("WifiConnect() dhcp failure error = nil")
	}
}

func TestWifiLinkReadsAssociation(t *testing.T) {
	r := &fakeRunner{
		outputs: map[string]string{
			"iw dev wlo1 link": "Connected to aa:bb (on wlo1)\n\tSSID: Office\n\tfreq: 5180.0\n\tsignal: -55.00 dBm\n",
			"iw dev wlo1 info": "Interface wlo1\n\tssid Office\n\ttype managed\n",
		},
	}
	svc := NewService(r)

	link := svc.wifiLink("wlo1")
	if link == nil || !link.Connected || link.SSID != "Office" || link.Signal != -55 || link.Frequency != 5180 {
		t.Fatalf("wifiLink() = %#v", link)
	}
}

func TestWifiLinkDown(t *testing.T) {
	svc := NewService(&fakeRunner{failOn: "iw dev wlo1 link"})

	link := svc.wifiLink("wlo1")
	if link == nil || link.Connected {
		t.Fatalf("wifiLink() down = %#v", link)
	}
}

func TestNewServiceDefaultsToSudoRunner(t *testing.T) {
	svc := NewService(nil)
	if _, ok := svc.run.(SudoRunner); !ok {
		t.Fatalf("NewService(nil) runner = %T, want SudoRunner", svc.run)
	}
}

func TestSudoRunnerInvokesSudo(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "sudo")
	// The fake sudo echoes its arguments and stdin so the test can assert both
	// code paths without touching the host.
	body := "#!/bin/sh\necho \"sudo $*\"\ncat\n"
	if err := os.WriteFile(script, []byte(body), 0o600); err != nil {
		t.Fatalf("write fake sudo: %v", err)
	}
	// #nosec G302 -- the fake sudo binary must be executable for the test
	if err := os.Chmod(script, 0o700); err != nil {
		t.Fatalf("chmod fake sudo: %v", err)
	}

	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	out, err := SudoRunner{}.Run("hostname")
	if err != nil {
		t.Fatalf("SudoRunner.Run() error = %v", err)
	}
	// The runner passes the resolved absolute path so sudoers path whitelists match.
	if !strings.Contains(out, "sudo -n ") || !strings.Contains(out, "hostname") {
		t.Fatalf("SudoRunner.Run() output = %q", out)
	}
	if !strings.Contains(out, "/hostname") {
		t.Fatalf("SudoRunner.Run() did not resolve hostname to an absolute path: %q", out)
	}

	if err := (SudoRunner{}).RunStdin("tee", []string{"/tmp/x"}, "content"); err != nil {
		t.Fatalf("SudoRunner.RunStdin() error = %v", err)
	}
}

func TestDedupeWiFiNetworksKeepsStrongest(t *testing.T) {
	networks := dedupeWiFiNetworks([]WiFiNetwork{
		{SSID: "Office", Signal: -70, Secure: true},
		{SSID: "Office", Signal: -45, Secure: true},
		{SSID: "Guest", Signal: -60},
	})
	if len(networks) != 2 {
		t.Fatalf("dedupe = %#v", networks)
	}

	best := map[string]int{}
	for _, network := range networks {
		best[network.SSID] = network.Signal
	}
	if best["Office"] != -45 || best["Guest"] != -60 {
		t.Fatalf("dedupe signals = %#v", best)
	}
}

func TestListNICsUsesAdminStateFlags(t *testing.T) {
	r := &fakeRunner{
		outputs: map[string]string{
			// wlo1 is administratively up but unassociated: operstate down.
			"ip -j addr show":  `[{"ifname":"wlo1","operstate":"DOWN","flags":["BROADCAST","MULTICAST","UP"],"link_type":"wlan","addr_info":[]}]`,
			"iw dev wlo1 link": "Not connected.",
		},
	}
	svc := NewService(r)

	nics, err := svc.ListNICs(context.Background())
	if err != nil {
		t.Fatalf("ListNICs() error = %v", err)
	}
	if len(nics) != 1 || nics[0].State != "up" {
		t.Fatalf("admin state = %#v", nics)
	}
	if nics[0].WiFi == nil || nics[0].WiFi.Connected {
		t.Fatalf("Not connected must map to Connected=false: %#v", nics[0].WiFi)
	}
}

func TestRouteRunnerFailures(t *testing.T) {
	svc := NewService(&fakeRunner{failOn: "ip route add"})
	if err := svc.AddRoute("10.63.0.0/24", "10.62.0.1", "enp1s0"); err == nil {
		t.Fatal("AddRoute() failure error = nil")
	}

	svc = NewService(&fakeRunner{failOn: "ip route del"})
	if err := svc.DeleteRoute("10.63.0.0/24"); err == nil {
		t.Fatal("DeleteRoute() failure error = nil")
	}

	svc = NewService(&fakeRunner{})
	if err := svc.AddRoute("10.63.0.0/24", "10.62.0.1", "bad name"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("AddRoute() bad device error = %v", err)
	}
	if err := svc.AddRoute("bad", "10.62.0.1", "enp1s0"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("AddRoute() bad destination error = %v", err)
	}
	if err := svc.DeleteRoute("bad"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("DeleteRoute() bad destination error = %v", err)
	}
}
