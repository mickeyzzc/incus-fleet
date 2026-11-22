package httpapi

import (
	"errors"
	"testing"

	"github.com/mickeyzzc/incus-fleet/internal/instances"
)

func TestFilterBondSlaveNetworks(t *testing.T) {
	networks := []instances.Network{
		{Name: "incusbr0", Managed: true},
		{Name: "eth0", Managed: false, Locations: []string{"m1"}}, // bond slave on m1
		{Name: "eth1", Managed: false, Locations: []string{"m1"}}, // free NIC
		{Name: "eth2", Managed: false},                            // no locations: slave on m2, found via the member set
		{Name: "wan0", Managed: false},                            // no locations: not a slave anywhere
	}
	slaves := map[string]map[string]string{
		"m1": {"eth0": "bond0"},
		"m2": {"eth2": "bond1"},
	}
	lookup := func(member string) (map[string]string, error) { return slaves[member], nil }

	out := filterBondSlaveNetworks(networks, []string{"m1", "m2"}, lookup)
	if len(out) != 3 {
		t.Fatalf("filtered = %#v", out)
	}
	for _, network := range out {
		if network.Name == "eth0" || network.Name == "eth2" {
			t.Fatalf("bond slave leaked through: %#v", out)
		}
		if network.Name == "incusbr0" && !network.Managed {
			t.Fatalf("managed network altered: %#v", network)
		}
	}

	// A failing lookup (node access unconfigured, member offline) degrades to
	// keeping every NIC visible.
	failing := func(string) (map[string]string, error) { return nil, errors.New("ssh down") }
	if out := filterBondSlaveNetworks(networks, []string{"m1", "m2"}, failing); len(out) != 5 {
		t.Fatalf("degraded filter = %#v", out)
	}
}
