package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTeamInvitationsStore(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test_invites.db")
	st, err := Open(dbPath)
	require.NoError(t, err)
	defer st.Close()

	// 1. Create org and inviter user
	org := &Organization{
		ID:   "org_test_inv",
		Name: "Test Org",
		Slug: "test-org-inv",
	}
	require.NoError(t, st.Organizations().Create(ctx, org))

	inviter := &User{
		ID:           "usr_inviter",
		Email:        "admin@pikpik.dev",
		PasswordHash: "hash123",
		Role:         "admin",
	}
	require.NoError(t, st.Users().Create(ctx, inviter))

	// 2. Create invitation
	inv := &TeamInvitation{
		ID:        "inv_test_1",
		OrgID:     org.ID,
		Email:     "developer@pikpik.dev",
		Role:      "developer",
		TokenHash: "hash_invite_token_123",
		InvitedBy: inviter.ID,
		ExpiresAt: time.Now().UTC().Add(7 * 24 * time.Hour),
	}
	require.NoError(t, st.Invitations().Create(ctx, inv))

	// 3. GetByID and GetByTokenHash
	gotByID, err := st.Invitations().GetByID(ctx, inv.ID)
	require.NoError(t, err)
	assert.Equal(t, "developer@pikpik.dev", gotByID.Email)
	assert.Equal(t, "developer", gotByID.Role)

	gotByToken, err := st.Invitations().GetByTokenHash(ctx, inv.TokenHash)
	require.NoError(t, err)
	assert.Equal(t, inv.ID, gotByToken.ID)

	// 4. ListByOrg
	list, err := st.Invitations().ListByOrg(ctx, org.ID)
	require.NoError(t, err)
	assert.Len(t, list, 1)

	// 5. MarkAccepted
	acceptedTime := time.Now().UTC()
	require.NoError(t, st.Invitations().MarkAccepted(ctx, inv.ID, acceptedTime))
	gotAccepted, _ := st.Invitations().GetByID(ctx, inv.ID)
	assert.NotNil(t, gotAccepted.AcceptedAt)

	// 6. Delete
	require.NoError(t, st.Invitations().Delete(ctx, inv.ID))
	_, err = st.Invitations().GetByID(ctx, inv.ID)
	assert.Equal(t, ErrNotFound, err)
}

func TestProjectMembershipsStore(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test_memberships.db")
	st, err := Open(dbPath)
	require.NoError(t, err)
	defer st.Close()

	// Create org, user, and project
	org := &Organization{
		ID:   "org_test_mem",
		Name: "Test Org Mem",
		Slug: "test-org-mem",
	}
	require.NoError(t, st.Organizations().Create(ctx, org))

	user := &User{
		ID:           "usr_mem_1",
		Email:        "member@pikpik.dev",
		PasswordHash: "hash123",
		Role:         "developer",
	}
	require.NoError(t, st.Users().Create(ctx, user))

	proj := &Project{
		ID:    "prj_test_mem",
		OrgID: org.ID,
		Name:  "Test Project",
		Slug:  "test-proj-mem",
	}
	require.NoError(t, st.Projects().Create(ctx, proj))

	// 1. Set membership
	m := &ProjectMembership{
		ID:        "pm_1",
		ProjectID: proj.ID,
		UserID:    user.ID,
		Role:      "developer",
	}
	require.NoError(t, st.Memberships().Set(ctx, m))

	// 2. Get membership
	got, err := st.Memberships().Get(ctx, proj.ID, user.ID)
	require.NoError(t, err)
	assert.Equal(t, "developer", got.Role)

	// 3. Upsert / update role
	m.Role = "admin"
	require.NoError(t, st.Memberships().Set(ctx, m))
	gotUpdated, _ := st.Memberships().Get(ctx, proj.ID, user.ID)
	assert.Equal(t, "admin", gotUpdated.Role)

	// 4. List by project and by user
	byProj, err := st.Memberships().ListByProject(ctx, proj.ID)
	require.NoError(t, err)
	assert.Len(t, byProj, 1)

	byUser, err := st.Memberships().ListByUser(ctx, user.ID)
	require.NoError(t, err)
	assert.Len(t, byUser, 1)

	// 5. Delete membership
	require.NoError(t, st.Memberships().Delete(ctx, proj.ID, user.ID))
	_, err = st.Memberships().Get(ctx, proj.ID, user.ID)
	assert.Equal(t, ErrNotFound, err)
}

func TestIntegrationsStore(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test_integrations.db")
	st, err := Open(dbPath)
	require.NoError(t, err)
	defer st.Close()

	org := &Organization{
		ID:   "org_test_int",
		Name: "Test Org Int",
		Slug: "test-org-int",
	}
	require.NoError(t, st.Organizations().Create(ctx, org))

	// 1. Create integration
	it := &Integration{
		ID:                   "int_gh_1",
		OrgID:                org.ID,
		Name:                 "Production GitHub",
		Type:                 "git_github",
		CredentialsEncrypted: "v1:encrypted_token_data",
		ConfigJSON:           `{"app_id": 12345, "installation_id": 67890}`,
		Status:               "active",
	}
	require.NoError(t, st.Integrations().Create(ctx, it))

	// 2. GetByID
	got, err := st.Integrations().GetByID(ctx, it.ID)
	require.NoError(t, err)
	assert.Equal(t, "Production GitHub", got.Name)
	assert.Equal(t, "git_github", got.Type)

	// 3. ListByOrg and ListByType
	byOrg, err := st.Integrations().ListByOrg(ctx, org.ID)
	require.NoError(t, err)
	assert.Len(t, byOrg, 1)

	byType, err := st.Integrations().ListByType(ctx, org.ID, "git_github")
	require.NoError(t, err)
	assert.Len(t, byType, 1)

	// 4. Update
	it.Name = "Updated GitHub App"
	it.Status = "error"
	require.NoError(t, st.Integrations().Update(ctx, it))
	gotUpdated, _ := st.Integrations().GetByID(ctx, it.ID)
	assert.Equal(t, "Updated GitHub App", gotUpdated.Name)
	assert.Equal(t, "error", gotUpdated.Status)

	// 5. UpdateStatus
	require.NoError(t, st.Integrations().UpdateStatus(ctx, it.ID, "active"))
	gotStatus, _ := st.Integrations().GetByID(ctx, it.ID)
	assert.Equal(t, "active", gotStatus.Status)

	// 6. Delete
	require.NoError(t, st.Integrations().Delete(ctx, it.ID))
	_, err = st.Integrations().GetByID(ctx, it.ID)
	assert.Equal(t, ErrNotFound, err)
}

func TestUsersStoreListAndUpdateRole(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test_users_roles.db")
	st, err := Open(dbPath)
	require.NoError(t, err)
	defer st.Close()

	u1 := &User{
		ID:           "usr_test_role_1",
		Email:        "u1@pikpik.dev",
		PasswordHash: "hash1",
		Role:         "viewer",
	}
	u2 := &User{
		ID:           "usr_test_role_2",
		Email:        "u2@pikpik.dev",
		PasswordHash: "hash2",
		Role:         "developer",
	}
	require.NoError(t, st.Users().Create(ctx, u1))
	require.NoError(t, st.Users().Create(ctx, u2))

	// List
	list, err := st.Users().List(ctx, 10, 0)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(list), 2)

	// UpdateRole
	require.NoError(t, st.Users().UpdateRole(ctx, u1.ID, "admin"))
	gotU1, _ := st.Users().GetByID(ctx, u1.ID)
	assert.Equal(t, "admin", gotU1.Role)
}
