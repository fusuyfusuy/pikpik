package registry_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fusuycorp/pikpik/pkg/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

func TestRegistryConfig_LocalGeneration(t *testing.T) {
	cfg := registry.RegistryConfig{
		Enabled:        true,
		Domain:         "registry.yourdomain.com",
		StorageBackend: registry.StorageBackendLocal,
		LocalVolume:    "pikpik_vol_sys_registry_data",
		InternalPort:   5000,
	}

	yamlStr, err := registry.GenerateConfigYAML(cfg)
	require.NoError(t, err)
	assert.Contains(t, yamlStr, "rootdirectory: /var/lib/registry")
	assert.Contains(t, yamlStr, "addr: :5000")
	assert.Contains(t, yamlStr, "realm: pikpik-registry")
	assert.Contains(t, yamlStr, "blobdescriptor: inmemory")
	assert.Contains(t, yamlStr, "enabled: true")
}

func TestRegistryConfig_S3Generation(t *testing.T) {
	cfg := registry.RegistryConfig{
		Enabled:        true,
		Domain:         "registry.yourdomain.com",
		StorageBackend: registry.StorageBackendS3,
		InternalPort:   5000,
		S3Config: &registry.S3StorageConfig{
			AccessKey: "test-access-key",
			SecretKey: "test-secret-key",
			Region:    "us-east-1",
			Endpoint:  "https://r2.cloudflarestorage.com",
			Bucket:    "pikpik-registry-bucket",
			Secure:    true,
			V4Auth:    true,
		},
	}

	yamlStr, err := registry.GenerateConfigYAML(cfg)
	require.NoError(t, err)
	assert.Contains(t, yamlStr, "accesskey: test-access-key")
	assert.Contains(t, yamlStr, "secretkey: test-secret-key")
	assert.Contains(t, yamlStr, "region: us-east-1")
	assert.Contains(t, yamlStr, "regionendpoint: https://r2.cloudflarestorage.com")
	assert.Contains(t, yamlStr, "bucket: pikpik-registry-bucket")
	assert.Contains(t, yamlStr, "chunksize: 5242880")
	assert.Contains(t, yamlStr, "v4auth: true")
}

func TestRegistryConfig_Validation(t *testing.T) {
	// Missing S3Config when S3 backend is specified
	cfg := registry.RegistryConfig{
		Enabled:        true,
		StorageBackend: registry.StorageBackendS3,
	}
	_, err := registry.GenerateConfigYAML(cfg)
	assert.ErrorIs(t, err, registry.ErrInvalidRegistryConfig)

	// Missing S3 AccessKey
	cfg.S3Config = &registry.S3StorageConfig{
		SecretKey: "secret",
		Bucket:    "bucket",
	}
	_, err = registry.GenerateConfigYAML(cfg)
	assert.ErrorIs(t, err, registry.ErrInvalidRegistryConfig)

	// Unsupported backend
	cfg.StorageBackend = registry.StorageBackendType("azure_blob")
	_, err = registry.GenerateConfigYAML(cfg)
	assert.ErrorIs(t, err, registry.ErrInvalidRegistryConfig)
}

func TestRobotCredential_GenerationAndAuth(t *testing.T) {
	cred, err := registry.GenerateRobotCredential("proj_alpha", "CI deploy token")
	require.NoError(t, err)
	require.NotNil(t, cred)

	assert.True(t, strings.HasPrefix(cred.SecretToken, "pik_reg_"))
	assert.Equal(t, "pikpik-robot-proj_alpha", cred.Username)
	assert.Equal(t, "proj_alpha", cred.ProjectID)

	// Verify bcrypt hash matches the secret token
	err = bcrypt.CompareHashAndPassword([]byte(cred.BcryptHash), []byte(cred.SecretToken))
	assert.NoError(t, err)

	// Test htpasswd rendering and parsing
	htContent := registry.RenderHtpasswd([]registry.RobotCredential{*cred})
	assert.Contains(t, htContent, cred.Username+":"+cred.BcryptHash)

	entries := registry.ParseHtpasswd(htContent)
	assert.Equal(t, cred.BcryptHash, entries[cred.Username])

	// Verify Credential helper
	assert.True(t, registry.VerifyCredential(cred.Username, cred.SecretToken, htContent))
	assert.False(t, registry.VerifyCredential(cred.Username, "wrong_secret", htContent))
	assert.False(t, registry.VerifyCredential("unknown_user", cred.SecretToken, htContent))
}

func TestRobotCredential_GlobalProject(t *testing.T) {
	cred, err := registry.GenerateRobotCredential("global", "Global CI token")
	require.NoError(t, err)
	assert.Equal(t, "pikpik-ci-global", cred.Username)
}

func TestRegistryManager_RobotLifecycle(t *testing.T) {
	ctx := context.Background()
	mgr := registry.NewRegistryManager(nil, "", "")

	// 1. Create robot account
	cred1, err := mgr.CreateRobotAccount(ctx, "proj_1", "Project 1 CI")
	require.NoError(t, err)
	assert.NotEmpty(t, cred1.SecretToken)
	assert.Equal(t, "pikpik-robot-proj_1", cred1.Username)

	// 2. Reject duplicate robot for same project
	_, err = mgr.CreateRobotAccount(ctx, "proj_1", "Duplicate")
	assert.ErrorIs(t, err, registry.ErrDuplicateRobotAccount)

	// 3. List robot accounts (secretToken must be masked)
	list, err := mgr.ListRobotAccounts(ctx, "proj_1")
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, cred1.ID, list[0].ID)
	assert.Empty(t, list[0].SecretToken, "SecretToken should not be returned in list")

	// 4. Revoke robot account
	err = mgr.RevokeRobotAccount(ctx, cred1.ID)
	require.NoError(t, err)

	// 5. Revoking non-existent returns ErrRobotAccountNotFound
	err = mgr.RevokeRobotAccount(ctx, "non_existent_id")
	assert.ErrorIs(t, err, registry.ErrRobotAccountNotFound)

	// List should now be empty
	list, err = mgr.ListRobotAccounts(ctx, "proj_1")
	require.NoError(t, err)
	assert.Empty(t, list)
}

func TestRegistryManager_ReconcileMock(t *testing.T) {
	ctx := context.Background()
	mgr := registry.NewRegistryManager(nil, "", "")

	cfg := registry.RegistryConfig{
		Enabled:        true,
		Domain:         "registry.example.com",
		StorageBackend: registry.StorageBackendLocal,
	}

	status, err := mgr.Reconcile(ctx, cfg)
	require.NoError(t, err)
	assert.True(t, status.IsRunning)
	assert.NotEmpty(t, status.ContainerID)

	// Disable
	cfg.Enabled = false
	status, err = mgr.Reconcile(ctx, cfg)
	require.NoError(t, err)
	assert.False(t, status.IsRunning)
}

func TestRegistryManager_FilePermissions0600(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yml")
	htpasswdPath := filepath.Join(tempDir, "htpasswd")

	mgr := registry.NewRegistryManager(nil, configPath, htpasswdPath)

	cfg := registry.RegistryConfig{
		Enabled:        true,
		Domain:         "registry.example.com",
		StorageBackend: registry.StorageBackendLocal,
		InternalPort:   5000,
	}

	_, err := mgr.Reconcile(ctx, cfg)
	require.NoError(t, err)

	// Check config.yml permissions
	info, err := os.Stat(configPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm(), "config.yml should have 0600 permissions")

	// Check htpasswd permissions on creation
	infoHt, err := os.Stat(htpasswdPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), infoHt.Mode().Perm(), "htpasswd should have 0600 permissions")

	// Create robot account which calls syncHtpasswdLocked
	_, err = mgr.CreateRobotAccount(ctx, "proj_perm_test", "Permission test robot")
	require.NoError(t, err)

	infoHtAfter, err := os.Stat(htpasswdPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), infoHtAfter.Mode().Perm(), "htpasswd after robot creation should retain 0600 permissions")
}

