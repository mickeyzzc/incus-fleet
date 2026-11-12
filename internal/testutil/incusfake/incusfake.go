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
	Server           *api.Server
	ServerErr        error
	Cluster          *api.Cluster
	ClusterErr       error
	Members          []api.ClusterMember
	MembersErr       error
	Instances        []api.InstanceFull
	InstancesErr     error
	Instance         *api.Instance
	InstanceErr      error
	State            *api.InstanceState
	StateFunc        func(name string) (*api.InstanceState, error)
	StateErr         error
	UpdateStateErr   error
	CreateErr        error
	UpdateErr        error
	CopyErr          error
	DeleteErr        error
	Images           []api.Image
	ImagesErr        error
	Pools            []api.StoragePool
	PoolsErr         error
	Networks         []api.Network
	NetworksErr      error
	PoolNames        []string
	PoolNamesErr     error
	NetworkNames     []string
	NetworkNamesErr  error
	PoolResources    *api.ResourcesStoragePool
	PoolResourcesErr error
	Clustered        bool
	RemoteOperation  *RemoteOperation
	Operation        *Operation
	Disconnects      int

	mu              sync.Mutex
	calls           []string
	copyArgs        []*incus.InstanceCopyArgs
	createArgs      []api.InstancesPost
	updateArgs      []UpdateInstanceArgs
	updateStateArgs []api.InstanceStatePut
	copySource      incus.InstanceServer
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
