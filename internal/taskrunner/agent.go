package taskrunner

import (
	"context"
	"errors"
	"fmt"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/mickeyzzc/incus-fleet/internal/agentpb"
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

// AgentNetworkTask is the task type for asynchronous agent network changes.
const AgentNetworkTask = "agent.network"

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

		agentID := task.AgentID
		if agentID == "" {
			agent, err := lookup.GetAgentByMember(ctx, task.Cluster, task.Member)
			if err != nil {
				return "", fmt.Errorf("taskrunner: resolve agent: %w", err)
			}
			if agent.Status != store.AgentOnline {
				return "", fmt.Errorf("taskrunner: agent for %s/%s is %s", task.Cluster, task.Member, agent.Status)
			}
			agentID = agent.ID
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
