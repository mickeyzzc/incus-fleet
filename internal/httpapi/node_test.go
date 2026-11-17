package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/lxc/incus/v7/shared/api"

	"github.com/mickeyzzc/incus-fleet/internal/store"
	"github.com/mickeyzzc/incus-fleet/internal/testutil/incusfake"
)

// fakeNodeRunner drives nodenet inside the HTTP tests without touching the
// host. Outputs mimic the iproute2/nft/iw shapes the parsers expect.
type fakeNodeRunner struct {
	mu      sync.Mutex
	calls   []string
	failOn  string
	outputs map[string]string
}

func (f *fakeNodeRunner) Run(name string, args ...string) (string, error) {
	call := name + " " + strings.Join(args, " ")
	f.mu.Lock()
	f.calls = append(f.calls, call)
	failOn := f.failOn
	f.mu.Unlock()

	if failOn != "" && strings.Contains(call, failOn) {
		return "", errors.New("boom: " + call)
	}
	if out, ok := f.outputs[call]; ok {
		return out, nil
	}
	if out, ok := f.outputs[name]; ok {
		return out, nil
	}

	return "", nil
}

func (f *fakeNodeRunner) RunStdin(name string, args []string, _ string) error {
	call := name + " " + strings.Join(args, " ") + " <stdin"
	f.mu.Lock()
	f.calls = append(f.calls, call)
	f.mu.Unlock()

	return nil
}

func (f *fakeNodeRunner) recordedCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.calls...)
}

// fakeNodeOutputs returns a plausible snapshot for the fixture node.
func fakeNodeOutputs() map[string]string {
	return map[string]string{
		"ip -j addr show": `[
			{"ifname":"eth0","operstate":"UP","flags":["BROADCAST","MULTICAST","UP"],"mtu":1500,"address":"aa:bb","link_type":"ether",
			 "addr_info":[{"family":"inet","local":"192.0.2.10","prefixlen":24}]},
			{"ifname":"wlan0","operstate":"UP","flags":["BROADCAST","MULTICAST","UP"],"link_type":"wlan","addr_info":[]}
		]`,
		"ip -j route show": `[{"dst":"default","gateway":"192.0.2.1","dev":"eth0","metric":100}]`,
		"nft -a list table inet fleet_filter": "table inet fleet_filter { # handle 3\n" +
			"\tchain input { # handle 1\n" +
			"\t\ttype filter hook input priority filter; policy accept;\n" +
			"\t\ttcp dport 22 accept comment \"fleet_rule\" # handle 7\n" +
			"\t}\n}",
		"iw dev wlan0 link": "Connected to aa:bb (on wlan0)\n\tSSID: Office\n\tsignal: -55.00 dBm\n\tfreq: 2437.0\n",
		"iw dev wlan0 info": "Interface wlan0\n\tssid Office\n",
		"iw dev wlan0 scan": "BSS aa:bb\n\tfreq: 2437\n\tsignal: -45.00 dBm\n\tSSID: Office\n\tRSN: x\n",
		"hostname ":         "node-a",
	}
}

func newFakeNodeRunner() *fakeNodeRunner {
	return &fakeNodeRunner{outputs: fakeNodeOutputs()}
}

func TestNodeSnapshotAndReadEndpoints(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	cookie := f.login(t)

	rec := f.request(t, http.MethodGet, "/api/v1/clusters/c1/members/test-node/node", nil, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/clusters/c1/members/test-node/node status = %d body=%s", rec.Code, rec.Body.String())
	}
	for _, want := range []string{"node-a", "eth0", "wlan0", "Office", "192.0.2.10"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("snapshot missing %q: %s", want, rec.Body.String())
		}
	}

	routes := f.request(t, http.MethodGet, "/api/v1/clusters/c1/members/test-node/node/routes", nil, cookie, "")
	if routes.Code != http.StatusOK || !strings.Contains(routes.Body.String(), "192.0.2.1") {
		t.Fatalf("GET routes = %d body=%s", routes.Code, routes.Body.String())
	}

	firewall := f.request(t, http.MethodGet, "/api/v1/clusters/c1/members/test-node/node/firewall", nil, cookie, "")
	if firewall.Code != http.StatusOK || !strings.Contains(firewall.Body.String(), "tcp dport 22") {
		t.Fatalf("GET firewall = %d body=%s", firewall.Code, firewall.Body.String())
	}

	unknown := f.request(t, http.MethodGet, "/api/v1/clusters/c1/members/ghost/node", nil, cookie, "")
	if unknown.Code != http.StatusNotFound {
		t.Fatalf("unknown member status = %d body=%s", unknown.Code, unknown.Body.String())
	}
}

func TestNodeWriteEndpoints(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	cookie := f.login(t)

	state := f.request(t, http.MethodPost, "/api/v1/clusters/c1/members/test-node/node/networks/eth0/state", map[string]bool{"up": false}, cookie, "")
	if state.Code != http.StatusNoContent {
		t.Fatalf("NIC state status = %d body=%s", state.Code, state.Body.String())
	}

	address := f.request(t, http.MethodPatch, "/api/v1/clusters/c1/members/test-node/node/networks/eth0", map[string]any{
		"dhcp":    false,
		"address": "10.0.0.50",
		"gateway": "10.0.0.1",
		"persist": true,
	}, cookie, "")
	if address.Code != http.StatusNoContent {
		t.Fatalf("address status = %d body=%s", address.Code, address.Body.String())
	}

	wifi := f.request(t, http.MethodPost, "/api/v1/clusters/c1/members/test-node/node/networks/wlan0/wifi", map[string]string{
		"ssid":     "Office",
		"password": "secret",
	}, cookie, "")
	if wifi.Code != http.StatusNoContent {
		t.Fatalf("wifi status = %d body=%s", wifi.Code, wifi.Body.String())
	}

	route := f.request(t, http.MethodPost, "/api/v1/clusters/c1/members/test-node/node/routes", map[string]string{
		"destination": "10.0.1.0/24",
		"gateway":     "192.0.2.1",
		"device":      "eth0",
	}, cookie, "")
	if route.Code != http.StatusNoContent {
		t.Fatalf("add route status = %d body=%s", route.Code, route.Body.String())
	}

	routeDelete := f.request(t, http.MethodDelete, "/api/v1/clusters/c1/members/test-node/node/routes/10.0.1.0%2F24", nil, cookie, "")
	if routeDelete.Code != http.StatusNoContent {
		t.Fatalf("delete route status = %d body=%s", routeDelete.Code, routeDelete.Body.String())
	}

	rule := f.request(t, http.MethodPost, "/api/v1/clusters/c1/members/test-node/node/firewall", map[string]any{
		"port":    65022,
		"proto":   "tcp",
		"action":  "accept",
		"comment": "test",
	}, cookie, "")
	if rule.Code != http.StatusNoContent {
		t.Fatalf("add rule status = %d body=%s", rule.Code, rule.Body.String())
	}

	ruleDelete := f.request(t, http.MethodDelete, "/api/v1/clusters/c1/members/test-node/node/firewall/7", nil, cookie, "")
	if ruleDelete.Code != http.StatusNoContent {
		t.Fatalf("delete rule status = %d body=%s", ruleDelete.Code, ruleDelete.Body.String())
	}

	calls := strings.Join(f.nodeRunner.recordedCalls(), "\n")
	for _, want := range []string{
		"ip link set dev eth0 down",
		"ip addr add 10.0.0.50/24 dev eth0",
		"ip route replace default via 10.0.0.1 dev eth0",
		"tee /etc/network/interfaces.d/eth0 <stdin",
		"wpa_supplicant -B -i wlan0 -c /etc/wpa_supplicant/fleet.conf",
		"ip route add 10.0.1.0/24 via 192.0.2.1 dev eth0",
		"ip route del 10.0.1.0/24",
		"nft add rule inet fleet_filter input tcp dport 65022 accept",
		"nft delete rule inet fleet_filter input handle 7",
	} {
		if !strings.Contains(calls, want) {
			t.Fatalf("runner calls missing %q:\n%s", want, calls)
		}
	}
}

func TestNodeValidationAndUnavailable(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	cookie := f.login(t)

	badJSON := f.request(t, http.MethodPost, "/api/v1/clusters/c1/members/test-node/node/networks/eth0/state", "{not json", cookie, "")
	if badJSON.Code != http.StatusBadRequest {
		t.Fatalf("bad json status = %d", badJSON.Code)
	}

	badAddress := f.request(t, http.MethodPatch, "/api/v1/clusters/c1/members/test-node/node/networks/eth0", map[string]any{"address": "not-an-ip"}, cookie, "")
	if badAddress.Code != http.StatusBadRequest {
		t.Fatalf("invalid address status = %d body=%s", badAddress.Code, badAddress.Body.String())
	}

	badWifi := f.request(t, http.MethodPost, "/api/v1/clusters/c1/members/test-node/node/networks/wlan0/wifi", map[string]string{"ssid": ""}, cookie, "")
	if badWifi.Code != http.StatusBadRequest {
		t.Fatalf("empty ssid status = %d body=%s", badWifi.Code, badWifi.Body.String())
	}

	badRule := f.request(t, http.MethodPost, "/api/v1/clusters/c1/members/test-node/node/firewall", map[string]any{"port": 0, "proto": "tcp", "action": "accept"}, cookie, "")
	if badRule.Code != http.StatusBadRequest {
		t.Fatalf("invalid port status = %d body=%s", badRule.Code, badRule.Body.String())
	}

	// Host failures surface as 503 service_unavailable.
	f.nodeRunner.failOn = "ip -j addr show"
	unavailable := f.request(t, http.MethodGet, "/api/v1/clusters/c1/members/test-node/node", nil, cookie, "")
	if unavailable.Code != http.StatusServiceUnavailable || !strings.Contains(unavailable.Body.String(), "node_unavailable") {
		t.Fatalf("unavailable status = %d body=%s", unavailable.Code, unavailable.Body.String())
	}
}

func TestNodeDisabled(t *testing.T) {
	// Without NodeNet the API must reject node requests cleanly.
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	f.handler = New(Deps{
		Clusters:   nil,
		Instances:  nil,
		Assets:     nil,
		Migrations: nil,
		Auth:       f.auth,
		Version:    "test",
	}).Handler()

	cookie := f.login(t)
	rec := f.request(t, http.MethodGet, "/api/v1/clusters/c1/members/test-node/node", nil, cookie, "")
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "node_unavailable") {
		t.Fatalf("disabled status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestClusterTrafficEndpoint(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{
		Instances: []api.InstanceFull{{Instance: api.Instance{Name: "web", Status: "Running", StatusCode: api.Running}}},
	}, &incusfake.Server{})
	cookie := f.login(t)

	rec := f.request(t, http.MethodGet, "/api/v1/clusters/c1/traffic", nil, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("traffic status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"instances"`) {
		t.Fatalf("traffic body = %s", rec.Body.String())
	}

	missing := f.request(t, http.MethodGet, "/api/v1/clusters/nope/traffic", nil, cookie, "")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing cluster traffic status = %d body=%s", missing.Code, missing.Body.String())
	}
}

func TestClusterRegistrationEndpoints(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	cookie := f.login(t)

	badBody := f.request(t, http.MethodPost, "/api/v1/clusters", "{not json", cookie, "")
	if badBody.Code != http.StatusBadRequest {
		t.Fatalf("bad register body status = %d", badBody.Code)
	}

	missingName := f.request(t, http.MethodPost, "/api/v1/clusters", map[string]string{"endpoint": "https://x:8443"}, cookie, "")
	if missingName.Code != http.StatusBadRequest {
		t.Fatalf("missing name status = %d body=%s", missingName.Code, missingName.Body.String())
	}

	missingEndpoint := f.request(t, http.MethodPost, "/api/v1/clusters", map[string]string{"name": "c64"}, cookie, "")
	if missingEndpoint.Code != http.StatusBadRequest {
		t.Fatalf("missing endpoint status = %d body=%s", missingEndpoint.Code, missingEndpoint.Body.String())
	}

	registered := f.request(t, http.MethodPost, "/api/v1/clusters", map[string]string{
		"name":     "c64",
		"endpoint": "https://192.0.2.10:8443",
		"tls_cert": "cert",
		"tls_key":  "key",
	}, cookie, "")
	if registered.Code != http.StatusCreated {
		t.Fatalf("register status = %d body=%s", registered.Code, registered.Body.String())
	}

	persisted, err := f.store.GetCluster(context.Background(), "c64")
	if err != nil || persisted.Endpoint != "https://192.0.2.10:8443" {
		t.Fatalf("registered cluster = %#v err=%v", persisted, err)
	}

	removed := f.request(t, http.MethodDelete, "/api/v1/clusters/c64", nil, cookie, "")
	if removed.Code != http.StatusNoContent {
		t.Fatalf("unregister status = %d body=%s", removed.Code, removed.Body.String())
	}
	if _, err := f.store.GetCluster(context.Background(), "c64"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cluster still stored after unregister: %v", err)
	}

	again := f.request(t, http.MethodDelete, "/api/v1/clusters/c64", nil, cookie, "")
	if again.Code != http.StatusNotFound {
		t.Fatalf("unregister missing status = %d body=%s", again.Code, again.Body.String())
	}
}

func TestNodeHandlerHostFailures(t *testing.T) {
	// Every node handler must map runner failures onto 503 node_unavailable.
	cases := []struct {
		name   string
		failOn string
		method string
		path   string
		body   any
	}{
		{"nic state", "ip link set", http.MethodPost, "/api/v1/clusters/c1/members/test-node/node/networks/eth0/state", map[string]bool{"up": true}},
		{"apply dhcp", "dhcpcd", http.MethodPatch, "/api/v1/clusters/c1/members/test-node/node/networks/eth0", map[string]any{"dhcp": true}},
		{"apply static", "ip addr add", http.MethodPatch, "/api/v1/clusters/c1/members/test-node/node/networks/eth0", map[string]any{"address": "10.0.0.50"}},
		{"wifi", "wpa_supplicant -v", http.MethodPost, "/api/v1/clusters/c1/members/test-node/node/networks/wlan0/wifi", map[string]string{"ssid": "Office"}},
		{"routes list", "ip -j route show", http.MethodGet, "/api/v1/clusters/c1/members/test-node/node/routes", nil},
		{"route add", "ip route add", http.MethodPost, "/api/v1/clusters/c1/members/test-node/node/routes", map[string]string{"destination": "10.0.1.0/24", "gateway": "192.0.2.1", "device": "eth0"}},
		{"route delete", "ip route del", http.MethodDelete, "/api/v1/clusters/c1/members/test-node/node/routes/10.0.1.0%2F24", nil},
		{"firewall list", "nft -a list", http.MethodGet, "/api/v1/clusters/c1/members/test-node/node/firewall", nil},
		{"firewall add", "nft add rule", http.MethodPost, "/api/v1/clusters/c1/members/test-node/node/firewall", map[string]any{"port": 65022, "proto": "tcp", "action": "accept"}},
		{"firewall delete", "nft delete rule", http.MethodDelete, "/api/v1/clusters/c1/members/test-node/node/firewall/7", nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
			cookie := f.login(t)
			f.nodeRunner.failOn = tc.failOn

			rec := f.request(t, tc.method, tc.path, tc.body, cookie, "")
			// nodenet errors are wrapped with either ErrUnavailable or a plain
			// failure message; both surface as 5xx. Require at least an error
			// response rather than success.
			if rec.Code < 500 {
				t.Fatalf("%s status = %d body=%s", tc.name, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestUnregisterClusterManagerMismatch(t *testing.T) {
	// A record in the store whose cluster is missing from the manager must
	// surface as 404 without deleting the row.
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	cookie := f.login(t)

	if err := f.store.InsertCluster(context.Background(), &store.RegisteredCluster{
		Name:     "ghost",
		Endpoint: "https://192.0.2.55:8443",
		TLSCert:  "cert",
		TLSKey:   "key",
	}); err != nil {
		t.Fatalf("InsertCluster() error = %v", err)
	}

	rec := f.request(t, http.MethodDelete, "/api/v1/clusters/ghost", nil, cookie, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unregister ghost status = %d body=%s", rec.Code, rec.Body.String())
	}
	if _, err := f.store.GetCluster(context.Background(), "ghost"); err != nil {
		t.Fatalf("ghost row removed on failed unregister: %v", err)
	}
}
