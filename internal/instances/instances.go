// Package instances exposes cluster instance inventory for the fleet API.
package instances

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/lxc/incus/v7/shared/api"

	"github.com/mickeyzzc/incus-fleet/internal/clusters"
)

// ErrNotFound is returned when an instance does not exist on a cluster.
var ErrNotFound = errors.New("instances: not found")

// Service reads instance inventory from the cluster manager.
type Service struct {
	clusters *clusters.Manager

	// dial opens a TCP connection to a SPICE listener. Tests replace it to
	// point at an ephemeral listener; production uses net.DialTimeout.
	dial func(addr string, timeout time.Duration) (net.Conn, error)

	// workDir is the scratch directory for unattended-install ISO
	// remastering (default data/unattended relative to the server cwd).
	workDir string
	// iso performs the on-disk ISO surgery for unattended installs
	// (default: xorriso on PATH).
	iso isoTool
	// logger receives unattended watch lifecycle messages.
	logger *slog.Logger
}

// Option customizes the instance service.
type Option func(*Service)

// WithUnattendedWorkDir overrides the scratch directory used for ISO
// remastering.
func WithUnattendedWorkDir(dir string) Option {
	return func(s *Service) { s.workDir = dir }
}

// WithXorriso overrides the xorriso executable used for unattended ISO
// surgery.
func WithXorriso(path string) Option {
	return func(s *Service) { s.iso = newXorrisoTool(path) }
}

// withIsoTool replaces the ISO surgery tool (test hook).
func withIsoTool(tool isoTool) Option {
	return func(s *Service) { s.iso = tool }
}

// WithLogger attaches a logger for unattended watch lifecycle messages.
func WithLogger(logger *slog.Logger) Option {
	return func(s *Service) { s.logger = logger }
}

// New creates an instance inventory service.
func New(manager *clusters.Manager, opts ...Option) *Service {
	s := &Service{clusters: manager, dial: func(addr string, timeout time.Duration) (net.Conn, error) {
		return net.DialTimeout("tcp", addr, timeout)
	}, iso: newXorrisoTool("xorriso")}
	for _, opt := range opts {
		opt(s)
	}

	return s
}

// Summary is the list representation of an instance.
type Summary struct {
	Name              string         `json:"name"`
	Cluster           string         `json:"cluster"`
	Type              string         `json:"type"`
	Status            string         `json:"status"`
	StatusCode        api.StatusCode `json:"status_code"`
	Location          string         `json:"location,omitempty"`
	Project           string         `json:"project,omitempty"`
	Architecture      string         `json:"architecture,omitempty"`
	Description       string         `json:"description,omitempty"`
	Profiles          []string       `json:"profiles"`
	IPv4              []string       `json:"ipv4"`
	IPv6              []string       `json:"ipv6"`
	Memory            int64          `json:"memory"`
	Processes         int64          `json:"processes"`
	CreatedAt         time.Time      `json:"created_at"`
	StartedAt         time.Time      `json:"started_at,omitempty"`
	StoragePool       string         `json:"storage_pool,omitempty"`
	MigrationStateful bool           `json:"migration_stateful"`
	LiveMigratable    bool           `json:"live_migratable"`
}

// Detail is the single-instance representation, including state and safe
// selected configuration.
type Detail struct {
	Summary
	OS            string             `json:"os,omitempty"`
	OSVersion     string             `json:"os_version,omitempty"`
	KernelVersion string             `json:"kernel_version,omitempty"`
	Hostname      string             `json:"hostname,omitempty"`
	Ephemeral     bool               `json:"ephemeral"`
	Config        map[string]string  `json:"config"`
	Devices       api.DevicesMap     `json:"devices"`
	State         *api.InstanceState `json:"state,omitempty"`
}

// List returns instances for one cluster. instanceType may be empty for all.
func (s *Service) List(clusterName string, instanceType api.InstanceType) ([]Summary, error) {
	conn, err := s.clusters.Connection(clusterName)
	if err != nil {
		return nil, err
	}

	client, err := conn.Client()
	if err != nil {
		return nil, err
	}

	full, err := client.GetInstancesFull(instanceType)
	if err != nil {
		return nil, fmt.Errorf("instances: list %s: %w", clusterName, err)
	}

	list := make([]Summary, 0, len(full))
	for i := range full {
		list = append(list, summarize(clusterName, &full[i]))
	}

	sort.Slice(list, func(i, j int) bool {
		if list[i].Name == list[j].Name {
			return list[i].Location < list[j].Location
		}

		return list[i].Name < list[j].Name
	})

	return list, nil
}

// ListAll returns instances from every configured cluster. A cluster that is
// unreachable is reported in unavailable instead of failing the whole fleet
// view; the caller decides whether partial data is acceptable.
func (s *Service) ListAll(instanceType api.InstanceType) ([]Summary, []string, error) {
	var (
		all         []Summary
		unavailable []string
		names       = s.clusters.Names()
		lastErr     error
	)

	for _, name := range names {
		list, err := s.List(name, instanceType)
		if err != nil {
			unavailable = append(unavailable, name)
			lastErr = err
			continue
		}
		all = append(all, list...)
	}

	sort.Slice(all, func(i, j int) bool {
		if all[i].Cluster == all[j].Cluster {
			return all[i].Name < all[j].Name
		}

		return all[i].Cluster < all[j].Cluster
	})

	if len(names) > 0 && len(unavailable) == len(names) {
		return nil, unavailable, lastErr
	}

	return all, unavailable, nil
}

// Get returns one instance from a cluster.
func (s *Service) Get(clusterName, name string) (Detail, error) {
	conn, err := s.clusters.Connection(clusterName)
	if err != nil {
		return Detail{}, err
	}

	client, err := conn.Client()
	if err != nil {
		return Detail{}, err
	}

	instance, _, err := client.GetInstance(name)
	if err != nil {
		if isNotFound(err) {
			return Detail{}, fmt.Errorf("%w: %s:%s", ErrNotFound, clusterName, name)
		}

		return Detail{}, fmt.Errorf("instances: get %s:%s: %w", clusterName, name, err)
	}

	state, _, stateErr := client.GetInstanceState(name)
	if stateErr != nil && !isNotFound(stateErr) {
		return Detail{}, fmt.Errorf("instances: state %s:%s: %w", clusterName, name, stateErr)
	}

	detail := Detail{
		Summary:   summarizeInstance(clusterName, instance, state),
		Ephemeral: instance.Ephemeral,
		Config:    safeConfig(instance),
		Devices:   instance.ExpandedDevices,
		State:     state,
	}

	if state != nil && state.OSInfo != nil {
		detail.OS = state.OSInfo.OS
		detail.OSVersion = state.OSInfo.OSVersion
		detail.KernelVersion = state.OSInfo.KernelVersion
		detail.Hostname = state.OSInfo.Hostname
	}

	return detail, nil
}

func summarize(clusterName string, full *api.InstanceFull) Summary {
	summary := summarizeInstance(clusterName, &full.Instance, full.State)
	if full.State == nil {
		summary.Status = full.Status
		summary.StatusCode = full.StatusCode
	}

	return summary
}

func summarizeInstance(clusterName string, instance *api.Instance, state *api.InstanceState) Summary {
	summary := Summary{
		Name:              instance.Name,
		Cluster:           clusterName,
		Type:              instance.Type,
		Status:            instance.Status,
		StatusCode:        instance.StatusCode,
		Location:          instance.Location,
		Project:           instance.Project,
		Architecture:      instance.Architecture,
		Description:       instance.Description,
		Profiles:          append([]string{}, instance.Profiles...),
		IPv4:              []string{},
		IPv6:              []string{},
		CreatedAt:         instance.CreatedAt,
		StoragePool:       instance.ExpandedDevices["root"]["pool"],
		MigrationStateful: instance.ExpandedConfig["migration.stateful"] == "true",
	}

	if state != nil {
		summary.Status = state.Status
		summary.StatusCode = state.StatusCode
		summary.Memory = state.Memory.Usage
		summary.Processes = state.Processes
		summary.StartedAt = state.StartedAt
		summary.IPv4, summary.IPv6 = addresses(state)
	}

	summary.LiveMigratable = summary.Type == string(api.InstanceTypeVM) && summary.MigrationStateful

	return summary
}

func addresses(state *api.InstanceState) ([]string, []string) {
	names := make([]string, 0, len(state.Network))
	for name := range state.Network {
		names = append(names, name)
	}
	sort.Strings(names)

	var ipv4, ipv6 []string
	for _, name := range names {
		network := state.Network[name]
		for _, addr := range network.Addresses {
			switch addr.Family {
			case "inet":
				if addr.Address != "" && !strings.HasPrefix(addr.Address, "127.") {
					ipv4 = appendUnique(ipv4, addr.Address)
				}
			case "inet6":
				if addr.Address != "" && addr.Address != "::1" && !strings.HasPrefix(addr.Address, "fe80:") {
					ipv6 = appendUnique(ipv6, addr.Address)
				}
			}
		}
	}

	if ipv4 == nil {
		ipv4 = []string{}
	}
	if ipv6 == nil {
		ipv6 = []string{}
	}

	return ipv4, ipv6
}

func appendUnique(list []string, value string) []string {
	for _, item := range list {
		if item == value {
			return list
		}
	}

	return append(list, value)
}

func safeConfig(instance *api.Instance) map[string]string {
	keys := []string{
		"image.architecture",
		"image.description",
		"image.os",
		"image.release",
		"limits.cpu",
		"limits.memory",
		"migration.stateful",
		"security.secureboot",
	}

	out := make(map[string]string, len(keys))
	for _, key := range keys {
		if value := instance.ExpandedConfig[key]; value != "" {
			out[key] = value
		}
	}

	return out
}

func isNotFound(err error) bool {
	var statusErr api.StatusError
	if errors.As(err, &statusErr) {
		return statusErr.Status() == http.StatusNotFound
	}

	return false
}

// SetDialer replaces the TCP dialer used for SPICE bridging. It exists so tests
// can point the bridge at an ephemeral listener; production never calls it.
func (s *Service) SetDialer(dial func(addr string, timeout time.Duration) (net.Conn, error)) {
	s.dial = dial
}
