// Package plugininstall implements checkpointed, resumable plugin installs on
// top of the fleet host-task lease and event model. It never executes plugin
// code: each step composes existing fleet primitives (image, volume, instance,
// ops, config, health probe, wiring).
package plugininstall

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mickeyzzc/incus-fleet/internal/auth"
	"github.com/mickeyzzc/incus-fleet/internal/clusters"
	"github.com/mickeyzzc/incus-fleet/internal/instances"
	"github.com/mickeyzzc/incus-fleet/internal/plugin"
	"github.com/mickeyzzc/incus-fleet/internal/store"
	"github.com/mickeyzzc/incus-fleet/internal/taskrunner"
)

// TaskType is the durable host-task type used for plugin installs.
const TaskType = "plugin.install"

// Install steps are ordered 1-10 to match the plugin specification. Rollback
// runs in reverse from 9 down to 3.
var stepNames = []string{
	"validate", "placement", "image", "data_disk", "format_mount",
	"create_instance", "inject_config", "health", "activate_wiring", "publish",
}

var (
	// ErrCanceled is returned when an install observes a cancellation checkpoint.
	ErrCanceled = errors.New("plugininstall: install canceled")
	// ErrInvalidRequest marks a plan that cannot be installed.
	ErrInvalidRequest = errors.New("plugininstall: invalid request")
)

// InstanceOps is the subset of the instance service an install needs.
type InstanceOps interface {
	Create(ctx context.Context, clusterName string, req instances.CreateRequest) (instances.Detail, error)
	Delete(ctx context.Context, clusterName, name string, force, deleteVolumes bool) error
	Get(clusterName, name string) (instances.Detail, error)
	RunOps(ctx context.Context, clusterName, name string, req instances.OpsRequest) (instances.OpsResult, error)
	UploadFile(clusterName, name, filePath string, content []byte) error
	CreateVolume(clusterName, pool string, req instances.VolumeRequest) error
	DeleteVolume(clusterName, pool, name string) error
}

// ClusterLister chooses a placement for an install.
type ClusterLister interface{ List() []clusters.Summary }

// ImageResolver maps a manifest image alias to a cluster fingerprint.
type ImageResolver interface {
	Resolve(ctx context.Context, cluster, ref string) (string, error)
}

// TokenCreator issues the scoped plugin credential on publish.
type TokenCreator interface {
	CreateAPIToken(ctx context.Context, req auth.CreateTokenRequest) (*auth.APIToken, string, error)
}

// Request is the durable install plan stored in HostTask.Plan. It contains no
// secrets: the signing key and one-time token secret stay out of the database.
type Request struct {
	PluginID               string            `json:"id"`
	Name                   string            `json:"name,omitempty"`
	Manifest               plugin.Manifest   `json:"manifest"`
	Cluster                string            `json:"cluster,omitempty"`
	Member                 string            `json:"member,omitempty"`
	Instance               string            `json:"instance,omitempty"`
	Image                  string            `json:"image,omitempty"`
	Network                string            `json:"network,omitempty"`
	CPU                    string            `json:"cpu,omitempty"`
	Memory                 string            `json:"memory,omitempty"`
	Disk                   string            `json:"disk,omitempty"`
	Upstream               string            `json:"upstream,omitempty"`
	TemplateValues         map[string]string `json:"templates,omitempty"`
	DataDiskGB             int               `json:"data_disk_gb,omitempty"`
	Filesystem             string            `json:"filesystem,omitempty"`
	MountPoint             string            `json:"mount_point,omitempty"`
	StoragePool            string            `json:"storage_pool,omitempty"`
	Approvals              []string          `json:"approvals,omitempty"`
	Defaults               map[string]string `json:"defaults,omitempty"`
	HealthDelayMS          int               `json:"health_delay_ms,omitempty"`
	HealthIntervalMS       int               `json:"health_interval_ms,omitempty"`
	HealthFailureThreshold int               `json:"health_failure_threshold,omitempty"`
	ManifestSignature      string            `json:"manifest_signature,omitempty"`
	// SignedManifestBytes is the exact byte slice the signature covers. It is
	// non-empty only for signed community packages.
	SignedManifestBytes []byte `json:"manifest_bytes,omitempty"`
}

type checkpoint struct {
	Step            int      `json:"step"`
	Cluster         string   `json:"cluster,omitempty"`
	Member          string   `json:"member,omitempty"`
	Instance        string   `json:"instance,omitempty"`
	Image           string   `json:"image,omitempty"`
	Volume          string   `json:"volume,omitempty"`
	StoragePool     string   `json:"storage_pool,omitempty"`
	Upstream        string   `json:"upstream,omitempty"`
	WiringActivated []string `json:"wiring_activated,omitempty"`
}

// Config supplies the installer dependencies.
type Config struct {
	Store      *store.SQLStore
	Instances  InstanceOps
	Clusters   ClusterLister
	Images     ImageResolver
	Auth       TokenCreator
	Registry   *plugin.Registry
	SigningKey []byte
	// Sleep is injectable so tests do not depend on wall-clock delays.
	Sleep      func(context.Context, time.Duration) error
	HTTPClient *http.Client
	// HealthBaseURL overrides the probe target (useful for tests and for
	// deployments where the instance address is not the probe address).
	HealthBaseURL string
	Logger        *slog.Logger
}

// Installer executes plugin install tasks.
type Installer struct {
	cfg Config
}

// New builds an installer. A store is mandatory; the other dependencies are
// checked when the step that needs them runs, so a connect-only install can
// work without an image resolver.
func New(cfg Config) *Installer {
	if cfg.Store == nil {
		panic("plugininstall: store is required")
	}
	if cfg.Sleep == nil {
		cfg.Sleep = func(ctx context.Context, d time.Duration) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(d):
				return nil
			}
		}
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 5 * time.Second}
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	return &Installer{cfg: cfg}
}

// Enqueue records a new install task. Only one queued/running task may own a
// plugin id, so a double click cannot provision two instances.
func (i *Installer) Enqueue(ctx context.Context, req Request, owner string) (*store.HostTask, error) {
	if err := validateRequest(req, i.cfg.SigningKey); err != nil {
		return nil, err
	}
	plan, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("plugininstall: encode plan: %w", err)
	}
	tasks, err := i.cfg.Store.ListHostTasks(ctx, "", "", 0)
	if err != nil {
		return nil, fmt.Errorf("plugininstall: list install tasks: %w", err)
	}
	for _, task := range tasks {
		if task.Type != TaskType {
			continue
		}
		var existing Request
		if json.Unmarshal([]byte(task.Plan), &existing) == nil && existing.PluginID == req.PluginID &&
			(task.State == store.HostTaskQueued || task.State == store.HostTaskRunning || task.State == store.HostTaskDispatched) {
			return nil, fmt.Errorf("%w: install for %s is already running", store.ErrConflict, req.PluginID)
		}
	}

	now := time.Now().UTC()
	task := &store.HostTask{
		ID:             store.NewID(),
		Type:           TaskType,
		State:          store.HostTaskQueued,
		Cluster:        req.Cluster,
		Member:         req.Member,
		Plan:           string(plan),
		PlanHash:       planHash(plan),
		IdempotencyKey: "plugin-install:" + req.PluginID,
		CreatedBy:      owner,
		CreatedAt:      now,
		UpdatedAt:      now,
		Attempt:        1,
	}
	if err := i.cfg.Store.CreateHostTask(ctx, task); err != nil {
		return nil, fmt.Errorf("plugininstall: create task: %w", err)
	}
	_ = i.cfg.Store.AppendHostTaskEvent(ctx, &store.HostTaskEvent{
		ID: store.NewID(), TaskID: task.ID, At: now,
		State: store.HostTaskQueued, Message: "queued: " + req.PluginID,
	})

	return task, nil
}

func validateRequest(req Request, signingKey []byte) error {
	if strings.TrimSpace(req.PluginID) == "" {
		return fmt.Errorf("%w: plugin id is required", ErrInvalidRequest)
	}
	if req.Manifest.ID != req.PluginID {
		return fmt.Errorf("%w: manifest id %q does not match %q", ErrInvalidRequest, req.Manifest.ID, req.PluginID)
	}
	if err := req.Manifest.Validate(); err != nil {
		return err
	}
	if req.Manifest.Tier == plugin.TierCommunity && len(req.Approvals) > 0 {
		return fmt.Errorf("%w: community plugins cannot declare wiring", ErrInvalidRequest)
	}
	if req.ManifestSignature != "" && !plugin.VerifySignature([]byte(req.ManifestRaw()), req.ManifestSignature, signingKey) {
		return fmt.Errorf("%w: manifest signature is invalid", ErrInvalidRequest)
	}

	return nil
}

// ManifestRaw is populated by the manifest loader when a signature has to be
// checked. The plan itself stores only the parsed manifest and its signature;
// the loader recomputes the signed bytes before enqueuing.
func (r Request) ManifestRaw() []byte {
	if len(r.SignedManifestBytes) > 0 {
		return r.SignedManifestBytes
	}
	return nil
}

// Handler adapts the installer to taskrunner's lease/event model. A fleet
// instance with HA enabled claims plugin installs just like host tasks.
func (i *Installer) Handler() taskrunner.Handler {
	return func(ctx context.Context, task *store.HostTask) (string, error) {
		if task.Type != TaskType {
			return "", fmt.Errorf("plugininstall: unexpected task type %q", task.Type)
		}
		if err := i.execute(ctx, task); err != nil {
			return "", err
		}

		return "installed", nil
	}
}

// RunTask executes a queued or failed task in the calling process. It is used
// by single-instance deployments and by the synchronous compatibility endpoint.
func (i *Installer) RunTask(ctx context.Context, id string) error {
	task, err := i.cfg.Store.GetHostTask(ctx, id)
	if err != nil {
		return err
	}
	if task.Type != TaskType {
		return fmt.Errorf("%w: task %s is not a plugin install", ErrInvalidRequest, id)
	}
	if task.State == store.HostTaskCanceled {
		return ErrCanceled
	}
	if terminal(task.State) {
		return fmt.Errorf("%w: task is %s", store.ErrConflict, task.State)
	}
	now := time.Now().UTC()
	task.State = store.HostTaskRunning
	task.StartedAt = now
	task.UpdatedAt = now
	if err := i.cfg.Store.UpdateHostTask(ctx, task); err != nil {
		return fmt.Errorf("plugininstall: start task: %w", err)
	}
	_ = i.cfg.Store.AppendHostTaskEvent(ctx, &store.HostTaskEvent{
		ID: store.NewID(), TaskID: task.ID, At: now,
		State: store.HostTaskRunning, Message: "started",
	})

	err = i.execute(ctx, task)
	i.finish(ctx, task, err)

	return err
}

func (i *Installer) finish(ctx context.Context, task *store.HostTask, runErr error) {
	now := time.Now().UTC()
	task.UpdatedAt = now
	task.FinishedAt = now
	if runErr != nil {
		task.State = store.HostTaskFailed
		task.Result = "failed"
		task.Error = runErr.Error()
		task.ErrorCode = "install_failed"
	} else {
		task.State = store.HostTaskSucceeded
		task.Result = "succeeded"
		task.Error = ""
		task.ErrorCode = ""
	}
	if err := i.cfg.Store.UpdateHostTask(context.Background(), task); err != nil {
		i.cfg.Logger.Error("update plugin install task", "task", task.ID, "error", err)
	}
	state := task.State
	message := task.Result
	if errors.Is(runErr, ErrCanceled) {
		state = store.HostTaskCanceled
		message = "canceled"
	}
	_ = i.cfg.Store.AppendHostTaskEvent(context.Background(), &store.HostTaskEvent{
		ID: store.NewID(), TaskID: task.ID, At: now, State: state, Message: message,
	})
	if i.cfg.Registry != nil {
		i.cfg.Registry.Publish(ctx, plugin.Event{
			Type: plugin.EventHostTaskCompleted, Subject: task.ID,
			Payload: map[string]string{"plugin": task.IdempotencyKey, "result": message},
		})
	}
}

// Cancel requests cooperative cancellation; the next checkpoint observes it.
func (i *Installer) Cancel(ctx context.Context, id string) error {
	task, err := i.cfg.Store.GetHostTask(ctx, id)
	if err != nil {
		return err
	}
	if terminal(task.State) {
		return fmt.Errorf("%w: task is %s", store.ErrConflict, task.State)
	}
	task.State = store.HostTaskCanceled
	task.UpdatedAt = time.Now().UTC()
	if err := i.cfg.Store.UpdateHostTask(ctx, task); err != nil {
		return fmt.Errorf("plugininstall: cancel task: %w", err)
	}
	_ = i.cfg.Store.AppendHostTaskEvent(ctx, &store.HostTaskEvent{
		ID: store.NewID(), TaskID: task.ID, At: task.UpdatedAt,
		State: store.HostTaskCanceled, Message: "cancel requested",
	})

	return nil
}

// Resume re-queues a failed task from its last successful checkpoint.
func (i *Installer) Resume(ctx context.Context, id string) error {
	task, err := i.cfg.Store.GetHostTask(ctx, id)
	if err != nil {
		return err
	}
	if task.State != store.HostTaskFailed && task.State != store.HostTaskCanceled {
		return fmt.Errorf("%w: only failed or canceled tasks can resume", store.ErrConflict)
	}
	task.State = store.HostTaskQueued
	task.Attempt++
	task.Error = ""
	task.ErrorCode = ""
	task.UpdatedAt = time.Now().UTC()
	if err := i.cfg.Store.UpdateHostTask(ctx, task); err != nil {
		return fmt.Errorf("plugininstall: resume task: %w", err)
	}
	_ = i.cfg.Store.AppendHostTaskEvent(ctx, &store.HostTaskEvent{
		ID: store.NewID(), TaskID: task.ID, At: task.UpdatedAt,
		State: store.HostTaskQueued, Message: fmt.Sprintf("resume attempt %d", task.Attempt),
	})

	return nil
}

func terminal(state string) bool {
	switch state {
	case store.HostTaskSucceeded, store.HostTaskFailed, store.HostTaskCanceled:
		return true
	default:
		return false
	}
}

func (i *Installer) execute(ctx context.Context, task *store.HostTask) error {
	var req Request
	if err := json.Unmarshal([]byte(task.Plan), &req); err != nil {
		return fmt.Errorf("plugininstall: decode plan: %w", err)
	}
	cp := decodeCheckpoint(task.Progress)

	for step := cp.Step; step < len(stepNames); step++ {
		current, err := i.cfg.Store.GetHostTask(ctx, task.ID)
		if err != nil {
			return fmt.Errorf("plugininstall: read task state: %w", err)
		}
		if current.State == store.HostTaskCanceled {
			i.rollback(ctx, task, cp.Step)
			return ErrCanceled
		}
		if err := i.runStep(ctx, &req, &cp, step); err != nil {
			i.rollback(ctx, task, cp.Step)
			return fmt.Errorf("plugininstall: step %s: %w", stepNames[step], err)
		}
		cp.Step = step + 1
		if err := i.saveProgress(ctx, task, cp, step); err != nil {
			return err
		}
	}

	return nil
}

func (i *Installer) saveProgress(ctx context.Context, task *store.HostTask, cp checkpoint, step int) error {
	raw, err := json.Marshal(cp)
	if err != nil {
		return fmt.Errorf("plugininstall: encode checkpoint: %w", err)
	}
	task.Progress = string(raw)
	task.UpdatedAt = time.Now().UTC()
	if err := i.cfg.Store.UpdateHostTask(ctx, task); err != nil {
		return fmt.Errorf("plugininstall: save checkpoint: %w", err)
	}
	_ = i.cfg.Store.AppendHostTaskEvent(ctx, &store.HostTaskEvent{
		ID: store.NewID(), TaskID: task.ID, At: task.UpdatedAt,
		State: task.State, Message: "checkpoint: " + stepNames[step],
	})

	return nil
}

func decodeCheckpoint(raw string) checkpoint {
	var cp checkpoint
	if strings.TrimSpace(raw) != "" {
		_ = json.Unmarshal([]byte(raw), &cp)
	}
	if cp.Step < 0 || cp.Step > len(stepNames) {
		cp.Step = 0
	}

	return cp
}

func (i *Installer) runStep(ctx context.Context, req *Request, cp *checkpoint, step int) error {
	switch step {
	case 0: // validate
		return req.Manifest.Validate()
	case 1: // placement
		if req.Cluster != "" {
			cp.Cluster = req.Cluster
		} else if i.cfg.Clusters != nil {
			for _, summary := range i.cfg.Clusters.List() {
				if summary.Online {
					cp.Cluster = summary.Name
					break
				}
			}
		}
		if cp.Cluster == "" {
			return fmt.Errorf("%w: no online cluster found", ErrInvalidRequest)
		}
		cp.Member = req.Member
		if cp.Member == "" && i.cfg.Clusters != nil {
			for _, summary := range i.cfg.Clusters.List() {
				if summary.Name != cp.Cluster {
					continue
				}
				for _, member := range summary.Members {
					if member.Status == "Online" {
						cp.Member = member.Name
						break
					}
				}
			}
		}
	case 2: // image
		if i.cfg.Images == nil {
			return errors.New("image resolver is not configured")
		}
		fingerprint, err := i.cfg.Images.Resolve(ctx, cp.Cluster, req.Image)
		if err != nil {
			return err
		}
		cp.Image = fingerprint
	case 3: // data disk
		size := req.DataDiskGB
		if size == 0 && req.Manifest.Install.DataDisk.Required {
			size = req.Manifest.Install.DataDisk.DefaultGB
		}
		if size <= 0 || i.cfg.Instances == nil {
			return nil
		}
		pool := firstNonEmpty(req.StoragePool, "default")
		volume := "plugin-" + req.PluginID + "-data"
		req.Filesystem = firstNonEmpty(req.Filesystem, req.Manifest.Install.DataDisk.Filesystem)
		req.MountPoint = firstNonEmpty(req.MountPoint, req.Manifest.Install.DataDisk.MountPoint)
		if err := i.cfg.Instances.CreateVolume(cp.Cluster, pool, instances.VolumeRequest{
			Name: volume, Size: fmt.Sprintf("%dGiB", size),
			Config: map[string]string{"size": fmt.Sprintf("%dGiB", size)},
		}); err != nil {
			return fmt.Errorf("allocate data volume: %w", err)
		}
		cp.Volume = volume
		cp.StoragePool = pool
	case 4: // format and mount
		if cp.Volume == "" {
			return nil
		}
		result, err := i.cfg.Instances.RunOps(ctx, cp.Cluster, cp.Instance, instances.OpsRequest{
			Action: "run-script", Script: plugin.DataDiskScript(req.Manifest, req.DataDiskGB),
		})
		if err != nil {
			return err
		}
		if result.ExitCode != 0 {
			return fmt.Errorf("format script exited %d: %s", result.ExitCode, result.Stderr)
		}
	case 5: // create instance
		if i.cfg.Instances == nil {
			return errors.New("instance service is not configured")
		}
		cp.Instance = firstNonEmpty(req.Instance, "fleet-"+req.PluginID)
		create := instances.CreateRequest{
			Name: cp.Instance, Type: "container", Image: cp.Image,
			CPU: firstNonEmpty(req.CPU, "2"), Memory: firstNonEmpty(req.Memory, "2GiB"),
			RootDiskSize: firstNonEmpty(req.Disk, "20GiB"), StoragePool: firstNonEmpty(req.StoragePool, "default"),
			Network: firstNonEmpty(req.Network, req.Manifest.Install.Defaults.Network, "incusbr0"),
			Member:  cp.Member, Start: boolPtr(true),
		}
		if cp.Volume != "" {
			create.Disks = []instances.DiskSpec{{
				Name: "data", Path: firstNonEmpty(req.MountPoint, "/data"),
				Pool: cp.StoragePool, Source: cp.Volume,
			}}
		}
		if _, err := i.cfg.Instances.Create(ctx, cp.Cluster, create); err != nil {
			return err
		}
	case 6: // inject rendered configuration
		values, err := plugin.RenderTemplates(req.Manifest, req.TemplateValues)
		if err != nil {
			return err
		}
		script := plugin.ConfigScript(values)
		if err := i.cfg.Instances.UploadFile(cp.Cluster, cp.Instance, "/tmp/fleet-plugin-config.sh", []byte(script)); err != nil {
			return fmt.Errorf("upload config: %w", err)
		}
		result, err := i.cfg.Instances.RunOps(ctx, cp.Cluster, cp.Instance, instances.OpsRequest{
			Action: "run-script", Script: "sh /tmp/fleet-plugin-config.sh",
		})
		if err != nil {
			return err
		}
		if result.ExitCode != 0 {
			return fmt.Errorf("config script exited %d: %s", result.ExitCode, result.Stderr)
		}
	case 7: // health check gates wiring activation
		upstream, err := i.waitHealthy(ctx, req, cp)
		if err != nil {
			return err
		}
		cp.Upstream = upstream
	case 8: // activate approved wiring
		if i.cfg.Registry == nil {
			return errors.New("plugin registry is not configured")
		}
		if _, exists := i.cfg.Registry.Get(req.PluginID); !exists {
			if err := i.cfg.Registry.Register(plugin.Descriptor(req.Manifest)); err != nil {
				return fmt.Errorf("register installed plugin: %w", err)
			}
		}
		for _, key := range req.Approvals {
			if !req.Manifest.Capabilities.DeclaresWiring(key) {
				return fmt.Errorf("%w: wiring key %q was not declared by %s", ErrInvalidRequest, key, req.PluginID)
			}
			if err := i.cfg.Store.UpsertPluginWiring(ctx, &store.PluginWiring{
				PluginID: req.PluginID, Key: key, Value: cp.Upstream,
				DefaultValue: req.Defaults[key], Enabled: true, Approved: true,
			}); err != nil {
				return fmt.Errorf("persist wiring: %w", err)
			}
			if err := i.cfg.Registry.SetWiring(req.PluginID, key, cp.Upstream); err != nil {
				return err
			}
			cp.WiringActivated = append(cp.WiringActivated, key)
		}
	case 9: // publish the manager entry, scoped token, and event webhook
		entry := &store.InstalledPlugin{
			ID: req.PluginID, Name: firstNonEmpty(req.Name, req.Manifest.Name),
			Version: req.Manifest.Version, Upstream: cp.Upstream,
			UIPath: "/plugin/" + req.PluginID, Role: "viewer",
			Cluster: cp.Cluster, Instance: cp.Instance, Enabled: true,
		}
		if i.cfg.Auth != nil {
			token, _, err := i.cfg.Auth.CreateAPIToken(ctx, auth.CreateTokenRequest{
				Name: "plugin-" + req.PluginID, Scopes: []auth.Scope{auth.ScopeReadPlugins},
				PluginID: req.PluginID, InstanceID: cp.Instance,
			})
			if err != nil {
				return fmt.Errorf("issue plugin token: %w", err)
			}
			entry.TokenID = token.ID
		}
		if err := i.cfg.Store.CreateInstalledPlugin(ctx, entry); err != nil {
			return fmt.Errorf("publish installed plugin: %w", err)
		}
		if len(req.Manifest.Capabilities.EventWebhook.Events) > 0 && i.cfg.Registry != nil {
			sub, err := plugin.NewWebhookSubscriber(plugin.WebhookConfig{
				ID: req.PluginID, BaseURL: cp.Upstream, Path: req.Manifest.Capabilities.EventWebhook.Path,
				Events: req.Manifest.Capabilities.EventWebhook.Events,
				Secret: plugin.SignPayload(i.cfg.SigningKey, []byte("webhook:"+req.PluginID)),
			})
			if err != nil {
				return err
			}
			if err := i.cfg.Registry.RegisterSubscriber(req.PluginID, sub); err != nil {
				return err
			}
		}
	}

	return nil
}

func (i *Installer) rollback(_ context.Context, task *store.HostTask, completed int) {
	var req Request
	_ = json.Unmarshal([]byte(task.Plan), &req)
	var cp checkpoint
	_ = json.Unmarshal([]byte(task.Progress), &cp)

	// Reverse steps 9→3. Steps without external effects are skipped; each
	// executed rollback is recorded so an operator sees what was undone.
	for step := completed - 1; step >= 2; step-- {
		var err error
		switch step {
		case 9:
			err = i.cfg.Store.DeleteInstalledPlugin(context.Background(), req.PluginID)
		case 8:
			if i.cfg.Registry != nil {
				err = i.cfg.Registry.ClearWiring(req.PluginID)
			}
		case 5:
			if cp.Instance != "" && i.cfg.Instances != nil {
				err = i.cfg.Instances.Delete(context.Background(), cp.Cluster, cp.Instance, true, true)
			}
		case 3:
			if cp.Volume != "" && i.cfg.Instances != nil {
				err = i.cfg.Instances.DeleteVolume(cp.Cluster, cp.StoragePool, cp.Volume)
			}
		default:
			continue
		}
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			i.cfg.Logger.Warn("plugin install rollback failed", "task", task.ID, "step", stepNames[step], "error", err)
		}
		_ = i.cfg.Store.AppendHostTaskEvent(context.Background(), &store.HostTaskEvent{
			ID: store.NewID(), TaskID: task.ID, At: time.Now().UTC(),
			State: task.State, Message: "rollback: " + stepNames[step],
		})
	}
	// A resumed task must replay the undone steps (at least the data disk),
	// otherwise it would attach a volume that no longer exists.
	if completed > 3 {
		cp.Step = 2
		if raw, err := json.Marshal(cp); err == nil {
			task.Progress = string(raw)
			_ = i.cfg.Store.UpdateHostTask(context.Background(), task)
		}
	}
}

func (i *Installer) waitHealthy(ctx context.Context, req *Request, cp *checkpoint) (string, error) {
	upstream := req.Upstream
	if upstream == "" && i.cfg.Instances != nil {
		detail, err := i.cfg.Instances.Get(cp.Cluster, cp.Instance)
		if err != nil {
			return "", fmt.Errorf("read instance address: %w", err)
		}
		ip := ""
		for _, address := range detail.IPv4 {
			if strings.TrimSpace(address) != "" {
				ip = strings.TrimSpace(address)
				break
			}
		}
		if ip == "" {
			return "", fmt.Errorf("%w: instance %s has no address", ErrInvalidRequest, cp.Instance)
		}
		port := healthPort(req)
		upstream = fmt.Sprintf("http://%s:%d", ip, port)
	}
	upstream = strings.TrimRight(upstream, "/")
	endpoint := firstNonEmpty(req.Manifest.Health.Endpoint, "/health")

	base := i.cfg.HealthBaseURL
	if base == "" {
		base = upstream
	}
	url := strings.TrimRight(base, "/") + endpoint
	delay := time.Duration(req.HealthDelayMS) * time.Millisecond
	if delay <= 0 {
		delay = time.Duration(req.Manifest.Health.InitialDelaySeconds) * time.Second
	}
	interval := time.Duration(req.HealthIntervalMS) * time.Millisecond
	if interval <= 0 {
		interval = time.Duration(req.Manifest.Health.IntervalSeconds) * time.Second
	}
	threshold := req.HealthFailureThreshold
	if threshold <= 0 {
		threshold = req.Manifest.Health.FailureThreshold
	}
	if threshold <= 0 {
		threshold = 5
	}
	if err := i.cfg.Sleep(ctx, delay); err != nil {
		return "", err
	}
	for attempt := 1; attempt <= threshold; attempt++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return "", fmt.Errorf("plugininstall: health request: %w", err)
		}
		resp, err := i.cfg.HTTPClient.Do(request)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return upstream, nil
			}
		}
		if attempt < threshold {
			if err := i.cfg.Sleep(ctx, interval); err != nil {
				return "", err
			}
		}
	}

	return "", fmt.Errorf("%w: plugin %s did not become healthy after %d probes", ErrInvalidRequest, req.PluginID, threshold)
}

func healthPort(req *Request) int {
	for _, candidate := range []string{
		req.TemplateValues["listen_port"],
		func() string {
			for _, t := range req.Manifest.Install.Templates {
				if t.Type == plugin.TemplatePort && t.Default != "" {
					return t.Default
				}
			}
			return ""
		}(),
	} {
		if port, err := strconv.Atoi(candidate); err == nil && port > 0 && port <= 65535 {
			return port
		}
	}
	if req.Manifest.Capabilities.ProxyUI.UpstreamPort > 0 {
		return req.Manifest.Capabilities.ProxyUI.UpstreamPort
	}

	return 8080
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}

	return ""
}

func boolPtr(value bool) *bool { return &value }

func planHash(plan []byte) string {
	sum := sha256.Sum256(plan)
	return fmt.Sprintf("%x", sum[:])
}
