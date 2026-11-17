package metricsquery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestQueryRangePrometheus(t *testing.T) {
	var (
		path  string
		query string
		auth  string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		query = r.URL.RawQuery
		auth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"status":"success"}`))
	}))
	defer server.Close()

	proxy := New(Config{URL: server.URL, Type: "prometheus", BearerToken: "secret"})
	data, err := proxy.QueryRange(context.Background(), "fleet_host_load", time.Unix(100, 0), time.Unix(200, 0), 30*time.Second)
	if err != nil {
		t.Fatalf("QueryRange() error = %v", err)
	}
	if !strings.Contains(string(data), "success") {
		t.Fatalf("data = %s", data)
	}
	if path != "/api/v1/query_range" || !strings.Contains(query, "query=fleet_host_load") ||
		!strings.Contains(query, "start=100") || !strings.Contains(query, "step=30") {
		t.Fatalf("path=%s query=%s", path, query)
	}
	if auth != "Bearer secret" {
		t.Fatalf("auth = %q", auth)
	}
}

func TestQueryRangeVictoriaMetricsAndErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/prometheus/api/v1/query_range" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad query"))
	}))
	defer server.Close()

	proxy := New(Config{URL: server.URL, Type: "victoriametrics"})
	if _, err := proxy.QueryRange(context.Background(), "x", time.Now().Add(-time.Hour), time.Now(), time.Minute); err == nil {
		t.Fatal("QueryRange(upstream error) expected error")
	}
	if _, err := proxy.QueryRange(context.Background(), "", time.Now(), time.Now(), time.Minute); err == nil {
		t.Fatal("QueryRange(empty) expected error")
	}
}

func TestQueryRangeDefaultsAndBadURL(t *testing.T) {
	proxy := New(Config{URL: "://bad"})
	if _, err := proxy.QueryRange(context.Background(), "x", time.Now(), time.Now(), 0); err == nil {
		t.Fatal("bad URL expected error")
	}
}

func TestNewDefaults(t *testing.T) {
	proxy := New(Config{})
	if proxy.cfg.Timeout != 15*time.Second || proxy.cfg.Type != "prometheus" {
		t.Fatalf("defaults = %#v", proxy.cfg)
	}
}
