package nodenet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type persistRunner struct {
	calls  []string
	err    error
	failOn string
}

func (r *persistRunner) Run(name string, args ...string) (string, error) {
	call := name + " " + strings.Join(args, " ")
	r.calls = append(r.calls, call)
	if r.failOn != "" && strings.Contains(call, r.failOn) {
		return "", r.err
	}
	return "", nil
}

func (r *persistRunner) RunStdin(string, []string, string) error { return nil }

func TestDetectManager(t *testing.T) {
	cases := []struct {
		name  string
		setup func(string) error
		want  Manager
	}{
		{"netplan", func(root string) error { return os.MkdirAll(filepath.Join(root, "etc/netplan"), 0o750) }, ManagerNetplan},
		{"networkd", func(root string) error { return os.MkdirAll(filepath.Join(root, "etc/systemd/network"), 0o750) }, ManagerNetworkd},
		{"ifupdown", func(root string) error {
			if err := os.MkdirAll(filepath.Join(root, "etc/network"), 0o750); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(root, "etc/network/interfaces"), []byte("auto lo\n"), 0o644) //nolint:gosec // test fixture
		}, ManagerIfupdown},
		{"unknown", func(string) error { return nil }, ManagerUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := tc.setup(root); err != nil {
				t.Fatal(err)
			}
			if got := DetectManager(root); got != tc.want {
				t.Fatalf("DetectManager() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPersistAddressAdapters(t *testing.T) {
	req := AddressRequest{Address: "10.0.0.50/24", Gateway: "10.0.0.1"}

	cases := []struct {
		manager Manager
		path    string
		want    []string
	}{
		{ManagerIfupdown, "etc/network/interfaces.d/fleet-eth0", []string{"iface eth0 inet static", "address 10.0.0.50", "netmask 255.255.255.0", "gateway 10.0.0.1"}},
		{ManagerNetworkd, "etc/systemd/network/10-fleet-eth0.network", []string{"Name=eth0", "Address=10.0.0.50/24", "Gateway=10.0.0.1"}},
		{ManagerNetplan, "etc/netplan/90-fleet-eth0.yaml", []string{"eth0:", "10.0.0.50/24", "via: 10.0.0.1"}},
		{ManagerWicked, "etc/sysconfig/network/ifcfg-eth0", []string{"BOOTPROTO='static'", "IPADDR='10.0.0.50/24'", "GATEWAY='10.0.0.1'"}},
	}
	for _, tc := range cases {
		t.Run(string(tc.manager), func(t *testing.T) {
			root := t.TempDir()
			runner := &persistRunner{}
			if err := PersistAddress(runner, root, tc.manager, "eth0", req); err != nil {
				t.Fatalf("PersistAddress() error = %v", err)
			}
			data, err := os.ReadFile(filepath.Join(root, tc.path)) //nolint:gosec // test fixture
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range tc.want {
				if !strings.Contains(string(data), want) {
					t.Fatalf("file missing %q:\n%s", want, data)
				}
			}
		})
	}
}

func TestPersistAddressDHCPAndNetworkManager(t *testing.T) {
	root := t.TempDir()
	runner := &persistRunner{}
	if err := PersistAddress(runner, root, ManagerIfupdown, "eth0", AddressRequest{DHCP: true}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(root, "etc/network/interfaces.d/fleet-eth0")) //nolint:gosec // test fixture
	if !strings.Contains(string(data), "iface eth0 inet dhcp") {
		t.Fatalf("dhcp snippet = %s", data)
	}

	runner = &persistRunner{failOn: "connection show", err: os.ErrNotExist}
	if err := PersistAddress(runner, root, ManagerNetworkMgr, "eth0", AddressRequest{Address: "10.0.0.50/24", Gateway: "10.0.0.1"}); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(runner.calls, "\n")
	if !strings.Contains(joined, "connection add") || !strings.Contains(joined, "ipv4.addresses 10.0.0.50/24") {
		t.Fatalf("nmcli calls = %s", joined)
	}
}

func TestPersistAddressValidation(t *testing.T) {
	root := t.TempDir()
	if err := PersistAddress(nil, root, ManagerIfupdown, "bad name", AddressRequest{DHCP: true}); err == nil {
		t.Fatal("bad interface expected error")
	}
	if err := PersistAddress(nil, root, ManagerIfupdown, "eth0", AddressRequest{}); err == nil {
		t.Fatal("missing address expected error")
	}
	if err := PersistAddress(nil, root, ManagerIfupdown, "eth0", AddressRequest{Address: "not-ip"}); err == nil {
		t.Fatal("bad address expected error")
	}
}

func TestPersistNetworkManagerExistingProfile(t *testing.T) {
	root := t.TempDir()
	runner := &persistRunner{}
	if err := PersistAddress(runner, root, ManagerNetworkMgr, "eth0", AddressRequest{Address: "10.0.0.50/24", Gateway: "10.0.0.1"}); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(runner.calls, "\n")
	if !strings.Contains(joined, "connection modify") || !strings.Contains(joined, "connection up") {
		t.Fatalf("nmcli calls = %s", joined)
	}
}

func TestPersistAddressWriteError(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "etc"), []byte("not-a-directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := PersistAddress(nil, root, ManagerIfupdown, "eth0", AddressRequest{DHCP: true}); err == nil {
		t.Fatal("PersistAddress() expected write error")
	}
}
