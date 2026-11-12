package assets

import (
	"context"
	"fmt"
	"strings"

	"github.com/mickeyzzc/incus-fleet/internal/instances"
)

// Target is one instance the batch should act on.
type Target struct {
	Cluster  string `json:"cluster"`
	Instance string `json:"instance"`
	User     string `json:"user,omitempty"`
}

// BatchItem reports the per-instance outcome of a batch run.
type BatchItem struct {
	Cluster  string `json:"cluster"`
	Instance string `json:"instance"`
	OK       bool   `json:"ok"`
	ExitCode int    `json:"exit_code,omitempty"`
	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
	Error    string `json:"error,omitempty"`
}

// normalizeTargets trims and rejects empty entries.
func normalizeTargets(targets []Target) ([]Target, error) {
	var clean []Target
	for _, target := range targets {
		target.Cluster = strings.TrimSpace(target.Cluster)
		target.Instance = strings.TrimSpace(target.Instance)
		target.User = strings.TrimSpace(target.User)
		if target.Cluster == "" || target.Instance == "" {
			return nil, fmt.Errorf("%w: every target needs a cluster and an instance", instances.ErrInvalidRequest)
		}

		clean = append(clean, target)
	}
	if len(clean) == 0 {
		return nil, fmt.Errorf("%w: at least one target instance is required", instances.ErrInvalidRequest)
	}

	return clean, nil
}

// BatchInjectKeys appends the selected stored keys to the target account on
// every target instance. One failed instance does not stop the others.
func (s *Service) BatchInjectKeys(ctx context.Context, targets []Target, keyIDs []string) []BatchItem {
	clean, err := normalizeTargets(targets)
	if err != nil {
		return []BatchItem{{Error: err.Error()}}
	}
	if len(keyIDs) == 0 {
		return []BatchItem{{Error: "at least one stored key must be selected"}}
	}

	var material []string
	for _, id := range keyIDs {
		key, loadErr := s.store.GetKeystone(ctx, strings.TrimSpace(id))
		if loadErr != nil {
			return []BatchItem{{Error: fmt.Sprintf("load key %s: %v", id, loadErr)}}
		}
		material = append(material, strings.TrimSpace(key.PublicKey))
	}

	items := make([]BatchItem, 0, len(clean))
	for _, target := range clean {
		user := target.User
		if user == "" {
			user = "root"
		}

		result, runErr := s.instances.RunOps(ctx, target.Cluster, target.Instance, instances.OpsRequest{
			Action: "inject-key",
			User:   user,
			Key:    strings.Join(material, "\n"),
		})

		item := BatchItem{Cluster: target.Cluster, Instance: target.Instance}
		switch {
		case runErr != nil:
			item.Error = runErr.Error()
		case result.ExitCode != 0:
			item.ExitCode = result.ExitCode
			item.Error = fmt.Sprintf("exit code %d", result.ExitCode)
			item.Stderr = result.Stderr
		default:
			item.OK = true
			item.ExitCode = 0
			item.Stdout = result.Stdout
		}

		items = append(items, item)
	}

	return items
}

// BatchRunScript executes one stored script on every target instance.
func (s *Service) BatchRunScript(ctx context.Context, targets []Target, scriptID string) []BatchItem {
	clean, err := normalizeTargets(targets)
	if err != nil {
		return []BatchItem{{Error: err.Error()}}
	}

	script, loadErr := s.store.GetScript(ctx, strings.TrimSpace(scriptID))
	if loadErr != nil {
		return []BatchItem{{Error: fmt.Sprintf("load script %s: %v", scriptID, loadErr)}}
	}

	items := make([]BatchItem, 0, len(clean))
	for _, target := range clean {
		result, runErr := s.instances.RunOps(ctx, target.Cluster, target.Instance, instances.OpsRequest{
			Action: "run-script",
			Script: script.Body,
		})

		item := BatchItem{Cluster: target.Cluster, Instance: target.Instance}
		switch {
		case runErr != nil:
			item.Error = runErr.Error()
		case result.ExitCode != 0:
			item.ExitCode = result.ExitCode
			item.Error = fmt.Sprintf("exit code %d", result.ExitCode)
			item.Stdout = result.Stdout
			item.Stderr = result.Stderr
		default:
			item.OK = true
			item.ExitCode = 0
			item.Stdout = result.Stdout
		}

		items = append(items, item)
	}

	return items
}

// BatchPower starts, stops, or restarts every target instance.
func (s *Service) BatchPower(ctx context.Context, targets []Target, action string, force bool) []BatchItem {
	clean, err := normalizeTargets(targets)
	if err != nil {
		return []BatchItem{{Error: err.Error()}}
	}
	switch action {
	case "start", "stop", "restart":
	default:
		return []BatchItem{{Error: "action must be one of start, stop, restart"}}
	}

	items := make([]BatchItem, 0, len(clean))
	for _, target := range clean {
		item := BatchItem{Cluster: target.Cluster, Instance: target.Instance}
		if err := s.instances.Action(ctx, target.Cluster, target.Instance, instances.ActionRequest{
			Action: action,
			Force:  force,
		}); err != nil {
			item.Error = err.Error()
		} else {
			item.OK = true
		}

		items = append(items, item)
	}

	return items
}
