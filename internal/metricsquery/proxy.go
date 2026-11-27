// Package metricsquery proxies PromQL range queries to Prometheus or
// VictoriaMetrics.
package metricsquery

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Config describes one query backend.
type Config struct {
	URL         string
	Type        string
	BearerToken string
	Timeout     time.Duration
}

// Proxy forwards PromQL queries.
type Proxy struct {
	mu     sync.RWMutex
	cfg    Config
	client *http.Client
}

// New builds a proxy.
func New(cfg Config) *Proxy {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 15 * time.Second
	}
	if cfg.Type == "" {
		cfg.Type = "prometheus"
	}
	return &Proxy{cfg: cfg, client: &http.Client{Timeout: cfg.Timeout}}
}

// SetBackend replaces the query backend and type at runtime. Wiring activation
// calls it so no process restart is needed after a plugin install.
func (p *Proxy) SetBackend(rawURL, backendType string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if strings.TrimSpace(rawURL) != "" {
		p.cfg.URL = strings.TrimRight(strings.TrimSpace(rawURL), "/")
	}
	if strings.TrimSpace(backendType) != "" {
		p.cfg.Type = strings.TrimSpace(backendType)
	}
}

// QueryRange forwards a range query and returns the upstream JSON.
func (p *Proxy) QueryRange(ctx context.Context, query string, start, end time.Time, step time.Duration) ([]byte, error) {
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("metricsquery: query is required")
	}
	if step <= 0 {
		step = time.Minute
	}
	p.mu.RLock()
	cfg := p.cfg
	p.mu.RUnlock()
	base, err := url.Parse(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("metricsquery: invalid backend URL")
	}
	switch cfg.Type {
	case "victoriametrics":
		base.Path = strings.TrimRight(base.Path, "/") + "/prometheus/api/v1/query_range"
	default:
		base.Path = strings.TrimRight(base.Path, "/") + "/api/v1/query_range"
	}
	values := base.Query()
	values.Set("query", query)
	values.Set("start", fmt.Sprintf("%d", start.Unix()))
	values.Set("end", fmt.Sprintf("%d", end.Unix()))
	values.Set("step", fmt.Sprintf("%d", int64(step.Seconds())))
	base.RawQuery = values.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base.String(), nil)
	if err != nil {
		return nil, err
	}
	if cfg.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.BearerToken)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("metricsquery: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, fmt.Errorf("metricsquery: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("metricsquery: backend returned %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}

	return data, nil
}
