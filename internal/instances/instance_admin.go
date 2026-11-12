package instances

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lxc/incus/v7/shared/api"

	"github.com/mickeyzzc/incus-fleet/internal/clusters"
)

const snapshotTimeout = 15 * time.Minute

const volumeType = "custom"

// Snapshot describes one instance snapshot.
type Snapshot struct {
	Name      string `json:"name"`
	CreatedAt string `json:"created_at,omitempty"`
	Size      int64  `json:"size"`
	Stateful  bool   `json:"stateful"`
}

// SnapshotRequest creates an instance snapshot.
type SnapshotRequest struct {
	Name     string `json:"name"`
	Stateful bool   `json:"stateful,omitempty"`
}

// Volume describes one custom storage volume.
type Volume struct {
	Name        string            `json:"name"`
	Type        string            `json:"type"`
	ContentType string            `json:"content_type,omitempty"`
	Description string            `json:"description,omitempty"`
	Config      map[string]string `json:"config"`
	UsedBy      []string          `json:"used_by"`
	Location    string            `json:"location,omitempty"`
}

// VolumeRequest creates a custom storage volume.
type VolumeRequest struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Size        string            `json:"size,omitempty"`
	Config      map[string]string `json:"config,omitempty"`
}

// DeviceRequest attaches one disk, NIC, or proxy device to an instance.
type DeviceRequest struct {
	Name    string `json:"name"`
	Type    string `json:"type"` // disk, nic, or proxy
	Pool    string `json:"pool,omitempty"`
	Source  string `json:"source,omitempty"` // volume name or host path
	Path    string `json:"path,omitempty"`
	Size    string `json:"size,omitempty"`
	Network string `json:"network,omitempty"`
	IPv4    string `json:"ipv4,omitempty"`
	IPv6    string `json:"ipv6,omitempty"`
	Listen  string `json:"listen,omitempty"`  // proxy: tcp:host:port
	Connect string `json:"connect,omitempty"` // proxy: tcp:instance:port
	Boot    string `json:"boot,omitempty"`    // disk: boot.priority for installer ISOs
}

// ListSnapshots returns the snapshots of one instance.
func (s *Service) ListSnapshots(clusterName, name string) ([]Snapshot, error) {
	client, err := s.client(clusterName)
	if err != nil {
		return nil, err
	}

	raw, err := client.GetInstanceSnapshots(name)
	if err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("%w: %s:%s", ErrNotFound, clusterName, name)
		}

		return nil, fmt.Errorf("instances: list snapshots %s:%s: %w", clusterName, name, err)
	}

	out := make([]Snapshot, 0, len(raw))
	for _, snap := range raw {
		short := snap.Name
		if prefix := name + "/"; strings.HasPrefix(short, prefix) {
			short = strings.TrimPrefix(short, prefix)
		}

		created := ""
		if !snap.CreatedAt.IsZero() && snap.CreatedAt.Year() > 1 {
			created = snap.CreatedAt.UTC().Format(time.RFC3339)
		}

		out = append(out, Snapshot{
			Name:      short,
			CreatedAt: created,
			Size:      snap.Size,
			Stateful:  snap.Stateful,
		})
	}

	return out, nil
}

// CreateSnapshot takes a new snapshot and waits for the operation.
func (s *Service) CreateSnapshot(ctx context.Context, clusterName, name string, req SnapshotRequest) error {
	req.Name = strings.TrimSpace(req.Name)
	if validateName(req.Name) != nil {
		return fmt.Errorf("%w: snapshot name is invalid", ErrInvalidRequest)
	}

	client, err := s.client(clusterName)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, snapshotTimeout)
	defer cancel()

	op, err := client.CreateInstanceSnapshot(name, api.InstanceSnapshotsPost{
		Name:     req.Name,
		Stateful: req.Stateful,
	})
	if err != nil {
		return fmt.Errorf("instances: snapshot %s:%s/%s: %w", clusterName, name, req.Name, err)
	}

	return waitOperation(ctx, op, "snapshot "+name+"/"+req.Name)
}

// RestoreSnapshot rolls an instance back to a snapshot.
func (s *Service) RestoreSnapshot(ctx context.Context, clusterName, name, snapshot string) error {
	snapshot = strings.TrimSpace(snapshot)
	if snapshot == "" {
		return fmt.Errorf("%w: snapshot name is required", ErrInvalidRequest)
	}

	client, err := s.client(clusterName)
	if err != nil {
		return err
	}

	instance, etag, err := client.GetInstance(name)
	if err != nil {
		if isNotFound(err) {
			return fmt.Errorf("%w: %s:%s", ErrNotFound, clusterName, name)
		}

		return fmt.Errorf("instances: get %s:%s: %w", clusterName, name, err)
	}

	put := instance.InstancePut
	put.Restore = snapshot

	ctx, cancel := context.WithTimeout(ctx, snapshotTimeout)
	defer cancel()

	op, err := client.UpdateInstance(name, put, etag)
	if err != nil {
		return fmt.Errorf("instances: restore %s:%s/%s: %w", clusterName, name, snapshot, err)
	}

	return waitOperation(ctx, op, "restore "+name+"/"+snapshot)
}

// DeleteSnapshot removes one snapshot and waits for the operation.
func (s *Service) DeleteSnapshot(ctx context.Context, clusterName, name, snapshot string) error {
	snapshot = strings.TrimSpace(snapshot)
	if snapshot == "" {
		return fmt.Errorf("%w: snapshot name is required", ErrInvalidRequest)
	}

	client, err := s.client(clusterName)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, actionTimeout)
	defer cancel()

	op, err := client.DeleteInstanceSnapshot(name, snapshot)
	if err != nil {
		return fmt.Errorf("instances: delete snapshot %s:%s/%s: %w", clusterName, name, snapshot, err)
	}

	return waitOperation(ctx, op, "delete snapshot "+name+"/"+snapshot)
}

// Volumes lists the custom volumes of one storage pool, sorted by name.
func (s *Service) Volumes(clusterName, pool string) ([]Volume, error) {
	client, err := s.client(clusterName)
	if err != nil {
		return nil, err
	}

	raw, err := client.GetStoragePoolVolumes(pool)
	if err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("%w: %s:%s", ErrNotFound, clusterName, pool)
		}

		return nil, fmt.Errorf("instances: list volumes %s:%s: %w", clusterName, pool, err)
	}

	out := make([]Volume, 0, len(raw))
	for _, volume := range raw {
		if volume.Type != volumeType {
			continue
		}

		out = append(out, Volume{
			Name:        volume.Name,
			Type:        volume.Type,
			ContentType: volume.ContentType,
			Description: volume.Description,
			Config:      copyConfig(volume.Config),
			UsedBy:      copyStrings(volume.UsedBy),
			Location:    volume.Location,
		})
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})

	return out, nil
}

// CreateVolume creates a custom storage volume on a pool.
func (s *Service) CreateVolume(clusterName, pool string, req VolumeRequest) error {
	req.Name = strings.TrimSpace(req.Name)
	if validateName(req.Name) != nil {
		return fmt.Errorf("%w: volume name is invalid", ErrInvalidRequest)
	}

	config := make(map[string]string, len(req.Config)+1)
	for key, value := range req.Config {
		config[key] = value
	}

	if size := strings.TrimSpace(req.Size); size != "" {
		if err := validateSize("size", size); err != nil {
			return err
		}

		config["size"] = size
	}

	client, err := s.client(clusterName)
	if err != nil {
		return err
	}

	if err := client.CreateStoragePoolVolume(pool, api.StorageVolumesPost{
		Name:        req.Name,
		Type:        volumeType,
		Description: strings.TrimSpace(req.Description),
		Config:      config,
	}); err != nil {
		return fmt.Errorf("instances: create volume %s:%s/%s: %w", clusterName, pool, req.Name, err)
	}

	return nil
}

// DeleteVolume removes a custom volume from a pool.
func (s *Service) DeleteVolume(clusterName, pool, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("%w: volume name is required", ErrInvalidRequest)
	}

	client, err := s.client(clusterName)
	if err != nil {
		return err
	}

	if err := client.DeleteStoragePoolVolume(pool, volumeType, name); err != nil {
		return fmt.Errorf("instances: delete volume %s:%s/%s: %w", clusterName, pool, name, err)
	}

	return nil
}

// AttachDevice adds one disk or NIC device to an instance.
func (s *Service) AttachDevice(ctx context.Context, clusterName, name string, req DeviceRequest) (Detail, error) {
	deviceName := strings.TrimSpace(req.Name)
	if validateName(deviceName) != nil {
		return Detail{}, fmt.Errorf("%w: device name is invalid", ErrInvalidRequest)
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

	if _, exists := put.Devices[deviceName]; exists {
		return Detail{}, fmt.Errorf("%w: device %q already exists on %s", ErrInvalidRequest, deviceName, name)
	}

	device, err := buildDevice(client, clusterName, deviceName, req)
	if err != nil {
		return Detail{}, err
	}

	put.Devices[deviceName] = device

	ctx, cancel := context.WithTimeout(ctx, updateTimeout)
	defer cancel()

	op, err := client.UpdateInstance(name, put, etag)
	if err != nil {
		return Detail{}, fmt.Errorf("instances: attach %s:%s/%s: %w", clusterName, name, deviceName, err)
	}
	if err := waitOperation(ctx, op, "attach "+name+"/"+deviceName); err != nil {
		return Detail{}, err
	}

	return s.Get(clusterName, name)
}

// DetachDevice removes one locally defined device from an instance.
func (s *Service) DetachDevice(ctx context.Context, clusterName, name, deviceName string) (Detail, error) {
	deviceName = strings.TrimSpace(deviceName)

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

	if _, exists := put.Devices[deviceName]; !exists {
		return Detail{}, fmt.Errorf("%w: device %q is not attached to %s", ErrInvalidRequest, deviceName, name)
	}

	delete(put.Devices, deviceName)

	ctx, cancel := context.WithTimeout(ctx, updateTimeout)
	defer cancel()

	op, err := client.UpdateInstance(name, put, etag)
	if err != nil {
		return Detail{}, fmt.Errorf("instances: detach %s:%s/%s: %w", clusterName, name, deviceName, err)
	}
	if err := waitOperation(ctx, op, "detach "+name+"/"+deviceName); err != nil {
		return Detail{}, err
	}

	return s.Get(clusterName, name)
}

// DeleteImage removes an image from a cluster and waits for the operation.
func (s *Service) DeleteImage(ctx context.Context, clusterName, fingerprint string) error {
	fingerprint = strings.TrimSpace(fingerprint)
	if fingerprint == "" {
		return fmt.Errorf("%w: image fingerprint is required", ErrInvalidRequest)
	}

	client, err := s.client(clusterName)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, deleteTimeout)
	defer cancel()

	op, err := client.DeleteImage(fingerprint)
	if err != nil {
		return fmt.Errorf("instances: delete image %s:%s: %w", clusterName, fingerprint, err)
	}

	return waitOperation(ctx, op, "delete image "+fingerprint)
}

// buildDevice validates the request and renders the Incus device map.
func buildDevice(
	client clusters.InstanceServer,
	clusterName, deviceName string,
	req DeviceRequest,
) (map[string]string, error) {
	switch strings.TrimSpace(req.Type) {
	case "disk":
		path := strings.TrimSpace(req.Path)
		if path == "" {
			return nil, fmt.Errorf("%w: disk device %q requires a mount path", ErrInvalidRequest, deviceName)
		}

		device := map[string]string{"type": "disk", "path": path}
		if pool := strings.TrimSpace(req.Pool); pool != "" {
			device["pool"] = pool
		}
		if source := strings.TrimSpace(req.Source); source != "" {
			device["source"] = source
		}
		if size := strings.TrimSpace(req.Size); size != "" {
			if err := validateSize("size", size); err != nil {
				return nil, err
			}

			device["size"] = size
		}

		return device, nil
	case "nic":
		network := strings.TrimSpace(req.Network)
		if network == "" {
			return nil, fmt.Errorf("%w: nic device %q requires a network", ErrInvalidRequest, deviceName)
		}

		networks, err := client.GetNetworkNames()
		if err != nil {
			return nil, fmt.Errorf("instances: list networks for %s: %w", clusterName, err)
		}
		if !containsString(networks, network) {
			return nil, fmt.Errorf("%w: network %q does not exist on %s", ErrInvalidRequest, network, clusterName)
		}

		if err := validateIP("ipv4", req.IPv4, false); err != nil {
			return nil, err
		}
		if err := validateIP("ipv6", req.IPv6, true); err != nil {
			return nil, err
		}

		device := map[string]string{"type": "nic", "network": network}
		if ip := strings.TrimSpace(req.IPv4); ip != "" {
			device["ipv4.address"] = ip
		}
		if ip := strings.TrimSpace(req.IPv6); ip != "" {
			device["ipv6.address"] = ip
		}

		return device, nil
	case "proxy":
		listen := strings.TrimSpace(req.Listen)
		connect := strings.TrimSpace(req.Connect)
		if listen == "" || connect == "" {
			return nil, fmt.Errorf("%w: proxy device %q requires listen and connect", ErrInvalidRequest, deviceName)
		}
		if err := validateProxyEndpoint("listen", listen); err != nil {
			return nil, err
		}
		if err := validateProxyEndpoint("connect", connect); err != nil {
			return nil, err
		}

		return map[string]string{"type": "proxy", "listen": listen, "connect": connect}, nil
	default:
		return nil, fmt.Errorf("%w: device type must be disk, nic, or proxy", ErrInvalidRequest)
	}
}

// validateProxyEndpoint checks a tcp:/udp: proxy endpoint shape.
func validateProxyEndpoint(field, value string) error {
	if !strings.HasPrefix(value, "tcp:") && !strings.HasPrefix(value, "udp:") {
		return fmt.Errorf("%w: proxy %s must start with tcp: or udp:", ErrInvalidRequest, field)
	}

	addr := value[strings.Index(value, ":")+1:]
	host, port, ok := strings.Cut(addr, ":")
	if !ok || host == "" || port == "" {
		return fmt.Errorf("%w: proxy %s must be <proto>:<host>:<port>", ErrInvalidRequest, field)
	}

	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return fmt.Errorf("%w: proxy %s port must be between 1 and 65535", ErrInvalidRequest, field)
	}

	return nil
}
