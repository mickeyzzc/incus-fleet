// Package telemetry keeps a one-hour in-memory ring buffer for agent metrics.
package telemetry

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mickeyzzc/incus-fleet/internal/agentpb"
)

const (
	// DefaultInterval is the expected sample spacing.
	DefaultInterval = 10 * time.Second
	// Retention is the center-side history window.
	Retention = time.Hour
	// Points is the number of points per series at the default interval.
	Points = 360
	// DefaultMaxSeries bounds center memory.
	DefaultMaxSeries = 20000
)

// Sample is one timestamped value.
type Sample struct {
	At    time.Time `json:"at"`
	Value float64   `json:"value"`
}

// Series is one named metric with labels and history.
type Series struct {
	Name   string            `json:"name"`
	Labels map[string]string `json:"labels"`
	Type   string            `json:"type"`
	Points []Sample          `json:"points"`
}

type ring struct {
	name   string
	labels map[string]string
	kind   string
	points []Sample
}

// Store is a bounded, in-memory time-series store.
type Store struct {
	mu        sync.RWMutex
	maxSeries int
	order     []string
	series    map[string]*ring
}

// New builds a store. maxSeries <= 0 uses DefaultMaxSeries.
func New(maxSeries int) *Store {
	if maxSeries <= 0 {
		maxSeries = DefaultMaxSeries
	}
	return &Store{maxSeries: maxSeries, series: make(map[string]*ring)}
}

// Ingest adds one agent batch. Center-owned labels are injected and cannot be
// overridden by the agent.
func (s *Store) Ingest(batch *agentpb.TelemetryBatch, cluster, member string) int {
	if batch == nil || batch.AgentId == "" {
		return 0
	}
	added := 0
	for _, item := range batch.Series {
		if item == nil || !validName(item.Name) {
			continue
		}
		labels := sanitizeLabels(item.Labels)
		labels["cluster"] = cluster
		labels["member"] = member
		labels["agent_id"] = batch.AgentId
		key := seriesKey(item.Name, labels)
		s.mu.Lock()
		r, ok := s.series[key]
		if !ok {
			if len(s.series) >= s.maxSeries {
				s.mu.Unlock()
				continue
			}
			r = &ring{name: item.Name, labels: labels, kind: metricType(item.Type)}
			s.series[key] = r
			s.order = append(s.order, key)
		}
		for _, sample := range item.Samples {
			if sample == nil {
				continue
			}
			at := time.UnixMilli(sample.AtMs).UTC()
			if at.IsZero() || time.Since(at) > Retention || time.Since(at) < -5*time.Minute {
				continue
			}
			r.append(Sample{At: at, Value: sample.Value})
			added++
		}
		s.mu.Unlock()
	}

	return added
}

func (r *ring) append(sample Sample) {
	if len(r.points) > 0 {
		last := r.points[len(r.points)-1]
		if !sample.At.After(last.At) {
			return
		}
	}
	r.points = append(r.points, sample)
	cutoff := time.Now().UTC().Add(-Retention)
	start := 0
	for start < len(r.points) && r.points[start].At.Before(cutoff) {
		start++
	}
	if start > 0 {
		r.points = append([]Sample(nil), r.points[start:]...)
	}
	if len(r.points) > Points {
		r.points = append([]Sample(nil), r.points[len(r.points)-Points:]...)
	}
}

// Filter selects series and time ranges.
type Filter struct {
	Cluster string
	Member  string
	AgentID string
	Metric  string
	From    time.Time
	To      time.Time
}

// Query returns matching series with points in the requested range.
func (s *Store) Query(filter Filter) []Series {
	from, to := filter.From, filter.To
	if from.IsZero() {
		from = time.Now().UTC().Add(-Retention)
	}
	if to.IsZero() {
		to = time.Now().UTC()
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Series, 0)
	for _, key := range s.order {
		r := s.series[key]
		if r == nil || !matches(r, filter) {
			continue
		}
		points := make([]Sample, 0, len(r.points))
		for _, point := range r.points {
			if point.At.Before(from) || point.At.After(to) {
				continue
			}
			points = append(points, point)
		}
		if len(points) == 0 {
			continue
		}
		out = append(out, Series{
			Name: r.name, Labels: cloneLabels(r.labels), Type: r.kind, Points: points,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return seriesKey(out[i].Name, out[i].Labels) < seriesKey(out[j].Name, out[j].Labels)
	})

	return out
}

// Snapshot returns the latest point for every matching series.
func (s *Store) Snapshot(filter Filter) []Series {
	out := s.Query(filter)
	for i := range out {
		if len(out[i].Points) > 1 {
			out[i].Points = out[i].Points[len(out[i].Points)-1:]
		}
	}

	return out
}

// Len returns the number of stored series.
func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.series)
}

func matches(r *ring, filter Filter) bool {
	if filter.Metric != "" && !strings.HasPrefix(r.name, filter.Metric) {
		return false
	}
	for key, value := range map[string]string{
		"cluster": filter.Cluster, "member": filter.Member, "agent_id": filter.AgentID,
	} {
		if value != "" && r.labels[key] != value {
			return false
		}
	}

	return true
}

func seriesKey(name string, labels map[string]string) string {
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(name)
	for _, key := range keys {
		b.WriteByte(0)
		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(labels[key])
	}
	return b.String()
}

func sanitizeLabels(labels map[string]string) map[string]string {
	out := make(map[string]string, len(labels)+3)
	for key, value := range labels {
		if len(key) == 0 || len(key) > 64 || len(value) > 256 {
			continue
		}
		out[key] = value
	}
	return out
}

func cloneLabels(labels map[string]string) map[string]string {
	out := make(map[string]string, len(labels))
	for key, value := range labels {
		out[key] = value
	}
	return out
}

func validName(name string) bool {
	if !strings.HasPrefix(name, "fleet_") || len(name) > 200 {
		return false
	}
	for i, r := range name {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '_' || r == ':' || i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

func metricType(value agentpb.MetricType) string {
	if value == agentpb.MetricType_METRIC_TYPE_COUNTER {
		return "counter"
	}
	return "gauge"
}

// Validate checks a telemetry batch shape without mutating a store.
func Validate(batch *agentpb.TelemetryBatch) error {
	if batch == nil || batch.AgentId == "" {
		return fmt.Errorf("telemetry: agent_id is required")
	}
	for _, item := range batch.Series {
		if item == nil || !validName(item.Name) {
			return fmt.Errorf("telemetry: invalid metric name")
		}
	}
	return nil
}
