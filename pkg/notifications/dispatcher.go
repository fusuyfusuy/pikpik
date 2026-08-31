package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/fusuycorp/pikpik/pkg/store"
)

// Event represents a system or lifecycle event to be broadcast to notification channels.
type Event struct {
	OrgID     string            `json:"org_id"`
	ProjectID string            `json:"project_id,omitempty"`
	Type      string            `json:"type"` // e.g. "deploy:success", "deploy:failure", "backup:success", "backup:failure"
	Title     string            `json:"title"`
	Message   string            `json:"message"`
	Status    string            `json:"status"` // "success", "failure", "warning", "info"
	Metadata  map[string]string `json:"metadata,omitempty"`
	Timestamp time.Time         `json:"timestamp"`
}

// Dispatcher defines the notification broadcasting interface.
type Dispatcher interface {
	Dispatch(ctx context.Context, evt Event)
	TestChannel(ctx context.Context, ch *store.NotificationChannel) error
}

// DefaultDispatcher implements Dispatcher with asynchronous worker pool and formatters.
type DefaultDispatcher struct {
	st         store.NotificationStore
	httpClient *http.Client
	queue      chan Event
	wg         sync.WaitGroup
	ctx        context.Context
	cancel     context.CancelFunc
}

// NewDispatcher creates and starts a background notification dispatcher.
func NewDispatcher(ctx context.Context, st store.NotificationStore) *DefaultDispatcher {
	dispCtx, cancel := context.WithCancel(ctx)
	d := &DefaultDispatcher{
		st: st,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		queue:  make(chan Event, 256),
		ctx:    dispCtx,
		cancel: cancel,
	}

	// Start 2 concurrent worker goroutines
	for i := 0; i < 2; i++ {
		d.wg.Add(1)
		go d.worker()
	}

	return d
}

// Close gracefully stops the dispatcher worker pool.
func (d *DefaultDispatcher) Close() {
	d.cancel()
	close(d.queue)
	d.wg.Wait()
}

// Dispatch enqueues an event for non-blocking asynchronous delivery.
func (d *DefaultDispatcher) Dispatch(ctx context.Context, evt Event) {
	if evt.Timestamp.IsZero() {
		evt.Timestamp = time.Now().UTC()
	}
	select {
	case d.queue <- evt:
	default:
		// Queue full: drop non-blocking to protect latency invariants
	}
}

func (d *DefaultDispatcher) worker() {
	defer d.wg.Done()
	for {
		select {
		case <-d.ctx.Done():
			return
		case evt, ok := <-d.queue:
			if !ok {
				return
			}
			d.deliverEvent(evt)
		}
	}
}

func (d *DefaultDispatcher) deliverEvent(evt Event) {
	if d.st == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	channels, err := d.st.ListForEvent(ctx, evt.OrgID, evt.ProjectID, evt.Type)
	if err != nil || len(channels) == 0 {
		return
	}

	for _, ch := range channels {
		if !ch.Enabled {
			continue
		}
		_ = d.sendToChannel(ctx, ch, evt)
	}
}

// TestChannel synchronously tests a specific channel destination.
func (d *DefaultDispatcher) TestChannel(ctx context.Context, ch *store.NotificationChannel) error {
	testEvt := Event{
		OrgID:     ch.OrgID,
		ProjectID: ch.ProjectID,
		Type:      "system:test",
		Title:     "pikpik Test Notification",
		Message:   fmt.Sprintf("This is a test alert from pikpik PaaS for channel '%s'.", ch.Name),
		Status:    "info",
		Metadata: map[string]string{
			"channel_type": ch.Type,
			"channel_name": ch.Name,
		},
		Timestamp: time.Now().UTC(),
	}
	return d.sendToChannel(ctx, ch, testEvt)
}

func (d *DefaultDispatcher) sendToChannel(ctx context.Context, ch *store.NotificationChannel, evt Event) error {
	var payload []byte
	var err error

	switch ch.Type {
	case "discord":
		payload, err = formatDiscord(evt)
	case "slack":
		payload, err = formatSlack(evt)
	case "telegram":
		payload, err = formatTelegram(ch, evt)
	default: // generic webhook
		payload, err = json.Marshal(evt)
	}

	if err != nil {
		return fmt.Errorf("failed to format notification payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ch.TargetURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to create http request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "pikpik-PaaS/0.2.0")
	if ch.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+ch.AuthToken)
	}

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to post notification: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("notification endpoint returned status %d", resp.StatusCode)
	}

	return nil
}

// Formatter helpers
func formatDiscord(evt Event) ([]byte, error) {
	color := 0x3498db // blue info
	if evt.Status == "success" {
		color = 0x2ecc71 // green
	} else if evt.Status == "failure" {
		color = 0xe74c3c // red
	} else if evt.Status == "warning" {
		color = 0xf1c40f // yellow
	}

	var fields []map[string]any
	for k, v := range evt.Metadata {
		fields = append(fields, map[string]any{
			"name":   k,
			"value":  v,
			"inline": true,
		})
	}

	doc := map[string]any{
		"username": "pikpik PaaS",
		"embeds": []map[string]any{
			{
				"title":       evt.Title,
				"description": evt.Message,
				"color":       color,
				"fields":      fields,
				"timestamp":   evt.Timestamp.Format(time.RFC3339),
			},
		},
	}
	return json.Marshal(doc)
}

func formatSlack(evt Event) ([]byte, error) {
	color := "#3498db"
	if evt.Status == "success" {
		color = "#2ecc71"
	} else if evt.Status == "failure" {
		color = "#e74c3c"
	} else if evt.Status == "warning" {
		color = "#f1c40f"
	}

	var fields []map[string]any
	for k, v := range evt.Metadata {
		fields = append(fields, map[string]any{
			"title": k,
			"value": v,
			"short": true,
		})
	}

	doc := map[string]any{
		"attachments": []map[string]any{
			{
				"color":     color,
				"title":     evt.Title,
				"text":      evt.Message,
				"fields":    fields,
				"ts":        evt.Timestamp.Unix(),
				"footer":    "pikpik PaaS",
			},
		},
	}
	return json.Marshal(doc)
}

func formatTelegram(ch *store.NotificationChannel, evt Event) ([]byte, error) {
	icon := "ℹ️"
	if evt.Status == "success" {
		icon = "✅"
	} else if evt.Status == "failure" {
		icon = "❌"
	} else if evt.Status == "warning" {
		icon = "⚠️"
	}

	text := fmt.Sprintf("%s <b>%s</b>\n\n%s\n", icon, evt.Title, evt.Message)
	if len(evt.Metadata) > 0 {
		text += "\n<b>Details:</b>\n"
		for k, v := range evt.Metadata {
			text += fmt.Sprintf("• <i>%s:</i> <code>%s</code>\n", k, v)
		}
	}

	doc := map[string]any{
		"text":       text,
		"parse_mode": "HTML",
	}
	return json.Marshal(doc)
}
