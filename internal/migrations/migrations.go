// Package migrations orchestrates cross-cluster instance moves and copies.
package migrations

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	incus "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"

	"github.com/mickeyzzc/incus-fleet/internal/clusters"
	"github.com/mickeyzzc/incus-fleet/internal/store"
)

// Sentinel errors returned by the service.
var (
	ErrInvalidRequest = errors.New("migrations: invalid request")
	ErrConflict       = errors.New("migrations: conflict")
	ErrNotFound       = errors.New("migrations: not found")
)

// Migration status values persisted in the store.
const (
	StatusPending      = "pending"
	StatusPreflight    = "preflight"
	StatusStopping     = "stopping"
	StatusTransferring = "transferring"
	StatusStarting     = "starting"
	StatusVerifying    = "verifying"
	StatusDone         = "done"
	StatusFailed       = "failed"
	StatusCanceled     = "canceled"
)

// Modes supported by the service.
const (
	ModeMove = "move"
	ModeCopy = "copy"
)

// Request asks for one cross-cluster instance migration.
type Request struct {
	SourceCluster    string `json:"source_cluster"`
	TargetCluster    string `json:"target_cluster"`
	Instance         string `json:"instance"`
	Mode             string `json:"mode"`
	Live             bool   `json:"live"`
	IncludeSnapshots bool   `json:"include_snapshots"`
	AutoStart        *bool  `json:"auto_start,omitempty"`
}

func (r Request) autoStart() bool {
	if r.AutoStart == nil {
		return true
	}

	return *r.AutoStart
}

// Option tunes the service.
type Option func(*Service)

// WithRunTimeout overrides the maximum duration of one migration.
func WithRunTimeout(timeout time.Duration) Option {
	return func(s *Service) {
		if timeout > 0 {
			s.runTimeout = timeout
		}
	}
}

// WithOwner sets the stable instance identity recorded on each submitted
// migration. A restarting instance uses it to fail only its own interrupted
// tasks instead of those another instance is still running.
func WithOwner(owner string) Option {
	return func(s *Service) {
		s.owner = strings.TrimSpace(owner)
	}
}

// WithNetworkFixer installs the post-move guest network restoration step.
func WithNetworkFixer(fixer *NetworkFixer) Option {
	return func(s *Service) {
		s.network = fixer
	}
}

// Service schedules and executes migrations.
type Service struct {
	clusters   *clusters.Manager
	store      store.Store
	logger     *slog.Logger
	runTimeout time.Duration
	owner      string
	// network restores a guest's DHCP lease after a cross-cluster move. It is
	// optional so tests and single-cluster setups can run without it.
	network *NetworkFixer

	mu      sync.Mutex
	active  map[string]struct{}
	cancels map[string]context.CancelFunc
	wg      sync.WaitGroup
}

// New creates a migration service.
func New(manager *clusters.Manager, st store.Store, logger *slog.Logger, opts ...Option) *Service {
	if logger == nil {
		logger = slog.Default()
	}

	s := &Service{
		clusters:   manager,
		store:      st,
		logger:     logger,
		runTimeout: 6 * time.Hour,
		active:     make(map[string]struct{}),
		cancels:    make(map[string]context.CancelFunc),
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// Get returns a persisted migration by ID.
func (s *Service) Get(ctx context.Context, id string) (*store.Migration, error) {
	migration, err := s.store.GetMigration(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	if err != nil {
		return nil, err
	}

	return migration, nil
}

// List returns recent migrations.
func (s *Service) List(ctx context.Context, limit int) ([]*store.Migration, error) {
	return s.store.ListMigrations(ctx, limit)
}

// Wait blocks until all running migrations finish.
func (s *Service) Wait() {
	s.wg.Wait()
}

// Cancel aborts a pending or in-flight migration. Only migrations running on
// this fleet instance can be canceled; in an HA deployment a migration owned
// by another instance reports a conflict. The interrupted move restores the
// source instance when the copy had not completed.
func (s *Service) Cancel(ctx context.Context, id string) (*store.Migration, error) {
	migration, err := s.store.GetMigration(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	if err != nil {
		return nil, err
	}

	switch migration.Status {
	case StatusDone, StatusFailed, StatusCanceled:
		return nil, fmt.Errorf("%w: migration %s is already %s", ErrConflict, id, migration.Status)
	}

	s.mu.Lock()
	cancel, running := s.cancels[id]
	s.mu.Unlock()
	if !running {
		return nil, fmt.Errorf("%w: migration %s is not running on this fleet instance", ErrConflict, id)
	}

	cancel()

	// The terminal status is written by the interrupted execute path; return
	// the row as it stands now so the caller can keep polling.
	return s.store.GetMigration(ctx, id)
}

// Retry resubmits a terminal migration with the same parameters. Snapshots
// are not carried over: the original include_snapshots choice is not
// persisted, so a retry transfers the instance itself.
func (s *Service) Retry(ctx context.Context, id string) (*store.Migration, error) {
	migration, err := s.store.GetMigration(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	if err != nil {
		return nil, err
	}

	switch migration.Status {
	case StatusDone, StatusFailed, StatusCanceled:
	default:
		return nil, fmt.Errorf("%w: migration %s is still %s", ErrConflict, id, migration.Status)
	}

	return s.Submit(ctx, Request{
		SourceCluster: migration.SourceCluster,
		TargetCluster: migration.TargetCluster,
		Instance:      migration.Instance,
		Mode:          migration.Mode,
		Live:          migration.Live,
	})
}

// Submit validates a migration, persists it, and starts it asynchronously.
func (s *Service) Submit(ctx context.Context, req Request) (*store.Migration, error) {
	plan, err := s.prepare(ctx, req)
	if err != nil {
		return nil, err
	}

	key := plan.request.SourceCluster + "/" + plan.request.Instance
	if !s.acquire(key) {
		return nil, fmt.Errorf("%w: migration already running for %s", ErrConflict, key)
	}

	release := func() { s.release(key) }

	now := time.Now().UTC()
	migration := &store.Migration{
		ID:            newID(),
		SourceCluster: plan.request.SourceCluster,
		TargetCluster: plan.request.TargetCluster,
		Instance:      plan.request.Instance,
		InstanceType:  plan.instance.Type,
		Mode:          plan.request.Mode,
		Live:          plan.request.Live,
		Status:        StatusPending,
		Owner:         s.owner,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if err := s.store.CreateMigration(ctx, migration); err != nil {
		release()
		return nil, err
	}

	plan.migration = migration
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer release()
		s.execute(plan)
	}()

	return migration, nil
}

type plan struct {
	request    Request
	migration  *store.Migration
	source     *clusters.Connection
	target     *clusters.Connection
	instance   api.Instance
	state      *api.InstanceState
	wasRunning bool
}

func (s *Service) prepare(_ context.Context, req Request) (*plan, error) {
	req.SourceCluster = strings.TrimSpace(req.SourceCluster)
	req.TargetCluster = strings.TrimSpace(req.TargetCluster)
	req.Instance = strings.TrimSpace(req.Instance)
	req.Mode = strings.TrimSpace(req.Mode)

	if req.SourceCluster == "" || req.TargetCluster == "" {
		return nil, fmt.Errorf("%w: source_cluster and target_cluster are required", ErrInvalidRequest)
	}
	if req.SourceCluster == req.TargetCluster {
		return nil, fmt.Errorf("%w: source and target cluster must differ", ErrInvalidRequest)
	}
	if req.Instance == "" {
		return nil, fmt.Errorf("%w: instance is required", ErrInvalidRequest)
	}
	if req.Mode == "" {
		req.Mode = ModeMove
	}
	if req.Mode != ModeMove && req.Mode != ModeCopy {
		return nil, fmt.Errorf("%w: mode must be %q or %q", ErrInvalidRequest, ModeMove, ModeCopy)
	}
	if req.Live && req.Mode != ModeMove {
		return nil, fmt.Errorf("%w: live migration requires mode %q", ErrInvalidRequest, ModeMove)
	}

	source, err := s.clusters.Connection(req.SourceCluster)
	if err != nil {
		return nil, err
	}
	target, err := s.clusters.Connection(req.TargetCluster)
	if err != nil {
		return nil, err
	}

	sourceClient, err := source.Client()
	if err != nil {
		return nil, err
	}
	targetClient, err := target.Client()
	if err != nil {
		return nil, err
	}

	instance, _, err := sourceClient.GetInstance(req.Instance)
	if err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("%w: instance %s not found on %s", ErrNotFound, req.Instance, req.SourceCluster)
		}

		return nil, fmt.Errorf("migrations: inspect source instance: %w", err)
	}

	if _, _, err := targetClient.GetInstance(req.Instance); err == nil {
		return nil, fmt.Errorf("%w: instance %s already exists on %s", ErrConflict, req.Instance, req.TargetCluster)
	} else if !isNotFound(err) {
		return nil, fmt.Errorf("migrations: inspect target instance: %w", err)
	}

	state, _, err := sourceClient.GetInstanceState(req.Instance)
	if err != nil {
		return nil, fmt.Errorf("migrations: inspect source state: %w", err)
	}

	wasRunning := state.StatusCode == api.Running || state.StatusCode == api.Frozen
	if req.Live {
		if instance.Type != string(api.InstanceTypeVM) {
			return nil, fmt.Errorf("%w: live migration requires a virtual machine", ErrInvalidRequest)
		}
		if instance.ExpandedConfig["migration.stateful"] != "true" {
			return nil, fmt.Errorf("%w: live migration requires migration.stateful=true", ErrInvalidRequest)
		}
		if state.StatusCode != api.Running {
			return nil, fmt.Errorf("%w: live migration requires a running instance", ErrInvalidRequest)
		}
	}

	if pool := instance.ExpandedDevices["root"]["pool"]; pool != "" {
		pools, err := targetClient.GetStoragePoolNames()
		if err != nil {
			return nil, fmt.Errorf("migrations: inspect target storage pools: %w", err)
		}
		found := false
		for _, name := range pools {
			if name == pool {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("%w: storage pool %q is missing on %s", ErrInvalidRequest, pool, req.TargetCluster)
		}
	}

	return &plan{
		request:    req,
		source:     source,
		target:     target,
		instance:   *instance,
		state:      state,
		wasRunning: wasRunning,
	}, nil
}

func (s *Service) execute(p *plan) {
	ctx, cancel := context.WithTimeout(context.Background(), s.runTimeout)
	s.mu.Lock()
	s.cancels[p.migration.ID] = cancel
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.cancels, p.migration.ID)
		s.mu.Unlock()
		cancel()
	}()

	req := p.request
	migration := p.migration
	sourceClient, err := p.source.Client()
	if err != nil {
		s.markFailed(ctx, migration.ID, "connect source", err)
		return
	}
	targetClient, err := p.target.Client()
	if err != nil {
		s.markFailed(ctx, migration.ID, "connect target", err)
		return
	}

	s.setStatus(ctx, migration.ID, StatusPreflight, "validated", "")

	stoppedSource := false
	copyComplete := false
	move := req.Mode == ModeMove

	restartSource := func(stage string) {
		restartCtx, restartCancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer restartCancel()
		if !stoppedSource {
			return
		}
		if move && copyComplete {
			return
		}
		if err := ensureRunning(restartCtx, sourceClient, req.Instance); err != nil {
			s.logger.Error("failed to restart source instance", "migration", migration.ID, "instance", req.Instance, "stage", stage, "error", err)
		}
	}

	fail := func(stage string, err error) {
		if errors.Is(err, context.Canceled) {
			// Operator cancellation: the run context is already dead, so the
			// terminal status and the source restore need fresh contexts.
			s.setStatus(context.Background(), migration.ID, StatusCanceled, stage, "canceled by operator")
			restartSource(stage)
			return
		}
		s.markFailed(ctx, migration.ID, stage, err)
		restartSource(stage)
	}

	args := &incus.InstanceCopyArgs{
		Name:         req.Instance,
		Live:         req.Live,
		InstanceOnly: !req.IncludeSnapshots,
		Mode:         "pull",
	}

	var remote incus.RemoteOperation

	if req.Live {
		s.setStatus(ctx, migration.ID, StatusTransferring, "live migration", "")
	} else {
		if p.wasRunning {
			s.setStatus(ctx, migration.ID, StatusStopping, "stopping source", "")
			op, err := sourceClient.UpdateInstanceState(req.Instance, api.InstanceStatePut{
				Action:  "stop",
				Timeout: 300,
			}, "")
			if err != nil {
				fail("stop source", err)
				return
			}
			if err := op.WaitContext(ctx); err != nil {
				fail("stop source", err)
				return
			}
			stoppedSource = true
		}
		s.setStatus(ctx, migration.ID, StatusTransferring, "transferring instance", "")
	}

	remote, err = targetClient.CopyInstance(p.source.RawClient(), p.instance, args)
	if err != nil {
		fail("copy instance", err)
		return
	}

	if err := s.waitRemote(ctx, migration.ID, remote); err != nil {
		fail("transfer instance", err)
		return
	}
	copyComplete = true

	s.setStatus(ctx, migration.ID, StatusVerifying, "verifying target", "")

	if req.Live {
		// A live move must stop the source before the target takes over.
		if err := stopIfRunning(ctx, sourceClient, req.Instance); err != nil {
			fail("stop source", err)
			return
		}
		stoppedSource = true
	}

	if req.Live || (p.wasRunning && req.autoStart()) {
		s.setStatus(ctx, migration.ID, StatusStarting, "starting target", "")
		if err := ensureRunning(ctx, targetClient, req.Instance); err != nil {
			fail("start target", err)
			return
		}
	}

	if move {
		s.setStatus(ctx, migration.ID, StatusVerifying, "removing source", "")
		op, err := sourceClient.DeleteInstance(req.Instance)
		if err != nil {
			fail("delete source", err)
			return
		}
		if err := op.WaitContext(ctx); err != nil {
			fail("delete source", err)
			return
		}
	} else if stoppedSource {
		s.setStatus(ctx, migration.ID, StatusStarting, "restarting source", "")
		if err := ensureRunning(ctx, sourceClient, req.Instance); err != nil {
			fail("restart source", err)
			return
		}
	}

	// A moved guest keeps the DHCP lease it got on the source cluster, and the
	// target bridge cannot see that lease, so it would stay unreachable until
	// the lease expires. Ask the guest to release and renew now.
	if s.network != nil && req.Mode == ModeMove {
		s.setStatus(ctx, migration.ID, StatusVerifying, "restoring guest network", "")
		if err := s.network.RestoreLease(ctx, req.TargetCluster, req.Instance); err != nil {
			// Network restoration is best-effort: the data move succeeded and
			// failing the task here would misreport it.
			s.logger.Warn("guest network restore failed",
				"migration", migration.ID, "instance", req.Instance, "error", err)
		}
	}

	s.setStatus(ctx, migration.ID, StatusDone, "complete", "")
	s.logger.Info("migration complete", "migration", migration.ID, "instance", req.Instance, "source", req.SourceCluster, "target", req.TargetCluster, "mode", req.Mode)
}

func (s *Service) waitRemote(ctx context.Context, id string, remote incus.RemoteOperation) error {
	tracker := &progressTracker{service: s, id: id}

	if _, err := remote.AddHandler(func(op api.Operation) { tracker.update(ctx, op) }); err != nil {
		s.logger.Warn("failed to attach migration progress handler", "migration", id, "error", err)
	}

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- remote.Wait()
	}()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case err := <-waitCh:
			if err == nil {
				return nil
			}

			return s.recoverFromWaitError(ctx, remote, err)
		case <-ticker.C:
			if target, err := remote.GetTarget(); err == nil && target != nil {
				tracker.update(ctx, *target)
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// recoverFromWaitError handles the known Incus behavior where the CLI/event
// listener times out while the server-side migration keeps running.
func (s *Service) recoverFromWaitError(ctx context.Context, remote incus.RemoteOperation, waitErr error) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		target, err := remote.GetTarget()
		if err != nil {
			return waitErr
		}
		if target == nil {
			return waitErr
		}
		if target.StatusCode.IsFinal() {
			if target.StatusCode == api.Success {
				return nil
			}
			if target.Err != "" {
				return errors.New(target.Err)
			}

			return fmt.Errorf("migration operation ended with status %s", target.Status)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *Service) setStatus(ctx context.Context, id, status, progress, errText string) {
	migration := &store.Migration{
		ID:        id,
		Status:    status,
		Progress:  progress,
		Error:     errText,
		UpdatedAt: time.Now().UTC(),
	}
	if err := s.store.UpdateMigration(ctx, migration); err != nil {
		s.logger.Warn("failed to update migration status", "migration", id, "status", status, "error", err)
	}
}

func (s *Service) markFailed(ctx context.Context, id, stage string, err error) {
	s.setStatus(ctx, id, StatusFailed, stage, stage+": "+err.Error())
	s.logger.Error("migration failed", "migration", id, "stage", stage, "error", err)
}

type progressTracker struct {
	service *Service
	id      string
	mu      sync.Mutex
	last    string
}

func (t *progressTracker) update(ctx context.Context, op api.Operation) {
	text := progressText(op.Metadata)
	if text == "" {
		return
	}

	t.mu.Lock()
	if text == t.last {
		t.mu.Unlock()
		return
	}
	t.last = text
	t.mu.Unlock()

	if err := t.service.store.UpdateMigration(ctx, &store.Migration{
		ID:        t.id,
		Status:    StatusTransferring,
		Progress:  text,
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.service.logger.Debug("failed to persist migration progress", "migration", t.id, "error", err)
	}
}

func progressText(metadata map[string]any) string {
	for _, key := range []string{"block_progress", "download_progress", "migration_progress", "progress"} {
		if value, ok := metadata[key].(string); ok && value != "" {
			return value
		}
	}

	return ""
}

func ensureRunning(ctx context.Context, client clusters.InstanceServer, name string) error {
	state, _, err := client.GetInstanceState(name)
	if err != nil {
		return err
	}

	switch state.StatusCode {
	case api.Running:
		return nil
	case api.Frozen:
		op, err := client.UpdateInstanceState(name, api.InstanceStatePut{Action: "unfreeze", Timeout: 300}, "")
		if err != nil {
			return err
		}
		return op.WaitContext(ctx)
	default:
		op, err := client.UpdateInstanceState(name, api.InstanceStatePut{Action: "start", Timeout: 300}, "")
		if err != nil {
			return err
		}
		return op.WaitContext(ctx)
	}
}

func stopIfRunning(ctx context.Context, client clusters.InstanceServer, name string) error {
	state, _, err := client.GetInstanceState(name)
	if err != nil {
		return err
	}
	if state.StatusCode != api.Running && state.StatusCode != api.Frozen {
		return nil
	}

	op, err := client.UpdateInstanceState(name, api.InstanceStatePut{Action: "stop", Timeout: 300, Force: true}, "")
	if err != nil {
		return err
	}

	return op.WaitContext(ctx)
}

func (s *Service) acquire(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.active[key]; exists {
		return false
	}
	s.active[key] = struct{}{}

	return true
}

func (s *Service) release(key string) {
	s.mu.Lock()
	delete(s.active, key)
	s.mu.Unlock()
}

func isNotFound(err error) bool {
	var statusErr api.StatusError
	if errors.As(err, &statusErr) {
		return statusErr.Status() == http.StatusNotFound
	}

	return false
}

func newID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("m-%d", time.Now().UnixNano())
	}

	return hex.EncodeToString(buf[:])
}
