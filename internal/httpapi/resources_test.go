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
		{Name: "wan0", Managed: false},                            // no locations reported
	}
	slaves := map[string]map[string]string{"m1": {"eth0": "bond0"}}
	lookup := func(member string) (map[string]string, error) { return slaves[member], nil }

	out := filterBondSlaveNetworks(networks, lookup)
	if len(out) != 3 {
		t.Fatalf("filtered = %#v", out)
	}
	for _, network := range out {
		if network.Name == "eth0" {
			t.Fatalf("bond slave leaked through: %#v", out)
		}
		if network.Name == "incusbr0" && !network.Managed {
			t.Fatalf("managed network altered: %#v", network)
		}
	}

	// A failing lookup (node access unconfigured, member offline) degrades to
	// keeping every NIC visible.
	failing := func(string) (map[string]string, error) { return nil, errors.New("ssh down") }
	if out := filterBondSlaveNetworks(networks, failing); len(out) != 4 {
		t.Fatalf("degraded filter = %#v", out)
	}
}
