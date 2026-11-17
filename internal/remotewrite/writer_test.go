package remotewrite

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang/snappy"
	"google.golang.org/protobuf/proto"

	"github.com/mickeyzzc/incus-fleet/internal/prompb"
	"github.com/mickeyzzc/incus-fleet/internal/telemetry"
)

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

	writer := New(Config{URL: server.URL, BearerToken: "secret", BatchSize: 10, QueueSize: 10})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	writer.Start(ctx)
	now := time.Now().UTC()
	writer.Enqueue([]telemetry.Series{{
		Name: "fleet_host_load", Labels: map[string]string{"cluster": "c1", "member": "node-a"},
		Type: "gauge", Points: []telemetry.Sample{{At: now, Value: 1.5}},
	}})

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		hasBody := len(body) > 0
		mu.Unlock()
		if hasBody {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
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
}

func TestWriterRetriesServerErrors(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	writer := New(Config{URL: server.URL, BatchSize: 1, QueueSize: 1, Timeout: time.Second})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	writer.Start(ctx)
	writer.Enqueue([]telemetry.Series{{Name: "fleet_host_load", Points: []telemetry.Sample{{At: time.Now(), Value: 1}}}})
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if calls >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	writer.Close()
	if calls < 2 {
		t.Fatalf("calls = %d, want retry", calls)
	}
}

func TestWriterQueueOverflowAndString(t *testing.T) {
	writer := New(Config{URL: "https://example.com/write", QueueSize: 1, BatchSize: 1})
	writer.Enqueue([]telemetry.Series{{Name: "a"}, {Name: "b"}})
	if len(writer.queue) != 1 {
		t.Fatalf("queue length = %d", len(writer.queue))
	}
	if text := writer.cfg.String(); !strings.Contains(text, "remote-write") {
		t.Fatalf("String() = %q", text)
	}
	writer.Close()
}

func TestWriterNoopPaths(_ *testing.T) {
	writer := New(Config{URL: "", QueueSize: 1})
	writer.flush(context.Background(), nil)
	writer.flush(context.Background(), []telemetry.Series{{Name: "x"}})
	writer.Close()
	writer.Enqueue([]telemetry.Series{{Name: "x"}})
}

func TestWriterBadURLAndClientError(t *testing.T) {
	writer := New(Config{URL: "://bad", BatchSize: 1})
	writer.flush(context.Background(), []telemetry.Series{{Name: "x", Points: []telemetry.Sample{{At: time.Now(), Value: 1}}}})
	writer.Close()

	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()
	writer = New(Config{URL: server.URL, BatchSize: 1})
	writer.flush(context.Background(), []telemetry.Series{{Name: "x", Points: []telemetry.Sample{{At: time.Now(), Value: 1}}}})
	writer.Close()
	if calls != 1 {
		t.Fatalf("4xx retried: calls = %d", calls)
	}
}
