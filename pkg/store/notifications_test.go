package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNotificationStore_CRUD(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test_notify.db")
	st, err := Open(dbPath)
	require.NoError(t, err)
	defer st.Close()

	// 1. Create Organization & Project
	org := &Organization{
		ID:   "org_test_notify",
		Name: "Test Notify Org",
		Slug: "test-notify-org",
	}
	err = st.Organizations().Create(ctx, org)
	require.NoError(t, err)

	prj := &Project{
		ID:    "prj_test_notify",
		OrgID: org.ID,
		Name:  "Test Notify Project",
	}
	err = st.Projects().Create(ctx, prj)
	require.NoError(t, err)

	// 2. Create Notification Channels
	ch1 := &NotificationChannel{
		OrgID:     org.ID,
		ProjectID: prj.ID,
		Name:      "Discord Deploy Alerts",
		Type:      "discord",
		TargetURL: "https://discord.com/api/webhooks/123/abc",
		Events:    []string{"deploy:failure", "deploy:success"},
		Enabled:   true,
	}
	err = st.Notifications().Create(ctx, ch1)
	require.NoError(t, err)
	assert.NotEmpty(t, ch1.ID)

	ch2 := &NotificationChannel{
		OrgID:     org.ID,
		Name:      "Global Backup Webhook",
		Type:      "webhook",
		TargetURL: "https://webhook.site/test",
		Events:    []string{"backup:*"},
		Enabled:   true,
	}
	err = st.Notifications().Create(ctx, ch2)
	require.NoError(t, err)

	// 3. GetByID
	got, err := st.Notifications().GetByID(ctx, ch1.ID)
	require.NoError(t, err)
	assert.Equal(t, "Discord Deploy Alerts", got.Name)
	assert.Equal(t, "discord", got.Type)
	assert.Equal(t, 2, len(got.Events))
	assert.True(t, got.Enabled)

	// 4. ListByOrg
	orgChannels, err := st.Notifications().ListByOrg(ctx, org.ID)
	require.NoError(t, err)
	assert.Len(t, orgChannels, 2)

	// 5. ListByProject
	prjChannels, err := st.Notifications().ListByProject(ctx, prj.ID)
	require.NoError(t, err)
	assert.Len(t, prjChannels, 1)
	assert.Equal(t, ch1.ID, prjChannels[0].ID)

	// 6. ListForEvent
	deployFailChannels, err := st.Notifications().ListForEvent(ctx, org.ID, prj.ID, "deploy:failure")
	require.NoError(t, err)
	assert.Len(t, deployFailChannels, 1)
	assert.Equal(t, ch1.ID, deployFailChannels[0].ID)

	backupFailChannels, err := st.Notifications().ListForEvent(ctx, org.ID, prj.ID, "backup:failure")
	require.NoError(t, err)
	assert.Len(t, backupFailChannels, 1)
	assert.Equal(t, ch2.ID, backupFailChannels[0].ID)

	// 7. Update
	ch1.Name = "Discord Deploy Alerts (Updated)"
	ch1.Enabled = false
	err = st.Notifications().Update(ctx, ch1)
	require.NoError(t, err)

	updated, err := st.Notifications().GetByID(ctx, ch1.ID)
	require.NoError(t, err)
	assert.Equal(t, "Discord Deploy Alerts (Updated)", updated.Name)
	assert.False(t, updated.Enabled)

	// 8. Delete
	err = st.Notifications().Delete(ctx, ch1.ID)
	require.NoError(t, err)

	_, err = st.Notifications().GetByID(ctx, ch1.ID)
	assert.ErrorIs(t, err, ErrNotFound)
}
