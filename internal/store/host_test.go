package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestNewID(t *testing.T) {
	first := NewID()
	second := NewID()
	if len(first) != 32 || len(second) != 32 {
		t.Fatalf("NewID() lengths = %d, %d", len(first), len(second))
	}
	if first == second {
		t.Fatal("NewID() returned the same value twice")
	}
}

func TestSchemaMigrations(t *testing.T) {
	s := openSQLite(t)

	var version int
	if err := s.db.QueryRowContext(context.Background(), `SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil {
		t.Fatalf("schema version: %v", err)
	}
	if want := len(schemaMigrations); version != want {
		t.Fatalf("schema version = %d, want %d", version, want)
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
	if want := len(schemaMigrations); err != nil || version != want {
		t.Fatalf("SchemaVersion() = %d, %v; want %d", version, err, want)
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

func TestClampLimit(t *testing.T) {
	cases := map[int]int{-1: 50, 0: 50, 1: 1, 50: 50, 200: 200, 201: 200}
	for input, want := range cases {
		if got := clampLimit(input); got != want {
			t.Fatalf("clampLimit(%d) = %d, want %d", input, got, want)
		}
	}
}

func TestHostStoreRejectsNil(t *testing.T) {
	ctx := context.Background()
	s := openSQLite(t)

	if err := s.CreateHostTask(ctx, nil); err == nil {
		t.Fatal("CreateHostTask(nil) expected error")
	}
	if err := s.UpdateHostTask(ctx, nil); err == nil {
		t.Fatal("UpdateHostTask(nil) expected error")
	}
	if err := s.AppendHostTaskEvent(ctx, nil); err == nil {
		t.Fatal("AppendHostTaskEvent(nil) expected error")
	}
	if err := s.RecordHostAudit(ctx, nil); err == nil {
		t.Fatal("RecordHostAudit(nil) expected error")
	}
	if err := s.UpsertAgent(ctx, nil); err == nil {
		t.Fatal("UpsertAgent(nil) expected error")
	}
}

func TestHostTaskFilterByMemberOnly(t *testing.T) {
	ctx := context.Background()
	s := openSQLite(t)
	now := time.Now().UTC()

	for _, task := range []*HostTask{
		{ID: "a", Cluster: "c1", Member: "node-a", Type: "x", State: HostTaskQueued, CreatedAt: now, UpdatedAt: now},
		{ID: "b", Cluster: "c2", Member: "node-a", Type: "x", State: HostTaskQueued, CreatedAt: now.Add(time.Second), UpdatedAt: now},
	} {
		if err := s.CreateHostTask(ctx, task); err != nil {
			t.Fatal(err)
		}
	}

	list, err := s.ListHostTasks(ctx, "", "node-a", 0)
	if err != nil || len(list) != 2 {
		t.Fatalf("ListHostTasks(member only) = %#v, %v", list, err)
	}
}

func TestHostAuditFilterByCluster(t *testing.T) {
	ctx := context.Background()
	s := openSQLite(t)

	for _, entry := range []*HostAudit{
		{ID: "a", At: time.Now().UTC(), Backend: "local", Cluster: "c1", Member: "node-a", Action: "x", Result: "succeeded"},
		{ID: "b", At: time.Now().UTC().Add(time.Second), Backend: "ssh", Cluster: "c2", Member: "node-a", Action: "x", Result: "succeeded"},
	} {
		if err := s.RecordHostAudit(ctx, entry); err != nil {
			t.Fatal(err)
		}
	}

	list, err := s.ListHostAudit(ctx, "c1", "", 0)
	if err != nil || len(list) != 1 || list[0].ID != "a" {
		t.Fatalf("ListHostAudit(cluster only) = %#v, %v", list, err)
	}
}

func TestEnrollmentTokenLifecycle(t *testing.T) {
	ctx := context.Background()
	s := openSQLite(t)
	now := time.Now().UTC()

	token := &EnrollmentToken{
		ID:        "token-1",
		TokenHash: "hash-1",
		Cluster:   "c1",
		Member:    "node-a",
		ExpiresAt: now.Add(15 * time.Minute),
		CreatedBy: "admin",
		CreatedAt: now,
	}
	if err := s.CreateEnrollmentToken(ctx, token); err != nil {
		t.Fatalf("CreateEnrollmentToken() error = %v", err)
	}
	if err := s.CreateEnrollmentToken(ctx, token); err == nil {
		t.Fatal("duplicate token hash expected error")
	}

	if _, err := s.ConsumeEnrollmentToken(ctx, "hash-1", "c2", "node-a", now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Consume(wrong cluster) error = %v", err)
	}
	got, err := s.ConsumeEnrollmentToken(ctx, "hash-1", "c1", "node-a", now)
	if err != nil {
		t.Fatalf("Consume() error = %v", err)
	}
	if got.UsedAt.IsZero() {
		t.Fatal("Consume() did not mark used_at")
	}
	if _, err := s.ConsumeEnrollmentToken(ctx, "hash-1", "c1", "node-a", now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Consume(reused) error = %v", err)
	}

	expired := &EnrollmentToken{
		ID:        "token-2",
		TokenHash: "hash-2",
		Cluster:   "c1",
		Member:    "node-b",
		ExpiresAt: now.Add(-time.Minute),
		CreatedAt: now,
	}
	if err := s.CreateEnrollmentToken(ctx, expired); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConsumeEnrollmentToken(ctx, "hash-2", "c1", "node-b", now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Consume(expired) error = %v", err)
	}
}

func TestGetAgentByMember(t *testing.T) {
	ctx := context.Background()
	s := openSQLite(t)
	now := time.Now().UTC()
	agent := &Agent{
		ID: "agent-1", Cluster: "c1", Member: "node-a", Status: AgentOnline,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.UpsertAgent(ctx, agent); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetAgentByMember(ctx, "c1", "node-a")
	if err != nil || got.ID != "agent-1" {
		t.Fatalf("GetAgentByMember() = %#v, %v", got, err)
	}
	if _, err := s.GetAgentByMember(ctx, "c1", "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetAgentByMember(missing) error = %v", err)
	}
}

func TestSchemaVersionAfterClose(t *testing.T) {
	s := openSQLite(t)
	_ = s.Close()
	if _, err := s.SchemaVersion(context.Background()); err == nil {
		t.Fatal("SchemaVersion() expected error after close")
	}
}

func TestMigrationFailureLeavesVersionUnchanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{migrationsSchema, assetSchema(), clusterSchema, hostSchema} {
		if _, err := legacy.Exec(statement); err != nil {
			_ = legacy.Close()
			t.Fatal(err)
		}
	}
	if _, err := legacy.Exec(`INSERT INTO enrollment_tokens
		(id, token_hash, cluster, member, expires_at, used_at, created_by, created_at)
		VALUES ('a', 'same', 'c1', 'n1', 1, 0, '', 1), ('b', 'same', 'c1', 'n2', 1, 0, '', 1)`); err != nil {
		_ = legacy.Close()
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := Open(context.Background(), "sqlite", path); err == nil {
		t.Fatal("Open() expected migration failure")
	}
}

func TestHostTaskLeases(t *testing.T) {
	ctx := context.Background()
	s := openSQLite(t)
	now := time.Now().UTC()
	task := &HostTask{ID: "task-1", Cluster: "c1", Member: "node-a", Type: "x",
		State: HostTaskQueued, CreatedAt: now, UpdatedAt: now}
	if err := s.CreateHostTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	claimed, err := s.ClaimHostTask(ctx, "worker-1", time.Second)
	if err != nil || claimed.ID != "task-1" || claimed.LeaseOwner != "worker-1" || claimed.State != HostTaskDispatched {
		t.Fatalf("ClaimHostTask() = %#v, %v", claimed, err)
	}
	if _, err := s.ClaimHostTask(ctx, "worker-2", time.Second); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second claim error = %v", err)
	}
	if err := s.RenewHostTaskLease(ctx, "task-1", "worker-2", time.Now().Add(time.Minute)); !errors.Is(err, ErrConflict) {
		t.Fatalf("renew wrong owner error = %v", err)
	}
	if err := s.RenewHostTaskLease(ctx, "task-1", "worker-1", time.Now().Add(time.Minute)); err != nil {
		t.Fatalf("renew error = %v", err)
	}
	if err := s.ReleaseHostTask(ctx, "task-1", "worker-2"); !errors.Is(err, ErrConflict) {
		t.Fatalf("release wrong owner error = %v", err)
	}
	if err := s.ReleaseHostTask(ctx, "task-1", "worker-1"); err != nil {
		t.Fatal(err)
	}
	reclaimed, err := s.ClaimHostTask(ctx, "worker-2", time.Second)
	if err != nil || reclaimed.LeaseOwner != "worker-2" {
		t.Fatalf("reclaim = %#v, %v", reclaimed, err)
	}
	if err := s.ReleaseHostTask(ctx, "task-1", "worker-2"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ClaimHostTask(ctx, "worker-3", 10*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if _, err := s.ClaimHostTask(ctx, "worker-4", time.Second); err != nil {
		t.Fatalf("expired lease not claimable: %v", err)
	}
}

func TestHostTaskClaimIsAtomic(t *testing.T) {
	ctx := context.Background()
	s := openSQLite(t)
	now := time.Now().UTC()
	if err := s.CreateHostTask(ctx, &HostTask{ID: "task-race", Cluster: "c1", Member: "node-a",
		Type: "x", State: HostTaskQueued, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}

	// Two workers race for a single queued task: exactly one must win. This
	// guards the single-statement claim, which is the only form that stays
	// atomic on rqlite (its database/sql transactions are no-ops).
	const workers = 8
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		winners []string
	)
	for i := 0; i < workers; i++ {
		worker := fmt.Sprintf("worker-%d", i)
		wg.Add(1)
		go func() {
			defer wg.Done()
			task, err := s.ClaimHostTask(ctx, worker, time.Minute)
			if err != nil {
				if !errors.Is(err, ErrNotFound) {
					t.Errorf("claim %s: unexpected error %v", worker, err)
				}
				return
			}
			mu.Lock()
			winners = append(winners, task.LeaseOwner)
			mu.Unlock()
		}()
	}
	wg.Wait()

	if len(winners) != 1 {
		t.Fatalf("winners = %v, want exactly 1", winners)
	}
}

func TestAuditExportAndRevokeAgent(t *testing.T) {
	ctx := context.Background()
	s := openSQLite(t)
	now := time.Now().UTC()
	if err := s.RecordHostAudit(ctx, &HostAudit{ID: "a1", At: now, Backend: "local", Cluster: "c1", Member: "n1", Action: "x", Result: "succeeded"}); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordHostAudit(ctx, &HostAudit{ID: "a2", At: now, Backend: "ssh", Cluster: "c2", Member: "n2", Action: "y", Result: "failed"}); err != nil {
		t.Fatal(err)
	}
	all, err := s.ExportHostAudit(ctx, "", "")
	if err != nil || len(all) != 2 {
		t.Fatalf("ExportHostAudit() = %#v, %v", all, err)
	}
	one, err := s.ExportHostAudit(ctx, "c1", "")
	if err != nil || len(one) != 1 || one[0].ID != "a1" {
		t.Fatalf("ExportHostAudit(c1) = %#v, %v", one, err)
	}
	agent := &Agent{ID: "agent-1", Cluster: "c1", Member: "n1", Status: AgentOnline, CreatedAt: now, UpdatedAt: now}
	if err := s.UpsertAgent(ctx, agent); err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeAgent(ctx, "agent-1"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetAgent(ctx, "agent-1")
	if got.Status != AgentRevoked {
		t.Fatalf("status = %q", got.Status)
	}
	if err := s.RevokeAgent(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("RevokeAgent(missing) error = %v", err)
	}
}

func TestClaimEmptyAndRenewMissing(t *testing.T) {
	ctx := context.Background()
	s := openSQLite(t)
	if _, err := s.ClaimHostTask(ctx, "worker", time.Second); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Claim(empty) error = %v", err)
	}
	if err := s.RenewHostTaskLease(ctx, "missing", "worker", time.Now()); !errors.Is(err, ErrConflict) {
		t.Fatalf("Renew(missing) error = %v", err)
	}
	if err := s.ReleaseHostTask(ctx, "missing", "worker"); !errors.Is(err, ErrConflict) {
		t.Fatalf("Release(missing) error = %v", err)
	}
}

func TestClaimDefaultLeaseAndAuditMemberFilter(t *testing.T) {
	ctx := context.Background()
	s := openSQLite(t)
	now := time.Now().UTC()
	task := &HostTask{ID: "task-default", Cluster: "c1", Member: "n1", Type: "x", State: HostTaskQueued, CreatedAt: now, UpdatedAt: now}
	if err := s.CreateHostTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ClaimHostTask(ctx, "worker", 0); err != nil {
		t.Fatalf("ClaimHostTask(default lease) error = %v", err)
	}
	if err := s.RecordHostAudit(ctx, &HostAudit{ID: "a", At: now, Backend: "local", Cluster: "c1", Member: "n1", Action: "x", Result: "succeeded"}); err != nil {
		t.Fatal(err)
	}
	if got, err := s.ExportHostAudit(ctx, "", "n1"); err != nil || len(got) != 1 {
		t.Fatalf("ExportHostAudit(member) = %#v, %v", got, err)
	}
}

// TestApplyMigrationIsReRunnable reproduces the crash-between-ALTER-and-version
// case: the column already exists but the version row is missing. Re-applying
// must succeed instead of failing forever on "duplicate column name".
func TestApplyMigrationIsReRunnable(t *testing.T) {
	ctx := context.Background()
	s := openSQLite(t)

	// Simulate a partially applied v6: drop the version row and the index but
	// leave the owner column in place.
	if _, err := s.db.ExecContext(ctx, `DELETE FROM schema_migrations WHERE version = 6`); err != nil {
		t.Fatalf("delete version: %v", err)
	}
	if _, err := s.db.ExecContext(ctx, `DROP INDEX IF EXISTS idx_migrations_owner`); err != nil {
		t.Fatalf("drop index: %v", err)
	}

	var target schemaMigration
	for _, migration := range schemaMigrations {
		if migration.version == 6 {
			target = migration
		}
	}
	if target.version == 0 {
		t.Fatal("version 6 migration not found")
	}

	if err := s.applyMigration(ctx, target); err != nil {
		t.Fatalf("applyMigration() on partially applied schema = %v", err)
	}

	version, err := s.SchemaVersion(ctx)
	if want := len(schemaMigrations); err != nil || version != want {
		t.Fatalf("SchemaVersion() = %d, %v; want %d", version, err, want)
	}
}

// TestApplyMigrationTwiceIsIdempotent covers two fleet instances starting at
// once: both run the statements, and the loser must not error.
func TestApplyMigrationTwiceIsIdempotent(t *testing.T) {
	ctx := context.Background()
	s := openSQLite(t)

	var target schemaMigration
	for _, migration := range schemaMigrations {
		if migration.version == 6 {
			target = migration
		}
	}

	if _, err := s.db.ExecContext(ctx, `DELETE FROM schema_migrations WHERE version = 6`); err != nil {
		t.Fatalf("delete version: %v", err)
	}

	for i := 0; i < 2; i++ {
		if err := s.applyMigration(ctx, target); err != nil {
			t.Fatalf("applyMigration() pass %d = %v", i+1, err)
		}
	}
}

func TestColumnExists(t *testing.T) {
	ctx := context.Background()
	s := openSQLite(t)

	exists, err := s.columnExists(ctx, "migrations", "owner")
	if err != nil || !exists {
		t.Fatalf("columnExists(migrations.owner) = %v, %v; want true", exists, err)
	}

	exists, err = s.columnExists(ctx, "migrations", "no_such_column")
	if err != nil || exists {
		t.Fatalf("columnExists(migrations.no_such_column) = %v, %v; want false", exists, err)
	}
}

func TestIsDuplicateColumn(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{errors.New("some other failure"), false},
		{errors.New("duplicate column name: lease_owner"), true},
		{errors.New("column already exists"), true},
	}

	for _, tc := range cases {
		if got := isDuplicateColumn(tc.err); got != tc.want {
			t.Errorf("isDuplicateColumn(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}

func TestApplyMigrationStatementError(t *testing.T) {
	ctx := context.Background()
	s := openSQLite(t)

	// A statement that cannot run must surface as an error.
	broken := schemaMigration{
		version:    99,
		statements: []string{`THIS IS NOT SQL`},
	}
	if err := s.applyMigration(ctx, broken); err == nil {
		t.Fatal("applyMigration() accepted invalid SQL")
	}

	// The version must not be recorded, so a retry still sees the migration as
	// pending rather than silently skipping an incomplete schema.
	version, err := s.SchemaVersion(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if version >= 99 {
		t.Fatalf("failed migration recorded version %d", version)
	}
}

func TestColumnExistsUnknownTable(t *testing.T) {
	ctx := context.Background()
	s := openSQLite(t)

	// PRAGMA table_info on an unknown table yields no rows rather than an error,
	// so the probe reports "missing" and the caller attempts the ALTER.
	exists, err := s.columnExists(ctx, "no_such_table", "x")
	if err != nil {
		t.Fatalf("columnExists() error = %v", err)
	}
	if exists {
		t.Fatal("columnExists() reported a column on a missing table")
	}
}

// TestHostTaskNilAndMissingGuards covers the defensive branches on the host
// task and audit accessors.
func TestHostTaskNilAndMissingGuards(t *testing.T) {
	ctx := context.Background()
	s := openSQLite(t)

	if err := s.CreateHostTask(ctx, nil); err == nil {
		t.Fatal("CreateHostTask(nil) should fail")
	}
	if err := s.UpdateHostTask(ctx, nil); err == nil {
		t.Fatal("UpdateHostTask(nil) should fail")
	}
	if err := s.UpdateHostTask(ctx, &HostTask{ID: "missing"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateHostTask(missing) = %v", err)
	}
	if _, err := s.GetHostTask(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetHostTask(missing) = %v", err)
	}
	if _, err := s.ClaimHostTask(ctx, "owner", time.Minute); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ClaimHostTask(empty queue) = %v", err)
	}
	if err := s.AppendHostTaskEvent(ctx, nil); err == nil {
		t.Fatal("AppendHostTaskEvent(nil) should fail")
	}
	if err := s.RecordHostAudit(ctx, nil); err == nil {
		t.Fatal("RecordHostAudit(nil) should fail")
	}
}
