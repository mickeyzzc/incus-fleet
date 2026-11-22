package taskrunner

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/mickeyzzc/incus-fleet/internal/agentpb"
	"github.com/mickeyzzc/incus-fleet/internal/bootstrap"
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
	var body []byte
	if typ == agentpb.TaskType_TASK_TYPE_NODENET_SNAPSHOT {
		body, _ = proto.Marshal(&agentpb.NodenetSnapshot{Host: "node-a"})
	} else {
		body, _ = proto.Marshal(&agentpb.NodenetApplyResult{Ok: true, Action: "link-state", Backend: "helper"})
	}
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

func TestAgentSnapshotHandler(t *testing.T) {
	dispatcher := &fakeDispatcher{}
	handler := AgentSnapshotHandler(dispatcher, fakeLookup{agent: &store.Agent{ID: "agent-1", Status: store.AgentOnline}})
	summary, err := handler(context.Background(), &store.HostTask{ID: "t", AgentID: "agent-1"})
	if err != nil {
		t.Fatal(err)
	}
	if summary == "" || dispatcher.typ != agentpb.TaskType_TASK_TYPE_NODENET_SNAPSHOT {
		t.Fatalf("summary=%q type=%v", summary, dispatcher.typ)
	}
}

func TestAgentBootstrapHandler(t *testing.T) {
	spec := bootstrap.Spec{
		Version: 1, Mode: "join", Cluster: "c1", Member: "node-a",
		Address: "192.0.2.10", Preseed: "cluster: {enabled: true}", TaskID: "b1",
		Nonce: "n", ExpiresAt: time.Now().Add(time.Hour).Unix(), Signature: "signed",
	}
	plan, _ := json.Marshal(spec)
	dispatcher := &fakeDispatcher{result: &agentpb.TaskResult{
		State: agentpb.TaskState_TASK_STATE_SUCCEEDED,
		Payload: func() []byte {
			data, _ := proto.Marshal(&agentpb.IncusBootstrapResult{Ok: true, Mode: "bootstrap", Steps: []string{"install"}})
			return data
		}(),
	}}
	handler := AgentBootstrapHandler(dispatcher, fakeLookup{agent: &store.Agent{ID: "agent-1", Status: store.AgentOnline}})
	summary, err := handler(context.Background(), &store.HostTask{ID: "t", AgentID: "agent-1", Plan: string(plan)})
	if err != nil {
		t.Fatal(err)
	}
	if summary == "" || dispatcher.typ != agentpb.TaskType_TASK_TYPE_INCUS_BOOTSTRAP {
		t.Fatalf("summary=%q type=%v", summary, dispatcher.typ)
	}
	if _, err := handler(context.Background(), &store.HostTask{ID: "t", AgentID: "agent-1", Plan: "{}"}); err == nil {
		t.Fatal("unsigned bootstrap plan accepted")
	}
}

func TestAgentBootstrapHandlerErrors(t *testing.T) {
	lookup := fakeLookup{agent: &store.Agent{ID: "agent-1", Status: store.AgentOnline}}
	unsigned, _ := json.Marshal(bootstrap.Spec{TaskID: "b1", Signature: ""})
	cases := []struct {
		name       string
		task       *store.HostTask
		dispatcher AgentDispatcher
		lookup     fakeLookup
	}{
		{"nil dispatcher", &store.HostTask{AgentID: "a", Plan: "{}"}, nil, lookup},
		{"bad plan", &store.HostTask{AgentID: "a", Plan: "not-json"}, &fakeDispatcher{}, lookup},
		{"unsigned", &store.HostTask{AgentID: "a", Plan: string(unsigned)}, &fakeDispatcher{}, lookup},
		{"lookup error", &store.HostTask{Plan: `{"task_id":"b1","signature":"s"}`}, &fakeDispatcher{}, fakeLookup{err: errors.New("no")}},
		{"offline", &store.HostTask{Plan: `{"task_id":"b1","signature":"s"}`}, &fakeDispatcher{}, fakeLookup{agent: &store.Agent{Status: store.AgentOffline}}},
		{"dispatch error", &store.HostTask{AgentID: "a", Plan: `{"task_id":"b1","signature":"s"}`}, &fakeDispatcher{err: errors.New("down")}, lookup},
		{"failed result", &store.HostTask{AgentID: "a", Plan: `{"task_id":"b1","signature":"s"}`},
			&fakeDispatcher{result: &agentpb.TaskResult{State: agentpb.TaskState_TASK_STATE_FAILED, Error: "no"}}, lookup},
		{"invalid payload", &store.HostTask{AgentID: "a", Plan: `{"task_id":"b1","signature":"s"}`},
			&fakeDispatcher{result: &agentpb.TaskResult{State: agentpb.TaskState_TASK_STATE_SUCCEEDED, Payload: []byte("bad")}}, lookup},
		{"ok false", &store.HostTask{AgentID: "a", Plan: `{"task_id":"b1","signature":"s"}`},
			&fakeDispatcher{result: &agentpb.TaskResult{State: agentpb.TaskState_TASK_STATE_SUCCEEDED, Payload: mustProto(t, &agentpb.IncusBootstrapResult{Ok: false, Message: "nope"})}}, lookup},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := AgentBootstrapHandler(tc.dispatcher, tc.lookup)(context.Background(), tc.task); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestAgentSnapshotHandlerErrors(t *testing.T) {
	lookup := fakeLookup{agent: &store.Agent{ID: "a", Status: store.AgentOnline}}
	cases := []struct {
		name       string
		task       *store.HostTask
		dispatcher AgentDispatcher
		lookup     fakeLookup
	}{
		{"nil dispatcher", &store.HostTask{AgentID: "a"}, nil, lookup},
		{"no agent", &store.HostTask{}, &fakeDispatcher{}, fakeLookup{agent: &store.Agent{Status: store.AgentOffline}}},
		{"dispatch error", &store.HostTask{AgentID: "a"}, &fakeDispatcher{err: errors.New("down")}, lookup},
		{"failed result", &store.HostTask{AgentID: "a"}, &fakeDispatcher{result: &agentpb.TaskResult{State: agentpb.TaskState_TASK_STATE_FAILED}}, lookup},
		{"invalid payload", &store.HostTask{AgentID: "a"}, &fakeDispatcher{result: &agentpb.TaskResult{State: agentpb.TaskState_TASK_STATE_SUCCEEDED, Payload: []byte("bad")}}, lookup},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := AgentSnapshotHandler(tc.dispatcher, tc.lookup)(context.Background(), tc.task); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func mustProto(t *testing.T, message proto.Message) []byte {
	t.Helper()
	data, err := proto.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
