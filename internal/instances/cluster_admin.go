package instances

import (
	"fmt"
	"net"
	"sort"
	"strings"

	"github.com/lxc/incus/v7/shared/api"
)

// StoragePoolRequest creates or updates a cluster storage pool.
type StoragePoolRequest struct {
	Name        string            `json:"name,omitempty"`
	Driver      string            `json:"driver,omitempty"`
	Description *string           `json:"description,omitempty"`
	Config      map[string]string `json:"config,omitempty"`
}

// NetworkRequest creates or updates a managed cluster network.
type NetworkRequest struct {
	Name        string            `json:"name,omitempty"`
	Type        string            `json:"type,omitempty"`
	Description *string           `json:"description,omitempty"`
	Config      map[string]string `json:"config,omitempty"`
}

// Profile describes one instance profile.
type Profile struct {
	Name        string                       `json:"name"`
	Description string                       `json:"description,omitempty"`
	Config      map[string]string            `json:"config"`
	Devices     map[string]map[string]string `json:"devices"`
	UsedBy      []string                     `json:"used_by"`
}

// ProfileRequest creates or updates an instance profile.
type ProfileRequest struct {
	Name        string                       `json:"name,omitempty"`
	Description *string                      `json:"description,omitempty"`
	Config      map[string]string            `json:"config,omitempty"`
	Devices     map[string]map[string]string `json:"devices,omitempty"`
}

// MemberUpdateRequest changes non-destructive cluster member metadata.
type MemberUpdateRequest struct {
	Description   *string   `json:"description,omitempty"`
	FailureDomain *string   `json:"failure_domain,omitempty"`
	Groups        *[]string `json:"groups,omitempty"`
}

// CreateStoragePool creates a storage pool on a cluster.
func (s *Service) CreateStoragePool(clusterName string, req StoragePoolRequest) error {
	req.Name = strings.TrimSpace(req.Name)
	req.Driver = strings.TrimSpace(req.Driver)
	if err := validateResourceName("storage pool", req.Name); err != nil {
		return err
	}
	if req.Driver == "" {
		return fmt.Errorf("%w: storage pool driver is required", ErrInvalidRequest)
	}

	client, err := s.client(clusterName)
	if err != nil {
		return err
	}

	pool := api.StoragePoolsPost{
		Name:   req.Name,
		Driver: req.Driver,
		StoragePoolPut: api.StoragePoolPut{
			Config:      createConfig(req.Config),
			Description: stringValue(req.Description),
		},
	}
	if err := client.CreateStoragePool(pool); err != nil {
		return fmt.Errorf("instances: create storage pool %s:%s: %w", clusterName, req.Name, err)
	}

	return nil
}

// UpdateStoragePool merges configuration and description changes into a pool.
func (s *Service) UpdateStoragePool(clusterName, name string, req StoragePoolRequest) error {
	name = strings.TrimSpace(name)
	if err := validateResourceName("storage pool", name); err != nil {
		return err
	}

	client, err := s.client(clusterName)
	if err != nil {
		return err
	}

	current, etag, err := client.GetStoragePool(name)
	if err != nil {
		return fmt.Errorf("instances: get storage pool %s:%s: %w", clusterName, name, err)
	}

	pool := api.StoragePoolPut{
		Config:      mergeConfig(current.Config, req.Config),
		Description: current.Description,
	}
	if req.Description != nil {
		pool.Description = strings.TrimSpace(*req.Description)
	}

	if err := client.UpdateStoragePool(name, pool, etag); err != nil {
		return fmt.Errorf("instances: update storage pool %s:%s: %w", clusterName, name, err)
	}

	return nil
}

// DeleteStoragePool removes a storage pool from a cluster.
func (s *Service) DeleteStoragePool(clusterName, name string) error {
	name = strings.TrimSpace(name)
	if err := validateResourceName("storage pool", name); err != nil {
		return err
	}

	client, err := s.client(clusterName)
	if err != nil {
		return err
	}

	if err := client.DeleteStoragePool(name); err != nil {
		return fmt.Errorf("instances: delete storage pool %s:%s: %w", clusterName, name, err)
	}

	return nil
}

// CreateNetwork creates a managed network on a cluster.
func (s *Service) CreateNetwork(clusterName string, req NetworkRequest) error {
	req.Name = strings.TrimSpace(req.Name)
	req.Type = strings.TrimSpace(req.Type)
	if err := validateResourceName("network", req.Name); err != nil {
		return err
	}
	if req.Type == "" {
		return fmt.Errorf("%w: network type is required", ErrInvalidRequest)
	}
	if err := validateNetworkAddresses(req.Config); err != nil {
		return err
	}

	client, err := s.client(clusterName)
	if err != nil {
		return err
	}

	network := api.NetworksPost{
		Name: req.Name,
		Type: req.Type,
		NetworkPut: api.NetworkPut{
			Config:      createConfig(req.Config),
			Description: stringValue(req.Description),
		},
	}
	if err := client.CreateNetwork(network); err != nil {
		return fmt.Errorf("instances: create network %s:%s: %w", clusterName, req.Name, err)
	}

	return nil
}

// UpdateNetwork merges configuration and description changes into a managed
// network. Unmanaged networks are reported as invalid requests.
func (s *Service) UpdateNetwork(clusterName, name string, req NetworkRequest) error {
	name = strings.TrimSpace(name)
	if err := validateResourceName("network", name); err != nil {
		return err
	}
	if err := validateNetworkAddresses(req.Config); err != nil {
		return err
	}

	client, err := s.client(clusterName)
	if err != nil {
		return err
	}

	current, etag, err := client.GetNetwork(name)
	if err != nil {
		return fmt.Errorf("instances: get network %s:%s: %w", clusterName, name, err)
	}
	if !current.Managed {
		return fmt.Errorf("%w: network %q is not managed by Incus", ErrInvalidRequest, name)
	}

	network := api.NetworkPut{
		Config:      mergeConfig(current.Config, req.Config),
		Description: current.Description,
	}
	if req.Description != nil {
		network.Description = strings.TrimSpace(*req.Description)
	}

	if err := client.UpdateNetwork(name, network, etag); err != nil {
		return fmt.Errorf("instances: update network %s:%s: %w", clusterName, name, err)
	}

	return nil
}

// DeleteNetwork removes a managed network from a cluster.
func (s *Service) DeleteNetwork(clusterName, name string) error {
	name = strings.TrimSpace(name)
	if err := validateResourceName("network", name); err != nil {
		return err
	}

	client, err := s.client(clusterName)
	if err != nil {
		return err
	}

	if err := client.DeleteNetwork(name); err != nil {
		return fmt.Errorf("instances: delete network %s:%s: %w", clusterName, name, err)
	}

	return nil
}

// Profiles lists instance profiles on a cluster.
func (s *Service) Profiles(clusterName string) ([]Profile, error) {
	client, err := s.client(clusterName)
	if err != nil {
		return nil, err
	}

	list, err := client.GetProfiles()
	if err != nil {
		return nil, fmt.Errorf("instances: list profiles %s: %w", clusterName, err)
	}

	out := make([]Profile, 0, len(list))
	for _, profile := range list {
		out = append(out, Profile{
			Name:        profile.Name,
			Description: profile.Description,
			Config:      copyConfig(profile.Config),
			Devices:     copyDevices(profile.Devices),
			UsedBy:      copyStrings(profile.UsedBy),
		})
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})

	return out, nil
}

// CreateProfile creates an instance profile.
func (s *Service) CreateProfile(clusterName string, req ProfileRequest) error {
	req.Name = strings.TrimSpace(req.Name)
	if err := validateResourceName("profile", req.Name); err != nil {
		return err
	}
	if err := validateDevices(req.Devices); err != nil {
		return err
	}

	client, err := s.client(clusterName)
	if err != nil {
		return err
	}

	profile := api.ProfilesPost{
		Name: req.Name,
		ProfilePut: api.ProfilePut{
			Config:      createConfig(req.Config),
			Devices:     createDevices(req.Devices),
			Description: stringValue(req.Description),
		},
	}
	if err := client.CreateProfile(profile); err != nil {
		return fmt.Errorf("instances: create profile %s:%s: %w", clusterName, req.Name, err)
	}

	return nil
}

// UpdateProfile merges configuration and replaces devices when provided.
func (s *Service) UpdateProfile(clusterName, name string, req ProfileRequest) error {
	name = strings.TrimSpace(name)
	if err := validateResourceName("profile", name); err != nil {
		return err
	}
	if req.Devices != nil {
		if err := validateDevices(req.Devices); err != nil {
			return err
		}
	}

	client, err := s.client(clusterName)
	if err != nil {
		return err
	}

	current, etag, err := client.GetProfile(name)
	if err != nil {
		return fmt.Errorf("instances: get profile %s:%s: %w", clusterName, name, err)
	}

	profile := api.ProfilePut{
		Config:      mergeConfig(current.Config, req.Config),
		Devices:     copyDevices(current.Devices),
		Description: current.Description,
	}
	if req.Devices != nil {
		profile.Devices = createDevices(req.Devices)
	}
	if req.Description != nil {
		profile.Description = strings.TrimSpace(*req.Description)
	}

	if err := client.UpdateProfile(name, profile, etag); err != nil {
		return fmt.Errorf("instances: update profile %s:%s: %w", clusterName, name, err)
	}

	return nil
}

// DeleteProfile removes an instance profile from a cluster.
func (s *Service) DeleteProfile(clusterName, name string) error {
	name = strings.TrimSpace(name)
	if err := validateResourceName("profile", name); err != nil {
		return err
	}

	client, err := s.client(clusterName)
	if err != nil {
		return err
	}

	if err := client.DeleteProfile(name); err != nil {
		return fmt.Errorf("instances: delete profile %s:%s: %w", clusterName, name, err)
	}

	return nil
}

// UpdateMember changes member description, failure domain, or groups while
// preserving roles and configuration.
func (s *Service) UpdateMember(clusterName, name string, req MemberUpdateRequest) error {
	name = strings.TrimSpace(name)
	if err := validateResourceName("cluster member", name); err != nil {
		return err
	}

	client, err := s.client(clusterName)
	if err != nil {
		return err
	}

	current, etag, err := client.GetClusterMember(name)
	if err != nil {
		return fmt.Errorf("instances: get cluster member %s:%s: %w", clusterName, name, err)
	}

	member := api.ClusterMemberPut{
		Roles:         append([]string(nil), current.Roles...),
		FailureDomain: current.FailureDomain,
		Description:   current.Description,
		Config:        copyConfig(current.Config),
		Groups:        append([]string{}, current.Groups...),
	}
	if req.Description != nil {
		member.Description = strings.TrimSpace(*req.Description)
	}
	if req.FailureDomain != nil {
		member.FailureDomain = strings.TrimSpace(*req.FailureDomain)
	}
	if req.Groups != nil {
		member.Groups = cleanStrings(*req.Groups)
		if member.Groups == nil {
			member.Groups = []string{}
		}
	}

	if err := client.UpdateClusterMember(name, member, etag); err != nil {
		return fmt.Errorf("instances: update cluster member %s:%s: %w", clusterName, name, err)
	}

	return nil
}

// RemoveMember removes one member from the cluster. Incus requires the member
// to be evacuated or empty first; force only bypasses the health check.
func (s *Service) RemoveMember(clusterName, name string, force bool) error {
	name = strings.TrimSpace(name)
	if err := validateResourceName("cluster member", name); err != nil {
		return err
	}

	client, err := s.client(clusterName)
	if err != nil {
		return err
	}

	if _, _, err := client.GetClusterMember(name); err != nil {
		return fmt.Errorf("instances: get cluster member %s:%s: %w", clusterName, name, err)
	}

	if err := client.DeleteClusterMember(name, force); err != nil {
		// Incus refuses to remove a member that still owns instances. Surface
		// that as a caller error so the UI can explain the required next step
		// instead of a generic 500.
		if strings.Contains(err.Error(), "still has the following instances") {
			return fmt.Errorf("%w: member %s still hosts instances; migrate or delete them before removing the node: %v",
				ErrInvalidRequest, name, err)
		}
		return fmt.Errorf("instances: delete cluster member %s:%s: %w", clusterName, name, err)
	}

	return nil
}

func validateResourceName(kind, name string) error {
	if name == "" {
		return fmt.Errorf("%w: %s name is required", ErrInvalidRequest, kind)
	}
	if !instanceNamePattern.MatchString(name) {
		return fmt.Errorf("%w: %s name %q is invalid", ErrInvalidRequest, kind, name)
	}

	return nil
}

func validateNetworkAddresses(config map[string]string) error {
	for key, wantIPv6 := range map[string]bool{"ipv4.address": false, "ipv6.address": true} {
		value := strings.TrimSpace(config[key])
		if value == "" || value == "auto" || value == "none" {
			continue
		}

		ip, _, err := net.ParseCIDR(value)
		if err != nil {
			return fmt.Errorf("%w: network address %q is invalid", ErrInvalidRequest, value)
		}
		if (ip.To4() == nil) != wantIPv6 {
			return fmt.Errorf("%w: network address %q has the wrong address family", ErrInvalidRequest, value)
		}
	}

	return nil
}

func validateDevices(devices map[string]map[string]string) error {
	for name, device := range devices {
		if !instanceNamePattern.MatchString(name) {
			return fmt.Errorf("%w: device name %q is invalid", ErrInvalidRequest, name)
		}
		if strings.TrimSpace(device["type"]) == "" {
			return fmt.Errorf("%w: device %q requires a type", ErrInvalidRequest, name)
		}
	}

	return nil
}

func createConfig(config map[string]string) api.ConfigMap {
	out := api.ConfigMap{}
	for key, value := range requestConfig(config) {
		if value != "" {
			out[key] = value
		}
	}

	return out
}

func mergeConfig(base api.ConfigMap, patch map[string]string) api.ConfigMap {
	out := copyConfig(base)
	for key, value := range requestConfig(patch) {
		if value == "" {
			delete(out, key)
			continue
		}
		out[key] = value
	}

	return out
}

func requestConfig(config map[string]string) map[string]string {
	out := make(map[string]string, len(config))
	for key, value := range config {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = strings.TrimSpace(value)
	}

	return out
}

func createDevices(devices map[string]map[string]string) api.DevicesMap {
	out := api.DevicesMap{}
	for name, device := range devices {
		copied := map[string]string{}
		for key, value := range device {
			copied[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
		out[name] = copied
	}

	return out
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}

	return strings.TrimSpace(*value)
}

func cleanStrings(values []string) []string {
	if values == nil {
		return nil
	}

	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}

	return out
}
