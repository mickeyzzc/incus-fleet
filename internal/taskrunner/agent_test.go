package taskrunner

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/mickeyzzc/incus-fleet/internal/agentpb"
	"github.com/mickeyzzc/incus-fleet/internal/store"
)

type fakeDispatcher struct {
	typ     agentpb.TaskType
	payload []byte
	result  *agentpb.TaskResult
	err     error
}

func (f *fakeDispatcher) Dispatch(_ context.Context, _ string, typ agentpb.TaskType, payload []byte, _ time.Duration) (*agentpb.TaskResult, error) {
	f.typ = typ
	f.payload = payload
	if f.result != nil || f.err != nil {
		return f.result, f.err
	}
	body, _ := proto.Marshal(&agentpb.NodenetApplyResult{Ok: true, Action: "link-state", Backend: "helper"})
	return &agentpb.TaskResult{State: agentpb.TaskState_TASK_STATE_SUCCEEDED, Payload: body}, nil
}

type fakeLookup struct {
	agent *store.Agent
	err   error
}

func (f fakeLookup) GetAgentByMember(context.Context, string, string) (*store.Agent, error) {
	return f.agent, f.err
}

func TestAgentNetworkHandler(t *testing.T) {
	dispatcher := &fakeDispatcher{}
	handler := AgentNetworkHandler(dispatcher, fakeLookup{agent: &store.Agent{
		ID: "agent-1", Status: store.AgentOnline,
	}})
	task := &store.HostTask{
		ID: "task-1", Cluster: "c1", Member: "node-a", Type: AgentNetworkTask,
		Plan: `{"action":"link-state","interface":"eth0","up":false}`,
	}
	summary, err := handler(context.Background(), task)
	if err != nil {
		t.Fatal(err)
	}
	if summary != "link-state " {
		t.Fatalf("summary = %q", summary)
	}
	if dispatcher.typ != agentpb.TaskType_TASK_TYPE_NODENET_APPLY {
		t.Fatalf("task type = %v", dispatcher.typ)
	}
	var spec agentpb.NodenetApplySpec
	if err := proto.Unmarshal(dispatcher.payload, &spec); err != nil {
		t.Fatal(err)
	}
	if spec.Action != "link-state" || spec.Interface != "eth0" || spec.Up || spec.TaskId != "task-1" {
		t.Fatalf("spec = action=%q interface=%q up=%v task=%q", spec.Action, spec.Interface, spec.Up, spec.TaskId)
	}
}

func TestAgentNetworkHandlerErrors(t *testing.T) {
	task := &store.HostTask{ID: "task-1", Cluster: "c1", Member: "node-a", Type: AgentNetworkTask}
	cases := []struct {
		name       string
		dispatcher *fakeDispatcher
		lookup     fakeLookup
		plan       string
	}{
		{"nil dispatcher", nil, fakeLookup{}, ""},
		{"bad plan", &fakeDispatcher{}, fakeLookup{}, "{bad"},
		{"missing action", &fakeDispatcher{}, fakeLookup{}, "{}"},
		{"offline agent", &fakeDispatcher{}, fakeLookup{agent: &store.Agent{ID: "a", Status: store.AgentOffline}}, `{"action":"link-state"}`},
		{"lookup error", &fakeDispatcher{}, fakeLookup{err: errors.New("no agent")}, `{"action":"link-state"}`},
		{"dispatch error", &fakeDispatcher{err: errors.New("offline")}, fakeLookup{agent: &store.Agent{ID: "a", Status: store.AgentOnline}}, `{"action":"link-state"}`},
		{"failed result", &fakeDispatcher{result: &agentpb.TaskResult{State: agentpb.TaskState_TASK_STATE_FAILED, Error: "nope"}},
			fakeLookup{agent: &store.Agent{ID: "a", Status: store.AgentOnline}}, `{"action":"link-state"}`},
		{"invalid result", &fakeDispatcher{result: &agentpb.TaskResult{State: agentpb.TaskState_TASK_STATE_SUCCEEDED, Payload: []byte("bad")}},
			fakeLookup{agent: &store.Agent{ID: "a", Status: store.AgentOnline}}, `{"action":"link-state"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			task.Plan = tc.plan
			handler := AgentNetworkHandler(tc.dispatcher, tc.lookup)
			if _, err := handler(context.Background(), task); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}
