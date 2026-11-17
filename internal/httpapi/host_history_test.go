package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/mickeyzzc/incus-fleet/internal/store"
	"github.com/mickeyzzc/incus-fleet/internal/testutil/incusfake"
)

func TestNodeOperationsRecordTasksAndAudit(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	cookie := f.login(t)

	rec := f.request(t, http.MethodPost, "/api/v1/clusters/c1/members/test-node/node/networks/eth0/state",
		map[string]bool{"up": false}, cookie, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("state status = %d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get(headerBackend); got != "local" {
		t.Fatalf("backend header = %q", got)
	}
	taskID := rec.Header().Get(headerTask)
	if taskID == "" {
		t.Fatal("task header is empty")
	}

	tasks, err := f.store.ListHostTasks(context.Background(), "c1", "test-node", 10)
	if err != nil {
		t.Fatalf("ListHostTasks() error = %v", err)
	}
	if len(tasks) != 1 || tasks[0].ID != taskID || tasks[0].State != store.HostTaskSucceeded {
		t.Fatalf("tasks = %#v", tasks)
	}
	events, err := f.store.ListHostTaskEvents(context.Background(), taskID)
	if err != nil {
		t.Fatalf("ListHostTaskEvents() error = %v", err)
	}
	if len(events) != 2 || events[0].State != store.HostTaskRunning || events[1].State != store.HostTaskSucceeded {
		t.Fatalf("events = %#v", events)
	}

	audit, err := f.store.ListHostAudit(context.Background(), "c1", "test-node", 10)
	if err != nil {
		t.Fatalf("ListHostAudit() error = %v", err)
	}
	if len(audit) != 1 || audit[0].Action != actionNodeNICState || audit[0].Result != "succeeded" ||
		audit[0].Backend != "local" || audit[0].TaskID != taskID {
		t.Fatalf("audit = %#v", audit)
	}
}

func TestNodeOperationFailureRecordsTask(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	cookie := f.login(t)
	f.nodeRunner.failOn = "ip route add"

	rec := f.request(t, http.MethodPost, "/api/v1/clusters/c1/members/test-node/node/routes", map[string]string{
		"destination": "10.0.1.0/24",
		"gateway":     "192.0.2.1",
		"device":      "eth0",
	}, cookie, "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("route status = %d body=%s", rec.Code, rec.Body.String())
	}

	tasks, err := f.store.ListHostTasks(context.Background(), "c1", "test-node", 10)
	if err != nil {
		t.Fatalf("ListHostTasks() error = %v", err)
	}
	if len(tasks) != 1 || tasks[0].State != store.HostTaskFailed || tasks[0].Error == "" {
		t.Fatalf("tasks = %#v", tasks)
	}
	audit, err := f.store.ListHostAudit(context.Background(), "c1", "test-node", 10)
	if err != nil {
		t.Fatalf("ListHostAudit() error = %v", err)
	}
	if len(audit) != 1 || audit[0].Result != "failed" {
		t.Fatalf("audit = %#v", audit)
	}
}

func TestWiFiTaskDoesNotStorePassword(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	cookie := f.login(t)

	rec := f.request(t, http.MethodPost, "/api/v1/clusters/c1/members/test-node/node/networks/wlan0/wifi", map[string]string{
		"ssid":     "Office",
		"password": "super-secret",
	}, cookie, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("wifi status = %d body=%s", rec.Code, rec.Body.String())
	}

	taskID := rec.Header().Get(headerTask)
	task, err := f.store.GetHostTask(context.Background(), taskID)
	if err != nil {
		t.Fatalf("GetHostTask() error = %v", err)
	}
	if strings.Contains(task.Plan, "super-secret") {
		t.Fatalf("task plan leaked password: %s", task.Plan)
	}
	events, err := f.store.ListHostTaskEvents(context.Background(), taskID)
	if err != nil {
		t.Fatalf("ListHostTaskEvents() error = %v", err)
	}
	for _, event := range events {
		if strings.Contains(event.Data, "super-secret") {
			t.Fatalf("event leaked password: %s", event.Data)
		}
	}
}

func TestHostHistoryEndpoints(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	cookie := f.login(t)

	rec := f.request(t, http.MethodPost, "/api/v1/clusters/c1/members/test-node/node/routes", map[string]string{
		"destination": "10.0.1.0/24",
		"gateway":     "192.0.2.1",
		"device":      "eth0",
	}, cookie, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("route status = %d body=%s", rec.Code, rec.Body.String())
	}
	taskID := rec.Header().Get(headerTask)

	audit := f.request(t, http.MethodGet, "/api/v1/audit?cluster=c1&member=test-node&limit=10", nil, cookie, "")
	if audit.Code != http.StatusOK || !strings.Contains(audit.Body.String(), "node.route.add") {
		t.Fatalf("GET audit = %d body=%s", audit.Code, audit.Body.String())
	}

	tasks := f.request(t, http.MethodGet, "/api/v1/tasks?cluster=c1&member=test-node&limit=10", nil, cookie, "")
	if tasks.Code != http.StatusOK || !strings.Contains(tasks.Body.String(), taskID) {
		t.Fatalf("GET tasks = %d body=%s", tasks.Code, tasks.Body.String())
	}

	task := f.request(t, http.MethodGet, "/api/v1/tasks/"+taskID, nil, cookie, "")
	if task.Code != http.StatusOK || !strings.Contains(task.Body.String(), "events") {
		t.Fatalf("GET task = %d body=%s", task.Code, task.Body.String())
	}

	missing := f.request(t, http.MethodGet, "/api/v1/tasks/missing", nil, cookie, "")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing task status = %d body=%s", missing.Code, missing.Body.String())
	}

	badLimit := f.request(t, http.MethodGet, "/api/v1/audit?limit=500", nil, cookie, "")
	if badLimit.Code != http.StatusBadRequest {
		t.Fatalf("bad limit status = %d body=%s", badLimit.Code, badLimit.Body.String())
	}
}

func TestNodeSnapshotReportsBackend(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	cookie := f.login(t)

	rec := f.request(t, http.MethodGet, "/api/v1/clusters/c1/members/test-node/node", nil, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("snapshot status = %d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get(headerBackend); got != "local" {
		t.Fatalf("backend header = %q", got)
	}
	if !strings.Contains(rec.Body.String(), `"backend":"local"`) {
		t.Fatalf("snapshot body missing backend: %s", rec.Body.String())
	}
}

func TestHostHistoryDisabled(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	f.handler = New(Deps{Auth: f.auth, Version: "test"}).Handler()
	cookie := f.login(t)

	for _, path := range []string{"/api/v1/audit", "/api/v1/tasks", "/api/v1/tasks/missing"} {
		rec := f.request(t, http.MethodGet, path, nil, cookie, "")
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s status = %d body=%s", path, rec.Code, rec.Body.String())
		}
	}
}

func TestHostHistoryStoreErrors(t *testing.T) {
	f := newAPIFixture(t, &incusfake.Server{}, &incusfake.Server{})
	cookie := f.login(t)
	if err := f.store.Close(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/v1/audit", "/api/v1/tasks", "/api/v1/tasks/missing"} {
		rec := f.request(t, http.MethodGet, path, nil, cookie, "")
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("%s status = %d body=%s", path, rec.Code, rec.Body.String())
		}
	}
}
