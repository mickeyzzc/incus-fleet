package logsquery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestProxyQueriesAndReconfigures(t *testing.T) {
	var path, query, limit, auth string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		query = r.URL.Query().Get("query")
		limit = r.URL.Query().Get("limit")
		auth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"hits":[]}`))
	}))
	defer backend.Close()

	p := New(Config{URL: backend.URL, BearerToken: "example-token-test", Timeout: time.Second})
	data, err := p.Query(context.Background(), "level:error", time.Unix(100, 0), time.Unix(200, 0), 25)
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if string(data) != `{"hits":[]}` || path != "/select/logsql/query" || query != "level:error" || limit != "25" {
		t.Fatalf("response = %s path=%s query=%s limit=%s", data, path, query, limit)
	}
	if auth != "Bearer example-token-test" {
		t.Fatalf("auth = %q", auth)
	}

	// Wiring can replace the backend at runtime.
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"hits":[1]}`))
	}))
	defer other.Close()
	p.SetURL(other.URL)
	if p.URL() != other.URL {
		t.Fatalf("URL() = %q", p.URL())
	}
	data, err = p.Query(context.Background(), "level:info", time.Time{}, time.Time{}, 0)
	if err != nil || string(data) != `{"hits":[1]}` {
		t.Fatalf("reconfigured query = %s err=%v", data, err)
	}
}

func TestProxyErrors(t *testing.T) {
	p := New(Config{})
	if _, err := p.Query(context.Background(), "x", time.Time{}, time.Time{}, 0); err == nil {
		t.Fatal("expected missing backend error")
	}
	p.SetURL("://bad")
	if _, err := p.Query(context.Background(), "x", time.Time{}, time.Time{}, 0); err == nil {
		t.Fatal("expected invalid URL error")
	}
	if _, err := p.Query(context.Background(), " ", time.Time{}, time.Time{}, 0); err == nil {
		t.Fatal("expected missing query error")
	}
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer backend.Close()
	p.SetURL(backend.URL)
	if _, err := p.Query(context.Background(), "x", time.Time{}, time.Time{}, 0); err == nil || !strings.Contains(err.Error(), "502") {
		t.Fatalf("backend error = %v", err)
	}
}
