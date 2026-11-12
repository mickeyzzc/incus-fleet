package instances

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	incus "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"
)

const (
	opsTimeout       = 10 * time.Minute
	keyFingerprintRe = `^[0-9a-f]{12,64}$`
)

var fingerprintPattern = regexp.MustCompile(keyFingerprintRe)

// OpsRequest is one guest-side maintenance action. The payload for the
// action (script text, password, public key) is piped to the instance via
// stdin so secrets never appear in process argument lists.
type OpsRequest struct {
	Action   string `json:"action"`
	Script   string `json:"script,omitempty"`
	User     string `json:"user,omitempty"`
	Password string `json:"password,omitempty"`
	Key      string `json:"key,omitempty"`
}

// OpsResult reports what a guest-side maintenance action printed.
type OpsResult struct {
	ExitCode int    `json:"exit_code"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
}

// CloneRequest copies one instance into a new name on the same cluster.
type CloneRequest struct {
	Target string `json:"target"`
	Start  *bool  `json:"start,omitempty"`
}

// RunOps performs a guest-side maintenance action inside an instance.
func (s *Service) RunOps(ctx context.Context, clusterName, name string, req OpsRequest) (OpsResult, error) {
	req.Action = strings.TrimSpace(req.Action)
	req.User = strings.TrimSpace(req.User)

	var command []string
	var stdin string
	switch req.Action {
	case "run-script":
		if strings.TrimSpace(req.Script) == "" {
			return OpsResult{}, fmt.Errorf("%w: script is required", ErrInvalidRequest)
		}
		command = []string{"sh", "-s"}
		stdin = req.Script
	case "set-password":
		if req.User == "" {
			return OpsResult{}, fmt.Errorf("%w: user is required", ErrInvalidRequest)
		}
		if req.Password == "" {
			return OpsResult{}, fmt.Errorf("%w: password is required", ErrInvalidRequest)
		}
		command = []string{"chpasswd"}
		stdin = req.User + ":" + req.Password + "\n"
	case "inject-key":
		if req.User == "" {
			return OpsResult{}, fmt.Errorf("%w: user is required", ErrInvalidRequest)
		}
		key := strings.TrimSpace(req.Key)
		if key == "" || strings.ContainsAny(key, "\r\n") {
			return OpsResult{}, fmt.Errorf("%w: key must be a single-line SSH public key", ErrInvalidRequest)
		}
		command = []string{"sh", "-c",
			`H=$(eval echo ~"$1"); install -d -m 700 "$H/.ssh" && cat >> "$H/.ssh/authorized_keys" && chmod 600 "$H/.ssh/authorized_keys"`,
			"fleet-inject-key", req.User,
		}
		stdin = key + "\n"
	default:
		return OpsResult{}, fmt.Errorf("%w: unknown action %q", ErrInvalidRequest, req.Action)
	}

	return s.runGuestCommand(ctx, clusterName, name, command, stdin)
}

// runGuestCommand executes one non-interactive command inside an instance,
// piping stdin and capturing stdout/stderr.
func (s *Service) runGuestCommand(ctx context.Context, clusterName, name string, command []string, stdin string) (OpsResult, error) {
	client, err := s.client(clusterName)
	if err != nil {
		return OpsResult{}, err
	}

	var stdout, stderr strings.Builder
	args := &incus.InstanceExecArgs{
		Stdin:    strings.NewReader(stdin),
		Stdout:   &stdout,
		Stderr:   &stderr,
		DataDone: make(chan bool),
	}

	post := api.InstanceExecPost{Command: command, WaitForWS: true}
	op, err := client.ExecInstance(name, post, args)
	if err != nil {
		return OpsResult{}, fmt.Errorf("instances: exec %s:%s: %w", clusterName, name, err)
	}

	ctx, cancel := context.WithTimeout(ctx, opsTimeout)
	defer cancel()

	if waitErr := op.WaitContext(ctx); waitErr != nil {
		return OpsResult{}, fmt.Errorf("instances: exec %s:%s: %w", clusterName, name, waitErr)
	}

	// The io pumps close DataDone once all streams drained; bound the wait
	// so a misbehaving daemon cannot hang the request.
	select {
	case <-args.DataDone:
	case <-time.After(5 * time.Second):
	case <-ctx.Done():
		return OpsResult{}, fmt.Errorf("instances: exec %s:%s: %w", clusterName, name, ctx.Err())
	}

	result := OpsResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if code, ok := op.Get().Metadata["return"].(float64); ok {
		result.ExitCode = int(code)
	}

	return result, nil
}

// RebuildInstance recreates an instance root disk from another image. The
// instance must be stopped; existing data is discarded.
func (s *Service) RebuildInstance(ctx context.Context, clusterName, name, image string) (Detail, error) {
	image = strings.TrimSpace(image)
	if image == "" {
		return Detail{}, fmt.Errorf("%w: target image is required", ErrInvalidRequest)
	}

	client, err := s.client(clusterName)
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
	if instance.StatusCode != api.Stopped {
		return Detail{}, fmt.Errorf("%w: instance %s must be stopped before rebuilding", ErrInvalidRequest, name)
	}

	source := api.InstanceSource{Type: "image"}
	if fingerprintPattern.MatchString(image) {
		source.Fingerprint = image
	} else {
		source.Alias = image
	}

	op, err := client.RebuildInstance(name, api.InstanceRebuildPost{Source: source})
	if err != nil {
		return Detail{}, fmt.Errorf("instances: rebuild %s:%s: %w", clusterName, name, err)
	}

	ctx, cancel := context.WithTimeout(ctx, imageCopyWait)
	defer cancel()
	if err := waitOperation(ctx, op, "rebuild "+name); err != nil {
		return Detail{}, err
	}

	return s.Get(clusterName, name)
}

// RefreshImage re-downloads a cached image from its origin.
func (s *Service) RefreshImage(ctx context.Context, clusterName, fingerprint string) error {
	fingerprint = strings.TrimSpace(fingerprint)
	if fingerprint == "" {
		return fmt.Errorf("%w: image fingerprint is required", ErrInvalidRequest)
	}

	client, err := s.client(clusterName)
	if err != nil {
		return err
	}

	op, err := client.RefreshImage(fingerprint)
	if err != nil {
		return fmt.Errorf("instances: refresh image %s:%s: %w", clusterName, fingerprint, err)
	}

	ctx, cancel := context.WithTimeout(ctx, imageCopyWait)
	defer cancel()

	return waitOperation(ctx, op, "refresh image "+fingerprint[:min(len(fingerprint), 12)])
}

// CloneInstance copies an instance within its cluster under a new name.
func (s *Service) CloneInstance(ctx context.Context, clusterName, name string, req CloneRequest) (Detail, error) {
	target := strings.TrimSpace(req.Target)
	if target == "" {
		return Detail{}, fmt.Errorf("%w: target name is required", ErrInvalidRequest)
	}

	client, err := s.client(clusterName)
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

	start := false
	if req.Start != nil {
		start = *req.Start
	}

	// The daemon rejects copy requests that also request a start, so the
	// copy runs first and the clone is powered on separately. Volatile keys
	// (hardware addresses among them) must not travel with the copy or the
	// clone fights its source over the LAN address.
	config := copyConfig(instance.Config)
	for key := range config {
		if strings.HasPrefix(key, "volatile.") {
			delete(config, key)
		}
	}

	post := api.InstancesPost{
		Name: target,
		Type: api.InstanceType(instance.Type),
		InstancePut: api.InstancePut{
			Description: instance.Description,
			Profiles:    append([]string{}, instance.Profiles...),
			Config:      config,
			Devices:     copyDevices(instance.Devices),
		},
		Source: api.InstanceSource{Type: "copy", Source: name},
	}

	ctx, cancel := context.WithTimeout(ctx, createTimeout)
	defer cancel()

	op, err := client.CreateInstance(post)
	if err != nil {
		return Detail{}, fmt.Errorf("instances: clone %s:%s: %w", clusterName, name, err)
	}
	if err := waitOperation(ctx, op, "clone "+name); err != nil {
		return Detail{}, err
	}

	if start {
		state, stateErr := client.UpdateInstanceState(target, api.InstanceStatePut{Action: "start", Timeout: -1}, "")
		if stateErr == nil {
			if waitErr := waitOperation(ctx, state, "start "+target); waitErr != nil {
				return Detail{}, waitErr
			}
		}
	}

	return s.Get(clusterName, target)
}
