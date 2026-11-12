// Package incusfake provides test doubles for the Incus client interfaces.
package incusfake

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
	incus "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"
)

// Server is a configurable fake Incus server implementing the narrow
// interface used by the fleet services.
type Server struct {
	Server            *api.Server
	ServerErr         error
	Cluster           *api.Cluster
	ClusterErr        error
	Members           []api.ClusterMember
	MembersErr        error
	Instances         []api.InstanceFull
	InstancesErr      error
	Instance          *api.Instance
	InstanceErr       error
	State             *api.InstanceState
	StateFunc         func(name string) (*api.InstanceState, error)
	StateErr          error
	UpdateStateErr    error
	CreateErr         error
	UpdateErr         error
	CopyErr           error
	DeleteErr         error
	Images            []api.Image
	ImagesErr         error
	DeleteImageErr    error
	Snapshots         []api.InstanceSnapshot
	SnapshotsErr      error
	CreateSnapshotErr error
	DeleteSnapshotErr error
	Volumes           []api.StorageVolume
	VolumesErr        error
	CreateVolumeErr   error
	DeleteVolumeErr   error
	Pools             []api.StoragePool
	PoolsErr          error
	Pool              *api.StoragePool
	PoolErr           error
	CreatePoolErr     error
	UpdatePoolErr     error
	DeletePoolErr     error
	Networks          []api.Network
	NetworksErr       error
	Network           *api.Network
	NetworkErr        error
	CreateNetworkErr  error
	UpdateNetworkErr  error
	DeleteNetworkErr  error
	Profiles          []api.Profile
	ProfilesErr       error
	Profile           *api.Profile
	ProfileErr        error
	CreateProfileErr  error
	UpdateProfileErr  error
	DeleteProfileErr  error
	Member            *api.ClusterMember
	MemberErr         error
	UpdateMemberErr   error
	PoolNames         []string
	PoolNamesErr      error
	NetworkNames      []string
	NetworkNamesErr   error
	PoolResources     *api.ResourcesStoragePool
	PoolResourcesErr  error
	Clustered         bool
	RemoteOperation   *RemoteOperation
	Operation         *Operation
	Disconnects       int

	mu              sync.Mutex
	calls           []string
	copyArgs        []*incus.InstanceCopyArgs
	createArgs      []api.InstancesPost
	updateArgs      []UpdateInstanceArgs
	updateStateArgs []api.InstanceStatePut
	copySource      incus.InstanceServer
	createPools     []api.StoragePoolsPost
	updatePools     []UpdateStoragePoolArgs
	deletePools     []string
	createNetworks  []api.NetworksPost
	updateNetworks  []UpdateNetworkArgs
	deleteNetworks  []string
	createProfiles  []api.ProfilesPost
	updateProfiles  []UpdateProfileArgs
	deleteProfiles  []string
	updateMembers   []UpdateMemberArgs
	createSnapshots []SnapshotArgs
	deleteSnapshots []SnapshotArgs
	deleteImages    []string
	createVolumes   []VolumeArgs
	deleteVolumes   []VolumeArgs
}

// SnapshotArgs records one instance snapshot call.
type SnapshotArgs struct {
	Instance string
	Name     string
	Stateful bool
}

// VolumeArgs records one storage volume call.
type VolumeArgs struct {
	Pool   string
	Type   string
	Name   string
	Volume api.StorageVolumesPost
}

// CreateSnapshotArgs returns the arguments recorded by CreateInstanceSnapshot.
func (s *Server) CreateSnapshotArgs() []SnapshotArgs {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]SnapshotArgs(nil), s.createSnapshots...)
}

// DeleteSnapshotArgs returns the arguments recorded by DeleteInstanceSnapshot.
func (s *Server) DeleteSnapshotArgs() []SnapshotArgs {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]SnapshotArgs(nil), s.deleteSnapshots...)
}

// DeleteImageArgs returns the fingerprints recorded by DeleteImage.
func (s *Server) DeleteImageArgs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]string(nil), s.deleteImages...)
}

// CreateVolumeArgs returns the arguments recorded by CreateStoragePoolVolume.
func (s *Server) CreateVolumeArgs() []VolumeArgs {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]VolumeArgs(nil), s.createVolumes...)
}

// DeleteVolumeArgs returns the keys recorded by DeleteStoragePoolVolume.
func (s *Server) DeleteVolumeArgs() []VolumeArgs {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]VolumeArgs(nil), s.deleteVolumes...)
}

// UpdateStoragePoolArgs records one UpdateStoragePool call.
type UpdateStoragePoolArgs struct {
	Name string
	Pool api.StoragePoolPut
	ETag string
}

// UpdateNetworkArgs records one UpdateNetwork call.
type UpdateNetworkArgs struct {
	Name    string
	Network api.NetworkPut
	ETag    string
}

// UpdateProfileArgs records one UpdateProfile call.
type UpdateProfileArgs struct {
	Name    string
	Profile api.ProfilePut
	ETag    string
}

// UpdateMemberArgs records one UpdateClusterMember call.
type UpdateMemberArgs struct {
	Name   string
	Member api.ClusterMemberPut
	ETag   string
}

// UpdateInstanceArgs records one UpdateInstance call.
type UpdateInstanceArgs struct {
	Name     string
	Instance api.InstancePut
	ETag     string
}

// Calls returns a copy of the recorded call log.
func (s *Server) Calls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]string(nil), s.calls...)
}

// HasCall reports whether any recorded call starts with prefix.
func (s *Server) HasCall(prefix string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, call := range s.calls {
		if strings.HasPrefix(call, prefix) {
			return true
		}
	}

	return false
}

// CountCalls returns the number of recorded calls starting with prefix.
func (s *Server) CountCalls(prefix string) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	count := 0
	for _, call := range s.calls {
		if strings.HasPrefix(call, prefix) {
			count++
		}
	}

	return count
}

// CopyArgs returns the arguments recorded by CopyInstance.
func (s *Server) CopyArgs() []*incus.InstanceCopyArgs {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]*incus.InstanceCopyArgs(nil), s.copyArgs...)
}

// CopySource returns the source recorded by CopyInstance.
func (s *Server) CopySource() incus.InstanceServer {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.copySource
}

// UpdateStateArgs returns the arguments recorded by UpdateInstanceState.
func (s *Server) UpdateStateArgs() []api.InstanceStatePut {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]api.InstanceStatePut(nil), s.updateStateArgs...)
}

// CreateArgs returns the arguments recorded by CreateInstance.
func (s *Server) CreateArgs() []api.InstancesPost {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]api.InstancesPost(nil), s.createArgs...)
}

// UpdateArgs returns the arguments recorded by UpdateInstance.
func (s *Server) UpdateArgs() []UpdateInstanceArgs {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]UpdateInstanceArgs(nil), s.updateArgs...)
}

// CreatePoolArgs returns the arguments recorded by CreateStoragePool.
func (s *Server) CreatePoolArgs() []api.StoragePoolsPost {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]api.StoragePoolsPost(nil), s.createPools...)
}

// UpdatePoolArgs returns the arguments recorded by UpdateStoragePool.
func (s *Server) UpdatePoolArgs() []UpdateStoragePoolArgs {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]UpdateStoragePoolArgs(nil), s.updatePools...)
}

// DeletePoolArgs returns the names recorded by DeleteStoragePool.
func (s *Server) DeletePoolArgs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]string(nil), s.deletePools...)
}

// CreateNetworkArgs returns the arguments recorded by CreateNetwork.
func (s *Server) CreateNetworkArgs() []api.NetworksPost {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]api.NetworksPost(nil), s.createNetworks...)
}

// UpdateNetworkArgs returns the arguments recorded by UpdateNetwork.
func (s *Server) UpdateNetworkArgs() []UpdateNetworkArgs {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]UpdateNetworkArgs(nil), s.updateNetworks...)
}

// DeleteNetworkArgs returns the names recorded by DeleteNetwork.
func (s *Server) DeleteNetworkArgs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]string(nil), s.deleteNetworks...)
}

// CreateProfileArgs returns the arguments recorded by CreateProfile.
func (s *Server) CreateProfileArgs() []api.ProfilesPost {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]api.ProfilesPost(nil), s.createProfiles...)
}

// UpdateProfileArgs returns the arguments recorded by UpdateProfile.
func (s *Server) UpdateProfileArgs() []UpdateProfileArgs {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]UpdateProfileArgs(nil), s.updateProfiles...)
}

// DeleteProfileArgs returns the names recorded by DeleteProfile.
func (s *Server) DeleteProfileArgs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]string(nil), s.deleteProfiles...)
}

// UpdateMemberArgs returns the arguments recorded by UpdateClusterMember.
func (s *Server) UpdateMemberArgs() []UpdateMemberArgs {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]UpdateMemberArgs(nil), s.updateMembers...)
}

func (s *Server) record(call string) {
	s.mu.Lock()
	s.calls = append(s.calls, call)
	s.mu.Unlock()
}

// GetServer implements clusters.InstanceServer.
func (s *Server) GetServer() (*api.Server, string, error) {
	s.record("GetServer")
	if s.ServerErr != nil {
		return nil, "", s.ServerErr
	}
	if s.Server != nil {
		return s.Server, "", nil
	}

	return &api.Server{Environment: api.ServerEnvironment{
		Server:          "incus",
		ServerVersion:   "7.0.2",
		ServerName:      "test-node",
		ServerClustered: s.Clustered,
		Architectures:   []string{"x86_64"},
	}}, "", nil
}

// GetCluster implements clusters.InstanceServer.
func (s *Server) GetCluster() (*api.Cluster, string, error) {
	s.record("GetCluster")
	if s.ClusterErr != nil {
		return nil, "", s.ClusterErr
	}
	if s.Cluster != nil {
		return s.Cluster, "", nil
	}

	return &api.Cluster{Enabled: s.Clustered, ServerName: "test-node"}, "", nil
}

// GetClusterMembers implements clusters.InstanceServer.
func (s *Server) GetClusterMembers() ([]api.ClusterMember, error) {
	s.record("GetClusterMembers")
	if s.MembersErr != nil {
		return nil, s.MembersErr
	}
	if s.Members != nil {
		return s.Members, nil
	}

	return []api.ClusterMember{{ServerName: "test-node", Status: "Online", Architecture: "x86_64"}}, nil
}

// GetInstancesFull implements clusters.InstanceServer.
func (s *Server) GetInstancesFull(instanceType api.InstanceType) ([]api.InstanceFull, error) {
	s.record("GetInstancesFull:" + string(instanceType))
	if s.InstancesErr != nil {
		return nil, s.InstancesErr
	}

	return s.Instances, nil
}

// GetInstance implements clusters.InstanceServer.
func (s *Server) GetInstance(name string) (*api.Instance, string, error) {
	s.record("GetInstance:" + name)
	if s.InstanceErr != nil {
		return nil, "", s.InstanceErr
	}
	if s.Instance != nil && (s.Instance.Name == name || s.Instance.Name == "") {
		return s.Instance, "", nil
	}

	return nil, "", api.StatusErrorf(http.StatusNotFound, "Instance %q not found", name)
}

// GetInstanceState implements clusters.InstanceServer.
func (s *Server) GetInstanceState(name string) (*api.InstanceState, string, error) {
	s.record("GetInstanceState:" + name)
	if s.StateFunc != nil {
		state, err := s.StateFunc(name)
		if err != nil {
			return nil, "", err
		}

		return state, "", nil
	}
	if s.StateErr != nil {
		return nil, "", s.StateErr
	}
	if s.State != nil {
		return s.State, "", nil
	}

	return &api.InstanceState{Status: "Running", StatusCode: api.Running}, "", nil
}

// UpdateInstanceState implements clusters.InstanceServer.
func (s *Server) UpdateInstanceState(_ string, state api.InstanceStatePut, _ string) (incus.Operation, error) {
	s.record("UpdateInstanceState:" + state.Action)
	s.mu.Lock()
	s.updateStateArgs = append(s.updateStateArgs, state)
	s.mu.Unlock()

	if s.UpdateStateErr != nil {
		return nil, s.UpdateStateErr
	}
	if s.Operation != nil {
		return s.Operation, nil
	}

	return &Operation{}, nil
}

// CreateInstance implements clusters.InstanceServer.
func (s *Server) CreateInstance(instance api.InstancesPost) (incus.Operation, error) {
	s.record("CreateInstance:" + instance.Name)
	s.mu.Lock()
	s.createArgs = append(s.createArgs, instance)
	s.mu.Unlock()

	if s.CreateErr != nil {
		return nil, s.CreateErr
	}
	if s.Operation != nil {
		return s.Operation, nil
	}

	return &Operation{}, nil
}

// UpdateInstance implements clusters.InstanceServer.
func (s *Server) UpdateInstance(name string, instance api.InstancePut, etag string) (incus.Operation, error) {
	s.record("UpdateInstance:" + name)
	s.mu.Lock()
	s.updateArgs = append(s.updateArgs, UpdateInstanceArgs{Name: name, Instance: instance, ETag: etag})
	s.mu.Unlock()

	if s.UpdateErr != nil {
		return nil, s.UpdateErr
	}
	if s.Operation != nil {
		return s.Operation, nil
	}

	return &Operation{}, nil
}

// CopyInstance implements clusters.InstanceServer.
func (s *Server) CopyInstance(source incus.InstanceServer, instance api.Instance, args *incus.InstanceCopyArgs) (incus.RemoteOperation, error) {
	s.record("CopyInstance:" + instance.Name)
	s.mu.Lock()
	s.copyArgs = append(s.copyArgs, args)
	s.copySource = source
	s.mu.Unlock()

	if s.CopyErr != nil {
		return nil, s.CopyErr
	}
	if s.RemoteOperation != nil {
		return s.RemoteOperation, nil
	}

	return &RemoteOperation{
		Target: &api.Operation{Metadata: map[string]any{"block_progress": "test: 1GB (10MB/s)"}},
	}, nil
}

// DeleteInstance implements clusters.InstanceServer.
func (s *Server) DeleteInstance(name string) (incus.Operation, error) {
	s.record("DeleteInstance:" + name)
	if s.DeleteErr != nil {
		return nil, s.DeleteErr
	}
	if s.Operation != nil {
		return s.Operation, nil
	}

	return &Operation{}, nil
}

// GetStoragePoolNames implements clusters.InstanceServer.
func (s *Server) GetStoragePoolNames() ([]string, error) {
	s.record("GetStoragePoolNames")
	if s.PoolNamesErr != nil {
		return nil, s.PoolNamesErr
	}
	if s.PoolNames != nil {
		return s.PoolNames, nil
	}

	return []string{"default"}, nil
}

// GetStoragePools implements clusters.InstanceServer.
func (s *Server) GetStoragePools() ([]api.StoragePool, error) {
	s.record("GetStoragePools")
	if s.PoolsErr != nil {
		return nil, s.PoolsErr
	}
	if s.Pools != nil {
		return s.Pools, nil
	}

	return []api.StoragePool{{Name: "default", Driver: "dir", Config: api.ConfigMap{"source": "/srv/incus"}}}, nil
}

// GetImages implements clusters.InstanceServer.
func (s *Server) GetImages() ([]api.Image, error) {
	s.record("GetImages")
	if s.ImagesErr != nil {
		return nil, s.ImagesErr
	}

	return s.Images, nil
}

// DeleteImage implements clusters.InstanceServer.
func (s *Server) DeleteImage(fingerprint string) (incus.Operation, error) {
	s.record("DeleteImage:" + fingerprint)
	s.mu.Lock()
	s.deleteImages = append(s.deleteImages, fingerprint)
	s.mu.Unlock()

	if s.DeleteImageErr != nil {
		return nil, s.DeleteImageErr
	}
	if s.Operation != nil {
		return s.Operation, nil
	}

	return &Operation{}, nil
}

// GetInstanceSnapshots implements clusters.InstanceServer.
func (s *Server) GetInstanceSnapshots(instanceName string) ([]api.InstanceSnapshot, error) {
	s.record("GetInstanceSnapshots:" + instanceName)
	if s.SnapshotsErr != nil {
		return nil, s.SnapshotsErr
	}
	if !s.knowsInstance(instanceName) {
		return nil, api.StatusErrorf(http.StatusNotFound, "Instance %q not found", instanceName)
	}

	return append([]api.InstanceSnapshot(nil), s.Snapshots...), nil
}

// knowsInstance reports whether the fake serves an instance with this name.
func (s *Server) knowsInstance(instanceName string) bool {
	if s.Instance == nil && s.Instances == nil {
		return true
	}
	if s.Instance != nil && (s.Instance.Name == instanceName || s.Instance.Name == "") {
		return true
	}
	for i := range s.Instances {
		if s.Instances[i].Name == instanceName {
			return true
		}
	}

	return false
}

// CreateInstanceSnapshot implements clusters.InstanceServer.
func (s *Server) CreateInstanceSnapshot(instanceName string, snapshot api.InstanceSnapshotsPost) (incus.Operation, error) {
	s.record("CreateInstanceSnapshot:" + instanceName + "/" + snapshot.Name)
	s.mu.Lock()
	s.createSnapshots = append(s.createSnapshots, SnapshotArgs{Instance: instanceName, Name: snapshot.Name, Stateful: snapshot.Stateful})
	s.mu.Unlock()

	if s.CreateSnapshotErr != nil {
		return nil, s.CreateSnapshotErr
	}
	if s.Operation != nil {
		return s.Operation, nil
	}

	return &Operation{}, nil
}

// DeleteInstanceSnapshot implements clusters.InstanceServer.
func (s *Server) DeleteInstanceSnapshot(instanceName string, name string) (incus.Operation, error) {
	s.record("DeleteInstanceSnapshot:" + instanceName + "/" + name)
	s.mu.Lock()
	s.deleteSnapshots = append(s.deleteSnapshots, SnapshotArgs{Instance: instanceName, Name: name})
	s.mu.Unlock()

	if s.DeleteSnapshotErr != nil {
		return nil, s.DeleteSnapshotErr
	}
	if s.Operation != nil {
		return s.Operation, nil
	}

	return &Operation{}, nil
}

// GetStoragePoolVolumes implements clusters.InstanceServer.
func (s *Server) GetStoragePoolVolumes(pool string) ([]api.StorageVolume, error) {
	s.record("GetStoragePoolVolumes:" + pool)
	if s.VolumesErr != nil {
		return nil, s.VolumesErr
	}

	return append([]api.StorageVolume(nil), s.Volumes...), nil
}

// CreateStoragePoolVolume implements clusters.InstanceServer.
func (s *Server) CreateStoragePoolVolume(pool string, volume api.StorageVolumesPost) error {
	s.record("CreateStoragePoolVolume:" + pool + "/" + volume.Name)
	s.mu.Lock()
	s.createVolumes = append(s.createVolumes, VolumeArgs{Pool: pool, Volume: volume})
	s.mu.Unlock()

	return s.CreateVolumeErr
}

// DeleteStoragePoolVolume implements clusters.InstanceServer.
func (s *Server) DeleteStoragePoolVolume(pool string, volType string, name string) error {
	s.record("DeleteStoragePoolVolume:" + pool + "/" + volType + "/" + name)
	s.mu.Lock()
	s.deleteVolumes = append(s.deleteVolumes, VolumeArgs{Pool: pool, Type: volType, Name: name})
	s.mu.Unlock()

	return s.DeleteVolumeErr
}

// GetNetworkNames implements clusters.InstanceServer.
func (s *Server) GetNetworkNames() ([]string, error) {
	s.record("GetNetworkNames")
	if s.NetworkNamesErr != nil {
		return nil, s.NetworkNamesErr
	}
	if s.NetworkNames != nil {
		return s.NetworkNames, nil
	}

	return []string{"incusbr0"}, nil
}

// GetNetworks implements clusters.InstanceServer.
func (s *Server) GetNetworks() ([]api.Network, error) {
	s.record("GetNetworks")
	if s.NetworksErr != nil {
		return nil, s.NetworksErr
	}
	if s.Networks != nil {
		return s.Networks, nil
	}

	return []api.Network{{Name: "incusbr0", Type: "bridge", Managed: true, Config: api.ConfigMap{"ipv4.address": "10.0.0.0.1/24", "ipv6.address": "none"}}}, nil
}

// GetStoragePoolResources implements clusters.InstanceServer.
func (s *Server) GetStoragePoolResources(name string) (*api.ResourcesStoragePool, error) {
	s.record("GetStoragePoolResources:" + name)
	if s.PoolResourcesErr != nil {
		return nil, s.PoolResourcesErr
	}
	if s.PoolResources != nil {
		return s.PoolResources, nil
	}

	return &api.ResourcesStoragePool{Space: api.ResourcesStoragePoolSpace{Total: 1_000_000_000_000, Used: 100_000_000_000}}, nil
}

// GetStoragePool implements clusters.InstanceServer.
func (s *Server) GetStoragePool(name string) (*api.StoragePool, string, error) {
	s.record("GetStoragePool:" + name)
	if s.PoolErr != nil {
		return nil, "", s.PoolErr
	}
	if s.Pool != nil && (s.Pool.Name == name || s.Pool.Name == "") {
		return s.Pool, "pool-etag", nil
	}
	for i := range s.Pools {
		if s.Pools[i].Name == name {
			return &s.Pools[i], "pool-etag", nil
		}
	}
	if s.Pools == nil && name == "default" {
		return &api.StoragePool{Name: "default", Driver: "dir", Config: api.ConfigMap{"source": "/srv/incus"}}, "pool-etag", nil
	}

	return nil, "", api.StatusErrorf(http.StatusNotFound, "Storage pool %q not found", name)
}

// CreateStoragePool implements clusters.InstanceServer.
func (s *Server) CreateStoragePool(pool api.StoragePoolsPost) error {
	s.record("CreateStoragePool:" + pool.Name)
	s.mu.Lock()
	s.createPools = append(s.createPools, pool)
	s.mu.Unlock()

	return s.CreatePoolErr
}

// UpdateStoragePool implements clusters.InstanceServer.
func (s *Server) UpdateStoragePool(name string, pool api.StoragePoolPut, etag string) error {
	s.record("UpdateStoragePool:" + name)
	s.mu.Lock()
	s.updatePools = append(s.updatePools, UpdateStoragePoolArgs{Name: name, Pool: pool, ETag: etag})
	s.mu.Unlock()

	return s.UpdatePoolErr
}

// DeleteStoragePool implements clusters.InstanceServer.
func (s *Server) DeleteStoragePool(name string) error {
	s.record("DeleteStoragePool:" + name)
	s.mu.Lock()
	s.deletePools = append(s.deletePools, name)
	s.mu.Unlock()

	return s.DeletePoolErr
}

// GetNetwork implements clusters.InstanceServer.
func (s *Server) GetNetwork(name string) (*api.Network, string, error) {
	s.record("GetNetwork:" + name)
	if s.NetworkErr != nil {
		return nil, "", s.NetworkErr
	}
	if s.Network != nil && (s.Network.Name == name || s.Network.Name == "") {
		return s.Network, "network-etag", nil
	}
	for i := range s.Networks {
		if s.Networks[i].Name == name {
			return &s.Networks[i], "network-etag", nil
		}
	}
	if s.Networks == nil && name == "incusbr0" {
		return &api.Network{Name: "incusbr0", Type: "bridge", Managed: true, Config: api.ConfigMap{"ipv4.address": "10.0.0.0.1/24", "ipv6.address": "none"}}, "network-etag", nil
	}

	return nil, "", api.StatusErrorf(http.StatusNotFound, "Network %q not found", name)
}

// CreateNetwork implements clusters.InstanceServer.
func (s *Server) CreateNetwork(network api.NetworksPost) error {
	s.record("CreateNetwork:" + network.Name)
	s.mu.Lock()
	s.createNetworks = append(s.createNetworks, network)
	s.mu.Unlock()

	return s.CreateNetworkErr
}

// UpdateNetwork implements clusters.InstanceServer.
func (s *Server) UpdateNetwork(name string, network api.NetworkPut, etag string) error {
	s.record("UpdateNetwork:" + name)
	s.mu.Lock()
	s.updateNetworks = append(s.updateNetworks, UpdateNetworkArgs{Name: name, Network: network, ETag: etag})
	s.mu.Unlock()

	return s.UpdateNetworkErr
}

// DeleteNetwork implements clusters.InstanceServer.
func (s *Server) DeleteNetwork(name string) error {
	s.record("DeleteNetwork:" + name)
	s.mu.Lock()
	s.deleteNetworks = append(s.deleteNetworks, name)
	s.mu.Unlock()

	return s.DeleteNetworkErr
}

// GetProfiles implements clusters.InstanceServer.
func (s *Server) GetProfiles() ([]api.Profile, error) {
	s.record("GetProfiles")
	if s.ProfilesErr != nil {
		return nil, s.ProfilesErr
	}
	if s.Profiles != nil {
		return append([]api.Profile(nil), s.Profiles...), nil
	}

	return []api.Profile{{Name: "default", Config: api.ConfigMap{}, Devices: api.DevicesMap{}}}, nil
}

// GetProfile implements clusters.InstanceServer.
func (s *Server) GetProfile(name string) (*api.Profile, string, error) {
	s.record("GetProfile:" + name)
	if s.ProfileErr != nil {
		return nil, "", s.ProfileErr
	}
	if s.Profile != nil && (s.Profile.Name == name || s.Profile.Name == "") {
		return s.Profile, "profile-etag", nil
	}
	for i := range s.Profiles {
		if s.Profiles[i].Name == name {
			return &s.Profiles[i], "profile-etag", nil
		}
	}
	if s.Profiles == nil && name == "default" {
		return &api.Profile{Name: "default", Config: api.ConfigMap{}, Devices: api.DevicesMap{}}, "profile-etag", nil
	}

	return nil, "", api.StatusErrorf(http.StatusNotFound, "Profile %q not found", name)
}

// CreateProfile implements clusters.InstanceServer.
func (s *Server) CreateProfile(profile api.ProfilesPost) error {
	s.record("CreateProfile:" + profile.Name)
	s.mu.Lock()
	s.createProfiles = append(s.createProfiles, profile)
	s.mu.Unlock()

	return s.CreateProfileErr
}

// UpdateProfile implements clusters.InstanceServer.
func (s *Server) UpdateProfile(name string, profile api.ProfilePut, etag string) error {
	s.record("UpdateProfile:" + name)
	s.mu.Lock()
	s.updateProfiles = append(s.updateProfiles, UpdateProfileArgs{Name: name, Profile: profile, ETag: etag})
	s.mu.Unlock()

	return s.UpdateProfileErr
}

// DeleteProfile implements clusters.InstanceServer.
func (s *Server) DeleteProfile(name string) error {
	s.record("DeleteProfile:" + name)
	s.mu.Lock()
	s.deleteProfiles = append(s.deleteProfiles, name)
	s.mu.Unlock()

	return s.DeleteProfileErr
}

// GetClusterMember implements clusters.InstanceServer.
func (s *Server) GetClusterMember(name string) (*api.ClusterMember, string, error) {
	s.record("GetClusterMember:" + name)
	if s.MemberErr != nil {
		return nil, "", s.MemberErr
	}
	if s.Member != nil && (s.Member.ServerName == name || s.Member.ServerName == "") {
		return s.Member, "member-etag", nil
	}
	for i := range s.Members {
		if s.Members[i].ServerName == name {
			return &s.Members[i], "member-etag", nil
		}
	}
	if s.Members == nil && name == "test-node" {
		return &api.ClusterMember{ServerName: "test-node", Status: "Online"}, "member-etag", nil
	}

	return nil, "", api.StatusErrorf(http.StatusNotFound, "Cluster member %q not found", name)
}

// UpdateClusterMember implements clusters.InstanceServer.
func (s *Server) UpdateClusterMember(name string, member api.ClusterMemberPut, etag string) error {
	s.record("UpdateClusterMember:" + name)
	s.mu.Lock()
	s.updateMembers = append(s.updateMembers, UpdateMemberArgs{Name: name, Member: member, ETag: etag})
	s.mu.Unlock()

	return s.UpdateMemberErr
}

// IsClustered implements clusters.InstanceServer.
func (s *Server) IsClustered() bool {
	s.record("IsClustered")
	return s.Clustered
}

// Disconnect implements the optional closer used by clusters.Manager.
func (s *Server) Disconnect() {
	s.mu.Lock()
	s.Disconnects++
	s.mu.Unlock()
}

// RemoteOperation is a fake incus.RemoteOperation.
type RemoteOperation struct {
	Target   *api.Operation
	Err      error
	WaitFunc func() error
}

// AddHandler implements incus.RemoteOperation.
func (o *RemoteOperation) AddHandler(handler func(api.Operation)) (*incus.EventTarget, error) {
	if o.Target != nil {
		handler(*o.Target)
	}

	return &incus.EventTarget{}, nil
}

// CancelTarget implements incus.RemoteOperation.
func (o *RemoteOperation) CancelTarget() error {
	return nil
}

// GetTarget implements incus.RemoteOperation.
func (o *RemoteOperation) GetTarget() (*api.Operation, error) {
	if o.Err != nil {
		return nil, o.Err
	}

	return o.Target, nil
}

// Wait implements incus.RemoteOperation.
func (o *RemoteOperation) Wait() error {
	if o.WaitFunc != nil {
		return o.WaitFunc()
	}

	return o.Err
}

// Operation is a fake incus.Operation.
type Operation struct {
	Err      error
	WaitFunc func() error
}

// AddHandler implements incus.Operation.
func (o *Operation) AddHandler(func(api.Operation)) (*incus.EventTarget, error) {
	return &incus.EventTarget{}, nil
}

// Cancel implements incus.Operation.
func (o *Operation) Cancel() error {
	return nil
}

// Get implements incus.Operation.
func (o *Operation) Get() api.Operation {
	return api.Operation{}
}

// GetWebsocket implements incus.Operation.
func (o *Operation) GetWebsocket(string) (*websocket.Conn, error) {
	return nil, fmt.Errorf("not implemented")
}

// RemoveHandler implements incus.Operation.
func (o *Operation) RemoveHandler(*incus.EventTarget) error {
	return nil
}

// Refresh implements incus.Operation.
func (o *Operation) Refresh() error {
	return nil
}

// Wait implements incus.Operation.
func (o *Operation) Wait() error {
	if o.WaitFunc != nil {
		return o.WaitFunc()
	}

	return o.Err
}

// WaitContext implements incus.Operation.
func (o *Operation) WaitContext(_ context.Context) error {
	return o.Wait()
}
