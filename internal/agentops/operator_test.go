package agentops

import (
	"context"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/mickeyzzc/incus-fleet/internal/agentpb"
	"github.com/mickeyzzc/incus-fleet/internal/nodenet"
	"github.com/mickeyzzc/incus-fleet/internal/store"
)

type fakeDispatcher struct {
	typ     agentpb.TaskType
	payload []byte
}

func (f *fakeDispatcher) Dispatch(_ context.Context, _ string, typ agentpb.TaskType, payload []byte, _ time.Duration) (*agentpb.TaskResult, error) {
	f.typ = typ
	f.payload = payload
	var data []byte
	if typ == agentpb.TaskType_TASK_TYPE_NODENET_SNAPSHOT {
		response, _ := proto.Marshal(&agentpb.NodenetSnapshot{
			Host:         "node-a",
			Nics:         []*agentpb.NIC{{Name: "eth0", State: "up", Kind: "ethernet"}},
			Routes:       []*agentpb.Route{{Destination: "default", Gateway: "192.0.2.1", Device: "eth0"}},
			Capabilities: []string{"nics", "routes"},
		})
		data = response
	} else {
		response, _ := proto.Marshal(&agentpb.NodenetApplyResult{Ok: true, Action: "apply", Backend: "helper"})
		data = response
	}
	return &agentpb.TaskResult{State: agentpb.TaskState_TASK_STATE_SUCCEEDED, Payload: data}, nil
}

type fakeLookup struct{ agent *store.Agent }

func (f fakeLookup) GetAgentByMember(context.Context, string, string) (*store.Agent, error) {
	return f.agent, nil
}

func TestResolverAndSnapshot(t *testing.T) {
	dispatcher := &fakeDispatcher{}
	resolver := New(dispatcher, fakeLookup{agent: &store.Agent{ID: "agent-1", Status: store.AgentOnline}})
	operator, ok := resolver.Resolve(context.Background(), "c1", "node-a")
	if !ok {
		t.Fatal("Resolve() did not find online agent")
	}
	snapshot, err := operator.Snapshot(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Backend != "agent" || snapshot.Host != "node-a" || len(snapshot.NICs) != 1 || len(snapshot.Routes) != 1 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if err := operator.SetNICState("eth0", false); err != nil {
		t.Fatal(err)
	}
	if dispatcher.typ != agentpb.TaskType_TASK_TYPE_NODENET_APPLY {
		t.Fatalf("task type = %v", dispatcher.typ)
	}
	var spec agentpb.NodenetApplySpec
	if err := proto.Unmarshal(dispatcher.payload, &spec); err != nil {
		t.Fatal(err)
	}
	if spec.Action != "link-state" || spec.Interface != "eth0" || spec.Up {
		t.Fatalf("spec action=%s interface=%s up=%v", spec.Action, spec.Interface, spec.Up)
	}
	if err := operator.AddRoute("10.0.1.0/24", "192.0.2.1", "eth0"); err != nil {
		t.Fatal(err)
	}
	if err := operator.DeleteRoute("10.0.1.0/24"); err != nil {
		t.Fatal(err)
	}
	if err := operator.ApplyAddress(context.Background(), "eth0", nodenet.AddressRequest{Address: "10.0.0.50/24", Persist: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := operator.ListRoutes(); err != nil {
		t.Fatal(err)
	}
	if _, err := operator.ListFirewall(); err != nil {
		t.Fatal(err)
	}
	if _, err := operator.ScanWiFi("wlan0"); err != nil {
		t.Fatal(err)
	}
	if err := operator.AddFirewallRule(nodenet.FirewallRequest{Port: 22, Proto: "tcp", Action: "accept"}); err != nil {
		t.Fatal(err)
	}
	if err := operator.DeleteFirewallRule("7"); err != nil {
		t.Fatal(err)
	}
	if err := operator.WifiConnect("wlan0", "Office", "secret"); err != nil {
		t.Fatal(err)
	}
}

func TestResolverOfflineAndConversion(t *testing.T) {
	resolver := New(&fakeDispatcher{}, fakeLookup{agent: &store.Agent{ID: "agent-1", Status: store.AgentOffline}})
	if _, ok := resolver.Resolve(context.Background(), "c1", "node-a"); ok {
		t.Fatal("offline agent resolved")
	}
	converted := convertSnapshot(&agentpb.NodenetSnapshot{
		Host:     "node-a",
		Nics:     []*agentpb.NIC{{Name: "wlan0", Wifi: &agentpb.WiFiLink{Connected: true, Ssid: "Office", Signal: -50}}},
		WifiScan: []*agentpb.WiFiNetwork{{Ssid: "Office", Signal: -40, Secure: true}},
	}, "wlan0")
	if converted.Backend != "agent" || converted.NICs[0].WiFi == nil ||
		converted.NICs[0].WiFi.SSID != "Office" || converted.WifiScan[0].SSID != "Office" {
		t.Fatalf("converted = %#v", converted)
	}
}

type failingDispatcher struct {
	payload []byte
	state   agentpb.TaskState
}

func (f failingDispatcher) Dispatch(context.Context, string, agentpb.TaskType, []byte, time.Duration) (*agentpb.TaskResult, error) {
	return &agentpb.TaskResult{State: f.state, Payload: f.payload, Error: "boom"}, nil
}

func TestOperatorFailures(t *testing.T) {
	operator := &Operator{dispatcher: failingDispatcher{state: agentpb.TaskState_TASK_STATE_FAILED}, timeout: time.Second}
	if err := operator.SetNICState("eth0", true); err == nil {
		t.Fatal("failed task expected error")
	}
	operator.dispatcher = failingDispatcher{state: agentpb.TaskState_TASK_STATE_SUCCEEDED, payload: []byte("bad")}
	if err := operator.SetNICState("eth0", true); err == nil {
		t.Fatal("invalid payload expected error")
	}
	if _, err := operator.Snapshot(context.Background(), ""); err == nil {
		t.Fatal("invalid snapshot expected error")
	}
	badResult, _ := proto.Marshal(&agentpb.NodenetApplyResult{Ok: false, Error: "nope"})
	operator.dispatcher = failingDispatcher{state: agentpb.TaskState_TASK_STATE_SUCCEEDED, payload: badResult}
	if err := operator.SetNICState("eth0", true); err == nil {
		t.Fatal("ok=false expected error")
	}
}

type errorLookup struct{}

func (errorLookup) GetAgentByMember(context.Context, string, string) (*store.Agent, error) {
	return nil, store.ErrNotFound
}

func TestResolverLookupError(t *testing.T) {
	if _, ok := New(&fakeDispatcher{}, errorLookup{}).Resolve(context.Background(), "c1", "n1"); ok {
		t.Fatal("lookup error resolved")
	}
}

func TestSnapshotBackedMethodsFail(t *testing.T) {
	operator := &Operator{dispatcher: failingDispatcher{state: agentpb.TaskState_TASK_STATE_FAILED}, timeout: time.Second}
	if _, err := operator.ScanWiFi("wlan0"); err == nil {
		t.Fatal("ScanWiFi expected error")
	}
	if _, err := operator.ListRoutes(); err == nil {
		t.Fatal("ListRoutes expected error")
	}
	if _, err := operator.ListFirewall(); err == nil {
		t.Fatal("ListFirewall expected error")
	}
}
