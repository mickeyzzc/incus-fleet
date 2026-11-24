package instances

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/lxc/incus/v7/shared/api"
)

const (
	// spicePortMin/Max bound the TCP port the VM's SPICE server may listen
	// on; the range avoids privileged and Incus-owned ports.
	spicePortMin = 1024
	spicePortMax = 65535

	// spiceListenAddr keeps QEMU's SPICE listener on loopback. The fleet daemon
	// bridges browser websockets onto it over the Incus API, so the port is
	// never exposed to the cluster network. Binding 0.0.0.0 would hand anyone
	// with L3 reachability an unauthenticated console.
	spiceListenAddr = "127.0.0.1"

	// spiceArgMarker identifies the QEMU argument this service manages.
	spiceArgMarker = "-spice "
	spiceDialDelay = 3 * time.Second
	spiceDialTry   = 3
)

// SPICEPortFor derives a stable SPICE TCP port from an instance name so
// different VMs land on different ports without central bookkeeping.
func SPICEPortFor(name string) int {
	sum := 0
	for _, r := range name {
		sum = (sum*31 + int(r)) & 0x7fffffff
	}

	return 5910 + sum%80
}

// spicePortPattern extracts the managed -spice listener port from raw.qemu.
var spicePortPattern = regexp.MustCompile(`-spice\s+[^\n]*\bport=(\d+)`)

// spicePortFromConfig returns the managed SPICE port stored in raw.qemu, or 0.
func spicePortFromConfig(raw string) int {
	match := spicePortPattern.FindStringSubmatch(raw)
	if len(match) != 2 {
		return 0
	}
	port, err := strconv.Atoi(match[1])
	if err != nil || port < spicePortMin || port > spicePortMax {
		return 0
	}

	return port
}

// allocateSPICEPort picks a currently free loopback port for a new VM. The
// deterministic SPICEPortFor hash only covers 80 ports and collides as soon as
// the fleet runs more than a handful of VMs; a launch that loses that race
// otherwise fails with "binding socket ... failed".
func allocateSPICEPort() (int, error) {
	listener, err := net.Listen("tcp", net.JoinHostPort(spiceListenAddr, "0"))
	if err != nil {
		return 0, err
	}
	defer listener.Close() //nolint:errcheck // probing only

	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("instances: SPICE listener has unexpected address type %T", listener.Addr())
	}

	return addr.Port, nil
}

// SPICEPrepare reports the result of arming a VM's SPICE TCP listener.
type SPICEPrepare struct {
	Port            int  `json:"port"`
	Applied         bool `json:"applied"`
	RestartRequired bool `json:"restart_required"`
}

// PrepareSPICE points the VM's QEMU SPICE server at a TCP port so the fleet
// daemon can bridge browser websockets onto it channel by channel. The args
// only take effect when the VM (re)starts, so a running instance is power
// cycled when apply is set.
func (s *Service) PrepareSPICE(ctx context.Context, clusterName, name string, port int, apply bool) (SPICEPrepare, error) {
	// Port 0 means "use the port already stored in raw.qemu" (or derive one
	// for older instances); resolve it after loading the instance.
	if port != 0 && (port < spicePortMin || port > spicePortMax) {
		return SPICEPrepare{}, fmt.Errorf("%w: spice port must be between %d and %d", ErrInvalidRequest, spicePortMin, spicePortMax)
	}

	client, err := s.client(clusterName)
	if err != nil {
		return SPICEPrepare{}, err
	}

	instance, etag, err := client.GetInstance(name)
	if err != nil {
		if isNotFound(err) {
			return SPICEPrepare{}, fmt.Errorf("%w: %s:%s", ErrNotFound, clusterName, name)
		}

		return SPICEPrepare{}, fmt.Errorf("instances: get %s:%s: %w", clusterName, name, err)
	}

	// The web client requests the default (port 0): use the port the launch
	// already stored in raw.qemu so a dynamically allocated listener is not
	// overwritten by the collision-prone deterministic hash.
	if port == 0 {
		port = spicePortFromConfig(instance.Config["raw.qemu"])
		if port == 0 {
			port = SPICEPortFor(name)
		}
	}

	// Incus refuses raw.qemu updates while the VM runs; stop it up front and
	// boot it again once the new qemu args are in place.
	wasRunning := instance.StatusCode == api.Running || instance.StatusCode == api.Starting
	if wasRunning && apply {
		if err := s.Action(ctx, clusterName, name, ActionRequest{Action: "stop", Force: true}); err != nil {
			return SPICEPrepare{}, fmt.Errorf("instances: cycle %s:%s: %w", clusterName, name, err)
		}
		instance, etag, err = client.GetInstance(name)
		if err != nil {
			return SPICEPrepare{}, fmt.Errorf("instances: get %s:%s: %w", clusterName, name, err)
		}
	}

	put := instance.InstancePut
	put.Config = copyConfig(instance.Config)
	put.Devices = copyDevices(instance.Devices)
	put.Profiles = append([]string{}, instance.Profiles...)

	// Drop any previously managed -spice line, keep foreign raw.qemu args.
	var lines []string
	for _, line := range strings.Split(put.Config["raw.qemu"], "\n") {
		if strings.TrimSpace(line) != "" && !strings.Contains(line, spiceArgMarker) {
			lines = append(lines, line)
		}
	}
	lines = append(lines, fmt.Sprintf("%s unix=off,port=%d,disable-ticketing=on,addr=%s", spiceArgMarker, port, spiceListenAddr))
	put.Config["raw.qemu"] = strings.Join(lines, "\n")

	op, err := client.UpdateInstance(name, put, etag)
	if err != nil {
		return SPICEPrepare{}, fmt.Errorf("instances: prepare spice %s:%s: %w", clusterName, name, err)
	}
	if err := waitOperation(ctx, op, "prepare spice "+name); err != nil {
		return SPICEPrepare{}, err
	}

	result := SPICEPrepare{Port: port}
	if wasRunning && !apply {
		// Config is armed; the owner chooses when to power cycle.
		result.Applied = false
		result.RestartRequired = true
		return result, nil
	}

	// apply guarantees a running VM: the browser is about to open the
	// desktop, so boot the instance regardless of its previous state.
	if err := s.Action(ctx, clusterName, name, ActionRequest{Action: "start"}); err != nil {
		return SPICEPrepare{}, fmt.Errorf("instances: cycle %s:%s: %w", clusterName, name, err)
	}
	result.Applied = true
	result.RestartRequired = false

	return result, nil
}

// SPICEDial dials the SPICE TCP listener of the member currently hosting the
// instance. The host always comes from the cluster member list, never from
// user input, so the bridge cannot be abused to reach arbitrary hosts. The
// port must also be the one derived for this instance; accepting an arbitrary
// port would turn the bridge into a general TCP proxy inside the cluster.
func (s *Service) SPICEDial(_ context.Context, clusterName, name string, port int) (net.Conn, error) {
	if port < spicePortMin || port > spicePortMax {
		return nil, fmt.Errorf("%w: spice port must be between %d and %d", ErrInvalidRequest, spicePortMin, spicePortMax)
	}
	client, err := s.client(clusterName)
	if err != nil {
		return nil, err
	}

	instance, _, err := client.GetInstance(name)
	if err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("%w: %s:%s", ErrNotFound, clusterName, name)
		}

		return nil, fmt.Errorf("instances: get %s:%s: %w", clusterName, name, err)
	}

	// Only the port this instance actually stored may be bridged; accepting
	// an arbitrary port would turn the bridge into a general TCP proxy.
	expected := spicePortFromConfig(instance.Config["raw.qemu"])
	if expected == 0 {
		expected = SPICEPortFor(name)
	}
	if port != expected {
		return nil, fmt.Errorf("%w: spice port %d is not the port for instance %q", ErrInvalidRequest, port, name)
	}

	host := spiceMemberHost(client, instance.Location)

	var lastErr error
	for attempt := 0; attempt < spiceDialTry; attempt++ {
		conn, dialErr := s.dial(net.JoinHostPort(host, strconv.Itoa(port)), 5*time.Second)
		if dialErr == nil {
			return conn, nil
		}

		lastErr = dialErr
		time.Sleep(spiceDialDelay)
	}

	return nil, fmt.Errorf("instances: spice %s:%s: dial %s:%d: %w", clusterName, name, host, port, lastErr)
}

// clusterMemberLister is the slice of the client needed to resolve where a
// VM currently runs.
type clusterMemberLister interface {
	GetClusterMembers() ([]api.ClusterMember, error)
}

// memberHost extracts the host from a member URL such as
// https://10.0.0.1:8443, falling back to the loopback when unusable.
func memberHost(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" {
		return "127.0.0.1"
	}

	return parsed.Hostname()
}

// spiceMemberHost maps the instance location to the member's listen address.
// An empty location (standalone daemon) means the fleet-adjacent host itself.
func spiceMemberHost(client clusterMemberLister, location string) string {
	members, err := client.GetClusterMembers()
	if err != nil || len(members) == 0 {
		return "127.0.0.1"
	}

	if location == "" {
		location = members[0].ServerName
	}
	for i := range members {
		if members[i].ServerName == location {
			return memberHost(members[i].URL)
		}
	}

	return "127.0.0.1"
}
