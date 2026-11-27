package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// NotificationSink delivers fleet events to the URL wired through
// notifications.sink_url. It is the real consumer of that wiring key: when no
// plugin took over the key the subscriber is a no-op, so enabling it never
// changes event delivery by itself.
type NotificationSink struct {
	target func() string
	secret []byte
	client *http.Client
	events []string
}

// NewNotificationSink builds a sink subscriber. target returns the current
// wiring value; returning an empty string disables delivery.
func NewNotificationSink(target func() string, secret []byte, client *http.Client) *NotificationSink {
	if client == nil {
		client = &http.Client{Timeout: webhookTimeout}
	}
	return &NotificationSink{
		target: target, secret: append([]byte(nil), secret...), client: client,
		events: []string{
			EventAgentConnected, EventAgentDisconnected, EventAgentHeartbeat,
			EventHostTaskCompleted, EventMigrationCompleted, EventInstanceStateChange,
			EventWiringChanged,
		},
	}
}

// SubscribedEvents implements EventSubscription.
func (s *NotificationSink) SubscribedEvents() []string {
	return append([]string(nil), s.events...)
}

// HandleEvent implements EventSubscriber. Delivery is fire-and-forget so a slow
// notification center never blocks fleet event producers.
func (s *NotificationSink) HandleEvent(_ context.Context, e Event) error {
	if !containsEvent(s.events, e.Type) {
		return nil
	}
	target := ""
	if s.target != nil {
		target = strings.TrimSpace(s.target())
	}
	if target == "" {
		return nil
	}
	go func() { _ = s.deliver(target, e) }()

	return nil
}

func (s *NotificationSink) deliver(target string, e Event) error {
	body, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("plugin notification: encode event: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), webhookTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("plugin notification: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if len(s.secret) > 0 {
		req.Header.Set("X-Fleet-Signature", SignPayload(s.secret, body))
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("plugin notification: delivery failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("plugin notification: upstream returned %d", resp.StatusCode)
	}

	return nil
}

// SinkOwnsWebhook reports whether a plugin whose manifest declares the sink key
// should skip its own webhook subscriber because the sink already delivers to
// the same URL. This prevents duplicate notifications when bell is installed
// and its wiring is active.
func SinkOwnsWebhook(r *Registry, m Manifest) bool {
	if r == nil || !m.Capabilities.DeclaresWiring("notifications.sink_url") {
		return false
	}
	target, ok := r.WiringValue("notifications.sink_url")

	return ok && strings.TrimSpace(target) != ""
}
