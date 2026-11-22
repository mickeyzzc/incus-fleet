package nodenet

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateOverlayName(t *testing.T) {
	for _, name := range []string{"c62", "overlay-1", "a", "abc-def-123"} {
		if err := ValidateOverlayName(name); err != nil {
			t.Fatalf("ValidateOverlayName(%q) error = %v", name, err)
		}
	}
	for _, name := range []string{"", "Upper", "with space", "../etc", "a_b", "-lead", "trail-", strings.Repeat("x", 33)} {
		if err := ValidateOverlayName(name); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("ValidateOverlayName(%q): expected invalid, got %v", name, err)
		}
	}
}

func TestValidateDHCPConfig(t *testing.T) {
	valid := DHCPConfig{
		Name: "c62", Interface: "fleetbr0", RangeStart: "198.51.100.10", RangeEnd: "198.51.100.200",
		Gateway: "198.51.100.1", Netmask: "255.255.255.0",
	}
	if err := ValidateDHCPConfig(valid); err != nil {
		t.Fatalf("ValidateDHCPConfig() error = %v", err)
	}
	for _, tc := range []struct {
		name string
		cfg  DHCPConfig
	}{
		{"bad overlay", DHCPConfig{Name: "Bad Name", Interface: "fleetbr0", RangeStart: "198.51.100.10", RangeEnd: "198.51.100.20", Gateway: "198.51.100.1"}},
		{"bad interface", DHCPConfig{Name: "c62", Interface: "fleetbr0;rm", RangeStart: "198.51.100.10", RangeEnd: "198.51.100.20", Gateway: "198.51.100.1"}},
		{"bad start", DHCPConfig{Name: "c62", Interface: "fleetbr0", RangeStart: "x", RangeEnd: "198.51.100.20", Gateway: "198.51.100.1"}},
		{"reversed range", DHCPConfig{Name: "c62", Interface: "fleetbr0", RangeStart: "198.51.100.50", RangeEnd: "198.51.100.20", Gateway: "198.51.100.1"}},
		{"bad gateway", DHCPConfig{Name: "c62", Interface: "fleetbr0", RangeStart: "198.51.100.10", RangeEnd: "198.51.100.20", Gateway: "nope"}},
		{"ipv6", DHCPConfig{Name: "c62", Interface: "fleetbr0", RangeStart: "fd00::1", RangeEnd: "fd00::20", Gateway: "fd00::1"}},
		{"bad netmask", DHCPConfig{Name: "c62", Interface: "fleetbr0", RangeStart: "198.51.100.10", RangeEnd: "198.51.100.20", Gateway: "198.51.100.1", Netmask: "x"}},
		{"bad dns", DHCPConfig{Name: "c62", Interface: "fleetbr0", RangeStart: "198.51.100.10", RangeEnd: "198.51.100.20", Gateway: "198.51.100.1", DNS: "x"}},
	} {
		if err := ValidateDHCPConfig(tc.cfg); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("%s: expected invalid, got %v", tc.name, err)
		}
	}
}

func TestDHCPConfigContent(t *testing.T) {
	content := DHCPConfigContent(DHCPConfig{
		Name: "c62", Interface: "fleetbr0", RangeStart: "198.51.100.10", RangeEnd: "198.51.100.200",
		Gateway: "198.51.100.1",
	})
	for _, want := range []string{
		"interface=fleetbr0",
		"dhcp-range=198.51.100.10,198.51.100.200,255.255.255.0",
		"dhcp-option=option:router,198.51.100.1",
		"dhcp-option=option:dns-server,198.51.100.1",
		"dhcp-authoritative",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("missing %q in:\n%s", want, content)
		}
	}

	custom := DHCPConfigContent(DHCPConfig{
		Name: "c62", Interface: "fleetbr0", RangeStart: "198.51.100.10", RangeEnd: "198.51.100.200",
		Gateway: "198.51.100.1", DNS: "1.1.1.1",
	})
	if !strings.Contains(custom, "option:dns-server,1.1.1.1") {
		t.Fatalf("custom dns missing:\n%s", custom)
	}
}

func TestDHCPUnitContent(t *testing.T) {
	unit := DHCPUnitContent("c62")
	for _, want := range []string{
		"fleet-overlay-dhcp-c62.service",
		"ExecStart=/usr/sbin/dnsmasq --keep-in-foreground",
		"--conf-file=/etc/dnsmasq.d/fleet-overlay-c62.conf",
	} {
		if !strings.Contains(unit, want) {
			t.Fatalf("missing %q in:\n%s", want, unit)
		}
	}
}

func TestWriteAndStartStopDHCP(t *testing.T) {
	r := &fakeRunner{}
	svc := NewService(r)
	cfg := DHCPConfig{Name: "c62", Interface: "fleetbr0", RangeStart: "198.51.100.10", RangeEnd: "198.51.100.200", Gateway: "198.51.100.1"}

	if err := svc.WriteDHCPConfig(cfg); err != nil {
		t.Fatalf("WriteDHCPConfig() error = %v", err)
	}
	if len(r.stdinLog) != 2 {
		t.Fatalf("stdin = %#v", r.stdinLog)
	}
	if !strings.Contains(strings.Join(r.calls, "\n"), "/etc/dnsmasq.d/fleet-overlay-c62.conf") {
		t.Fatalf("calls = %#v", r.calls)
	}

	if err := svc.StartDHCP("c62"); err != nil {
		t.Fatalf("StartDHCP() error = %v", err)
	}
	if err := svc.StopDHCP("c62"); err != nil {
		t.Fatalf("StopDHCP() error = %v", err)
	}
	joined := strings.Join(r.calls, "\n")
	for _, want := range []string{
		"systemctl daemon-reload",
		"systemctl enable --now fleet-overlay-dhcp-c62",
		"systemctl disable --now fleet-overlay-dhcp-c62",
		"rm -f /etc/dnsmasq.d/fleet-overlay-c62.conf",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in:\n%s", want, joined)
		}
	}

	if err := svc.WriteDHCPConfig(DHCPConfig{Name: "bad name"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected validation error, got %v", err)
	}
	if err := svc.StartDHCP("bad name"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected validation error, got %v", err)
	}
}

func TestStopDHCPToleratesMissingUnit(t *testing.T) {
	r := &fakeRunner{failOn: "systemctl disable"}
	svc := NewService(r)
	// The fake returns "boom: ..." for the disable call, which is not a
	// missing-unit error, so the failure must surface.
	if err := svc.StopDHCP("c62"); err == nil {
		t.Fatal("expected error for non-missing failure")
	}
}

func TestNATLifecycle(t *testing.T) {
	r := &fakeRunner{}
	svc := NewService(r)
	cfg := NATConfig{Source: "198.51.100.0/24", OutDevice: "enp1s0", Comment: "c62"}

	if err := svc.AddNAT(cfg); err != nil {
		t.Fatalf("AddNAT() error = %v", err)
	}
	joined := strings.Join(r.calls, "\n")
	for _, want := range []string{
		"nft add table ip fleet_nat_c62",
		"nft add chain ip fleet_nat_c62 postrouting",
		`nft add rule ip fleet_nat_c62 postrouting ip saddr 198.51.100.0/24 oifname "enp1s0" masquerade`,
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in:\n%s", want, joined)
		}
	}

	if err := svc.DeleteNAT("c62"); err != nil {
		t.Fatalf("DeleteNAT() error = %v", err)
	}
	if !strings.Contains(strings.Join(r.calls, "\n"), "nft delete table ip fleet_nat_c62") {
		t.Fatalf("calls = %#v", r.calls)
	}

	if err := svc.AddNAT(NATConfig{Source: "nope", Comment: "c62"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected validation error, got %v", err)
	}
	if err := svc.AddNAT(NATConfig{Source: "10.0.0.0/8", Comment: "Bad Name"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected overlay name validation error, got %v", err)
	}
}

func TestNATTableNaming(t *testing.T) {
	table, err := natTable("my-overlay")
	if err != nil {
		t.Fatalf("natTable() error = %v", err)
	}
	if table != "fleet_nat_my_overlay" {
		t.Fatalf("table = %q", table)
	}
	if _, err := natTable("bad name"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected validation error, got %v", err)
	}
}

func TestSudoersContent(t *testing.T) {
	content := SudoersContent("incus-fleet")
	for _, want := range []string{
		"incus-fleet ALL=(root) NOPASSWD: \\",
		"/usr/bin/ip",
		"/usr/bin/nft",
		"/usr/bin/systemctl",
		"/usr/bin/tee /etc/dnsmasq.d/fleet-overlay-*.conf",
		"/usr/bin/tee /etc/systemd/system/fleet-overlay-dhcp-*.service",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("missing %q in:\n%s", want, content)
		}
	}
	// The final rule must not carry a trailing continuation.
	if strings.HasSuffix(strings.TrimSpace(content), "\\") {
		t.Fatalf("sudoers must not end with a continuation: %q", content)
	}
}

// TestNetmaskDefault covers the empty-netmask fallback.
func TestNetmaskDefault(t *testing.T) {
	if got := netmaskOr24(""); got != "255.255.255.0" {
		t.Fatalf("netmaskOr24(\"\") = %q", got)
	}
	if got := netmaskOr24("255.255.0.0"); got != "255.255.0.0" {
		t.Fatalf("netmaskOr24(custom) = %q", got)
	}
}

// TestLinkExistsNonExistent covers the missing-link branch, which callers rely
// on for idempotent apply.
func TestLinkExistsNonExistent(t *testing.T) {
	// iproute2 reports a missing link on stderr with "does not exist", which
	// LinkExists turns into a clean false rather than an error.
	exists, err := NewService(missingLinkRunner{}).LinkExists("fleetbr0")
	if exists || err != nil {
		t.Fatalf("LinkExists() = %v, %v", exists, err)
	}
}

// missingLinkRunner mimics `ip link show <missing>`.
type missingLinkRunner struct{}

func (missingLinkRunner) Run(string, ...string) (string, error) {
	return `Device "fleetbr0" does not exist.`, errors.New("exit status 1")
}
func (missingLinkRunner) RunStdin(string, []string, string) error { return nil }

// TestValidateOverlayNameRejectsTrailingDash covers the name-boundary rule.
func TestValidateOverlayNameRejectsTrailingDash(t *testing.T) {
	for _, name := range []string{"a-", "-a", "a b", "A"} {
		if err := ValidateOverlayName(name); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("ValidateOverlayName(%q) = %v", name, err)
		}
	}
}

// TestValidateBridgeAddressBoundaries covers the MTU range edges.
func TestValidateBridgeAddressBoundaries(t *testing.T) {
	if err := ValidateBridge(BridgeConfig{Name: "fleetbr0", MTU: 576}); err != nil {
		t.Fatalf("MTU 576 should be accepted: %v", err)
	}
	if err := ValidateBridge(BridgeConfig{Name: "fleetbr0", MTU: 9000}); err != nil {
		t.Fatalf("MTU 9000 should be accepted: %v", err)
	}
	if err := ValidateBridge(BridgeConfig{Name: "fleetbr0", MTU: 575}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("MTU 575 should be rejected: %v", err)
	}
	if err := ValidateBridge(BridgeConfig{Name: "fleetbr0", MTU: 9001}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("MTU 9001 should be rejected: %v", err)
	}
}
