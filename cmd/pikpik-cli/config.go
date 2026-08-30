package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Config represents the multi-context configuration schema stored in ~/.pikpik/config.json
type Config struct {
	Schema         string             `json:"$schema,omitempty"`
	Version        int                `json:"version"`
	CurrentContext string             `json:"current_context"`
	Contexts       map[string]Context `json:"contexts"`
}

// Context stores the connection and auth settings for a specific pikpik instance.
type Context struct {
	ServerURL      string `json:"server_url"`
	Token          string `json:"token"`
	TLSSkipVerify  bool   `json:"tls_skip_verify"`
	DefaultProject string `json:"default_project,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

// ConfigManager handles atomic, thread-safe configuration file persistence.
type ConfigManager struct {
	configPath string
}

// NewConfigManager initializes the ConfigManager targeting ~/.pikpik/config.json.
func NewConfigManager() (*ConfigManager, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get user home directory: %w", err)
	}
	dir := filepath.Join(home, ".pikpik")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create ~/.pikpik directory: %w", err)
	}
	return &ConfigManager{configPath: filepath.Join(dir, "config.json")}, nil
}

// NewConfigManagerWithPath creates a ConfigManager with an explicit custom file path (used for tests).
func NewConfigManagerWithPath(path string) (*ConfigManager, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}
	return &ConfigManager{configPath: path}, nil
}

// Load reads and parses ~/.pikpik/config.json. If the file does not exist, a default config is returned.
func (cm *ConfigManager) Load() (*Config, error) {
	if _, err := os.Stat(cm.configPath); os.IsNotExist(err) {
		return &Config{
			Schema:         "https://pikpik.dev/schemas/config.v1.json",
			Version:        1,
			CurrentContext: "default",
			Contexts:       make(map[string]Context),
		}, nil
	}

	data, err := os.ReadFile(cm.configPath)
	if err != nil {
		return nil, fmt.Errorf("reading config failed: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config failed: %w", err)
	}
	if cfg.Contexts == nil {
		cfg.Contexts = make(map[string]Context)
	}
	return &cfg, nil
}

// Save writes the Config atomically using a temporary file and os.Rename (POSIX 0600 mode).
func (cm *ConfigManager) Save(cfg *Config) error {
	if cfg.Schema == "" {
		cfg.Schema = "https://pikpik.dev/schemas/config.v1.json"
	}
	if cfg.Version == 0 {
		cfg.Version = 1
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("serializing config failed: %w", err)
	}

	tmpPath := fmt.Sprintf("%s.tmp.%d", cm.configPath, time.Now().UnixNano())
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return fmt.Errorf("writing tmp config failed: %w", err)
	}

	if err := os.Rename(tmpPath, cm.configPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("atomic rename failed: %w", err)
	}

	return nil
}

// GetActiveContext returns the active Context, or an error if none is configured.
func (cm *ConfigManager) GetActiveContext(cfg *Config) (*Context, string, error) {
	ctxName := cfg.CurrentContext
	if envCtx := os.Getenv("PIKPIK_CONTEXT"); envCtx != "" {
		ctxName = envCtx
	}
	if ctxName == "" {
		ctxName = "default"
	}

	// Environment variable overrides for server and token
	envServer := os.Getenv("PIKPIK_SERVER_URL")
	envToken := os.Getenv("PIKPIK_TOKEN")
	if envServer != "" {
		return &Context{
			ServerURL:      envServer,
			Token:          envToken,
			TimeoutSeconds: 30,
		}, "env", nil
	}

	ctx, exists := cfg.Contexts[ctxName]
	if !exists {
		return nil, ctxName, fmt.Errorf("context %q not found in ~/.pikpik/config.json. Run 'pikpik login <url>' first", ctxName)
	}
	return &ctx, ctxName, nil
}
