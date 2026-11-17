// Package remotewrite sends center telemetry to Prometheus-compatible remote
// write endpoints through a bounded, disk-backed spool.
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
	SpoolDir    string
	MaxBytes    int64
}

// Writer queues telemetry on disk and sends it in batches. Spool files are
// removed only after a successful write (or a permanent 4xx rejection).
type Writer struct {
	cfg    Config
	client *http.Client
	queue  chan spoolEntry
	stop   chan struct{}
	spool  *spool
	once   sync.Once
	wg     sync.WaitGroup
}

// New builds a writer and replays any files left by a previous process.
func New(cfg Config) (*Writer, error) {
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = 10000
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 500
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}
	sp, err := openSpool(cfg.SpoolDir, cfg.QueueSize, cfg.MaxBytes)
	if err != nil {
		return nil, err
	}
	entries, err := sp.load()
	if err != nil {
		return nil, err
	}
	w := &Writer{
		cfg:    cfg,
		client: &http.Client{Timeout: cfg.Timeout},
		queue:  make(chan spoolEntry, cfg.QueueSize),
		stop:   make(chan struct{}),
		spool:  sp,
	}
	for _, entry := range entries {
		select {
		case w.queue <- entry:
		default:
			sp.remove(entry)
		}
	}

	return w, nil
}

// Start launches the background sender.
func (w *Writer) Start(ctx context.Context) {
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		w.run(ctx)
	}()
}

// Enqueue persists series before queueing them. The oldest spool file is
// removed when the queue or disk budget is full.
func (w *Writer) Enqueue(series []telemetry.Series) {
	if len(series) == 0 {
		return
	}
	select {
	case <-w.stop:
		return
	default:
	}
	entry, err := w.spool.append(series)
	if err != nil {
		return
	}

	for {
		select {
		case w.queue <- entry:
			return
		case <-w.stop:
			w.spool.remove(entry)
			return
		default:
			select {
			case old := <-w.queue:
				w.spool.remove(old)
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
	batch := make([]spoolEntry, 0, w.cfg.BatchSize)
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	retry := time.NewTimer(0)
	if !retry.Stop() {
		<-retry.C
	}
	var pending []spoolEntry

	for {
		if len(pending) > 0 {
			select {
			case <-ctx.Done():
				_ = w.attempt(context.Background(), pending)
				return
			case <-w.stop:
				_ = w.attempt(context.Background(), pending)
				return
			case <-retry.C:
				pending = w.attempt(ctx, pending)
				if len(pending) > 0 {
					retry.Reset(2 * time.Second)
				}
			}
			continue
		}

		select {
		case <-ctx.Done():
			pending = append(pending, batch...)
			_ = w.attempt(context.Background(), pending)
			return
		case <-w.stop:
			pending = append(pending, batch...)
			_ = w.attempt(context.Background(), pending)
			return
		case item := <-w.queue:
			batch = append(batch, item)
			if seriesCount(batch) >= w.cfg.BatchSize {
				pending = w.attempt(ctx, batch)
				batch = nil
				if len(pending) > 0 {
					retry.Reset(2 * time.Second)
				}
			}
		case <-timer.C:
			if len(batch) > 0 {
				pending = w.attempt(ctx, batch)
				batch = nil
				if len(pending) > 0 {
					retry.Reset(2 * time.Second)
				}
			}
			timer.Reset(time.Second)
		}
	}
}

func seriesCount(batch []spoolEntry) int {
	total := 0
	for _, entry := range batch {
		total += len(entry.series)
	}
	return total
}

// attempt sends a batch and returns the entries that must be retried. Success
// and permanent 4xx rejections delete the spool files.
func (w *Writer) attempt(ctx context.Context, batch []spoolEntry) []spoolEntry {
	if len(batch) == 0 {
		return nil
	}
	sent, permanent := w.flush(ctx, batch)
	if sent || permanent {
		for _, entry := range batch {
			w.spool.remove(entry)
		}
		return nil
	}

	return batch
}

func (w *Writer) flush(ctx context.Context, batch []spoolEntry) (bool, bool) {
	if len(batch) == 0 {
		return true, false
	}
	if w.cfg.URL == "" {
		return true, false
	}
	request := &prompb.WriteRequest{}
	for _, entry := range batch {
		for _, series := range entry.series {
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
	}
	payload, err := proto.Marshal(request)
	if err != nil {
		return false, true
	}
	body := snappy.Encode(nil, payload)

	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.cfg.URL, bytes.NewReader(body))
		if err != nil {
			return false, true
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
				return true, false
			}
			if resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode < 500 {
				return false, true
			}
		}
		select {
		case <-ctx.Done():
			return false, false
		case <-time.After(time.Duration(attempt+1) * 200 * time.Millisecond):
		}
	}

	return false, false
}

// String returns a safe description without credentials.
func (c Config) String() string {
	return fmt.Sprintf("remote-write(%s, queue=%d, batch=%d, spool=%s)", c.URL, c.QueueSize, c.BatchSize, c.SpoolDir)
}
