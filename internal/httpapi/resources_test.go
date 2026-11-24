package httpapi

import (
	"errors"
	"testing"

	"github.com/mickeyzzc/incus-fleet/internal/instances"
)

func TestEnrichNetworks(t *testing.T) {
	networks := []instances.Network{
		{Name: "incusbr0", Managed: true},
		{Name: "eth0", Managed: false, Locations: []string{"m1"}}, // bond slave on m1 (own locations)
		{Name: "eth1", Managed: false, Locations: []string{"m1"}}, // free NIC, located
		{Name: "eth2", Managed: false},                            // unlocated: slave on m2, backfilled then dropped
		{Name: "wan0", Managed: false},                            // unlocated: exists on m1 only, backfilled
		{Name: "lo", Managed: false},                              // loopback: never a macvlan parent
	}
	inventories := map[string]map[string]string{
		"m1": {"eth0": "bond0", "eth1": "", "wan0": ""},
		"m2": {"eth2": "bond1"},
	}
	lookup := func(member string) (map[string]string, error) { return inventories[member], nil }

	out := enrichNetworks(networks, []string{"m1", "m2"}, lookup)
	if len(out) != 3 {
		t.Fatalf("filtered = %#v", out)
	}
	seen := map[string]instances.Network{}
	for _, network := range out {
		seen[network.Name] = network
	}
	if _, ok := seen["eth0"]; ok {
		t.Fatalf("bond slave leaked through: %#v", out)
	}
	if _, ok := seen["lo"]; ok {
		t.Fatalf("loopback leaked through: %#v", out)
	}
	if _, ok := seen["eth2"]; ok {
		t.Fatalf("unlocated bond slave leaked through: %#v", out)
	}
	if got := seen["wan0"].Locations; len(got) != 1 || got[0] != "m1" {
		t.Fatalf("wan0 locations = %#v", got)
	}
	if got := seen["eth1"].Locations; len(got) != 1 || got[0] != "m1" {
		t.Fatalf("eth1 locations must be preserved: %#v", got)
	}
	if !seen["incusbr0"].Managed {
		t.Fatalf("managed network altered: %#v", seen["incusbr0"])
	}

	// A failing lookup (node access unconfigured, member offline) degrades to
	// keeping every NIC visible and unlocated.
	failing := func(string) (map[string]string, error) { return nil, errors.New("ssh down") }
	if out := enrichNetworks(networks, []string{"m1", "m2"}, failing); len(out) != 5 {
		t.Fatalf("degraded filter = %#v", out)
	}
}
