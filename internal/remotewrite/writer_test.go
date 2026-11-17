package remotewrite

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang/snappy"
	"google.golang.org/protobuf/proto"

	"github.com/mickeyzzc/incus-fleet/internal/prompb"
	"github.com/mickeyzzc/incus-fleet/internal/telemetry"
)

func newWriter(t *testing.T, cfg Config) *Writer {
	t.Helper()
	if cfg.SpoolDir == "" {
		cfg.SpoolDir = t.TempDir()
	}
	if cfg.SpoolPartition == "" {
		cfg.SpoolPartition = "test"
	}
	writer, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return writer
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition was not met before the deadline")
}

func TestWriterSendsSnappyProtobuf(t *testing.T) {
	var (
		mu   sync.Mutex
		body []byte
		auth string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		mu.Lock()
		body = data
		auth = r.Header.Get("Authorization")
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	writer := newWriter(t, Config{URL: server.URL, BearerToken: "secret", BatchSize: 10, QueueSize: 10})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	writer.Start(ctx)
	writer.Enqueue([]telemetry.Series{{
		Name: "fleet_host_load", Labels: map[string]string{"cluster": "c1", "member": "node-a"},
		Type: "gauge", Points: []telemetry.Sample{{At: time.Now().UTC(), Value: 1.5}},
	}})

	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(body) > 0
	})
	writer.Close()
	mu.Lock()
	defer mu.Unlock()
	if len(body) == 0 {
		t.Fatal("remote write body is empty")
	}
	if auth != "Bearer secret" {
		t.Fatalf("authorization = %q", auth)
	}
	raw, err := snappy.Decode(nil, body)
	if err != nil {
		t.Fatal(err)
	}
	var request prompb.WriteRequest
	if err := proto.Unmarshal(raw, &request); err != nil {
		t.Fatal(err)
	}
	if len(request.Timeseries) != 1 || request.Timeseries[0].Labels[0].Name != "__name__" {
		t.Fatalf("timeseries = %d", len(request.Timeseries))
	}
	if writer.spool.count() != 0 {
		t.Fatalf("spool was not cleared: %d", writer.spool.count())
	}
}

func TestWriterRetriesServerErrors(t *testing.T) {
	var calls int
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		calls++
		current := calls
		mu.Unlock()
		if current == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	writer := newWriter(t, Config{URL: server.URL, BatchSize: 1, QueueSize: 1, Timeout: time.Second})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	writer.Start(ctx)
	writer.Enqueue([]telemetry.Series{{Name: "fleet_host_load", Points: []telemetry.Sample{{At: time.Now(), Value: 1}}}})
	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return calls >= 2
	})
	writer.Close()
	if writer.spool.count() != 0 {
		t.Fatalf("successful retry left spool files: %d", writer.spool.count())
	}
}

func TestWriterQueueOverflowAndString(t *testing.T) {
	writer := newWriter(t, Config{URL: "https://example.com/write", QueueSize: 1, BatchSize: 1})
	writer.Enqueue([]telemetry.Series{{Name: "a"}})
	writer.Enqueue([]telemetry.Series{{Name: "b"}})
	if len(writer.queue) != 1 {
		t.Fatalf("queue length = %d", len(writer.queue))
	}
	if writer.spool.count() != 1 {
		t.Fatalf("spool length = %d", writer.spool.count())
	}
	if text := writer.cfg.String(); !strings.Contains(text, "remote-write") {
		t.Fatalf("String() = %q", text)
	}
	writer.Close()
}

func TestWriterNoopPaths(t *testing.T) {
	writer, err := New(Config{URL: "", QueueSize: 1, SpoolDir: t.TempDir()})
	if err == nil {
		writer.flush(context.Background(), nil)
		writer.Close()
		writer.Enqueue([]telemetry.Series{})
	}
}

func TestWriterBadURLAndClientError(t *testing.T) {
	writer := newWriter(t, Config{URL: "://bad", BatchSize: 1})
	entry, err := writer.spool.append([]telemetry.Series{{Name: "x", Points: []telemetry.Sample{{At: time.Now(), Value: 1}}}})
	if err != nil {
		t.Fatal(err)
	}
	sent, permanent := writer.flush(context.Background(), []spoolEntry{entry})
	if sent || !permanent {
		t.Fatalf("bad URL result = sent=%v permanent=%v", sent, permanent)
	}
	writer.Close()

	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()
	writer = newWriter(t, Config{URL: server.URL, BatchSize: 1})
	entry, err = writer.spool.append([]telemetry.Series{{Name: "x", Points: []telemetry.Sample{{At: time.Now(), Value: 1}}}})
	if err != nil {
		t.Fatal(err)
	}
	if remaining := writer.attempt(context.Background(), []spoolEntry{entry}); remaining != nil {
		t.Fatalf("4xx should be dropped, remaining=%d", len(remaining))
	}
	if calls != 1 {
		t.Fatalf("4xx retried: calls = %d", calls)
	}
	writer.Close()
}

func TestWriterReplaysSpoolAfterRestart(t *testing.T) {
	dir := t.TempDir()
	first := newWriter(t, Config{URL: "", QueueSize: 10, SpoolDir: dir})
	first.Enqueue([]telemetry.Series{{Name: "fleet_host_load", Points: []telemetry.Sample{{At: time.Now(), Value: 1}}}})
	first.Close()
	if first.spool.count() != 1 {
		t.Fatalf("spool before restart = %d", first.spool.count())
	}

	received := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		select {
		case received <- struct{}{}:
		default:
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	second := newWriter(t, Config{URL: server.URL, QueueSize: 10, BatchSize: 10, SpoolDir: dir})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	second.Start(ctx)
	select {
	case <-received:
	case <-time.After(5 * time.Second):
		t.Fatal("replayed batch was not sent")
	}
	second.Close()
	waitFor(t, func() bool { return second.spool.count() == 0 })
}

func TestWriterRetainsSpoolOnServerFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	dir := t.TempDir()
	writer := newWriter(t, Config{URL: server.URL, QueueSize: 10, BatchSize: 1, SpoolDir: dir})
	entry, err := writer.spool.append([]telemetry.Series{{Name: "x", Points: []telemetry.Sample{{At: time.Now(), Value: 1}}}})
	if err != nil {
		t.Fatal(err)
	}
	if remaining := writer.attempt(context.Background(), []spoolEntry{entry}); len(remaining) != 1 {
		t.Fatalf("failed batch remaining = %d", len(remaining))
	}
	if writer.spool.count() != 1 {
		t.Fatalf("failed batch was deleted: %d", writer.spool.count())
	}
	writer.Close()
}

func TestSpoolBoundedRetention(t *testing.T) {
	sp, err := openSpool(t.TempDir(), "test", 2, 200)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if _, err := sp.append([]telemetry.Series{{
			Name:   "fleet_host_load",
			Points: []telemetry.Sample{{At: time.Now(), Value: float64(i)}},
		}}); err != nil {
			t.Fatal(err)
		}
	}
	if sp.count() > 2 {
		t.Fatalf("entry retention exceeded: %d", sp.count())
	}
	if sp.bytes() > 200 {
		t.Fatalf("byte retention exceeded: %d", sp.bytes())
	}
	entries, err := sp.load()
	if err != nil || len(entries) == 0 {
		t.Fatalf("load after trim = %d, %v", len(entries), err)
	}
}

func TestWriterRetriesPendingBatch(t *testing.T) {
	var (
		mu    sync.Mutex
		calls int
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		calls++
		current := calls
		mu.Unlock()
		if current < 4 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	writer := newWriter(t, Config{URL: server.URL, QueueSize: 10, BatchSize: 1, Timeout: time.Second})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	writer.Start(ctx)
	writer.Enqueue([]telemetry.Series{{Name: "x", Points: []telemetry.Sample{{At: time.Now(), Value: 1}}}})
	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return calls >= 4
	})
	writer.Close()
	waitFor(t, func() bool { return writer.spool.count() == 0 })
}

func TestSpoolValidationAndCorruption(t *testing.T) {
	if defaults, err := openSpool(t.TempDir(), "test", 0, 0); err != nil || defaults.maxEntries != 10000 ||
		defaults.maxBytes != DefaultMaxBytes {
		t.Fatalf("openSpool(defaults) = %#v, %v", defaults, err)
	}
	if _, err := openSpool("", "test", 1, 1); err == nil {
		t.Fatal("openSpool(empty) expected error")
	}
	dir := t.TempDir()
	sp, err := openSpool(dir, "test", 2, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sp.append(nil); err == nil {
		t.Fatal("append(empty) expected error")
	}
	if err := os.WriteFile(filepath.Join(sp.dir, "00000000000000000001-000001.json"), []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, err := sp.load()
	if err != nil || len(entries) != 0 {
		t.Fatalf("load(corrupt) = %d, %v", len(entries), err)
	}
	if sp.count() != 0 {
		t.Fatalf("corrupt file was not removed: %d", sp.count())
	}

	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Config{SpoolDir: filepath.Join(blocker, "queue"), QueueSize: 1}); err == nil {
		t.Fatal("New(blocked spool) expected error")
	}
}

func TestEnqueueAfterClose(t *testing.T) {
	writer := newWriter(t, Config{URL: "https://example.com/write", QueueSize: 2})
	writer.Close()
	writer.Enqueue([]telemetry.Series{{Name: "late"}})
	if writer.spool.count() != 0 {
		t.Fatalf("late enqueue left spool files: %d", writer.spool.count())
	}
}

func TestWriterContextCancelWithFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	writer := newWriter(t, Config{URL: server.URL, QueueSize: 10, BatchSize: 1})
	ctx, cancel := context.WithCancel(context.Background())
	writer.Start(ctx)
	writer.Enqueue([]telemetry.Series{{Name: "x", Points: []telemetry.Sample{{At: time.Now(), Value: 1}}}})
	time.Sleep(50 * time.Millisecond)
	cancel()
	writer.Close()
	if writer.spool.count() == 0 {
		t.Fatal("failed batch was not retained for replay")
	}
}

func TestWriterCloseWithQueuedBatch(t *testing.T) {
	received := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		select {
		case received <- struct{}{}:
		default:
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	writer := newWriter(t, Config{URL: server.URL, QueueSize: 10, BatchSize: 10})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	writer.Start(ctx)
	writer.Enqueue([]telemetry.Series{{Name: "x", Points: []telemetry.Sample{{At: time.Now(), Value: 1}}}})
	writer.Close()
	select {
	case <-received:
	case <-time.After(3 * time.Second):
		t.Fatal("queued batch was not flushed on close")
	}
}

func TestSpoolEdgeCases(t *testing.T) {
	dir := t.TempDir()
	sp, err := openSpool(dir, "test", 2, 1024)
	if err != nil {
		t.Fatal(err)
	}
	sp.remove(spoolEntry{path: filepath.Join(sp.dir, "missing.json"), size: 10})
	if sp.count() != 0 || sp.bytes() != 0 {
		t.Fatalf("empty spool count=%d bytes=%d", sp.count(), sp.bytes())
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if sp.count() != 0 || sp.bytes() != 0 {
		t.Fatalf("missing dir count=%d bytes=%d", sp.count(), sp.bytes())
	}
	sp.trim()
}

func TestSpoolLoadUnreadableFile(t *testing.T) {
	dir := t.TempDir()
	sp, err := openSpool(dir, "test", 2, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sp.append([]telemetry.Series{{Name: "x", Points: []telemetry.Sample{{At: time.Now(), Value: 1}}}}); err != nil {
		t.Fatal(err)
	}
	var jsonFiles []string
	entries, err := os.ReadDir(sp.dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), spoolSuffix) {
			jsonFiles = append(jsonFiles, entry.Name())
		}
	}
	if len(jsonFiles) != 1 {
		t.Fatalf("spool json files = %d, %v", len(jsonFiles), err)
	}
	path := filepath.Join(sp.dir, jsonFiles[0])
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	if loaded, err := sp.load(); err != nil || len(loaded) != 0 {
		t.Fatalf("load(unreadable) = %d, %v", len(loaded), err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSpoolLoadAppliesRetention(t *testing.T) {
	dir := t.TempDir()
	sp, err := openSpool(dir, "test", 5, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := sp.append([]telemetry.Series{{Name: "x", Points: []telemetry.Sample{{At: time.Now(), Value: float64(i)}}}}); err != nil {
			t.Fatal(err)
		}
	}
	sp.Close()
	tight, err := openSpool(dir, "test", 1, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := tight.load()
	if err != nil || len(entries) != 1 {
		t.Fatalf("load(retention) = %d, %v", len(entries), err)
	}
	if tight.count() != 1 {
		t.Fatalf("retention left %d files", tight.count())
	}
}

func TestEnqueueSpoolWriteFailure(t *testing.T) {
	writer := newWriter(t, Config{URL: "https://example.com/write", QueueSize: 2})
	if err := os.Chmod(writer.spool.dir, 0o500); err != nil { //nolint:gosec // test permissions
		t.Fatal(err)
	}
	writer.Enqueue([]telemetry.Series{{Name: "x"}})
	if len(writer.queue) != 0 {
		t.Fatalf("failed spool write queued an entry: %d", len(writer.queue))
	}
	if err := os.Chmod(writer.spool.dir, 0o700); err != nil { //nolint:gosec // test permissions
		t.Fatal(err)
	}
	writer.Close()
}

func TestNewSpoolLoadFailure(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatal(err)
	}
	_, err := New(Config{SpoolDir: dir})
	_ = os.Chmod(dir, 0o700) //nolint:gosec // restore test directory
	if err == nil {
		t.Fatal("New(unreadable spool) expected error")
	}
}

func TestRunWithCanceledContext(t *testing.T) {
	writer := newWriter(t, Config{URL: "", QueueSize: 2})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	writer.Start(ctx)
	writer.Enqueue([]telemetry.Series{{Name: "x"}})
	time.Sleep(20 * time.Millisecond)
	writer.Close()
}

func TestSpoolPartitionsAreIsolated(t *testing.T) {
	root := t.TempDir()
	first := newWriter(t, Config{URL: "", SpoolDir: root, SpoolPartition: "app-a", QueueSize: 2})
	second := newWriter(t, Config{URL: "", SpoolDir: root, SpoolPartition: "app-b", QueueSize: 2})
	first.Enqueue([]telemetry.Series{{Name: "a"}})
	second.Enqueue([]telemetry.Series{{Name: "b"}})

	if first.spool.count() != 1 || second.spool.count() != 1 {
		t.Fatalf("partition counts = %d, %d", first.spool.count(), second.spool.count())
	}
	if first.spool.dir != filepath.Join(root, "app-a") || second.spool.dir != filepath.Join(root, "app-b") {
		t.Fatalf("partition dirs = %q, %q", first.spool.dir, second.spool.dir)
	}
	first.Close()
	second.Close()
}

func TestSpoolLegacyRootMigration(t *testing.T) {
	root := t.TempDir()
	data, err := json.Marshal([]telemetry.Series{{Name: "legacy"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "00000000000000000001-000001.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	writer := newWriter(t, Config{URL: "", SpoolDir: root, SpoolPartition: "app-a", QueueSize: 2})
	if writer.spool.count() != 1 {
		t.Fatalf("legacy batch not migrated: %d", writer.spool.count())
	}
	if _, err := os.Stat(filepath.Join(root, "00000000000000000001-000001.json")); !os.IsNotExist(err) {
		t.Fatalf("legacy file still in root: %v", err)
	}
	writer.Close()
}

func TestSpoolStalePartitionTakeover(t *testing.T) {
	root := t.TempDir()
	first := newWriter(t, Config{URL: "", SpoolDir: root, SpoolPartition: "app-a", QueueSize: 2})
	first.Enqueue([]telemetry.Series{{Name: "a"}})
	first.Close()

	received := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		select {
		case received <- struct{}{}:
		default:
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	other := newWriter(t, Config{URL: server.URL, SpoolDir: root, SpoolPartition: "app-b", QueueSize: 2})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	other.Start(ctx)
	select {
	case <-received:
	case <-time.After(3 * time.Second):
		t.Fatal("stale app-a partition was not claimed by app-b")
	}
	other.Close()
	if _, err := os.Stat(filepath.Join(root, "app-a")); !os.IsNotExist(err) {
		t.Fatalf("stale partition still exists: %v", err)
	}
}

func TestSpoolActivePartitionNotClaimed(t *testing.T) {
	root := t.TempDir()
	active := newWriter(t, Config{URL: "", SpoolDir: root, SpoolPartition: "app-a", QueueSize: 2})
	active.Enqueue([]telemetry.Series{{Name: "a"}})

	other := newWriter(t, Config{URL: "", SpoolDir: root, SpoolPartition: "app-b", QueueSize: 2})
	if other.spool.count() != 0 {
		t.Fatalf("active partition was claimed: %d", other.spool.count())
	}
	if active.spool.count() != 1 {
		t.Fatalf("active partition lost data: %d", active.spool.count())
	}
	other.Close()
	active.Close()
}

func TestSpoolSamePartitionLocked(t *testing.T) {
	root := t.TempDir()
	first := newWriter(t, Config{URL: "", SpoolDir: root, SpoolPartition: "app-a", QueueSize: 2})
	second, err := New(Config{SpoolDir: root, SpoolPartition: "app-a", QueueSize: 2})
	if err == nil {
		second.Close()
		t.Fatal("second writer acquired the same partition")
	}
	first.Close()
}

func TestSanitizePartition(t *testing.T) {
	cases := map[string]string{
		"app-a":   "app-a",
		"app b/c": "app-b-c",
		"...":     "default",
	}
	for input, want := range cases {
		if got := sanitizePartition(input); got != want {
			t.Fatalf("sanitizePartition(%q) = %q, want %q", input, got, want)
		}
	}
	if got := sanitizePartition(""); got == "" || got == "default" {
		t.Fatalf("sanitizePartition(empty) is not collision-free: %q", got)
	}
	if got := sanitizePartition("   "); got == "" || got == "default" {
		t.Fatalf("sanitizePartition(whitespace) is not collision-free: %q", got)
	}
	if got := sanitizePartitionName("..."); got != "center" {
		t.Fatalf("sanitizePartitionName(...) = %q", got)
	}
	if got := defaultPartition(); !strings.Contains(got, strconv.Itoa(os.Getpid())) {
		t.Fatalf("defaultPartition() = %q", got)
	}
}

func TestOpenSpoolLegacyMigrationFailure(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "app"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o000); err != nil { //nolint:gosec // test permissions
		t.Fatal(err)
	}
	_, err := openSpool(root, "app", 1, 1)
	_ = os.Chmod(root, 0o700) //nolint:gosec // restore test directory
	if err == nil {
		t.Fatal("openSpool(unreadable root) expected error")
	}
}

func TestOpenSpoolLegacyMigrationRenameFailure(t *testing.T) {
	root := t.TempDir()
	partition := filepath.Join(root, "app")
	if err := os.MkdirAll(partition, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "00000000000000000001-000001.json"), []byte("[]"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(partition, 0o500); err != nil { //nolint:gosec // test permissions
		t.Fatal(err)
	}
	_, err := openSpool(root, "app", 1, 1)
	_ = os.Chmod(partition, 0o700) //nolint:gosec // restore test directory
	if err == nil {
		t.Fatal("openSpool(read-only partition) expected migration error")
	}
}

func TestSpoolCloseAndLockHelpers(t *testing.T) {
	var nilSpool *spool
	nilSpool.Close()
	unlockFile(nil)

	sp, err := openSpool(t.TempDir(), "test", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	sp.Close()
	sp.Close() // second close is a no-op
}

func TestMigrateLegacyRootUnreadable(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o000); err != nil { //nolint:gosec // test permissions
		t.Fatal(err)
	}
	err := migrateLegacyRoot(root, filepath.Join(root, "app"))
	_ = os.Chmod(root, 0o700) //nolint:gosec // restore test directory
	if err == nil {
		t.Fatal("migrateLegacyRoot(unreadable) expected error")
	}
}

func TestClaimStalePartitionRenameFailure(t *testing.T) {
	root := t.TempDir()
	stale := filepath.Join(root, "stale")
	own := filepath.Join(root, "own")
	if err := os.MkdirAll(stale, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(own, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stale, "batch.json"), []byte("[]"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(own, 0o500); err != nil { //nolint:gosec // test permissions
		t.Fatal(err)
	}
	err := claimStalePartitions(root, own)
	_ = os.Chmod(own, 0o700) //nolint:gosec // restore test directory
	if err == nil {
		t.Fatal("claimStalePartitions(read-only target) expected error")
	}
}
