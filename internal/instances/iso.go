package instances

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	incus "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"

	"github.com/mickeyzzc/incus-fleet/internal/clusters"
)

const isoVolumeType = "custom"

// ErrInUse is returned when an ISO volume is still attached to an instance.
// The message is user-facing: it tells the operator how to unblock deletion.
var ErrInUse = errors.New("ISO is still attached to an instance; detach it from the instance's devices before deleting")

// ISOSummary describes one installer ISO stored as a custom volume.
type ISOSummary struct {
	Name        string `json:"name"`
	Pool        string `json:"pool"`
	Description string `json:"description,omitempty"`
	Size        string `json:"size,omitempty"`
}

// LaunchISORequest creates an empty virtual machine that boots from an ISO.
type LaunchISORequest struct {
	Name        string `json:"name"`
	ISOName     string `json:"iso_name"`
	StoragePool string `json:"storage_pool,omitempty"`
	Network     string `json:"network,omitempty"`
	RootDisk    string `json:"root_disk,omitempty"`
	Start       *bool  `json:"start,omitempty"`
}

// ImportISO uploads an ISO image into a cluster as a custom storage volume.
// The volume name is derived from the file name and returned.
func (s *Service) ImportISO(ctx context.Context, clusterName, pool, filename string, reader io.Reader) (string, error) {
	filename = strings.TrimSpace(filename)
	pool = strings.TrimSpace(pool)
	if filename == "" {
		return "", fmt.Errorf("%w: iso filename is required", ErrInvalidRequest)
	}
	if pool == "" {
		return "", fmt.Errorf("%w: storage pool is required", ErrInvalidRequest)
	}

	client, err := s.client(clusterName)
	if err != nil {
		return "", err
	}

	name := strings.TrimSuffix(filename, ".iso")
	if name == "" {
		name = filename
	}

	ctx, cancel := context.WithTimeout(ctx, importTimeout)
	defer cancel()

	op, err := client.CreateStoragePoolVolumeFromISO(pool, incus.StorageVolumeBackupArgs{
		Name:       name,
		BackupFile: reader,
	})
	if err != nil {
		return "", fmt.Errorf("instances: import iso %s:%s: %w", clusterName, filename, err)
	}
	if err := waitOperation(ctx, op, "import iso "+name); err != nil {
		return "", err
	}

	return name, nil
}

// ListISOs lists installer ISO volumes across all storage pools.
func (s *Service) ListISOs(clusterName string) ([]ISOSummary, error) {
	client, err := s.client(clusterName)
	if err != nil {
		return nil, err
	}

	pools, err := client.GetStoragePoolNames()
	if err != nil {
		return nil, fmt.Errorf("instances: list storage pools for %s: %w", clusterName, err)
	}

	var isos []ISOSummary
	for _, pool := range pools {
		volumes, err := client.GetStoragePoolVolumes(pool)
		if err != nil {
			return nil, fmt.Errorf("instances: list volumes %s:%s: %w", clusterName, pool, err)
		}

		for i := range volumes {
			if volumes[i].ContentType != "iso" {
				continue
			}
			isos = append(isos, ISOSummary{
				Name:        volumes[i].Name,
				Pool:        pool,
				Description: volumes[i].Description,
				Size:        volumes[i].Config["size"],
			})
		}
	}

	return isos, nil
}

// DeleteISO removes one ISO volume from its pool.
func (s *Service) DeleteISO(clusterName, pool, name string) error {
	name = strings.TrimSpace(name)
	pool = strings.TrimSpace(pool)
	if name == "" {
		return fmt.Errorf("%w: iso volume name is required", ErrInvalidRequest)
	}
	if pool == "" {
		return fmt.Errorf("%w: storage pool is required", ErrInvalidRequest)
	}

	client, err := s.client(clusterName)
	if err != nil {
		return err
	}

	if err := client.DeleteStoragePoolVolume(pool, isoVolumeType, name); err != nil {
		if isNotFound(err) {
			return fmt.Errorf("%w: %s:%s/%s", ErrNotFound, clusterName, pool, name)
		}
		if strings.Contains(strings.ToLower(err.Error()), "still in use") {
			// Incus rejects the delete while the ISO is attached as a device.
			// Surface an actionable 409 instead of a generic 500.
			return fmt.Errorf("iso %q: %w", name, ErrInUse)
		}

		return fmt.Errorf("instances: delete iso %s:%s/%s: %w", clusterName, pool, name, err)
	}

	return nil
}

// LaunchFromISO creates an empty VM with the ISO attached as the boot device
// so the installer starts on first power-on.
func (s *Service) LaunchFromISO(ctx context.Context, clusterName string, req LaunchISORequest) (Detail, error) {
	req.Name = strings.TrimSpace(req.Name)
	req.ISOName = strings.TrimSpace(req.ISOName)
	req.StoragePool = strings.TrimSpace(req.StoragePool)
	if req.StoragePool == "" {
		req.StoragePool = "default"
	}
	if req.Name == "" {
		return Detail{}, fmt.Errorf("%w: instance name is required", ErrInvalidRequest)
	}
	if req.ISOName == "" {
		return Detail{}, fmt.Errorf("%w: iso volume name is required", ErrInvalidRequest)
	}

	client, err := s.client(clusterName)
	if err != nil {
		return Detail{}, err
	}

	pools, err := client.GetStoragePoolNames()
	if err != nil {
		return Detail{}, fmt.Errorf("instances: list storage pools for %s: %w", clusterName, err)
	}
	if !containsString(pools, req.StoragePool) {
		return Detail{}, fmt.Errorf("%w: storage pool %q does not exist on %s", ErrInvalidRequest, req.StoragePool, clusterName)
	}

	root := map[string]string{"type": "disk", "path": "/", "pool": req.StoragePool}
	if req.RootDisk != "" {
		root["size"] = req.RootDisk
	}
	devices := api.DevicesMap{
		"root": root,
		// An ISO custom volume must NOT have a path: Incus replies "Custom ISO
		// volumes cannot have a path defined". boot.priority makes it the boot
		// device. The instance also has to live on the member that holds the
		// volume, otherwise creation fails with "Storage volume not found".
		"iso0": {
			"type":          "disk",
			"pool":          req.StoragePool,
			"source":        req.ISOName,
			"boot.priority": "1",
		},
	}

	if network := strings.TrimSpace(req.Network); network != "" {
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

		if target.Managed {
			devices["eth0"] = map[string]string{"type": "nic", "network": network}
		} else {
			devices["eth0"] = map[string]string{"type": "nic", "nictype": "macvlan", "parent": network}
		}
	}

	start := true
	if req.Start != nil {
		start = *req.Start
	}

	// Arm a qemu SPICE TCP listener up front so the remote desktop is
	// available from the very first boot without another restart, and
	// relax secure boot: most distribution installers ship boot loaders the
	// default VM firmware rejects.
	spicePort, err := allocateSPICEPort()
	if err != nil {
		return Detail{}, fmt.Errorf("instances: allocate spice port: %w", err)
	}

	config := api.ConfigMap{}
	config["raw.qemu"] = fmt.Sprintf("%s unix=off,port=%d,disable-ticketing=on,addr=%s", // loopback only; bridged over the Incus API
		spiceArgMarker, spicePort, spiceListenAddr)
	config["security.secureboot"] = "false"

	// A cluster-wide custom ISO volume only exists on the member that holds it,
	// so the VM must be created there. Creating on another member fails with
	// "Failed loading custom volume: Storage volume not found".
	member, err := s.isoVolumeMember(client, req.StoragePool, req.ISOName)
	if err != nil {
		return Detail{}, err
	}

	post := api.InstancesPost{
		Name: req.Name,
		Type: api.InstanceTypeVM,
		InstancePut: api.InstancePut{
			Config:  config,
			Devices: devices,
		},
		Source: api.InstanceSource{Type: "none"},
		Start:  start,
	}
	if member != "" {
		// The pool volume is stored on one member only. Bind the create call to
		// that member so the ISO device resolves; the assertion is safe because
		// clusters.InstanceServer is always satisfied by the Incus client.
		targeted, ok := any(client).(interface {
			UseTarget(name string) incus.InstanceServer
		})
		if !ok {
			return Detail{}, fmt.Errorf("%w: cluster client does not support targeting", ErrInvalidRequest)
		}
		return s.createISOInstance(ctx, targeted.UseTarget(member), clusterName, req, post)
	}

	return s.createISOInstance(ctx, client, clusterName, req, post)
}

// createISOInstance creates the VM on the given client and returns its detail.
func (s *Service) createISOInstance(ctx context.Context, client instanceCreator,
	clusterName string, req LaunchISORequest, post api.InstancesPost,
) (Detail, error) {
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

// isoVolumeMember returns the cluster member that stores a custom ISO volume, or
// an empty string when the member cannot be determined (single-node clusters and
// older daemons). Pinning the VM to that member is what makes the ISO device
// resolvable.
func (s *Service) isoVolumeMember(client clusters.InstanceServer, pool, volume string) (string, error) {
	vol, _, err := client.GetStoragePoolVolume(pool, "custom", volume)
	if err != nil {
		// Surface a clear message instead of Incus's device-validation error.
		if isNotFound(err) {
			return "", fmt.Errorf("%w: ISO volume %q not found in pool %q", ErrNotFound, volume, pool)
		}
		return "", fmt.Errorf("instances: inspect iso volume %s/%s: %w", pool, volume, err)
	}

	return vol.Location, nil
}

// instanceCreator is the single method createISOInstance needs; both the fleet
// client and a member-bound Incus client satisfy it.
type instanceCreator interface {
	CreateInstance(instance api.InstancesPost) (incus.Operation, error)
}
