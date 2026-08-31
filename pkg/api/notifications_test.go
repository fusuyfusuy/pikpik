package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fusuycorp/pikpik/pkg/api"
)

func TestNotificationRoutes_CRUD(t *testing.T) {
	server, ctrl, token := setupAPITestServer()
	defer server.Close()

	// 1. Create Notification Channel (POST)
	createReq := api.CreateNotificationChannelRequest{
		Name:      "Discord Webhook Alerts",
		Type:      "discord",
		TargetURL: "https://discord.com/api/webhooks/123/abc",
		Events:    []string{"deploy:failure", "deploy:success"},
	}

	resp := authedJSONRequest(t, server.URL, token, http.MethodPost, "/api/v1/notifications/channels", createReq)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusCreated, resp.StatusCode)
	var createdEnvelope api.Response[api.NotificationChannel]
	err := json.NewDecoder(resp.Body).Decode(&createdEnvelope)
	require.NoError(t, err)
	assert.True(t, createdEnvelope.Success)
	assert.NotEmpty(t, createdEnvelope.Data.ID)
	assert.Equal(t, "Discord Webhook Alerts", createdEnvelope.Data.Name)
	assert.Equal(t, "discord", createdEnvelope.Data.Type)
	channelID := createdEnvelope.Data.ID

	// 2. List Notification Channels (GET)
	resp = authedJSONRequest(t, server.URL, token, http.MethodGet, "/api/v1/notifications/channels", nil)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var listEnvelope api.Response[[]api.NotificationChannel]
	err = json.NewDecoder(resp.Body).Decode(&listEnvelope)
	require.NoError(t, err)
	assert.Len(t, listEnvelope.Data, 1)
	assert.Equal(t, channelID, listEnvelope.Data[0].ID)

	// 3. Update Notification Channel (PUT)
	updateReq := api.UpdateNotificationChannelRequest{
		Name: "Discord Webhook Alerts (Production)",
	}
	resp = authedJSONRequest(t, server.URL, token, http.MethodPut, "/api/v1/notifications/channels/"+channelID, updateReq)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var updateEnvelope api.Response[api.NotificationChannel]
	err = json.NewDecoder(resp.Body).Decode(&updateEnvelope)
	require.NoError(t, err)
	assert.Equal(t, "Discord Webhook Alerts (Production)", updateEnvelope.Data.Name)

	// 4. Test Notification Channel Ping (POST /test)
	// Create mock webhook receiver
	testServer := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusOK)
	}))
	defer testServer.Close()

	// Point channel to test server
	_, err = ctrl.UpdateNotificationChannel(context.Background(), channelID, &api.UpdateNotificationChannelRequest{
		TargetURL: testServer.URL,
	})
	require.NoError(t, err)

	resp = authedJSONRequest(t, server.URL, token, http.MethodPost, "/api/v1/notifications/channels/"+channelID+"/test", nil)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// 5. Delete Notification Channel (DELETE)
	resp = authedJSONRequest(t, server.URL, token, http.MethodDelete, "/api/v1/notifications/channels/"+channelID, nil)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// 6. Verify Delete (List is empty)
	channels, err := ctrl.ListNotificationChannels(context.Background(), "")
	require.NoError(t, err)
	assert.Empty(t, channels)
}
