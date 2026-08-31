package notifications

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fusuycorp/pikpik/pkg/store"
)

type mockNotificationStore struct {
	store.NotificationStore
	channels []*store.NotificationChannel
	mu       sync.Mutex
}

func (m *mockNotificationStore) ListForEvent(ctx context.Context, orgID, projectID, event string) ([]*store.NotificationChannel, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var matched []*store.NotificationChannel
	for _, ch := range m.channels {
		for _, e := range ch.Events {
			if e == "*" || e == event {
				matched = append(matched, ch)
				break
			}
		}
	}
	return matched, nil
}

func TestDispatcher_FormatsAndDelivery(t *testing.T) {
	var receivedHeaders http.Header
	var receivedBody []byte
	var mu sync.Mutex

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		receivedHeaders = r.Header.Clone()
		receivedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	mockSt := &mockNotificationStore{
		channels: []*store.NotificationChannel{
			{
				ID:        "ntf_discord",
				OrgID:     "org_1",
				Name:      "Discord Channel",
				Type:      "discord",
				TargetURL: ts.URL,
				Events:    []string{"deploy:failure"},
				Enabled:   true,
			},
			{
				ID:        "ntf_slack",
				OrgID:     "org_1",
				Name:      "Slack Channel",
				Type:      "slack",
				TargetURL: ts.URL,
				Events:    []string{"deploy:success"},
				Enabled:   true,
			},
			{
				ID:        "ntf_webhook",
				OrgID:     "org_1",
				Name:      "Generic Webhook",
				Type:      "webhook",
				TargetURL: ts.URL,
				AuthToken: "secret_webhook_token_123",
				Events:    []string{"backup:*"},
				Enabled:   true,
			},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	d := NewDispatcher(ctx, mockSt)
	defer d.Close()

	// 1. Test Discord Delivery
	d.Dispatch(ctx, Event{
		OrgID:     "org_1",
		Type:      "deploy:failure",
		Title:     "Deployment Failed",
		Message:   "Service web-frontend failed to start",
		Status:    "failure",
		Metadata:  map[string]string{"service": "web-frontend", "exit_code": "1"},
		Timestamp: time.Now().UTC(),
	})

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	var discordDoc map[string]any
	err := json.Unmarshal(receivedBody, &discordDoc)
	require.NoError(t, err)
	assert.Equal(t, "pikpik PaaS", discordDoc["username"])
	embeds := discordDoc["embeds"].([]any)
	assert.NotEmpty(t, embeds)
	embed0 := embeds[0].(map[string]any)
	assert.Equal(t, "Deployment Failed", embed0["title"])
	assert.Equal(t, float64(0xe74c3c), embed0["color"]) // red
	mu.Unlock()

	// 2. Test Slack Delivery
	d.Dispatch(ctx, Event{
		OrgID:     "org_1",
		Type:      "deploy:success",
		Title:     "Deployment Succeeded",
		Message:   "Service web-frontend is live",
		Status:    "success",
		Timestamp: time.Now().UTC(),
	})

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	var slackDoc map[string]any
	err = json.Unmarshal(receivedBody, &slackDoc)
	require.NoError(t, err)
	attachments := slackDoc["attachments"].([]any)
	assert.NotEmpty(t, attachments)
	att0 := attachments[0].(map[string]any)
	assert.Equal(t, "#2ecc71", att0["color"]) // green
	assert.Equal(t, "Deployment Succeeded", att0["title"])
	mu.Unlock()

	// 3. Test Webhook with Auth Token
	ch := &store.NotificationChannel{
		ID:        "ntf_direct_test",
		Name:      "Direct Test Webhook",
		Type:      "webhook",
		TargetURL: ts.URL,
		AuthToken: "my_secret_pat",
		Enabled:   true,
	}
	err = d.TestChannel(ctx, ch)
	require.NoError(t, err)

	mu.Lock()
	assert.Equal(t, "Bearer my_secret_pat", receivedHeaders.Get("Authorization"))
	var testEvt Event
	err = json.Unmarshal(receivedBody, &testEvt)
	require.NoError(t, err)
	assert.Equal(t, "system:test", testEvt.Type)
	assert.Equal(t, "pikpik Test Notification", testEvt.Title)
	mu.Unlock()
}

func TestSanitizeMetadata_CredentialScrubbing(t *testing.T) {
	input := map[string]string{
		"db_password":   "supersecretpassword",
		"api_token":     "pat_1234567890",
		"auth_header":   "Bearer abc",
		"database_uri":  "postgres://user:pass@localhost:5432/db",
		"redis_dsn":     "redis://:secret@localhost:6379",
		"service_name":  "my-app",
		"environment":   "production",
		"normal_field":  "safe_value",
	}

	sanitized := sanitizeMetadata(input)

	assert.Equal(t, "********", sanitized["db_password"])
	assert.Equal(t, "********", sanitized["api_token"])
	assert.Equal(t, "********", sanitized["auth_header"])
	assert.Equal(t, "********", sanitized["database_uri"])
	assert.Equal(t, "********", sanitized["redis_dsn"])
	assert.Equal(t, "my-app", sanitized["service_name"])
	assert.Equal(t, "production", sanitized["environment"])
	assert.Equal(t, "safe_value", sanitized["normal_field"])
}

