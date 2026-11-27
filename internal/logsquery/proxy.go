// Package logsquery proxies log search requests to VictoriaLogs.
package logsquery

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

// Config describes one log search backend.
type Config struct {
	URL         string
	BearerToken string
	Timeout     time.Duration
}

// Proxy forwards log search queries. Its URL can be replaced at runtime when a
// plugin install activates the logs.query_url wiring key.
type Proxy struct {
	mu     sync.RWMutex
	url    string
	token  string
	client *http.Client
}

// New builds a proxy. An empty URL is allowed: the proxy stays inert until a
// wiring override provides a backend.
func New(cfg Config) *Proxy {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 15 * time.Second
	}

	return &Proxy{url: strings.TrimRight(cfg.URL, "/"), token: cfg.BearerToken, client: &http.Client{Timeout: cfg.Timeout}}
}

// SetURL replaces the active backend without a process restart.
func (p *Proxy) SetURL(raw string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.url = strings.TrimRight(strings.TrimSpace(raw), "/")
}

// URL returns the active backend, if any.
func (p *Proxy) URL() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.url
}

// Query forwards a LogsQL query and returns the upstream JSON body.
func (p *Proxy) Query(ctx context.Context, query string, start, end time.Time, limit int) ([]byte, error) {
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("logsquery: query is required")
	}
	p.mu.RLock()
	base := p.url
	token := p.token
	p.mu.RUnlock()
	if base == "" {
		return nil, fmt.Errorf("logsquery: no log backend is configured")
	}
	target, err := url.Parse(base)
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" {
		return nil, fmt.Errorf("logsquery: invalid backend URL")
	}
	target.Path = strings.TrimRight(target.Path, "/") + "/select/logsql/query"
	values := target.Query()
	values.Set("query", query)
	if !start.IsZero() {
		values.Set("start", start.UTC().Format(time.RFC3339Nano))
	}
	if !end.IsZero() {
		values.Set("end", end.UTC().Format(time.RFC3339Nano))
	}
	if limit > 0 {
		values.Set("limit", fmt.Sprintf("%d", limit))
	}
	target.RawQuery = values.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("logsquery: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, fmt.Errorf("logsquery: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("logsquery: backend returned %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}

	return data, nil
}
