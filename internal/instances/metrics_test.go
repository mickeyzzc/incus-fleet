package instances

import (
	"errors"
	"net/http"
	"testing"

	"github.com/lxc/incus/v7/shared/api"

	"github.com/mickeyzzc/incus-fleet/internal/testutil/incusfake"
)

func TestInstanceMetricsMapsState(t *testing.T) {
	srv := &incusfake.Server{
		State: &api.InstanceState{
			Status:     "Running",
			StatusCode: api.Running,
			Pid:        4242,
			Processes:  87,
			CPU: api.InstanceStateCPU{
				Usage:         3_600_000_000_000,
				AllocatedTime: 8_000_000_000,
			},
			Memory: api.InstanceStateMemory{
				Usage:         73248768,
				UsagePeak:     73785344,
				Total:         16106127360,
				SwapUsage:     1024,
				SwapUsagePeak: 2048,
			},
			Disk: map[string]api.InstanceStateDisk{
				"/":         {Usage: 502239232, Total: 20_000_000_000},
				"/mnt/data": {Usage: 1_000, Total: 2_000},
			},
			Network: map[string]api.InstanceStateNetwork{
				"lo": {
					State: "up",
					Type:  "loopback",
					Counters: api.InstanceStateNetworkCounters{
						BytesReceived: 10, BytesSent: 10,
					},
				},
				"eth0": {
					State: "up",
					Type:  "broadcast",
					Counters: api.InstanceStateNetworkCounters{
						BytesReceived: 1024, PacketsReceived: 8, ErrorsReceived: 1, PacketsDroppedInbound: 2,
						BytesSent: 2048, PacketsSent: 9, ErrorsSent: 3, PacketsDroppedOutbound: 4,
					},
				},
			},
		},
	}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})

	metrics, err := svc.InstanceMetrics("c1", "web")
	if err != nil {
		t.Fatalf("InstanceMetrics() error = %v", err)
	}

	if metrics.Status != "Running" || metrics.PID != 4242 || metrics.Processes != 87 {
		t.Fatalf("InstanceMetrics() identity = %+v", metrics)
	}
	if metrics.CPU.Usage != 3_600_000_000_000 || metrics.CPU.AllocatedTime != 8_000_000_000 {
		t.Fatalf("InstanceMetrics() cpu = %+v", metrics.CPU)
	}
	if metrics.Memory.Usage != 73248768 || metrics.Memory.Total != 16106127360 || metrics.Memory.SwapUsage != 1024 {
		t.Fatalf("InstanceMetrics() memory = %+v", metrics.Memory)
	}
	if len(metrics.Disk) != 2 {
		t.Fatalf("InstanceMetrics() disk entries = %d", len(metrics.Disk))
	}
	if metrics.Disk[0].Mountpoint != "/" || metrics.Disk[0].Usage != 502239232 || metrics.Disk[0].Total != 20_000_000_000 {
		t.Fatalf("InstanceMetrics() disk[0] = %+v", metrics.Disk[0])
	}
	if metrics.Disk[1].Mountpoint != "/mnt/data" {
		t.Fatalf("InstanceMetrics() disk order broken: %+v", metrics.Disk)
	}
	if len(metrics.Network) != 1 {
		t.Fatalf("InstanceMetrics() network entries = %d (loopback should be skipped)", len(metrics.Network))
	}
	net := metrics.Network[0]
	if net.Interface != "eth0" || net.RxBytes != 1024 || net.TxBytes != 2048 || net.RxDropped != 2 || net.TxDropped != 4 {
		t.Fatalf("InstanceMetrics() network = %+v", net)
	}
	if metrics.At.IsZero() {
		t.Fatal("InstanceMetrics() timestamp missing")
	}
}

func TestInstanceMetricsErrors(t *testing.T) {
	srv := &incusfake.Server{StateErr: errors.New("agent down")}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})

	if _, err := svc.InstanceMetrics("missing", "web"); err == nil {
		t.Fatal("InstanceMetrics() expected unknown cluster error")
	}
	if _, err := svc.InstanceMetrics("c1", "web"); err == nil {
		t.Fatal("InstanceMetrics() expected propagated state error")
	}

	// Stopped instances report their state without error.
	srv.StateErr = nil
	srv.State = &api.InstanceState{Status: "Stopped", StatusCode: api.Stopped}
	metrics, err := svc.InstanceMetrics("c1", "web")
	if err != nil {
		t.Fatalf("InstanceMetrics() stopped error = %v", err)
	}
	if metrics.Status != "Stopped" {
		t.Fatalf("InstanceMetrics() stopped status = %q", metrics.Status)
	}
}

func TestInstanceStateNotFoundSurfacesNotFound(t *testing.T) {
	srv := &incusfake.Server{StateFunc: func(string) (*api.InstanceState, error) {
		return nil, api.StatusErrorf(http.StatusNotFound, "missing")
	}}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})

	if _, err := svc.InstanceMetrics("c1", "gone"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("InstanceMetrics() missing instance = %v", err)
	}
}

func TestClusterTrafficAggregatesRunningInstances(t *testing.T) {
	srv := &incusfake.Server{
		Instances: []api.InstanceFull{
			{Instance: api.Instance{Name: "web", Status: "Running", StatusCode: api.Running}},
			{Instance: api.Instance{Name: "db", Status: "Stopped", StatusCode: api.Stopped}},
			{Instance: api.Instance{Name: "cache", Status: "Running", StatusCode: api.Running}},
		},
		StateFunc: func(name string) (*api.InstanceState, error) {
			if name == "cache" {
				return nil, errors.New("agent down")
			}

			return &api.InstanceState{
				Status: "Running",
				Network: map[string]api.InstanceStateNetwork{
					"lo": {
						Counters: api.InstanceStateNetworkCounters{BytesReceived: 500, BytesSent: 500},
					},
					"eth0": {
						Counters: api.InstanceStateNetworkCounters{
							BytesReceived: 1000, BytesSent: 2000,
							PacketsReceived: 10, PacketsSent: 20,
						},
					},
				},
			}, nil
		},
	}
	svc := newTestService(t, map[string]*incusfake.Server{"c1": srv})

	totals, err := svc.ClusterTraffic("c1")
	if err != nil {
		t.Fatalf("ClusterTraffic() error = %v", err)
	}

	if totals.TotalRxBytes != 1000 || totals.TotalTxBytes != 2000 {
		t.Fatalf("ClusterTraffic() bytes = %+v", totals)
	}
	if totals.TotalRxPackets != 10 || totals.TotalTxPackets != 20 {
		t.Fatalf("ClusterTraffic() packets = %+v", totals)
	}
	// The stopped instance is skipped and the failing one is tolerated.
	if len(totals.Instances) != 1 || totals.Instances[0].Name != "web" {
		t.Fatalf("ClusterTraffic() instances = %#v", totals.Instances)
	}
	if totals.At.IsZero() {
		t.Fatalf("ClusterTraffic() At is zero")
	}

	if _, err := svc.ClusterTraffic("missing"); err == nil {
		t.Fatal("ClusterTraffic() unknown cluster error = nil")
	}
}
