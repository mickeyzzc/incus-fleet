package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Host task states. A task that disconnects while running becomes unknown
// rather than failed: the host operation may have completed.
const (
	HostTaskQueued     = "queued"
	HostTaskDispatched = "dispatched"
	HostTaskAccepted   = "accepted"
	HostTaskRunning    = "running"
	HostTaskSucceeded  = "succeeded"
	HostTaskFailed     = "failed"
	HostTaskCanceled   = "canceled"
	HostTaskUnknown    = "unknown"
)

// Agent states.
const (
	AgentPending = "pending"
	AgentOnline  = "online"
	AgentOffline = "offline"
	AgentRevoked = "revoked"
)

// HostTask is one durable host operation.
type HostTask struct {
	ID             string    `json:"id"`
	AgentID        string    `json:"agent_id,omitempty"`
	Cluster        string    `json:"cluster"`
	Member         string    `json:"member"`
	Type           string    `json:"type"`
	State          string    `json:"state"`
	IdempotencyKey string    `json:"idempotency_key,omitempty"`
	Plan           string    `json:"plan,omitempty"`
	PlanHash       string    `json:"plan_hash,omitempty"`
	Progress       string    `json:"progress,omitempty"`
	Result         string    `json:"result,omitempty"`
	ErrorCode      string    `json:"error_code,omitempty"`
	Error          string    `json:"error,omitempty"`
	Attempt        int       `json:"attempt"`
	LeaseUntil     time.Time `json:"lease_until,omitempty"`
	CreatedBy      string    `json:"created_by,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	DispatchedAt   time.Time `json:"dispatched_at,omitempty"`
	StartedAt      time.Time `json:"started_at,omitempty"`
	FinishedAt     time.Time `json:"finished_at,omitempty"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// HostTaskEvent is one state transition or progress message for a task.
type HostTaskEvent struct {
	ID      string    `json:"id"`
	TaskID  string    `json:"task_id"`
	At      time.Time `json:"at"`
	State   string    `json:"state"`
	Message string    `json:"message,omitempty"`
	Data    string    `json:"data,omitempty"`
}

// HostAudit is one immutable host operation audit record.
type HostAudit struct {
	ID         string    `json:"id"`
	At         time.Time `json:"at"`
	Actor      string    `json:"actor,omitempty"`
	SourceIP   string    `json:"source_ip,omitempty"`
	Backend    string    `json:"backend"`
	Cluster    string    `json:"cluster"`
	Member     string    `json:"member"`
	Action     string    `json:"action"`
	TaskID     string    `json:"task_id,omitempty"`
	BeforeHash string    `json:"before_hash,omitempty"`
	AfterHash  string    `json:"after_hash,omitempty"`
	Result     string    `json:"result"`
	Detail     string    `json:"detail,omitempty"`
}

// Agent is one enrolled host agent.
type Agent struct {
	ID              string    `json:"id"`
	Cluster         string    `json:"cluster"`
	Member          string    `json:"member"`
	Hostname        string    `json:"hostname,omitempty"`
	Status          string    `json:"status"`
	CertFingerprint string    `json:"cert_fingerprint,omitempty"`
	CertNotAfter    time.Time `json:"cert_not_after,omitempty"`
	Version         string    `json:"version,omitempty"`
	OS              string    `json:"os,omitempty"`
	Kernel          string    `json:"kernel,omitempty"`
	Arch            string    `json:"arch,omitempty"`
	Capabilities    string    `json:"capabilities,omitempty"`
	Facts           string    `json:"facts,omitempty"`
	LastSeen        time.Time `json:"last_seen,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

const hostSchema = `
CREATE TABLE IF NOT EXISTS agents (
	id TEXT PRIMARY KEY,
	cluster TEXT NOT NULL,
	member TEXT NOT NULL,
	hostname TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL,
	cert_fingerprint TEXT NOT NULL DEFAULT '',
	cert_not_after BIGINT NOT NULL DEFAULT 0,
	version TEXT NOT NULL DEFAULT '',
	os TEXT NOT NULL DEFAULT '',
	kernel TEXT NOT NULL DEFAULT '',
	arch TEXT NOT NULL DEFAULT '',
	capabilities TEXT NOT NULL DEFAULT '[]',
	facts TEXT NOT NULL DEFAULT '{}',
	last_seen BIGINT NOT NULL DEFAULT 0,
	created_at BIGINT NOT NULL,
	updated_at BIGINT NOT NULL,
	UNIQUE(cluster, member)
);
CREATE INDEX IF NOT EXISTS idx_agents_status_seen ON agents(status, last_seen DESC);

CREATE TABLE IF NOT EXISTS enrollment_tokens (
	id TEXT PRIMARY KEY,
	token_hash TEXT NOT NULL,
	cluster TEXT NOT NULL,
	member TEXT NOT NULL,
	expires_at BIGINT NOT NULL,
	used_at BIGINT NOT NULL DEFAULT 0,
	created_by TEXT NOT NULL DEFAULT '',
	created_at BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_enrollment_tokens_expires ON enrollment_tokens(expires_at);

CREATE TABLE IF NOT EXISTS agent_certs (
	id TEXT PRIMARY KEY,
	agent_id TEXT NOT NULL,
	serial TEXT NOT NULL,
	fingerprint TEXT NOT NULL,
	not_before BIGINT NOT NULL,
	not_after BIGINT NOT NULL,
	revoked_at BIGINT NOT NULL DEFAULT 0,
	created_at BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_agent_certs_agent ON agent_certs(agent_id, not_after DESC);

CREATE TABLE IF NOT EXISTS host_tasks (
	id TEXT PRIMARY KEY,
	agent_id TEXT NOT NULL DEFAULT '',
	cluster TEXT NOT NULL,
	member TEXT NOT NULL,
	type TEXT NOT NULL,
	state TEXT NOT NULL,
	idempotency_key TEXT NOT NULL DEFAULT '',
	plan TEXT NOT NULL DEFAULT '',
	plan_hash TEXT NOT NULL DEFAULT '',
	progress TEXT NOT NULL DEFAULT '',
	result TEXT NOT NULL DEFAULT '',
	error_code TEXT NOT NULL DEFAULT '',
	error TEXT NOT NULL DEFAULT '',
	attempt INTEGER NOT NULL DEFAULT 0,
	lease_until BIGINT NOT NULL DEFAULT 0,
	created_by TEXT NOT NULL DEFAULT '',
	created_at BIGINT NOT NULL,
	dispatched_at BIGINT NOT NULL DEFAULT 0,
	started_at BIGINT NOT NULL DEFAULT 0,
	finished_at BIGINT NOT NULL DEFAULT 0,
	updated_at BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_host_tasks_agent_state ON host_tasks(agent_id, state);
CREATE INDEX IF NOT EXISTS idx_host_tasks_member_created ON host_tasks(cluster, member, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_host_tasks_state_lease ON host_tasks(state, lease_until);

CREATE TABLE IF NOT EXISTS host_task_events (
	id TEXT PRIMARY KEY,
	task_id TEXT NOT NULL,
	at BIGINT NOT NULL,
	state TEXT NOT NULL,
	message TEXT NOT NULL DEFAULT '',
	data TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_host_task_events_task ON host_task_events(task_id, at);

CREATE TABLE IF NOT EXISTS host_audit (
	id TEXT PRIMARY KEY,
	at BIGINT NOT NULL,
	actor TEXT NOT NULL DEFAULT '',
	source_ip TEXT NOT NULL DEFAULT '',
	backend TEXT NOT NULL,
	cluster TEXT NOT NULL,
	member TEXT NOT NULL,
	action TEXT NOT NULL,
	task_id TEXT NOT NULL DEFAULT '',
	before_hash TEXT NOT NULL DEFAULT '',
	after_hash TEXT NOT NULL DEFAULT '',
	result TEXT NOT NULL,
	detail TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_host_audit_at ON host_audit(at DESC);
CREATE INDEX IF NOT EXISTS idx_host_audit_member_at ON host_audit(cluster, member, at DESC);
`

func unixNanos(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}

	return t.UTC().UnixNano()
}

func timeFromNanos(ns int64) time.Time {
	if ns == 0 {
		return time.Time{}
	}

	return time.Unix(0, ns).UTC()
}

func clampLimit(limit int) int {
	if limit <= 0 {
		return 50
	}
	if limit > 200 {
		return 200
	}

	return limit
}

// CreateHostTask inserts one durable host task.
func (s *SQLStore) CreateHostTask(ctx context.Context, task *HostTask) error {
	if task == nil {
		return errors.New("store: host task is nil")
	}
	now := time.Now().UTC()
	if task.CreatedAt.IsZero() {
		task.CreatedAt = now
	}
	if task.UpdatedAt.IsZero() {
		task.UpdatedAt = task.CreatedAt
	}

	const query = `INSERT INTO host_tasks
		(id, agent_id, cluster, member, type, state, idempotency_key, plan, plan_hash,
		 progress, result, error_code, error, attempt, lease_until, created_by,
		 created_at, dispatched_at, started_at, finished_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := s.db.ExecContext(ctx, s.rebind(query),
		task.ID, task.AgentID, task.Cluster, task.Member, task.Type, task.State,
		task.IdempotencyKey, task.Plan, task.PlanHash, task.Progress, task.Result,
		task.ErrorCode, task.Error, task.Attempt, unixNanos(task.LeaseUntil), task.CreatedBy,
		unixNanos(task.CreatedAt), unixNanos(task.DispatchedAt), unixNanos(task.StartedAt),
		unixNanos(task.FinishedAt), unixNanos(task.UpdatedAt))
	if err != nil {
		return fmt.Errorf("store: create host task: %w", err)
	}

	return nil
}

// UpdateHostTask updates mutable task fields.
func (s *SQLStore) UpdateHostTask(ctx context.Context, task *HostTask) error {
	if task == nil {
		return errors.New("store: host task is nil")
	}
	if task.UpdatedAt.IsZero() {
		task.UpdatedAt = time.Now().UTC()
	}

	const query = `UPDATE host_tasks SET
		agent_id = ?, state = ?, progress = ?, result = ?, error_code = ?, error = ?,
		attempt = ?, lease_until = ?, dispatched_at = ?, started_at = ?, finished_at = ?,
		updated_at = ?
		WHERE id = ?`
	result, err := s.db.ExecContext(ctx, s.rebind(query),
		task.AgentID, task.State, task.Progress, task.Result, task.ErrorCode, task.Error,
		task.Attempt, unixNanos(task.LeaseUntil), unixNanos(task.DispatchedAt),
		unixNanos(task.StartedAt), unixNanos(task.FinishedAt), unixNanos(task.UpdatedAt),
		task.ID)
	if err != nil {
		return fmt.Errorf("store: update host task: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: update host task: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("store: host task %q: %w", task.ID, ErrNotFound)
	}

	return nil
}

// GetHostTask loads one task by ID.
func (s *SQLStore) GetHostTask(ctx context.Context, id string) (*HostTask, error) {
	const query = `SELECT id, agent_id, cluster, member, type, state, idempotency_key,
		plan, plan_hash, progress, result, error_code, error, attempt, lease_until,
		created_by, created_at, dispatched_at, started_at, finished_at, updated_at
		FROM host_tasks WHERE id = ?`
	row := s.db.QueryRowContext(ctx, s.rebind(query), id)
	task, err := scanHostTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("store: host task %q: %w", id, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("store: get host task: %w", err)
	}

	return task, nil
}

// ListHostTasks returns tasks newest first, optionally filtered by member.
func (s *SQLStore) ListHostTasks(ctx context.Context, cluster, member string, limit int) ([]*HostTask, error) {
	query := `SELECT id, agent_id, cluster, member, type, state, idempotency_key,
		plan, plan_hash, progress, result, error_code, error, attempt, lease_until,
		created_by, created_at, dispatched_at, started_at, finished_at, updated_at
		FROM host_tasks`
	args := []any{}
	clauses := make([]string, 0, 2)
	if cluster != "" {
		clauses = append(clauses, "cluster = ?")
		args = append(args, cluster)
	}
	if member != "" {
		clauses = append(clauses, "member = ?")
		args = append(args, member)
	}
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	query += " ORDER BY created_at DESC LIMIT ?"
	args = append(args, clampLimit(limit))

	rows, err := s.db.QueryContext(ctx, s.rebind(query), args...)
	if err != nil {
		return nil, fmt.Errorf("store: list host tasks: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]*HostTask, 0)
	for rows.Next() {
		task, err := scanHostTask(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan host task: %w", err)
		}
		out = append(out, task)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list host tasks rows: %w", err)
	}

	return out, nil
}

// AppendHostTaskEvent adds one event to a task.
func (s *SQLStore) AppendHostTaskEvent(ctx context.Context, event *HostTaskEvent) error {
	if event == nil {
		return errors.New("store: host task event is nil")
	}
	if event.At.IsZero() {
		event.At = time.Now().UTC()
	}
	const query = `INSERT INTO host_task_events (id, task_id, at, state, message, data)
		VALUES (?, ?, ?, ?, ?, ?)`
	if _, err := s.db.ExecContext(ctx, s.rebind(query),
		event.ID, event.TaskID, unixNanos(event.At), event.State, event.Message, event.Data); err != nil {
		return fmt.Errorf("store: append host task event: %w", err)
	}

	return nil
}

// ListHostTaskEvents returns a task's events oldest first.
func (s *SQLStore) ListHostTaskEvents(ctx context.Context, taskID string) ([]*HostTaskEvent, error) {
	const query = `SELECT id, task_id, at, state, message, data
		FROM host_task_events WHERE task_id = ? ORDER BY at, id`
	rows, err := s.db.QueryContext(ctx, s.rebind(query), taskID)
	if err != nil {
		return nil, fmt.Errorf("store: list host task events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]*HostTaskEvent, 0)
	for rows.Next() {
		var event HostTaskEvent
		var at int64
		if err := rows.Scan(&event.ID, &event.TaskID, &at, &event.State, &event.Message, &event.Data); err != nil {
			return nil, fmt.Errorf("store: scan host task event: %w", err)
		}
		event.At = timeFromNanos(at)
		out = append(out, &event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list host task events rows: %w", err)
	}

	return out, nil
}

// RecordHostAudit stores one immutable audit record.
func (s *SQLStore) RecordHostAudit(ctx context.Context, entry *HostAudit) error {
	if entry == nil {
		return errors.New("store: host audit is nil")
	}
	if entry.At.IsZero() {
		entry.At = time.Now().UTC()
	}
	const query = `INSERT INTO host_audit
		(id, at, actor, source_ip, backend, cluster, member, action, task_id,
		 before_hash, after_hash, result, detail)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	if _, err := s.db.ExecContext(ctx, s.rebind(query),
		entry.ID, unixNanos(entry.At), entry.Actor, entry.SourceIP, entry.Backend,
		entry.Cluster, entry.Member, entry.Action, entry.TaskID, entry.BeforeHash,
		entry.AfterHash, entry.Result, entry.Detail); err != nil {
		return fmt.Errorf("store: record host audit: %w", err)
	}

	return nil
}

// ListHostAudit returns audit records newest first, optionally filtered.
func (s *SQLStore) ListHostAudit(ctx context.Context, cluster, member string, limit int) ([]*HostAudit, error) {
	query := `SELECT id, at, actor, source_ip, backend, cluster, member, action,
		task_id, before_hash, after_hash, result, detail FROM host_audit`
	args := []any{}
	clauses := make([]string, 0, 2)
	if cluster != "" {
		clauses = append(clauses, "cluster = ?")
		args = append(args, cluster)
	}
	if member != "" {
		clauses = append(clauses, "member = ?")
		args = append(args, member)
	}
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	query += " ORDER BY at DESC, id DESC LIMIT ?"
	args = append(args, clampLimit(limit))

	rows, err := s.db.QueryContext(ctx, s.rebind(query), args...)
	if err != nil {
		return nil, fmt.Errorf("store: list host audit: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]*HostAudit, 0)
	for rows.Next() {
		var entry HostAudit
		var at int64
		if err := rows.Scan(&entry.ID, &at, &entry.Actor, &entry.SourceIP, &entry.Backend,
			&entry.Cluster, &entry.Member, &entry.Action, &entry.TaskID,
			&entry.BeforeHash, &entry.AfterHash, &entry.Result, &entry.Detail); err != nil {
			return nil, fmt.Errorf("store: scan host audit: %w", err)
		}
		entry.At = timeFromNanos(at)
		out = append(out, &entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list host audit rows: %w", err)
	}

	return out, nil
}

// UpsertAgent inserts or updates one agent registration.
func (s *SQLStore) UpsertAgent(ctx context.Context, agent *Agent) error {
	if agent == nil {
		return errors.New("store: agent is nil")
	}
	now := time.Now().UTC()
	if agent.CreatedAt.IsZero() {
		agent.CreatedAt = now
	}
	if agent.UpdatedAt.IsZero() {
		agent.UpdatedAt = now
	}

	const query = `INSERT INTO agents
		(id, cluster, member, hostname, status, cert_fingerprint, cert_not_after,
		 version, os, kernel, arch, capabilities, facts, last_seen, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			cluster = excluded.cluster, member = excluded.member, hostname = excluded.hostname,
			status = excluded.status, cert_fingerprint = excluded.cert_fingerprint,
			cert_not_after = excluded.cert_not_after, version = excluded.version,
			os = excluded.os, kernel = excluded.kernel, arch = excluded.arch,
			capabilities = excluded.capabilities, facts = excluded.facts,
			last_seen = excluded.last_seen, updated_at = excluded.updated_at`
	if _, err := s.db.ExecContext(ctx, s.rebind(query),
		agent.ID, agent.Cluster, agent.Member, agent.Hostname, agent.Status,
		agent.CertFingerprint, unixNanos(agent.CertNotAfter), agent.Version, agent.OS,
		agent.Kernel, agent.Arch, agent.Capabilities, agent.Facts, unixNanos(agent.LastSeen),
		unixNanos(agent.CreatedAt), unixNanos(agent.UpdatedAt)); err != nil {
		return fmt.Errorf("store: upsert agent: %w", err)
	}

	return nil
}

// GetAgent loads one agent by ID.
func (s *SQLStore) GetAgent(ctx context.Context, id string) (*Agent, error) {
	const query = `SELECT id, cluster, member, hostname, status, cert_fingerprint,
		cert_not_after, version, os, kernel, arch, capabilities, facts, last_seen,
		created_at, updated_at FROM agents WHERE id = ?`
	row := s.db.QueryRowContext(ctx, s.rebind(query), id)
	agent, err := scanAgent(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("store: agent %q: %w", id, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("store: get agent: %w", err)
	}

	return agent, nil
}

// ListAgents returns all enrolled agents ordered by cluster and member.
func (s *SQLStore) ListAgents(ctx context.Context) ([]*Agent, error) {
	const query = `SELECT id, cluster, member, hostname, status, cert_fingerprint,
		cert_not_after, version, os, kernel, arch, capabilities, facts, last_seen,
		created_at, updated_at FROM agents ORDER BY cluster, member`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("store: list agents: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]*Agent, 0)
	for rows.Next() {
		agent, err := scanAgent(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan agent: %w", err)
		}
		out = append(out, agent)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list agents rows: %w", err)
	}

	return out, nil
}

func scanHostTask(row rowScanner) (*HostTask, error) {
	var (
		task       HostTask
		lease      int64
		created    int64
		dispatched int64
		started    int64
		finished   int64
		updated    int64
	)
	if err := row.Scan(&task.ID, &task.AgentID, &task.Cluster, &task.Member, &task.Type,
		&task.State, &task.IdempotencyKey, &task.Plan, &task.PlanHash, &task.Progress,
		&task.Result, &task.ErrorCode, &task.Error, &task.Attempt, &lease, &task.CreatedBy,
		&created, &dispatched, &started, &finished, &updated); err != nil {
		return nil, err
	}
	task.LeaseUntil = timeFromNanos(lease)
	task.CreatedAt = timeFromNanos(created)
	task.DispatchedAt = timeFromNanos(dispatched)
	task.StartedAt = timeFromNanos(started)
	task.FinishedAt = timeFromNanos(finished)
	task.UpdatedAt = timeFromNanos(updated)

	return &task, nil
}

func scanAgent(row rowScanner) (*Agent, error) {
	var (
		agent      Agent
		certExpiry int64
		lastSeen   int64
		created    int64
		updated    int64
	)
	if err := row.Scan(&agent.ID, &agent.Cluster, &agent.Member, &agent.Hostname,
		&agent.Status, &agent.CertFingerprint, &certExpiry, &agent.Version, &agent.OS,
		&agent.Kernel, &agent.Arch, &agent.Capabilities, &agent.Facts, &lastSeen,
		&created, &updated); err != nil {
		return nil, err
	}
	agent.CertNotAfter = timeFromNanos(certExpiry)
	agent.LastSeen = timeFromNanos(lastSeen)
	agent.CreatedAt = timeFromNanos(created)
	agent.UpdatedAt = timeFromNanos(updated)

	return &agent, nil
}
