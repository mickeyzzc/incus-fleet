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

// SPICEQemuArg builds the managed QEMU argument for a SPICE listener.
func SPICEQemuArg(port int) string {
	return fmt.Sprintf("%s unix=off,port=%d,disable-ticketing=on,addr=%s", spiceArgMarker, port, spiceListenAddr)
}

// EnsureSPICEConfig adds the managed SPICE listener to a VM config when absent
// and returns the port to use. Unrelated raw.qemu arguments are preserved, and
// an existing managed listener is left untouched so a running VM never needs a
// power cycle just to open its desktop.
func EnsureSPICEConfig(config api.ConfigMap, requested int) (int, error) {
	if requested < 0 || requested > spicePortMax {
		return 0, fmt.Errorf("%w: spice port must be between %d and %d", ErrInvalidRequest, spicePortMin, spicePortMax)
	}

	port := requested
	if port == 0 {
		port = spicePortFromConfig(config["raw.qemu"])
	}
	if port == 0 {
		allocated, err := allocateSPICEPort()
		if err != nil {
			return 0, fmt.Errorf("instances: allocate SPICE port: %w", err)
		}
		port = allocated
	}

	raw := config["raw.qemu"]
	if spicePortFromConfig(raw) == port {
		return port, nil
	}

	var lines []string
	for _, line := range strings.Split(raw, "\n") {
		if strings.TrimSpace(line) != "" && !strings.Contains(line, spiceArgMarker) {
			lines = append(lines, line)
		}
	}
	lines = append(lines, SPICEQemuArg(port))
	config["raw.qemu"] = strings.Join(lines, "\n")

	return port, nil
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

	// VMs are created with a SPICE listener already in raw.qemu. The web
	// client asks for port 0, which keeps that listener instead of replacing
	// it with the old collision-prone hash.
	existing := spicePortFromConfig(instance.Config["raw.qemu"])
	if port == 0 {
		if existing != 0 {
			port = existing
		} else {
			port = SPICEPortFor(name)
		}
	}

	wasRunning := instance.StatusCode == api.Running || instance.StatusCode == api.Starting
	needsUpdate := existing != port
	stopped := false

	// Incus refuses raw.qemu updates while a VM runs, so a running VM is only
	// stopped when the listener actually needs to change. With creation-time
	// SPICE configuration this is no longer the case on a normal desktop open.
	if needsUpdate && wasRunning && apply {
		if err := s.Action(ctx, clusterName, name, ActionRequest{Action: "stop", Force: true}); err != nil {
			return SPICEPrepare{}, fmt.Errorf("instances: cycle %s:%s: %w", clusterName, name, err)
		}
		instance, etag, err = client.GetInstance(name)
		if err != nil {
			return SPICEPrepare{}, fmt.Errorf("instances: get %s:%s: %w", clusterName, name, err)
		}
		stopped = true
	}

	if needsUpdate {
		put := instance.InstancePut
		put.Config = copyConfig(instance.Config)
		put.Devices = copyDevices(instance.Devices)
		put.Profiles = append([]string{}, instance.Profiles...)

		port, err = EnsureSPICEConfig(put.Config, port)
		if err != nil {
			return SPICEPrepare{}, err
		}

		op, err := client.UpdateInstance(name, put, etag)
		if err != nil {
			return SPICEPrepare{}, fmt.Errorf("instances: prepare spice %s:%s: %w", clusterName, name, err)
		}
		if err := waitOperation(ctx, op, "prepare spice "+name); err != nil {
			return SPICEPrepare{}, err
		}

		if wasRunning && !apply {
			// Config is armed; the owner chooses when to power cycle.
			return SPICEPrepare{Port: port, Applied: false, RestartRequired: true}, nil
		}
	}

	// apply guarantees a running VM: the browser is about to open the desktop.
	// A VM that already runs with the right listener is left untouched.
	if apply && (!wasRunning || stopped) {
		if err := s.Action(ctx, clusterName, name, ActionRequest{Action: "start"}); err != nil {
			return SPICEPrepare{}, fmt.Errorf("instances: cycle %s:%s: %w", clusterName, name, err)
		}
	}

	return SPICEPrepare{Port: port, Applied: true, RestartRequired: false}, nil
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
