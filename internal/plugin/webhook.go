package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const webhookTimeout = 5 * time.Second

// WebhookConfig describes fleet-to-plugin event delivery. Bearer and Secret
// come from the plugin's scoped credential; neither is ever logged.
type WebhookConfig struct {
	ID      string
	BaseURL string
	Path    string
	Events  []string
	Secret  string
	Bearer  string
	Client  *http.Client
	// OnError, when set, receives delivery failures. Credentials are never
	// included in the error text.
	OnError func(error)
}

// WebhookSubscriber delivers signed events to one plugin's /events endpoint.
type WebhookSubscriber struct {
	id       string
	endpoint string
	events   []string
	secret   string
	bearer   string
	client   *http.Client
	onError  func(error)
}

// NewWebhookSubscriber builds a subscriber and validates its endpoint.
func NewWebhookSubscriber(cfg WebhookConfig) (*WebhookSubscriber, error) {
	if strings.TrimSpace(cfg.ID) == "" || len(cfg.Events) == 0 {
		return nil, fmt.Errorf("%w: webhook subscriber requires id and events", ErrInvalidManifest)
	}
	if !strings.HasPrefix(cfg.Path, "/") {
		return nil, fmt.Errorf("%w: webhook path must be absolute", ErrInvalidManifest)
	}
	base, err := url.Parse(strings.TrimRight(cfg.BaseURL, "/"))
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" {
		return nil, fmt.Errorf("%w: webhook base URL must be absolute http(s)", ErrInvalidManifest)
	}
	client := cfg.Client
	if client == nil {
		client = &http.Client{Timeout: webhookTimeout}
	}

	return &WebhookSubscriber{
		id: cfg.ID, endpoint: strings.TrimRight(cfg.BaseURL, "/") + cfg.Path,
		events: append([]string(nil), cfg.Events...),
		secret: cfg.Secret, bearer: cfg.Bearer, client: client, onError: cfg.OnError,
	}, nil
}

// SubscribedEvents lists the event kinds this subscriber accepts.
func (w *WebhookSubscriber) SubscribedEvents() []string {
	return append([]string(nil), w.events...)
}

// HandleEvent hands the event to a bounded delivery goroutine. Returning
// immediately keeps a slow plugin from blocking fleet's event producers; the
// registry therefore never waits on plugin network I/O.
func (w *WebhookSubscriber) HandleEvent(_ context.Context, e Event) error {
	if !containsEvent(w.events, e.Type) {
		return nil
	}
	go func() {
		if err := w.deliver(e); err != nil && w.onError != nil {
			w.onError(err)
		}
	}()

	return nil
}

func (w *WebhookSubscriber) deliver(e Event) error {
	body, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("plugin webhook: encode event: %w", err)
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, w.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("plugin webhook: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if w.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+w.bearer)
	}
	if w.secret != "" {
		req.Header.Set("X-Fleet-Signature", SignPayload([]byte(w.secret), body))
	}

	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("plugin webhook: delivery failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("plugin webhook: upstream returned %d", resp.StatusCode)
	}

	return nil
}

// ID returns the plugin id this subscriber serves.
func (w *WebhookSubscriber) ID() string { return w.id }
