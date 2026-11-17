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

// QueryRange forwards a range query and returns the upstream JSON.
func (p *Proxy) QueryRange(ctx context.Context, query string, start, end time.Time, step time.Duration) ([]byte, error) {
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("metricsquery: query is required")
	}
	if step <= 0 {
		step = time.Minute
	}
	base, err := url.Parse(p.cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("metricsquery: invalid backend URL")
	}
	switch p.cfg.Type {
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
	if p.cfg.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+p.cfg.BearerToken)
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
