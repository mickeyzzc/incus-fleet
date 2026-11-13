package instances

import (
	"fmt"
	"sort"
	"time"

	"github.com/lxc/incus/v7/shared/api"
)

// InstanceMetrics is a point-in-time resource snapshot of one instance.
// Counters are cumulative since instance start; the UI derives rates by
// polling and differencing successive snapshots.
type InstanceMetrics struct {
	Name      string        `json:"name"`
	Status    string        `json:"status"`
	PID       int64         `json:"pid"`
	Processes int64         `json:"processes"`
	CPU       MetricsCPU    `json:"cpu"`
	Memory    MetricsMemory `json:"memory"`
	Disk      []MetricsDisk `json:"disk"`
	Network   []MetricsNet  `json:"network"`
	At        time.Time     `json:"at"`
}

// MetricsCPU reports cumulative CPU time in nanoseconds.
type MetricsCPU struct {
	Usage         int64 `json:"usage"`
	AllocatedTime int64 `json:"allocated_time"`
}

// MetricsMemory reports resident memory in bytes.
type MetricsMemory struct {
	Usage         int64 `json:"usage"`
	UsagePeak     int64 `json:"usage_peak"`
	Total         int64 `json:"total"`
	SwapUsage     int64 `json:"swap_usage"`
	SwapUsagePeak int64 `json:"swap_usage_peak"`
}

// MetricsDisk is one mounted filesystem inside the instance.
type MetricsDisk struct {
	Mountpoint string `json:"mountpoint"`
	Usage      int64  `json:"usage"`
	Total      int64  `json:"total"`
}

// MetricsNet is one network interface with its cumulative counters.
type MetricsNet struct {
	Interface string `json:"interface"`
	State     string `json:"state"`
	Type      string `json:"type"`
	RxBytes   int64  `json:"rx_bytes"`
	RxPackets int64  `json:"rx_packets"`
	RxErrors  int64  `json:"rx_errors"`
	RxDropped int64  `json:"rx_dropped"`
	TxBytes   int64  `json:"tx_bytes"`
	TxPackets int64  `json:"tx_packets"`
	TxErrors  int64  `json:"tx_errors"`
	TxDropped int64  `json:"tx_dropped"`
}

// InstanceMetrics reads the live state of one instance and maps it into the
// fleet metrics representation.
func (s *Service) InstanceMetrics(clusterName, name string) (*InstanceMetrics, error) {
	conn, err := s.clusters.Connection(clusterName)
	if err != nil {
		return nil, err
	}

	client, err := conn.Client()
	if err != nil {
		return nil, err
	}

	state, _, err := client.GetInstanceState(name)
	if err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("%w: %s:%s", ErrNotFound, clusterName, name)
		}

		return nil, fmt.Errorf("instances: metrics %s:%s: %w", clusterName, name, err)
	}

	return mapInstanceState(name, state), nil
}

// mapInstanceState converts an Incus state payload into fleet metrics.
// Disks are sorted by mountpoint, loopback interfaces are skipped, and the
// remaining interfaces are sorted by name for stable rendering.
func mapInstanceState(name string, state *api.InstanceState) *InstanceMetrics {
	metrics := &InstanceMetrics{
		Name:      name,
		Status:    state.Status,
		PID:       state.Pid,
		Processes: state.Processes,
		CPU: MetricsCPU{
			Usage:         state.CPU.Usage,
			AllocatedTime: state.CPU.AllocatedTime,
		},
		Memory: MetricsMemory{
			Usage:         state.Memory.Usage,
			UsagePeak:     state.Memory.UsagePeak,
			Total:         state.Memory.Total,
			SwapUsage:     state.Memory.SwapUsage,
			SwapUsagePeak: state.Memory.SwapUsagePeak,
		},
		Disk:    []MetricsDisk{},
		Network: []MetricsNet{},
		At:      time.Now(),
	}

	for mountpoint, disk := range state.Disk {
		metrics.Disk = append(metrics.Disk, MetricsDisk{
			Mountpoint: mountpoint,
			Usage:      disk.Usage,
			Total:      disk.Total,
		})
	}
	sort.Slice(metrics.Disk, func(i, j int) bool {
		return metrics.Disk[i].Mountpoint < metrics.Disk[j].Mountpoint
	})

	for iface, net := range state.Network {
		if iface == "lo" {
			continue
		}

		metrics.Network = append(metrics.Network, MetricsNet{
			Interface: iface,
			State:     net.State,
			Type:      net.Type,
			RxBytes:   net.Counters.BytesReceived,
			RxPackets: net.Counters.PacketsReceived,
			RxErrors:  net.Counters.ErrorsReceived,
			RxDropped: net.Counters.PacketsDroppedInbound,
			TxBytes:   net.Counters.BytesSent,
			TxPackets: net.Counters.PacketsSent,
			TxErrors:  net.Counters.ErrorsSent,
			TxDropped: net.Counters.PacketsDroppedOutbound,
		})
	}
	sort.Slice(metrics.Network, func(i, j int) bool {
		return metrics.Network[i].Interface < metrics.Network[j].Interface
	})

	return metrics
}
