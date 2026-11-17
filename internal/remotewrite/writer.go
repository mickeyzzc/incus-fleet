// Package remotewrite sends center telemetry to Prometheus-compatible remote
// write endpoints using a bounded in-memory queue.
package remotewrite

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/golang/snappy"
	"google.golang.org/protobuf/proto"

	"github.com/mickeyzzc/incus-fleet/internal/prompb"
	"github.com/mickeyzzc/incus-fleet/internal/telemetry"
)

// Config controls one remote write target.
type Config struct {
	URL         string
	BearerToken string
	Timeout     time.Duration
	QueueSize   int
	BatchSize   int
}

// Writer queues telemetry and writes batches.
type Writer struct {
	cfg    Config
	client *http.Client
	queue  chan telemetry.Series
	stop   chan struct{}
	once   sync.Once
	wg     sync.WaitGroup
}

// New builds a writer. Call Start before Enqueue.
func New(cfg Config) *Writer {
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = 10000
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 500
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}
	return &Writer{
		cfg:    cfg,
		client: &http.Client{Timeout: cfg.Timeout},
		queue:  make(chan telemetry.Series, cfg.QueueSize),
		stop:   make(chan struct{}),
	}
}

// Start launches the background sender.
func (w *Writer) Start(ctx context.Context) {
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		w.run(ctx)
	}()
}

// Enqueue adds series without blocking; the oldest item is dropped when full.
func (w *Writer) Enqueue(series []telemetry.Series) {
	for _, item := range series {
		select {
		case w.queue <- item:
		case <-w.stop:
			return
		default:
			select {
			case <-w.queue:
			default:
			}
			select {
			case w.queue <- item:
			default:
			}
		}
	}
}

// Close stops the writer and waits for the current batch.
func (w *Writer) Close() {
	w.once.Do(func() { close(w.stop) })
	w.wg.Wait()
}

func (w *Writer) run(ctx context.Context) {
	batch := make([]telemetry.Series, 0, w.cfg.BatchSize)
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stop:
			w.flush(context.Background(), batch)
			return
		case item := <-w.queue:
			batch = append(batch, item)
			if len(batch) >= w.cfg.BatchSize {
				w.flush(ctx, batch)
				batch = batch[:0]
			}
		case <-timer.C:
			if len(batch) > 0 {
				w.flush(ctx, batch)
				batch = batch[:0]
			}
			timer.Reset(time.Second)
		}
	}
}

func (w *Writer) flush(ctx context.Context, batch []telemetry.Series) {
	if len(batch) == 0 || w.cfg.URL == "" {
		return
	}
	request := &prompb.WriteRequest{}
	for _, series := range batch {
		ts := &prompb.TimeSeries{}
		labels := make([]*prompb.Label, 0, len(series.Labels)+1)
		labels = append(labels, &prompb.Label{Name: "__name__", Value: series.Name})
		keys := make([]string, 0, len(series.Labels))
		for key := range series.Labels {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			labels = append(labels, &prompb.Label{Name: key, Value: series.Labels[key]})
		}
		ts.Labels = labels
		for _, point := range series.Points {
			ts.Samples = append(ts.Samples, &prompb.Sample{
				Value: point.Value, Timestamp: point.At.UnixMilli(),
			})
		}
		request.Timeseries = append(request.Timeseries, ts)
	}
	payload, err := proto.Marshal(request)
	if err != nil {
		return
	}
	body := snappy.Encode(nil, payload)

	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.cfg.URL, bytes.NewReader(body))
		if err != nil {
			return
		}
		req.Header.Set("Content-Encoding", "snappy")
		req.Header.Set("Content-Type", "application/x-protobuf")
		req.Header.Set("X-Prometheus-Remote-Write-Version", "0.1.0")
		req.Header.Set("User-Agent", "incus-fleet-remote-write")
		if w.cfg.BearerToken != "" {
			req.Header.Set("Authorization", "Bearer "+w.cfg.BearerToken)
		}
		resp, err := w.client.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return
			}
			if resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode < 500 {
				return
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(attempt+1) * 200 * time.Millisecond):
		}
	}
}

// String returns a safe description without credentials.
func (c Config) String() string {
	return fmt.Sprintf("remote-write(%s, queue=%d, batch=%d)", c.URL, c.QueueSize, c.BatchSize)
}
