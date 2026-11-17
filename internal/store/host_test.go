package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestSchemaMigrations(t *testing.T) {
	s := openSQLite(t)

	var version int
	if err := s.db.QueryRowContext(context.Background(), `SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil {
		t.Fatalf("schema version: %v", err)
	}
	if version != 2 {
		t.Fatalf("schema version = %d, want 2", version)
	}
}

func TestSchemaMigrationFromLegacyDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{migrationsSchema, migrationsIndex, assetSchema(), clusterSchema} {
		if _, err := legacy.Exec(statement); err != nil {
			_ = legacy.Close()
			t.Fatalf("legacy schema: %v", err)
		}
	}
	if _, err := legacy.Exec(`INSERT INTO migrations
		(id, source_cluster, target_cluster, instance, instance_type, mode, live, status, progress, error, created_at, updated_at)
		VALUES ('old', 'c1', 'c2', 'web', 'container', 'copy', 0, 'done', 'complete', '', 1, 1)`); err != nil {
		_ = legacy.Close()
		t.Fatalf("legacy data: %v", err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := Open(context.Background(), "sqlite", path)
	if err != nil {
		t.Fatalf("Open(legacy) error = %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	version, err := s.SchemaVersion(context.Background())
	if err != nil || version != 2 {
		t.Fatalf("SchemaVersion() = %d, %v", version, err)
	}
	old, err := s.GetMigration(context.Background(), "old")
	if err != nil || old.Status != "done" {
		t.Fatalf("legacy migration = %#v, %v", old, err)
	}
}

func TestHostTaskLifecycle(t *testing.T) {
	ctx := context.Background()
	s := openSQLite(t)
	now := time.Now().UTC()

	task := &HostTask{
		ID:             "task-1",
		AgentID:        "agent-1",
		Cluster:        "c1",
		Member:         "node-a",
		Type:           "nodenet.apply",
		State:          HostTaskQueued,
		IdempotencyKey: "idem-1",
		Plan:           `{"iface":"eth0"}`,
		PlanHash:       "hash-1",
		CreatedBy:      "admin",
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := s.CreateHostTask(ctx, task); err != nil {
		t.Fatalf("CreateHostTask() error = %v", err)
	}

	got, err := s.GetHostTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetHostTask() error = %v", err)
	}
	if got.State != HostTaskQueued || got.Cluster != "c1" || got.Member != "node-a" || !got.CreatedAt.Equal(now) {
		t.Fatalf("GetHostTask() = %#v", got)
	}

	task.State = HostTaskRunning
	task.Progress = "applying"
	task.Attempt = 1
	task.StartedAt = now.Add(time.Second)
	task.UpdatedAt = task.StartedAt
	if err := s.UpdateHostTask(ctx, task); err != nil {
		t.Fatalf("UpdateHostTask() error = %v", err)
	}

	got, err = s.GetHostTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetHostTask(updated) error = %v", err)
	}
	if got.State != HostTaskRunning || got.Progress != "applying" || got.Attempt != 1 || !got.StartedAt.Equal(task.StartedAt) {
		t.Fatalf("updated task = %#v", got)
	}

	task2 := &HostTask{
		ID:        "task-2",
		Cluster:   "c1",
		Member:    "node-b",
		Type:      "incus.bootstrap",
		State:     HostTaskQueued,
		CreatedAt: now.Add(time.Minute),
		UpdatedAt: now.Add(time.Minute),
	}
	if err := s.CreateHostTask(ctx, task2); err != nil {
		t.Fatalf("CreateHostTask(task2) error = %v", err)
	}

	list, err := s.ListHostTasks(ctx, "c1", "node-a", 10)
	if err != nil {
		t.Fatalf("ListHostTasks() error = %v", err)
	}
	if len(list) != 1 || list[0].ID != task.ID {
		t.Fatalf("ListHostTasks() = %#v", list)
	}

	all, err := s.ListHostTasks(ctx, "", "", 10)
	if err != nil {
		t.Fatalf("ListHostTasks(all) error = %v", err)
	}
	if len(all) != 2 || all[0].ID != task2.ID {
		t.Fatalf("ListHostTasks(all) = %#v", all)
	}

	if _, err := s.GetHostTask(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetHostTask(missing) error = %v", err)
	}

	missing := &HostTask{ID: "missing", State: HostTaskFailed, UpdatedAt: now}
	if err := s.UpdateHostTask(ctx, missing); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateHostTask(missing) error = %v", err)
	}
}

func TestHostTaskEvents(t *testing.T) {
	ctx := context.Background()
	s := openSQLite(t)
	now := time.Now().UTC()

	event := &HostTaskEvent{
		ID:      "event-1",
		TaskID:  "task-1",
		At:      now,
		State:   HostTaskRunning,
		Message: "started",
		Data:    `{"step":1}`,
	}
	if err := s.AppendHostTaskEvent(ctx, event); err != nil {
		t.Fatalf("AppendHostTaskEvent() error = %v", err)
	}

	events, err := s.ListHostTaskEvents(ctx, "task-1")
	if err != nil {
		t.Fatalf("ListHostTaskEvents() error = %v", err)
	}
	if len(events) != 1 || events[0].State != HostTaskRunning || events[0].Message != "started" {
		t.Fatalf("events = %#v", events)
	}
}

func TestHostAudit(t *testing.T) {
	ctx := context.Background()
	s := openSQLite(t)
	now := time.Now().UTC()

	entry := &HostAudit{
		ID:         "audit-1",
		At:         now,
		Actor:      "admin",
		SourceIP:   "192.0.2.20",
		Backend:    "ssh",
		Cluster:    "c1",
		Member:     "node-a",
		Action:     "node.route.add",
		TaskID:     "task-1",
		BeforeHash: "before",
		AfterHash:  "after",
		Result:     "succeeded",
		Detail:     `{"destination":"10.0.1.0/24"}`,
	}
	if err := s.RecordHostAudit(ctx, entry); err != nil {
		t.Fatalf("RecordHostAudit() error = %v", err)
	}

	list, err := s.ListHostAudit(ctx, "c1", "node-a", 10)
	if err != nil {
		t.Fatalf("ListHostAudit() error = %v", err)
	}
	if len(list) != 1 || list[0].Action != "node.route.add" || !list[0].At.Equal(now) {
		t.Fatalf("audit = %#v", list)
	}

	other, err := s.ListHostAudit(ctx, "c2", "", 10)
	if err != nil {
		t.Fatalf("ListHostAudit(c2) error = %v", err)
	}
	if len(other) != 0 {
		t.Fatalf("ListHostAudit(c2) = %#v", other)
	}
}

func TestAgentUpsertAndList(t *testing.T) {
	ctx := context.Background()
	s := openSQLite(t)
	now := time.Now().UTC()

	agent := &Agent{
		ID:              "agent-1",
		Cluster:         "c1",
		Member:          "node-a",
		Hostname:        "node-a",
		Status:          AgentPending,
		CertFingerprint: "fp-1",
		CertNotAfter:    now.Add(90 * 24 * time.Hour),
		Version:         "0.2.0",
		OS:              "debian",
		Kernel:          "6.12",
		Arch:            "amd64",
		Capabilities:    `["nodenet.snapshot"]`,
		Facts:           `{"network_manager":"systemd-networkd"}`,
		LastSeen:        now,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := s.UpsertAgent(ctx, agent); err != nil {
		t.Fatalf("UpsertAgent() error = %v", err)
	}

	got, err := s.GetAgent(ctx, agent.ID)
	if err != nil {
		t.Fatalf("GetAgent() error = %v", err)
	}
	if got.Status != AgentPending || got.Member != "node-a" || !got.CertNotAfter.Equal(agent.CertNotAfter) {
		t.Fatalf("agent = %#v", got)
	}

	agent.Status = AgentOnline
	agent.LastSeen = now.Add(time.Minute)
	agent.UpdatedAt = agent.LastSeen
	if err := s.UpsertAgent(ctx, agent); err != nil {
		t.Fatalf("UpsertAgent(update) error = %v", err)
	}

	list, err := s.ListAgents(ctx)
	if err != nil {
		t.Fatalf("ListAgents() error = %v", err)
	}
	if len(list) != 1 || list[0].Status != AgentOnline {
		t.Fatalf("agents = %#v", list)
	}

	if _, err := s.GetAgent(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetAgent(missing) error = %v", err)
	}
}
