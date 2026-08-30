package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadEnvFile(t *testing.T) {
	tmpDir := t.TempDir()
	envPath := filepath.Join(tmpDir, "agent.env")

	envContent := `# Agent Configuration
PIKPIK_NODE_ID=node_test_env_01
PIKPIK_NODE_NAME=test-worker
PIKPIK_NODE_ROLE=worker
PIKPIK_CONTROL_PLANE_URL=wss://cp.example.com/agent/v1/connect
PIKPIK_ENROLLMENT_TOKEN=test_token_secret
PIKPIK_TLS_CERT_FILE=/etc/certs/agent.crt
PIKPIK_TLS_KEY_FILE=/etc/certs/agent.key
PIKPIK_TLS_CA_FILE=/etc/certs/ca.crt
PIKPIK_INSECURE_SKIP_VERIFY=true
PIKPIK_HOST_METRIC_INTERVAL_SEC=3
PIKPIK_CONTAINER_METRIC_INTERVAL_SEC=7
PIKPIK_DOCKER_SOCKET=/custom/docker.sock
`
	err := os.WriteFile(envPath, []byte(envContent), 0644)
	require.NoError(t, err)

	cfg := parseFlagsAndEnv([]string{})
	loadEnvFile(envPath, &cfg)

	assert.Equal(t, "node_test_env_01", cfg.NodeID)
	assert.Equal(t, "test-worker", cfg.NodeName)
	assert.Equal(t, "worker", cfg.NodeRole)
	assert.Equal(t, "wss://cp.example.com/agent/v1/connect", cfg.ControlPlaneURL)
	assert.Equal(t, "test_token_secret", cfg.EnrollmentToken)
	assert.Equal(t, "/etc/certs/agent.crt", cfg.TLSCertFile)
	assert.Equal(t, "/etc/certs/agent.key", cfg.TLSKeyFile)
	assert.Equal(t, "/etc/certs/ca.crt", cfg.TLSCAFile)
	assert.True(t, cfg.InsecureSkipVerify)
	assert.Equal(t, 3*time.Second, cfg.HostMetricInterval)
	assert.Equal(t, 7*time.Second, cfg.ContainerMetricInterval)
	assert.Equal(t, "/custom/docker.sock", cfg.DockerSocket)
}
