package taskrunner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/mickeyzzc/incus-fleet/internal/agentpb"
	"github.com/mickeyzzc/incus-fleet/internal/bootstrap"
	"github.com/mickeyzzc/incus-fleet/internal/store"
)

// AgentDispatcher is the agent task transport.
type AgentDispatcher interface {
	Dispatch(ctx context.Context, agentID string, typ agentpb.TaskType, payload []byte, timeout time.Duration) (*agentpb.TaskResult, error)
}

// AgentLookup resolves an enrolled agent for a task member.
type AgentLookup interface {
	GetAgentByMember(ctx context.Context, cluster, member string) (*store.Agent, error)
}

// Task types executed by the background dispatcher.
const (
	AgentNetworkTask   = "agent.network"
	AgentBootstrapTask = "agent.bootstrap"
	AgentSnapshotTask  = "agent.snapshot"
)

// AgentNetworkHandler executes one queued NodenetApplySpec through an agent.
func AgentNetworkHandler(dispatcher AgentDispatcher, lookup AgentLookup) Handler {
	return func(ctx context.Context, task *store.HostTask) (string, error) {
		if dispatcher == nil {
			return "", errors.New("taskrunner: agent gateway is not configured")
		}
		var spec agentpb.NodenetApplySpec
		if task.Plan != "" {
			if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal([]byte(task.Plan), &spec); err != nil {
				return "", fmt.Errorf("taskrunner: invalid network plan: %w", err)
			}
		}
		if spec.Action == "" {
			return "", errors.New("taskrunner: network plan has no action")
		}
		spec.TaskId = task.ID

		agentID, err := resolveAgentID(ctx, task, lookup)
		if err != nil {
			return "", err
		}

		payload, err := proto.Marshal(&spec)
		if err != nil {
			return "", err
		}
		result, err := dispatcher.Dispatch(ctx, agentID, agentpb.TaskType_TASK_TYPE_NODENET_APPLY, payload, 30*time.Second)
		if err != nil {
			return "", err
		}
		if result.State != agentpb.TaskState_TASK_STATE_SUCCEEDED {
			message := result.Error
			if message == "" {
				message = "agent task failed"
			}
			return "", errors.New(message)
		}
		var applied agentpb.NodenetApplyResult
		if err := proto.Unmarshal(result.Payload, &applied); err != nil {
			return "", fmt.Errorf("taskrunner: decode agent result: %w", err)
		}
		if !applied.Ok {
			return "", errors.New(applied.Error)
		}

		return applied.Action + " " + applied.Persistence, nil
	}
}

// AgentBootstrapHandler dispatches a pre-signed bootstrap spec. Signing stays
// with the admin/automation that created the plan.
func AgentBootstrapHandler(dispatcher AgentDispatcher, lookup AgentLookup) Handler {
	return func(ctx context.Context, task *store.HostTask) (string, error) {
		if dispatcher == nil {
			return "", errors.New("taskrunner: agent gateway is not configured")
		}
		var spec bootstrap.Spec
		if err := json.Unmarshal([]byte(task.Plan), &spec); err != nil {
			return "", fmt.Errorf("taskrunner: invalid bootstrap plan: %w", err)
		}
		if spec.Signature == "" || spec.TaskID == "" {
			return "", errors.New("taskrunner: bootstrap plan must be signed and include task_id")
		}
		agentID, err := resolveAgentID(ctx, task, lookup)
		if err != nil {
			return "", err
		}
		result, err := dispatcher.Dispatch(ctx, agentID, agentpb.TaskType_TASK_TYPE_INCUS_BOOTSTRAP, []byte(task.Plan), 10*time.Minute)
		if err != nil {
			return "", err
		}
		if result.State != agentpb.TaskState_TASK_STATE_SUCCEEDED {
			return "", errors.New(result.Error)
		}
		var applied agentpb.IncusBootstrapResult
		if err := proto.Unmarshal(result.Payload, &applied); err != nil {
			return "", fmt.Errorf("taskrunner: decode bootstrap result: %w", err)
		}
		if !applied.Ok {
			return "", errors.New(applied.Message)
		}
		return fmt.Sprintf("bootstrap %s: %d steps", applied.Mode, len(applied.Steps)), nil
	}
}

// AgentSnapshotHandler dispatches the read-only network snapshot and reports
// its size. It is useful for connectivity/latency probes from the dispatcher.
func AgentSnapshotHandler(dispatcher AgentDispatcher, lookup AgentLookup) Handler {
	return func(ctx context.Context, task *store.HostTask) (string, error) {
		if dispatcher == nil {
			return "", errors.New("taskrunner: agent gateway is not configured")
		}
		agentID, err := resolveAgentID(ctx, task, lookup)
		if err != nil {
			return "", err
		}
		result, err := dispatcher.Dispatch(ctx, agentID, agentpb.TaskType_TASK_TYPE_NODENET_SNAPSHOT, nil, 15*time.Second)
		if err != nil {
			return "", err
		}
		if result.State != agentpb.TaskState_TASK_STATE_SUCCEEDED {
			return "", errors.New(result.Error)
		}
		var snapshot agentpb.NodenetSnapshot
		if err := proto.Unmarshal(result.Payload, &snapshot); err != nil {
			return "", fmt.Errorf("taskrunner: decode snapshot: %w", err)
		}
		return fmt.Sprintf("snapshot %s: %d nics, %d routes", snapshot.Host, len(snapshot.Nics), len(snapshot.Routes)), nil
	}
}

func resolveAgentID(ctx context.Context, task *store.HostTask, lookup AgentLookup) (string, error) {
	if task.AgentID != "" {
		return task.AgentID, nil
	}
	if lookup == nil {
		return "", errors.New("taskrunner: agent lookup is not configured")
	}
	agent, err := lookup.GetAgentByMember(ctx, task.Cluster, task.Member)
	if err != nil {
		return "", fmt.Errorf("taskrunner: resolve agent: %w", err)
	}
	if agent.Status != store.AgentOnline {
		return "", fmt.Errorf("taskrunner: agent for %s/%s is %s", task.Cluster, task.Member, agent.Status)
	}
	return agent.ID, nil
}
