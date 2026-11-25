// Package incusfake provides test doubles for the Incus client interfaces.
package incusfake

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	incus "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"

	"github.com/mickeyzzc/incus-fleet/internal/apitypes"
)

// Server is a configurable fake Incus server implementing the narrow
// interface used by the fleet services. It embeds the full SDK interface
// (nil) so test doubles can be passed wherever a raw client is required;
// calling the embedded methods panics, which is why every method the fleet
// services use is implemented explicitly.
type Server struct {
	incus.InstanceServer

	Server            *api.Server
	ServerErr         error
	Cluster           *api.Cluster
	ClusterErr        error
	Members           []api.ClusterMember
	MembersErr        error
	MembersFunc       func() ([]api.ClusterMember, error)
	Instances         []api.InstanceFull
	InstancesErr      error
	Instance          *api.Instance
	ExtraInstances    []api.Instance
	InstanceErr       error
	State             *api.InstanceState
	StateFunc         func(name string) (*api.InstanceState, error)
	StateErr          error
	UpdateStateErr    error
	CreateErr         error
	UpdateErr         error
	CopyErr           error
	DeleteErr         error
	ConsoleErr        error
	ExecErr           error
	ExecOutput        string
	ExecErrOutput     string
	GetFileErr        error
	CreateFileErr     error
	ImageErr          error
	CreateImageErr    error
	CopyImageErr      error
	CreateISOErr      error
	RebuildErr        error
	RefreshImageErr   error
	DeleteFileErr     error
	ListFilesErr      error
	MkdirErr          error
	Files             map[string][]byte
	ListFiles         []apitypes.FileEntry
	Images            []api.Image
	ImagesErr         error
	DeleteImageErr    error
	Snapshots         []api.InstanceSnapshot
	SnapshotsErr      error
	CreateSnapshotErr error
	DeleteSnapshotErr error
	Volumes           []api.StorageVolume
	VolumesErr        error
	VolumeErr         error
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
	NetworkLeases     []api.NetworkLease
	NetworkLeasesErr  error
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
	CreateMemberErr   error
	JoinToken         *api.ClusterMemberJoinToken
	JoinTokenMetadata map[string]any
	UpdateMemberErr   error
	DeleteMemberErr   error
	RenameMemberErr   error
	RenameInstanceErr error
	PoolNames         []string
	PoolNamesErr      error
	NetworkNames      []string
	NetworkNamesErr   error
	PoolResources     *api.ResourcesStoragePool
	PoolResourcesErr  error
	ServerResources   *api.Resources
	ServerResErr      error
	Clustered         bool
	RemoteOperation   *RemoteOperation
	Operation         *Operation
	Disconnects       int
	// ISOExportBytes is served as the raw volume backup payload for
	// GetStorageVolumeBackupFile (used by the unattended install tests).
	ISOExportBytes []byte

	mu              sync.Mutex
	calls           []string
	backupExports   []BackupExportArgs
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
	renameMembers   []RenameMemberArgs
	createMembers   []api.ClusterMembersPost
	deletedMembers  []DeleteMemberArgs
	renameArgs      []RenameInstanceArgs
	targets         []string
	createSnapshots []SnapshotArgs
	deleteSnapshots []SnapshotArgs
	deleteImages    []string
	createdImages   []ImageArgs
	copiedImages    []ImageArgs
	isoImports      []ISOArgs
	rebuilds        []RebuildArgs
	refreshedImages []string
	createVolumes   []VolumeArgs
	deleteVolumes   []VolumeArgs
	consoleCalls    int
	execArgs        []api.InstanceExecPost
	createFiles     []FileArgs
	deleteFiles     []string
	mkdirs          []string
}

// FileArgs records one CreateInstanceFile call.
type FileArgs struct {
	Path    string
	Content []byte
	Mode    int64
}

// ImageArgs records one image create/copy call.
type ImageArgs struct {
	Filename    string
	Fingerprint string
	Content     []byte
	Type        string
	Aliases     []api.ImageAlias
}

// CreateImageArgs returns the arguments recorded by CreateImage.
func (s *Server) CreateImageArgs() []ImageArgs {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]ImageArgs(nil), s.createdImages...)
}

// CopyImageArgs returns the arguments recorded by CopyImage.
func (s *Server) CopyImageArgs() []ImageArgs {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]ImageArgs(nil), s.copiedImages...)
}

// CreateFileArgs returns the arguments recorded by CreateInstanceFile.
func (s *Server) CreateFileArgs() []FileArgs {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]FileArgs(nil), s.createFiles...)
}

// DeleteFileArgs returns the paths recorded by DeleteInstanceFile.
func (s *Server) DeleteFileArgs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]string(nil), s.deleteFiles...)
}

// MkdirArgs returns the paths recorded by MkdirInstanceDir.
func (s *Server) MkdirArgs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]string(nil), s.mkdirs...)
}

// ExecArgs returns the requests recorded by ExecInstance.
func (s *Server) ExecArgs() []api.InstanceExecPost {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]api.InstanceExecPost(nil), s.execArgs...)
}

// ConsoleCalls returns how many times ConsoleInstance was called.
func (s *Server) ConsoleCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.consoleCalls
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

// ISOArgs records one CreateStoragePoolVolumeFromISO call.
type ISOArgs struct {
	Pool   string
	Name   string
	Reader bool
}

// RebuildArgs records one RebuildInstance call.
type RebuildArgs struct {
	Instance string
	Source   api.InstanceSource
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

// CreateISOArgs returns the calls recorded by CreateStoragePoolVolumeFromISO.
func (s *Server) CreateISOArgs() []ISOArgs {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]ISOArgs(nil), s.isoImports...)
}

// RebuildArgs returns the calls recorded by RebuildInstance.
func (s *Server) RebuildArgs() []RebuildArgs {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]RebuildArgs(nil), s.rebuilds...)
}

// RefreshImageArgs returns the fingerprints recorded by RefreshImage.
func (s *Server) RefreshImageArgs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]string(nil), s.refreshedImages...)
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

// RenameMemberArgs records one RenameClusterMember call.
type RenameMemberArgs struct {
	Name string
	Post api.ClusterMemberPost
}

// UpdateInstanceArgs records one UpdateInstance call.
type UpdateInstanceArgs struct {
	Name     string
	Instance api.InstancePut
	ETag     string
}

// DeleteMemberArgs records one DeleteClusterMember call.
type DeleteMemberArgs struct {
	Name  string
	Force bool
}

// RenameInstanceArgs records one RenameInstance call.
type RenameInstanceArgs struct {
	Name string
	Post api.InstancePost
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

// RenameMemberArgs returns the arguments recorded by RenameClusterMember.
func (s *Server) RenameMemberArgs() []RenameMemberArgs {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]RenameMemberArgs(nil), s.renameMembers...)
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
	if s.MembersFunc != nil {
		return s.MembersFunc()
	}
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
	for i := range s.ExtraInstances {
		if s.ExtraInstances[i].Name == name {
			return &s.ExtraInstances[i], "", nil
		}
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

// RenameInstance implements clusters.InstanceServer.
func (s *Server) RenameInstance(name string, instance api.InstancePost) (incus.Operation, error) {
	s.record("RenameInstance:" + name + ":" + instance.Name)
	s.mu.Lock()
	s.renameArgs = append(s.renameArgs, RenameInstanceArgs{Name: name, Post: instance})
	s.mu.Unlock()

	if s.RenameInstanceErr != nil {
		return nil, s.RenameInstanceErr
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

// GetImage implements clusters.InstanceServer.
func (s *Server) GetImage(fingerprint string) (*api.Image, string, error) {
	s.record("GetImage:" + fingerprint)
	if s.ImageErr != nil {
		return nil, "", s.ImageErr
	}
	for i := range s.Images {
		if strings.HasPrefix(s.Images[i].Fingerprint, fingerprint) {
			return &s.Images[i], "image-etag", nil
		}
	}

	return nil, "", api.StatusErrorf(http.StatusNotFound, "Image %q not found", fingerprint)
}

// CreateImage implements clusters.InstanceServer.
func (s *Server) CreateImage(image api.ImagesPost, args *incus.ImageCreateArgs) (incus.Operation, error) {
	s.record("CreateImage:" + image.Filename)
	content, err := io.ReadAll(args.MetaFile)
	if err != nil {
		return nil, fmt.Errorf("read image upload: %w", err)
	}

	s.mu.Lock()
	s.createdImages = append(s.createdImages, ImageArgs{Filename: image.Filename, Content: content, Type: args.Type, Aliases: image.Aliases})
	s.mu.Unlock()

	if s.CreateImageErr != nil {
		return nil, s.CreateImageErr
	}
	if s.Operation != nil {
		return s.Operation, nil
	}

	return &Operation{}, nil
}

// CopyImage implements clusters.InstanceServer.
func (s *Server) CopyImage(_ incus.ImageServer, image api.Image, args *incus.ImageCopyArgs) (incus.RemoteOperation, error) {
	s.record("CopyImage:" + image.Fingerprint)
	entry := ImageArgs{Fingerprint: image.Fingerprint}
	if args != nil {
		for _, alias := range args.Aliases {
			entry.Aliases = append(entry.Aliases, api.ImageAlias{Name: alias.Name})
		}
	}
	s.mu.Lock()
	s.copiedImages = append(s.copiedImages, entry)
	s.mu.Unlock()

	if s.CopyImageErr != nil {
		return nil, s.CopyImageErr
	}
	if s.RemoteOperation != nil {
		return s.RemoteOperation, nil
	}

	return &RemoteOperation{}, nil
}

// CreateStoragePoolVolumeFromISO implements clusters.InstanceServer.
func (s *Server) CreateStoragePoolVolumeFromISO(pool string, args incus.StorageVolumeBackupArgs) (incus.Operation, error) {
	s.record("CreateStoragePoolVolumeFromISO:" + pool + "/" + args.Name)
	entry := ISOArgs{Pool: pool, Name: args.Name}
	if args.BackupFile != nil {
		entry.Reader = true
		_, _ = io.Copy(io.Discard, args.BackupFile)
	}

	s.mu.Lock()
	s.isoImports = append(s.isoImports, entry)
	s.mu.Unlock()

	if s.CreateISOErr != nil {
		return nil, s.CreateISOErr
	}
	if s.Operation != nil {
		return s.Operation, nil
	}

	return &Operation{}, nil
}

// RebuildInstance implements clusters.InstanceServer.
func (s *Server) RebuildInstance(instanceName string, req api.InstanceRebuildPost) (incus.Operation, error) {
	s.record("RebuildInstance:" + instanceName)
	s.mu.Lock()
	s.rebuilds = append(s.rebuilds, RebuildArgs{Instance: instanceName, Source: req.Source})
	s.mu.Unlock()

	if s.RebuildErr != nil {
		return nil, s.RebuildErr
	}
	if s.Operation != nil {
		return s.Operation, nil
	}

	return &Operation{}, nil
}

// RefreshImage implements clusters.InstanceServer.
func (s *Server) RefreshImage(fingerprint string) (incus.Operation, error) {
	s.record("RefreshImage:" + fingerprint)
	s.mu.Lock()
	s.refreshedImages = append(s.refreshedImages, fingerprint)
	s.mu.Unlock()

	if s.RefreshImageErr != nil {
		return nil, s.RefreshImageErr
	}
	if s.Operation != nil {
		return s.Operation, nil
	}

	return &Operation{}, nil
}

// UseProject implements the full SDK interface; the fake ignores projects
// and returns itself so test doubles survive reconnects with a project set.
func (s *Server) UseProject(project string) incus.InstanceServer {
	s.record("UseProject:" + project)

	return s
}

// ConsoleInstanceDynamic implements clusters.InstanceServer.
func (s *Server) ConsoleInstanceDynamic(instanceName string, _ api.InstanceConsolePost, _ *incus.InstanceConsoleArgs) (incus.Operation, func(io.ReadWriteCloser) error, error) {
	s.record("ConsoleInstanceDynamic:" + instanceName)
	s.mu.Lock()
	s.consoleCalls++
	s.mu.Unlock()

	if s.ConsoleErr != nil {
		return nil, nil, s.ConsoleErr
	}
	if s.Operation != nil {
		return s.Operation, nil, nil
	}

	return &Operation{}, nil, nil
}

// ExecInstance implements clusters.InstanceServer.
func (s *Server) ExecInstance(instanceName string, exec api.InstanceExecPost, args *incus.InstanceExecArgs) (incus.Operation, error) {
	s.record("ExecInstance:" + instanceName)
	s.mu.Lock()
	s.execArgs = append(s.execArgs, exec)
	s.mu.Unlock()

	if s.ExecErr != nil {
		return nil, s.ExecErr
	}

	// Emulate a non-interactive run: canned output and closed data channel.
	if args != nil {
		if s.ExecOutput != "" && args.Stdout != nil {
			_, _ = io.WriteString(args.Stdout, s.ExecOutput)
		}
		if s.ExecErrOutput != "" && args.Stderr != nil {
			_, _ = io.WriteString(args.Stderr, s.ExecErrOutput)
		}
		if args.DataDone != nil {
			close(args.DataDone)
		}
	}

	if s.Operation != nil {
		return s.Operation, nil
	}

	return &Operation{}, nil
}

// GetInstanceFile implements clusters.InstanceServer.
func (s *Server) GetInstanceFile(instanceName string, path string) (io.ReadCloser, *incus.InstanceFileResponse, error) {
	s.record("GetInstanceFile:" + instanceName + ":" + path)
	if s.GetFileErr != nil {
		return nil, nil, s.GetFileErr
	}
	if content, ok := s.Files[path]; ok {
		return io.NopCloser(bytes.NewReader(content)), &incus.InstanceFileResponse{
			Type: "file",
			Mode: 0o644,
		}, nil
	}

	return nil, nil, api.StatusErrorf(http.StatusNotFound, "File %q not found", path)
}

// CreateInstanceFile implements clusters.InstanceServer.
func (s *Server) CreateInstanceFile(instanceName string, path string, args incus.InstanceFileArgs) error {
	s.record("CreateInstanceFile:" + instanceName + ":" + path)
	if s.CreateFileErr != nil {
		return s.CreateFileErr
	}

	content, err := io.ReadAll(args.Content)
	if err != nil {
		return fmt.Errorf("read upload: %w", err)
	}

	s.mu.Lock()
	s.createFiles = append(s.createFiles, FileArgs{Path: path, Content: content, Mode: int64(args.Mode)})
	s.mu.Unlock()

	return nil
}

// DeleteInstanceFile implements clusters.InstanceServer.
func (s *Server) DeleteInstanceFile(instanceName string, path string) error {
	s.record("DeleteInstanceFile:" + instanceName + ":" + path)
	s.mu.Lock()
	s.deleteFiles = append(s.deleteFiles, path)
	s.mu.Unlock()

	return s.DeleteFileErr
}

// ListInstanceFiles implements clusters.InstanceServer.
func (s *Server) ListInstanceFiles(instanceName string, dir string) ([]apitypes.FileEntry, error) {
	s.record("ListInstanceFiles:" + instanceName + ":" + dir)
	if s.ListFilesErr != nil {
		return nil, s.ListFilesErr
	}

	return append([]apitypes.FileEntry(nil), s.ListFiles...), nil
}

// MkdirInstanceDir implements clusters.InstanceServer.
func (s *Server) MkdirInstanceDir(instanceName string, dir string) error {
	s.record("MkdirInstanceDir:" + instanceName + ":" + dir)
	s.mu.Lock()
	s.mkdirs = append(s.mkdirs, dir)
	s.mu.Unlock()

	return s.MkdirErr
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

// UseTarget returns the fake itself so member-bound calls still record against
// the same server. The real client returns a client scoped to one member.
func (s *Server) UseTarget(name string) incus.InstanceServer {
	s.record("UseTarget:" + name)
	s.mu.Lock()
	s.targets = append(s.targets, name)
	s.mu.Unlock()

	return s
}

// GetStoragePoolVolume implements clusters.InstanceServer.
func (s *Server) GetStoragePoolVolume(pool string, volType string, name string) (*api.StorageVolume, string, error) {
	s.record("GetStoragePoolVolume:" + pool + "/" + volType + "/" + name)
	if s.VolumeErr != nil {
		return nil, "", s.VolumeErr
	}

	for i := range s.Volumes {
		if s.Volumes[i].Name == name {
			volume := s.Volumes[i]
			return &volume, "", nil
		}
	}

	// Mirror the real client, which answers with a StatusError so callers can
	// distinguish "missing" from a transport failure.
	return nil, "", api.StatusErrorf(http.StatusNotFound, "storage volume %q not found", name)
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

// BackupExportArgs records one volume backup export request.
type BackupExportArgs struct {
	Pool   string
	Volume string
}

// CreateStorageVolumeBackup implements clusters.InstanceServer.
func (s *Server) CreateStorageVolumeBackup(pool string, volName string, _ api.StorageVolumeBackupsPost) (incus.Operation, error) {
	s.record("CreateStorageVolumeBackup:" + pool + "/" + volName)

	if s.Operation != nil {
		return s.Operation, nil
	}

	return &Operation{}, nil
}

// GetStorageVolumeBackupFile implements clusters.InstanceServer. The served
// bytes come from ISOExportBytes.
func (s *Server) GetStorageVolumeBackupFile(pool string, volName string, name string, req *incus.BackupFileRequest) (*incus.BackupFileResponse, error) {
	s.record("GetStorageVolumeBackupFile:" + pool + "/" + volName + "/" + name)

	s.mu.Lock()
	s.backupExports = append(s.backupExports, BackupExportArgs{Pool: pool, Volume: volName})
	s.mu.Unlock()

	if req == nil || req.BackupFile == nil {
		return nil, fmt.Errorf("incusfake: BackupFileRequest.BackupFile is required")
	}

	size, err := io.Copy(req.BackupFile, bytes.NewReader(s.ISOExportBytes))
	if err != nil {
		return nil, err
	}

	return &incus.BackupFileResponse{Size: size}, nil
}

// DeleteStorageVolumeBackup implements clusters.InstanceServer.
func (s *Server) DeleteStorageVolumeBackup(pool string, volName string, name string) (incus.Operation, error) {
	s.record("DeleteStorageVolumeBackup:" + pool + "/" + volName + "/" + name)

	if s.Operation != nil {
		return s.Operation, nil
	}

	return &Operation{}, nil
}

// ExportBackupArgs returns the volume backup export calls recorded so far.
func (s *Server) ExportBackupArgs() []BackupExportArgs {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]BackupExportArgs, 0, len(s.backupExports))
	out = append(out, s.backupExports...)

	return out
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

	return []api.Network{{Name: "incusbr0", Type: "bridge", Managed: true, Config: api.ConfigMap{"ipv4.address": "10.0.0.1/24", "ipv6.address": "none"}}}, nil
}

// GetNetworkLeases implements clusters.InstanceServer.
func (s *Server) GetNetworkLeases(network string) ([]api.NetworkLease, error) {
	s.record("GetNetworkLeases:" + network)
	if s.NetworkLeasesErr != nil {
		return nil, s.NetworkLeasesErr
	}

	return s.NetworkLeases, nil
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

// GetServerResources implements clusters.InstanceServer.
func (s *Server) GetServerResources() (*api.Resources, error) {
	s.record("GetServerResources")
	if s.ServerResErr != nil {
		return nil, s.ServerResErr
	}
	if s.ServerResources != nil {
		return s.ServerResources, nil
	}

	return &api.Resources{
		CPU:    api.ResourcesCPU{Architecture: "x86_64", Total: 4},
		Memory: api.ResourcesMemory{Used: 2_000_000_000, Total: 8_000_000_000},
	}, nil
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
		return &api.Network{Name: "incusbr0", Type: "bridge", Managed: true, Config: api.ConfigMap{"ipv4.address": "10.0.0.1/24", "ipv6.address": "none"}}, "network-etag", nil
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

// CreateClusterMember implements clusters.InstanceServer.
func (s *Server) CreateClusterMember(member api.ClusterMembersPost) (incus.Operation, error) {
	s.record("CreateClusterMember:" + member.ServerName)
	s.mu.Lock()
	s.createMembers = append(s.createMembers, member)
	s.mu.Unlock()

	if s.CreateMemberErr != nil {
		return nil, s.CreateMemberErr
	}

	if s.JoinTokenMetadata != nil {
		return &Operation{Metadata: s.JoinTokenMetadata}, nil
	}

	token := s.JoinToken
	if token == nil {
		token = &api.ClusterMemberJoinToken{
			ServerName:  member.ServerName,
			Fingerprint: "fleet-fingerprint",
			Addresses:   []string{"192.0.2.10:8443"},
			Secret:      "join-secret",
			ExpiresAt:   time.Now().Add(time.Hour),
		}
	} else {
		copied := *token
		copied.ServerName = member.ServerName
		token = &copied
	}

	addresses := make([]any, 0, len(token.Addresses))
	for _, address := range token.Addresses {
		addresses = append(addresses, address)
	}

	return &Operation{Metadata: map[string]any{
		"serverName":  token.ServerName,
		"secret":      token.Secret,
		"fingerprint": token.Fingerprint,
		"addresses":   addresses,
		"expiresAt":   token.ExpiresAt.Format(time.RFC3339Nano),
	}}, nil
}

// CreateMemberArgs returns the join-token requests recorded by
// CreateClusterMember.
func (s *Server) CreateMemberArgs() []api.ClusterMembersPost {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]api.ClusterMembersPost(nil), s.createMembers...)
}

// UpdateClusterMember implements clusters.InstanceServer.
func (s *Server) UpdateClusterMember(name string, member api.ClusterMemberPut, etag string) error {
	s.record("UpdateClusterMember:" + name)
	s.mu.Lock()
	s.updateMembers = append(s.updateMembers, UpdateMemberArgs{Name: name, Member: member, ETag: etag})
	s.mu.Unlock()

	return s.UpdateMemberErr
}

// RenameClusterMember implements clusters.InstanceServer.
func (s *Server) RenameClusterMember(name string, member api.ClusterMemberPost) error {
	s.record("RenameClusterMember:" + name)
	s.mu.Lock()
	s.renameMembers = append(s.renameMembers, RenameMemberArgs{Name: name, Post: member})
	s.mu.Unlock()

	return s.RenameMemberErr
}

// DeleteClusterMember implements clusters.InstanceServer.
func (s *Server) DeleteClusterMember(name string, force bool) error {
	s.record("DeleteClusterMember:" + name)
	s.mu.Lock()
	s.deletedMembers = append(s.deletedMembers, DeleteMemberArgs{Name: name, Force: force})
	s.mu.Unlock()

	return s.DeleteMemberErr
}

// DeletedMembers returns the members removed through DeleteClusterMember.
func (s *Server) DeletedMembers() []DeleteMemberArgs {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]DeleteMemberArgs(nil), s.deletedMembers...)
}

// RenameArgs returns the rename requests recorded by RenameInstance.
func (s *Server) RenameArgs() []RenameInstanceArgs {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]RenameInstanceArgs(nil), s.renameArgs...)
}

// Targets returns the member names selected with UseTarget.
func (s *Server) Targets() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]string(nil), s.targets...)
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
	Err              error
	WaitFunc         func() error
	GetWebsocketFunc func(string) (*websocket.Conn, error)
	// FDSecrets mirrors the daemon operation metadata so relay code can
	// resolve fd names into websocket secrets.
	FDSecrets map[string]string
	// Metadata is returned verbatim from Get().
	Metadata map[string]any
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
	op := api.Operation{}
	if len(o.FDSecrets) > 0 || len(o.Metadata) > 0 {
		metadata := make(map[string]any, len(o.Metadata)+1)
		for key, value := range o.Metadata {
			metadata[key] = value
		}
		if len(o.FDSecrets) > 0 {
			fds := make(map[string]any, len(o.FDSecrets))
			for name, secret := range o.FDSecrets {
				fds[name] = secret
			}
			metadata["fds"] = fds
		}
		op.Metadata = metadata
	}

	return op
}

// GetWebsocket implements incus.Operation.
func (o *Operation) GetWebsocket(fd string) (*websocket.Conn, error) {
	if o.GetWebsocketFunc != nil {
		return o.GetWebsocketFunc(fd)
	}

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

// WebsocketStub serves loopback websockets for relay tests. Every
// GetWebsocket call on the returned operation dials the stub server and the
// server-side connection arrives on Conns, tagged with the requested fd.
type WebsocketStub struct {
	server *httptest.Server
	op     *Operation
	Conns  chan StubConn
	closed chan struct{}
}

// StubConn pairs one stub websocket with the fd that requested it.
type StubConn struct {
	FD   string
	Conn *websocket.Conn
}

// NewWebsocketStub starts the loopback websocket server.
func NewWebsocketStub() *WebsocketStub {
	conns := make(chan StubConn, 8)
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		conns <- StubConn{FD: r.URL.Query().Get("fd"), Conn: conn}
	}))

	dialer := websocket.Dialer{}
	closed := make(chan struct{})
	secrets := map[string]string{"0": "secret-data", "control": "secret-control"}
	names := map[string]string{"secret-data": "0", "secret-control": "control"}
	op := &Operation{
		FDSecrets: secrets,
		GetWebsocketFunc: func(secret string) (*websocket.Conn, error) {
			fd, ok := names[secret]
			if !ok {
				fd = secret
			}
			url := "ws" + strings.TrimPrefix(server.URL, "http") + "/?fd=" + fd
			conn, _, err := dialer.Dial(url, nil)
			if err != nil {
				return nil, err
			}

			return conn, nil
		},
		// The relay tears down as soon as the operation finishes, so the
		// stub operation waits until the test closes it.
		WaitFunc: func() error {
			<-closed
			return nil
		},
	}

	return &WebsocketStub{server: server, op: op, Conns: conns, closed: closed}
}

// Operation returns the wired fake operation.
func (s *WebsocketStub) Operation() *Operation {
	return s.op
}

// Close shuts the loopback server down and releases the operation wait.
func (s *WebsocketStub) Close() {
	close(s.closed)
	s.server.Close()
}
