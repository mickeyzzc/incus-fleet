package instances

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	incus "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"
	"github.com/lxc/incus/v7/shared/units"

	"github.com/mickeyzzc/incus-fleet/internal/clusters"
)

// ErrInvalidRequest is returned when instance management input is invalid.
var ErrInvalidRequest = errors.New("instances: invalid request")

// overlayBridgePrefix marks fleet-managed overlay bridges. The overlay service
// creates them on every member with this prefix, so instance creation can tell
// them apart from a physical NIC name and attach a bridged NIC instead of the
// macvlan device it gives unmanaged physical interfaces.
const overlayBridgePrefix = "fleetbr"

// isOverlayBridge reports whether network names a fleet overlay bridge.
func isOverlayBridge(network string) bool {
	return strings.HasPrefix(network, overlayBridgePrefix)
}

const (
	actionTimeout = 5 * time.Minute
	createTimeout = 15 * time.Minute
	updateTimeout = 5 * time.Minute
	deleteTimeout = 5 * time.Minute
)

var instanceNamePattern = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9_.-]*[A-Za-z0-9])?$`)

// CreateRequest describes a new instance.
type CreateRequest struct {
	Name         string   `json:"name"`
	Type         string   `json:"type,omitempty"`
	Image        string   `json:"image"`
	Description  string   `json:"description,omitempty"`
	Profiles     []string `json:"profiles,omitempty"`
	CPU          string   `json:"cpu,omitempty"`
	Memory       string   `json:"memory,omitempty"`
	RootDiskSize string   `json:"root_disk_size,omitempty"`
	StoragePool  string   `json:"storage_pool,omitempty"`
	Network      string   `json:"network,omitempty"`
	IPv4         string   `json:"ipv4,omitempty"`
	IPv6         string   `json:"ipv6,omitempty"`
	Start        *bool    `json:"start,omitempty"`
}

// UpdateRequest changes selected instance settings. Nil pointers are unchanged;
// empty strings clear limits and static addresses.
type UpdateRequest struct {
	Description  *string `json:"description,omitempty"`
	CPU          *string `json:"cpu,omitempty"`
	Memory       *string `json:"memory,omitempty"`
	RootDiskSize *string `json:"root_disk_size,omitempty"`
	Device       string  `json:"device,omitempty"`
	Network      *string `json:"network,omitempty"`
	IPv4         *string `json:"ipv4,omitempty"`
	IPv6         *string `json:"ipv6,omitempty"`
}

// ActionRequest starts, stops, or restarts an instance.
type ActionRequest struct {
	Action string `json:"action"`
	Force  bool   `json:"force,omitempty"`
}

// Image is the list representation of a local Incus image.
type Image struct {
	Fingerprint  string    `json:"fingerprint"`
	Aliases      []string  `json:"aliases"`
	Type         string    `json:"type"`
	Architecture string    `json:"architecture"`
	OS           string    `json:"os,omitempty"`
	Release      string    `json:"release,omitempty"`
	Description  string    `json:"description,omitempty"`
	Size         int64     `json:"size"`
	Cached       bool      `json:"cached"`
	CreatedAt    time.Time `json:"created_at"`
}

// StoragePool describes one cluster storage pool.
type StoragePool struct {
	Name        string            `json:"name"`
	Driver      string            `json:"driver"`
	Source      string            `json:"source,omitempty"`
	Status      string            `json:"status,omitempty"`
	Description string            `json:"description,omitempty"`
	Config      map[string]string `json:"config,omitempty"`
	UsedBy      []string          `json:"used_by"`
	Locations   []string          `json:"locations"`
	Total       int64             `json:"total"`
	Used        int64             `json:"used"`
}

// Network describes one cluster network.
type Network struct {
	Name        string            `json:"name"`
	Type        string            `json:"type"`
	Managed     bool              `json:"managed"`
	Status      string            `json:"status,omitempty"`
	Description string            `json:"description,omitempty"`
	IPv4        string            `json:"ipv4,omitempty"`
	IPv6        string            `json:"ipv6,omitempty"`
	Config      map[string]string `json:"config,omitempty"`
	UsedBy      []string          `json:"used_by"`
	Locations   []string          `json:"locations"`
}

// Resources groups the storage pools and networks of one cluster.
type Resources struct {
	StoragePools []StoragePool `json:"storage_pools"`
	Networks     []Network     `json:"networks"`
}

// Create makes an instance from a local image and waits for the operation.
func (s *Service) Create(ctx context.Context, clusterName string, req CreateRequest) (Detail, error) {
	req.Name = strings.TrimSpace(req.Name)
	if err := validateCreateRequest(req); err != nil {
		return Detail{}, err
	}

	client, err := s.client(clusterName)
	if err != nil {
		return Detail{}, err
	}

	instanceType := api.InstanceTypeContainer
	if req.Type == string(api.InstanceTypeVM) {
		instanceType = api.InstanceTypeVM
	}

	pool := strings.TrimSpace(req.StoragePool)
	if pool == "" {
		pool = "default"
	}

	pools, err := client.GetStoragePoolNames()
	if err != nil {
		return Detail{}, fmt.Errorf("instances: list storage pools for %s: %w", clusterName, err)
	}
	if !containsString(pools, pool) {
		return Detail{}, fmt.Errorf("%w: storage pool %q does not exist on %s", ErrInvalidRequest, pool, clusterName)
	}

	network := strings.TrimSpace(req.Network)
	var networkManaged bool
	if network != "" {
		networks, err := client.GetNetworks()
		if err != nil {
			return Detail{}, fmt.Errorf("instances: list networks for %s: %w", clusterName, err)
		}

		var target *api.Network
		for i := range networks {
			if networks[i].Name == network {
				target = &networks[i]
				break
			}
		}
		if target == nil {
			return Detail{}, fmt.Errorf("%w: network %q does not exist on %s", ErrInvalidRequest, network, clusterName)
		}

		networkManaged = target.Managed
		// Unmanaged parents are attached as macvlan and take addresses from
		// the physical LAN; device-level static addresses only apply to
		// managed bridges. Fleet's overlay bridge runs its own dnsmasq without
		// MAC reservations, so it cannot honour a device-level static address
		// either.
		if !networkManaged && (req.IPv4 != "" || req.IPv6 != "") {
			if isOverlayBridge(network) {
				return Detail{}, fmt.Errorf("%w: overlay network %q hands out DHCP addresses; set a static address inside the instance instead", ErrInvalidRequest, network)
			}
			return Detail{}, fmt.Errorf("%w: physical NIC %q takes addresses from the LAN DHCP server; configure a static address inside the instance instead", ErrInvalidRequest, network)
		}
	}

	config := api.ConfigMap{}
	if req.CPU != "" {
		config["limits.cpu"] = req.CPU
	}
	if req.Memory != "" {
		config["limits.memory"] = req.Memory
	}

	root := map[string]string{"type": "disk", "path": "/", "pool": pool}
	if req.RootDiskSize != "" {
		root["size"] = req.RootDiskSize
	}
	devices := api.DevicesMap{"root": root}

	if network != "" {
		nic := map[string]string{"type": "nic"}
		switch {
		case networkManaged:
			nic["network"] = network
			if req.IPv4 != "" {
				nic["ipv4.address"] = req.IPv4
			}
			if req.IPv6 != "" {
				nic["ipv6.address"] = req.IPv6
			}
		case isOverlayBridge(network):
			// A fleet-managed overlay bridge is a plain host bridge, so Incus
			// does not list it as a managed network. Attach it as a bridged NIC
			// so the guest keeps using DHCP from the overlay gateway instead of
			// landing on the physical LAN the way macvlan would.
			nic["nictype"] = "bridged"
			nic["parent"] = network
		default:
			nic["nictype"] = "macvlan"
			nic["parent"] = network
		}
		devices["eth0"] = nic
	}

	start := true
	if req.Start != nil {
		start = *req.Start
	}

	post := api.InstancesPost{
		Name: req.Name,
		Type: instanceType,
		InstancePut: api.InstancePut{
			Description: req.Description,
			Profiles:    cleanProfiles(req.Profiles),
			Config:      config,
			Devices:     devices,
		},
		Source: api.InstanceSource{Type: "image", Fingerprint: req.Image},
		Start:  start,
	}

	ctx, cancel := context.WithTimeout(ctx, createTimeout)
	defer cancel()

	op, err := client.CreateInstance(post)
	if err != nil {
		return Detail{}, fmt.Errorf("instances: create %s:%s: %w", clusterName, req.Name, err)
	}
	if err := waitOperation(ctx, op, "create "+req.Name); err != nil {
		return Detail{}, err
	}

	return s.Get(clusterName, req.Name)
}

// Update changes CPU, memory, root disk, NIC, and description settings.
func (s *Service) Update(ctx context.Context, clusterName, name string, req UpdateRequest) (Detail, error) {
	if err := validateUpdateRequest(req); err != nil {
		return Detail{}, err
	}

	client, err := s.client(clusterName)
	if err != nil {
		return Detail{}, err
	}

	instance, etag, err := client.GetInstance(name)
	if err != nil {
		if isNotFound(err) {
			return Detail{}, fmt.Errorf("%w: %s:%s", ErrNotFound, clusterName, name)
		}

		return Detail{}, fmt.Errorf("instances: get %s:%s: %w", clusterName, name, err)
	}

	put := instance.InstancePut
	put.Config = copyConfig(instance.Config)
	put.Devices = copyDevices(instance.Devices)
	put.Profiles = append([]string{}, instance.Profiles...)

	if req.CPU != nil {
		setOrDelete(put.Config, "limits.cpu", strings.TrimSpace(*req.CPU))
	}
	if req.Memory != nil {
		setOrDelete(put.Config, "limits.memory", strings.TrimSpace(*req.Memory))
	}
	if req.Description != nil {
		put.Description = *req.Description
	}

	if req.RootDiskSize != nil {
		size := strings.TrimSpace(*req.RootDiskSize)
		root, ok := put.Devices["root"]
		if !ok {
			root = map[string]string{"type": "disk", "path": "/", "pool": expandedValue(instance, "root", "pool", "default")}
		}
		setOrDelete(root, "size", size)
		put.Devices["root"] = root
	}

	if req.Network != nil || req.IPv4 != nil || req.IPv6 != nil {
		deviceName := strings.TrimSpace(req.Device)
		if deviceName == "" {
			deviceName = "eth0"
		}

		nic, ok := put.Devices[deviceName]
		if !ok {
			expanded, exists := instance.ExpandedDevices[deviceName]
			if !exists {
				return Detail{}, fmt.Errorf("%w: network device %q not found on %s", ErrInvalidRequest, deviceName, name)
			}
			nic = copyDevice(expanded)
		}

		if req.Network != nil {
			network := strings.TrimSpace(*req.Network)
			if network == "" {
				return Detail{}, fmt.Errorf("%w: network is required", ErrInvalidRequest)
			}
			networks, err := client.GetNetworks()
			if err != nil {
				return Detail{}, fmt.Errorf("instances: list networks for %s: %w", clusterName, err)
			}
			target, err := findNetwork(networks, network, clusterName)
			if err != nil {
				return Detail{}, err
			}
			staticIPv4 := ""
			if req.IPv4 != nil {
				staticIPv4 = strings.TrimSpace(*req.IPv4)
			}
			staticIPv6 := ""
			if req.IPv6 != nil {
				staticIPv6 = strings.TrimSpace(*req.IPv6)
			}
			if err := applyNICNetwork(nic, target, network, staticIPv4, staticIPv6); err != nil {
				return Detail{}, err
			}
		}

		if req.IPv4 != nil {
			setOrDelete(nic, "ipv4.address", strings.TrimSpace(*req.IPv4))
		}
		if req.IPv6 != nil {
			setOrDelete(nic, "ipv6.address", strings.TrimSpace(*req.IPv6))
		}

		put.Devices[deviceName] = nic
	}

	ctx, cancel := context.WithTimeout(ctx, updateTimeout)
	defer cancel()

	op, err := client.UpdateInstance(name, put, etag)
	if err != nil {
		return Detail{}, fmt.Errorf("instances: update %s:%s: %w", clusterName, name, err)
	}
	if err := waitOperation(ctx, op, "update "+name); err != nil {
		return Detail{}, err
	}

	return s.Get(clusterName, name)
}

// Action starts, stops, or restarts an instance.
//
// A guest without the incus agent (an ISO-booted installer, for example)
// cannot answer an elegant stop/restart, so Incus would block until the full
// graceful timeout before failing — the console buttons then appear dead for
// minutes. When the state reports no agent activity, stop/restart skip the
// wait and force immediately.
func (s *Service) Action(ctx context.Context, clusterName, name string, req ActionRequest) error {
	action := strings.TrimSpace(req.Action)
	switch action {
	case "start", "stop", "restart":
	default:
		return fmt.Errorf("%w: action must be one of start, stop, restart", ErrInvalidRequest)
	}

	client, err := s.client(clusterName)
	if err != nil {
		return err
	}

	force := req.Force
	timeout := 300
	if action != "start" && !force {
		if state, _, err := client.GetInstanceState(name); err == nil && state != nil && state.Processes < 0 {
			force = true
			timeout = 0
		}
	}

	ctx, cancel := context.WithTimeout(ctx, actionTimeout)
	defer cancel()

	op, err := client.UpdateInstanceState(name, api.InstanceStatePut{
		Action:  action,
		Timeout: timeout,
		Force:   force,
	}, "")
	if err != nil {
		return fmt.Errorf("instances: %s %s:%s: %w", action, clusterName, name, err)
	}

	return waitOperation(ctx, op, action+" "+name)
}

// Delete stops a running instance before deleting it. When deleteVolumes is
// set, attached custom volumes (disk devices with pool+source) are removed
// afterwards; shared ISO installer assets are kept.
func (s *Service) Delete(ctx context.Context, clusterName, name string, force, deleteVolumes bool) error {
	client, err := s.client(clusterName)
	if err != nil {
		return err
	}

	state, _, err := client.GetInstanceState(name)
	if err != nil {
		if isNotFound(err) {
			return fmt.Errorf("%w: %s:%s", ErrNotFound, clusterName, name)
		}

		return fmt.Errorf("instances: state %s:%s: %w", clusterName, name, err)
	}

	var attached []attachedVolume
	if deleteVolumes {
		attached = attachedCustomVolumes(client, name)
	}

	ctx, cancel := context.WithTimeout(ctx, deleteTimeout)
	defer cancel()

	if state != nil && (state.StatusCode == api.Running || strings.EqualFold(state.Status, "running")) {
		// Same agent-less shortcut as Action: an ISO-booted installer cannot
		// answer an elegant stop, so deleting it would otherwise stall on the
		// graceful wait first.
		stopForce := force
		stopTimeout := 300
		if !stopForce && state.Processes < 0 {
			stopForce = true
			stopTimeout = 0
		}
		op, err := client.UpdateInstanceState(name, api.InstanceStatePut{
			Action:  "stop",
			Timeout: stopTimeout,
			Force:   stopForce,
		}, "")
		if err != nil {
			return fmt.Errorf("instances: stop %s:%s before delete: %w", clusterName, name, err)
		}
		if err := waitOperation(ctx, op, "stop "+name+" before delete"); err != nil {
			return err
		}
	}

	op, err := client.DeleteInstance(name)
	if err != nil {
		return fmt.Errorf("instances: delete %s:%s: %w", clusterName, name, err)
	}
	if err := waitOperation(ctx, op, "delete "+name); err != nil {
		return err
	}

	if len(attached) > 0 {
		s.deleteAttachedVolumes(client, clusterName, name, attached)
	}

	return nil
}

// attachedVolume is one custom volume referenced by a disk device.
type attachedVolume struct {
	pool   string
	volume string
}

// attachedCustomVolumes collects the pool+source pairs of the instance's
// disk devices. Missing configs degrade to an empty list: the option then
// simply has nothing to remove.
func attachedCustomVolumes(client clusters.InstanceServer, name string) []attachedVolume {
	inst, _, err := client.GetInstance(name)
	if err != nil || inst == nil {
		return nil
	}

	var out []attachedVolume
	for _, device := range inst.Devices {
		if device["type"] != "disk" || device["pool"] == "" || device["source"] == "" {
			continue
		}

		out = append(out, attachedVolume{pool: device["pool"], volume: device["source"]})
	}

	return out
}

// deleteAttachedVolumes removes the instance's custom volumes, best effort.
// ISO volumes are shared installer assets and are never deleted; anything
// else that fails is logged so the deletion itself still counts as done.
func (s *Service) deleteAttachedVolumes(client clusters.InstanceServer, clusterName, name string, attached []attachedVolume) {
	for _, vol := range attached {
		v, _, err := client.GetStoragePoolVolume(vol.pool, "custom", vol.volume)
		if err != nil {
			continue // volume already gone or inaccessible; nothing to clean
		}
		if v != nil && v.ContentType == "iso" {
			continue // shared installer asset, not instance-owned storage
		}

		if err := client.DeleteStoragePoolVolume(vol.pool, "custom", vol.volume); err != nil {
			if s.logger != nil {
				s.logger.Warn("failed to delete attached volume", "cluster", clusterName, "instance", name, "pool", vol.pool, "volume", vol.volume, "error", err)
			}
		}
	}
}

// Images lists the local images available on a cluster.
func (s *Service) Images(clusterName string) ([]Image, error) {
	client, err := s.client(clusterName)
	if err != nil {
		return nil, err
	}

	images, err := client.GetImages()
	if err != nil {
		return nil, fmt.Errorf("instances: list images %s: %w", clusterName, err)
	}

	list := make([]Image, 0, len(images))
	for _, image := range images {
		aliases := make([]string, 0, len(image.Aliases))
		for _, alias := range image.Aliases {
			aliases = append(aliases, alias.Name)
		}
		sort.Strings(aliases)

		list = append(list, Image{
			Fingerprint:  image.Fingerprint,
			Aliases:      aliases,
			Type:         image.Type,
			Architecture: image.Architecture,
			OS:           image.Properties["os"],
			Release:      image.Properties["release"],
			Description:  image.Properties["description"],
			Size:         image.Size,
			Cached:       image.Cached,
			CreatedAt:    image.CreatedAt,
		})
	}

	sort.Slice(list, func(i, j int) bool {
		if list[i].CreatedAt.Equal(list[j].CreatedAt) {
			return list[i].Fingerprint < list[j].Fingerprint
		}

		return list[i].CreatedAt.After(list[j].CreatedAt)
	})

	return list, nil
}

// MemberNames lists the cluster member names; used by callers that must
// contact every member (bond-slave detection on unmanaged NICs whose Incus
// listing carries no locations).
func (s *Service) MemberNames(clusterName string) ([]string, error) {
	client, err := s.client(clusterName)
	if err != nil {
		return nil, err
	}

	members, err := client.GetClusterMembers()
	if err != nil {
		return nil, fmt.Errorf("instances: list members %s: %w", clusterName, err)
	}

	names := make([]string, 0, len(members))
	for _, member := range members {
		names = append(names, member.ServerName)
	}
	sort.Strings(names)

	return names, nil
}

// Resources returns storage pools and networks with usage details.
func (s *Service) Resources(clusterName string) (Resources, error) {
	client, err := s.client(clusterName)
	if err != nil {
		return Resources{}, err
	}

	pools, err := client.GetStoragePools()
	if err != nil {
		return Resources{}, fmt.Errorf("instances: list storage pools %s: %w", clusterName, err)
	}

	out := Resources{
		StoragePools: make([]StoragePool, 0, len(pools)),
		Networks:     []Network{},
	}

	for _, pool := range pools {
		usage, err := client.GetStoragePoolResources(pool.Name)
		if err != nil {
			return Resources{}, fmt.Errorf("instances: storage usage %s:%s: %w", clusterName, pool.Name, err)
		}

		out.StoragePools = append(out.StoragePools, StoragePool{
			Name:        pool.Name,
			Driver:      pool.Driver,
			Source:      pool.Config["source"],
			Status:      pool.Status,
			Description: pool.Description,
			Config:      copyConfig(pool.Config),
			UsedBy:      copyStrings(pool.UsedBy),
			Locations:   copyStrings(pool.Locations),
			Total:       clampUint64ToInt64(usage.Space.Total),
			Used:        clampUint64ToInt64(usage.Space.Used),
		})
	}

	sort.Slice(out.StoragePools, func(i, j int) bool {
		return out.StoragePools[i].Name < out.StoragePools[j].Name
	})

	networks, err := client.GetNetworks()
	if err != nil {
		return Resources{}, fmt.Errorf("instances: list networks %s: %w", clusterName, err)
	}

	for _, network := range networks {
		out.Networks = append(out.Networks, Network{
			Name:        network.Name,
			Type:        network.Type,
			Managed:     network.Managed,
			Status:      network.Status,
			Description: network.Description,
			IPv4:        network.Config["ipv4.address"],
			IPv6:        network.Config["ipv6.address"],
			Config:      copyConfig(network.Config),
			UsedBy:      copyStrings(network.UsedBy),
			Locations:   copyStrings(network.Locations),
		})
	}

	sort.Slice(out.Networks, func(i, j int) bool {
		return out.Networks[i].Name < out.Networks[j].Name
	})

	return out, nil
}

// findNetwork locates one network by name in a listing.
func findNetwork(networks []api.Network, name, clusterName string) (*api.Network, error) {
	for i := range networks {
		if networks[i].Name == name {
			return &networks[i], nil
		}
	}

	return nil, fmt.Errorf("%w: network %q does not exist on %s", ErrInvalidRequest, name, clusterName)
}

// applyNICNetwork translates a selected network into the right NIC device
// shape: managed networks attach through the "network" key, unmanaged
// physical parents through nictype=macvlan + parent — matching the create
// path, so Incus never receives an unmanaged name in "network" ("Network not
// found"). Static device addresses only apply to managed bridges.
func applyNICNetwork(nic map[string]string, target *api.Network, network, staticIPv4, staticIPv6 string) error {
	if target.Managed {
		nic["network"] = network
		// "network" and "nictype"/"parent" are mutually exclusive in Incus;
		// a legacy NIC (nictype+parent) must drop those keys on switch or
		// the update is rejected outright.
		delete(nic, "nictype")
		delete(nic, "parent")
		return nil
	}

	if staticIPv4 != "" || staticIPv6 != "" {
		return fmt.Errorf("%w: physical NIC %q takes addresses from the LAN DHCP server; configure a static address inside the instance instead", ErrInvalidRequest, network)
	}

	delete(nic, "network")
	delete(nic, "ipv4.address")
	delete(nic, "ipv6.address")
	nic["nictype"] = "macvlan"
	nic["parent"] = network

	return nil
}

func (s *Service) client(clusterName string) (clusters.InstanceServer, error) {
	conn, err := s.clusters.Connection(clusterName)
	if err != nil {
		return nil, err
	}

	client, err := conn.Client()
	if err != nil {
		return nil, err
	}

	return client, nil
}

func validateCreateRequest(req CreateRequest) error {
	if err := validateName(req.Name); err != nil {
		return err
	}

	switch req.Type {
	case "", string(api.InstanceTypeContainer), string(api.InstanceTypeVM):
	default:
		return fmt.Errorf("%w: type must be container or virtual-machine", ErrInvalidRequest)
	}

	if strings.TrimSpace(req.Image) == "" {
		return fmt.Errorf("%w: image is required", ErrInvalidRequest)
	}

	if err := validateUpdateFields(req.CPU, req.Memory, req.RootDiskSize); err != nil {
		return err
	}

	if (req.IPv4 != "" || req.IPv6 != "") && strings.TrimSpace(req.Network) == "" {
		return fmt.Errorf("%w: network is required for a static address", ErrInvalidRequest)
	}

	if err := validateIP("ipv4", req.IPv4, false); err != nil {
		return err
	}
	if err := validateIP("ipv6", req.IPv6, true); err != nil {
		return err
	}

	return nil
}

func validateUpdateRequest(req UpdateRequest) error {
	values := [3]string{}
	if req.CPU != nil {
		values[0] = *req.CPU
	}
	if req.Memory != nil {
		values[1] = *req.Memory
	}
	if req.RootDiskSize != nil {
		values[2] = *req.RootDiskSize
	}
	if err := validateUpdateFields(values[0], values[1], values[2]); err != nil {
		return err
	}

	if req.IPv4 != nil {
		if err := validateIP("ipv4", *req.IPv4, false); err != nil {
			return err
		}
	}
	if req.IPv6 != nil {
		if err := validateIP("ipv6", *req.IPv6, true); err != nil {
			return err
		}
	}

	return nil
}

func validateUpdateFields(cpu, memory, diskSize string) error {
	if strings.TrimSpace(cpu) != "" {
		if err := validateCPU(cpu); err != nil {
			return err
		}
	}
	if strings.TrimSpace(memory) != "" {
		if err := validateSize("memory", memory); err != nil {
			return err
		}
	}
	if strings.TrimSpace(diskSize) != "" {
		if err := validateSize("root disk size", diskSize); err != nil {
			return err
		}
	}

	return nil
}

func validateName(name string) error {
	if name == "" {
		return fmt.Errorf("%w: name is required", ErrInvalidRequest)
	}
	if len(name) > 63 || !instanceNamePattern.MatchString(name) {
		return fmt.Errorf("%w: name must be 1-63 letters, digits, dots, dashes, or underscores and start/end with a letter or digit", ErrInvalidRequest)
	}

	return nil
}

func validateCPU(value string) error {
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return fmt.Errorf("%w: cpu must be a core list such as 2, 0-3, or 1,3", ErrInvalidRequest)
		}

		bounds := strings.Split(part, "-")
		if len(bounds) > 2 {
			return fmt.Errorf("%w: cpu must be a core list such as 2, 0-3, or 1,3", ErrInvalidRequest)
		}

		start, err := strconv.Atoi(bounds[0])
		if err != nil || start < 0 {
			return fmt.Errorf("%w: cpu must be a core list such as 2, 0-3, or 1,3", ErrInvalidRequest)
		}
		if len(bounds) == 2 {
			end, err := strconv.Atoi(bounds[1])
			if err != nil || end < start {
				return fmt.Errorf("%w: cpu range %q is invalid", ErrInvalidRequest, part)
			}
		}
	}

	return nil
}

func validateSize(field, value string) error {
	size, err := units.ParseByteSizeString(strings.TrimSpace(value))
	if err != nil || size <= 0 {
		return fmt.Errorf("%w: %s must be a positive size such as 512MiB", ErrInvalidRequest, field)
	}

	return nil
}

func validateIP(field, value string, wantIPv6 bool) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}

	ip := net.ParseIP(value)
	if ip == nil {
		return fmt.Errorf("%w: %s address %q is invalid", ErrInvalidRequest, field, value)
	}
	isIPv6 := ip.To4() == nil
	if isIPv6 != wantIPv6 {
		return fmt.Errorf("%w: %s address %q has the wrong address family", ErrInvalidRequest, field, value)
	}

	return nil
}

func waitOperation(ctx context.Context, op incus.Operation, what string) error {
	if op == nil {
		return nil
	}
	if err := op.WaitContext(ctx); err != nil {
		return fmt.Errorf("instances: %s: %w", what, err)
	}

	return nil
}

func containsString(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}

	return false
}

func cleanProfiles(profiles []string) []string {
	if profiles == nil {
		return nil
	}

	out := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		if profile = strings.TrimSpace(profile); profile != "" {
			out = append(out, profile)
		}
	}

	return out
}

func copyConfig(config api.ConfigMap) api.ConfigMap {
	out := make(api.ConfigMap, len(config))
	for key, value := range config {
		out[key] = value
	}

	return out
}

func copyDevice(device map[string]string) map[string]string {
	out := make(map[string]string, len(device))
	for key, value := range device {
		out[key] = value
	}

	return out
}

func copyDevices(devices api.DevicesMap) api.DevicesMap {
	out := make(api.DevicesMap, len(devices))
	for name, device := range devices {
		out[name] = copyDevice(device)
	}

	return out
}

func copyStrings(values []string) []string {
	if values == nil {
		return []string{}
	}

	return append([]string{}, values...)
}

// clampUint64ToInt64 converts byte counts reported by Incus without allowing
// values above math.MaxInt64 to wrap into negative numbers.
func clampUint64ToInt64(value uint64) int64 {
	if value > math.MaxInt64 {
		return math.MaxInt64
	}

	return int64(value)
}

func setOrDelete(values map[string]string, key, value string) {
	if value == "" {
		delete(values, key)
		return
	}

	values[key] = value
}

func expandedValue(instance *api.Instance, device, key, fallback string) string {
	if value := instance.ExpandedDevices[device][key]; value != "" {
		return value
	}

	return fallback
}
