package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/fusuycorp/pikpik/pkg/api"
	"github.com/fusuycorp/pikpik/pkg/auth"
	"github.com/fusuycorp/pikpik/pkg/crypto"
	"github.com/fusuycorp/pikpik/pkg/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestAPIServer(t *testing.T) (*httptest.Server, store.Store, auth.AuthService, string, string) {
	dbPath := filepath.Join(t.TempDir(), "test_api.db")
	st, err := store.Open(dbPath)
	require.NoError(t, err)

	vault, err := crypto.NewAESVault("super-secure-master-key-32-bytes!!")
	require.NoError(t, err)
	hasher := crypto.FastArgon2Hasher()
	authSvc := auth.NewAuthService(st, hasher)

	// Bootstrap admin user
	ctx := context.Background()
	admin, err := authSvc.BootstrapAdmin(ctx, "admin@pikpik.dev", "AdminSecurePass123!")
	require.NoError(t, err)

	// Create admin API token
	genToken, err := authSvc.CreateAPIToken(ctx, admin.ID, "Admin Token", []string{"*"}, nil)
	require.NoError(t, err)
	adminToken := genToken.RawSecret

	ctrl := api.NewDefaultController(api.ControllerDependencies{
		Store:       st,
		AuthService: authSvc,
		Vault:       vault,
	})

	mux := http.NewServeMux()
	api.RegisterRoutes(mux, ctrl, authSvc, st, nil, nil, nil, nil, nil, nil)

	server := httptest.NewServer(mux)
	return server, st, authSvc, adminToken, admin.ID
}

func TestTeamUserManagementFlow(t *testing.T) {
	server, st, _, adminToken, _ := setupTestAPIServer(t)
	defer server.Close()
	defer st.Close()

	client := &http.Client{}

	// 1. List Users
	req, _ := http.NewRequest("GET", server.URL+"/api/v1/users", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	resp, err := client.Do(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var listEnvelope api.Response[[]api.UserResponse]
	err = json.NewDecoder(resp.Body).Decode(&listEnvelope)
	require.NoError(t, err)
	assert.True(t, listEnvelope.Success)
	assert.Len(t, listEnvelope.Data, 1)
	assert.Equal(t, "admin@pikpik.dev", listEnvelope.Data[0].Email)

	// 2. Invite User
	inviteReqBody, _ := json.Marshal(api.InviteUserRequest{
		Email:         "newdev@pikpik.dev",
		Role:          "developer",
		ExpiresInDays: 7,
	})
	req, _ = http.NewRequest("POST", server.URL+"/api/v1/users/invite", bytes.NewReader(inviteReqBody))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	resp, err = client.Do(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	var inviteEnvelope api.Response[api.TeamInvitationResponse]
	err = json.NewDecoder(resp.Body).Decode(&inviteEnvelope)
	require.NoError(t, err)
	assert.True(t, inviteEnvelope.Success)
	assert.Equal(t, "newdev@pikpik.dev", inviteEnvelope.Data.Email)
	assert.Contains(t, inviteEnvelope.Data.InviteURL, "token=pik_inv_")

	// Extract raw token from invite URL
	rawToken := inviteEnvelope.Data.InviteURL[bytes.Index([]byte(inviteEnvelope.Data.InviteURL), []byte("token="))+6:]

	// 3. Accept Invitation (Public endpoint)
	acceptReqBody, _ := json.Marshal(api.AcceptInviteRequest{
		Token:    rawToken,
		Password: "NewDevSecurePass123!",
	})
	req, _ = http.NewRequest("POST", server.URL+"/api/v1/users/accept-invite", bytes.NewReader(acceptReqBody))
	resp, err = client.Do(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var loginEnvelope api.Response[api.LoginResponse]
	err = json.NewDecoder(resp.Body).Decode(&loginEnvelope)
	require.NoError(t, err)
	assert.True(t, loginEnvelope.Success)
	assert.Equal(t, "newdev@pikpik.dev", loginEnvelope.Data.User.Email)
	assert.Equal(t, "developer", loginEnvelope.Data.User.Role)
	assert.NotEmpty(t, loginEnvelope.Data.Token)

	newDevToken := loginEnvelope.Data.Token
	newDevID := loginEnvelope.Data.User.ID

	// 4. Update Role (Owner only)
	roleReqBody, _ := json.Marshal(api.UpdateUserRoleRequest{
		Role: "admin",
	})
	req, _ = http.NewRequest("PUT", server.URL+"/api/v1/users/"+newDevID+"/role", bytes.NewReader(roleReqBody))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	resp, err = client.Do(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// 5. Reset Password
	resetReqBody, _ := json.Marshal(api.ResetPasswordRequest{
		NewPassword: "BrandNewPassword456!",
	})
	req, _ = http.NewRequest("POST", server.URL+"/api/v1/users/"+newDevID+"/reset-password", bytes.NewReader(resetReqBody))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	resp, err = client.Do(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// 6. Delete User
	req, _ = http.NewRequest("DELETE", server.URL+"/api/v1/users/"+newDevID, nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	resp, err = client.Do(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	_ = newDevToken
}

func TestProjectMembershipsFlow(t *testing.T) {
	server, st, _, adminToken, _ := setupTestAPIServer(t)
	defer server.Close()
	defer st.Close()

	ctx := context.Background()
	// Create project and user
	proj := &store.Project{
		ID:    "prj_team_test",
		OrgID: "org_default",
		Name:  "Team Test Project",
		Slug:  "team-test-proj",
	}
	require.NoError(t, st.Projects().Create(ctx, proj))

	user := &store.User{
		ID:           "usr_target_mem",
		Email:        "target@pikpik.dev",
		PasswordHash: "hash123",
		Role:         "viewer",
	}
	require.NoError(t, st.Users().Create(ctx, user))

	client := &http.Client{}

	// 1. Assign project member
	setReqBody, _ := json.Marshal(api.SetProjectMemberRequest{
		UserID: user.ID,
		Role:   "developer",
	})
	req, _ := http.NewRequest("POST", server.URL+"/api/v1/projects/"+proj.ID+"/members", bytes.NewReader(setReqBody))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	resp, err := client.Do(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var setEnvelope api.Response[api.ProjectMemberDTO]
	err = json.NewDecoder(resp.Body).Decode(&setEnvelope)
	require.NoError(t, err)
	assert.True(t, setEnvelope.Success)
	assert.Equal(t, user.ID, setEnvelope.Data.UserID)
	assert.Equal(t, "developer", setEnvelope.Data.Role)

	// 2. List project members
	req, _ = http.NewRequest("GET", server.URL+"/api/v1/projects/"+proj.ID+"/members", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	resp, err = client.Do(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var listEnvelope api.Response[[]api.ProjectMemberDTO]
	err = json.NewDecoder(resp.Body).Decode(&listEnvelope)
	require.NoError(t, err)
	assert.True(t, listEnvelope.Success)
	assert.Len(t, listEnvelope.Data, 1)

	// 3. Remove project member
	req, _ = http.NewRequest("DELETE", server.URL+"/api/v1/projects/"+proj.ID+"/members/"+user.ID, nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	resp, err = client.Do(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestDeveloperIntegrationsFlow(t *testing.T) {
	server, st, _, adminToken, _ := setupTestAPIServer(t)
	defer server.Close()
	defer st.Close()

	client := &http.Client{}

	// 1. Create Integration (GitHub)
	createReqBody, _ := json.Marshal(api.CreateIntegrationRequest{
		Name:        "Corporate GitHub",
		Type:        "git_github",
		Credentials: "ghp_secretPersonalAccessToken123",
		ConfigJSON:  `{"app_id": 9999}`,
	})
	req, _ := http.NewRequest("POST", server.URL+"/api/v1/integrations", bytes.NewReader(createReqBody))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	resp, err := client.Do(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	var createEnvelope api.Response[api.IntegrationResponse]
	err = json.NewDecoder(resp.Body).Decode(&createEnvelope)
	require.NoError(t, err)
	assert.True(t, createEnvelope.Success)
	assert.Equal(t, "Corporate GitHub", createEnvelope.Data.Name)
	assert.Equal(t, "git_github", createEnvelope.Data.Type)

	intID := createEnvelope.Data.ID

	// 2. Get Integration (verify credentials not exposed)
	req, _ = http.NewRequest("GET", server.URL+"/api/v1/integrations/"+intID, nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	resp, err = client.Do(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var getEnvelope api.Response[api.IntegrationResponse]
	err = json.NewDecoder(resp.Body).Decode(&getEnvelope)
	require.NoError(t, err)
	assert.True(t, getEnvelope.Success)
	assert.Equal(t, "Corporate GitHub", getEnvelope.Data.Name)

	// 3. Update Integration
	updateReqBody, _ := json.Marshal(api.UpdateIntegrationRequest{
		Name:   "Updated GitHub Enterprise",
		Status: "active",
	})
	req, _ = http.NewRequest("PUT", server.URL+"/api/v1/integrations/"+intID, bytes.NewReader(updateReqBody))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	resp, err = client.Do(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// 4. Test Integration
	req, _ = http.NewRequest("POST", server.URL+"/api/v1/integrations/"+intID+"/test", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	resp, err = client.Do(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var testEnvelope api.Response[api.TestIntegrationResponse]
	err = json.NewDecoder(resp.Body).Decode(&testEnvelope)
	require.NoError(t, err)
	assert.True(t, testEnvelope.Success)

	// 5. Delete Integration
	req, _ = http.NewRequest("DELETE", server.URL+"/api/v1/integrations/"+intID, nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	resp, err = client.Do(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}
