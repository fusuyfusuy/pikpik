package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/docker/docker/client"
	"github.com/fusuycorp/pikpik/pkg/agent"
	"github.com/fusuycorp/pikpik/pkg/api"
	"github.com/fusuycorp/pikpik/pkg/auth"
	"github.com/fusuycorp/pikpik/pkg/backup"
	"github.com/fusuycorp/pikpik/pkg/backup/s3"
	"github.com/fusuycorp/pikpik/pkg/config"
	"github.com/fusuycorp/pikpik/pkg/crypto"
	"github.com/fusuycorp/pikpik/pkg/deploy"
	"github.com/fusuycorp/pikpik/pkg/ingress"
	"github.com/fusuycorp/pikpik/pkg/orchestration"
	"github.com/fusuycorp/pikpik/pkg/registry"
	"github.com/fusuycorp/pikpik/pkg/store"
	"github.com/fusuycorp/pikpik/pkg/telemetry"
	flag "github.com/spf13/pflag"
)

var (
	// Version is injected during build via -ldflags="-X main.Version=1.0.0"
	Version = "1.0.0"
)

// ServerConfig holds the unified runtime startup configuration.
type ServerConfig struct {
	ListenAddr      string
	DBPath          string
	DockerSocket    string
	CaddyAdminURL   string
	EnrollmentToken string
	AdminEmail      string
	AdminPassword   string
	DataDir         string
	AllowedOrigins  []string
}

func main() {
	cfg := parseConfig(os.Args[1:])

	log.Printf("Starting pikpik Control Plane v%s [Invariant 2: Unified Runtime]", Version)
	log.Printf("Data Directory: %s | SQLite DB: %s", cfg.DataDir, cfg.DBPath)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)

	server, cleanup, err := setupUnifiedServer(ctx, cfg)
	if err != nil {
		log.Fatalf("Failed to initialize unified server runtime: %v", err)
	}
	defer cleanup()

	// Start HTTP Server
	errChan := make(chan error, 1)
	go func() {
		log.Printf("Control plane listening on http://%s", cfg.ListenAddr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errChan <- err
		}
	}()

	// Await signal or server error
	select {
	case sig := <-sigChan:
		log.Printf("Received termination signal %s, initiating graceful shutdown...", sig)
	case err := <-errChan:
		log.Fatalf("HTTP server failure: %v", err)
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server graceful shutdown error: %v", err)
	}
	log.Println("pikpik unified runtime stopped cleanly.")
}

func parseConfig(args []string) ServerConfig {
	var cfg ServerConfig

	dataDir := os.Getenv("PIKPIK_DATA_DIR")
	if dataDir == "" {
		dataDir = "./data"
	}

	var showVersion bool
	var corsOrigins string

	fs := flag.NewFlagSet("pikpik", flag.ContinueOnError)
	fs.StringVarP(&cfg.ListenAddr, "listen", "l", getEnvOrDefault("PIKPIK_LISTEN_ADDR", ":8080"), "HTTP listen address (e.g. :8080 or 127.0.0.1:8080)")
	fs.StringVarP(&cfg.DataDir, "data-dir", "d", dataDir, "Root directory for database and internal storage")
	fs.StringVar(&cfg.DBPath, "db", getEnvOrDefault("PIKPIK_DB_PATH", ""), "SQLite state database path (defaults to <data-dir>/pikpik.db)")
	fs.StringVarP(&cfg.DockerSocket, "docker-socket", "s", getEnvOrDefault("PIKPIK_DOCKER_SOCKET", "/var/run/docker.sock"), "Docker Engine Unix domain socket")
	fs.StringVarP(&cfg.CaddyAdminURL, "caddy-url", "c", getEnvOrDefault("PIKPIK_CADDY_ADMIN_URL", "http://127.0.0.1:2019"), "Caddy dynamic Admin REST API URL")
	fs.StringVarP(&cfg.EnrollmentToken, "token", "t", getEnvOrDefault("PIKPIK_ENROLLMENT_TOKEN", ""), "Worker node agent enrollment token (auto-generated on first boot if not set)")
	fs.StringVarP(&cfg.AdminEmail, "admin-email", "e", getEnvOrDefault("PIKPIK_ADMIN_EMAIL", "admin@pikpik.local"), "Initial bootstrap owner email")
	fs.StringVarP(&cfg.AdminPassword, "admin-password", "p", getEnvOrDefault("PIKPIK_ADMIN_PASSWORD", ""), "Initial bootstrap owner password (auto-generated on first boot if not set)")
	fs.StringVar(&corsOrigins, "cors-allowed-origins", getEnvOrDefault("PIKPIK_CORS_ALLOWED_ORIGINS", ""), "Comma-separated list of allowed CORS origins")
	fs.BoolVarP(&showVersion, "version", "v", false, "Display pikpik server version")

	_ = fs.Parse(args)

	if corsOrigins != "" {
		for _, o := range strings.Split(corsOrigins, ",") {
			if trimmed := strings.TrimSpace(o); trimmed != "" {
				cfg.AllowedOrigins = append(cfg.AllowedOrigins, trimmed)
			}
		}
	}

	if showVersion || (len(args) > 0 && args[0] == "version") {
		fmt.Printf("pikpik unified control plane v%s\n", Version)
		os.Exit(0)
	}

	// No operator-supplied admin password: generate a random one instead of a
	// guessable hardcoded default. Shown once so the operator can capture it.
	if cfg.AdminPassword == "" {
		generated, err := generateSecureSecret(18)
		if err != nil {
			log.Fatalf("failed to generate initial admin password: %v", err)
		}
		cfg.AdminPassword = generated
		log.Printf("PIKPIK_ADMIN_PASSWORD not set — generated initial admin password (save this, it will not be shown again): %s", generated)
	}

	// No operator-supplied enrollment token: generate a random one instead of
	// the well-known hardcoded default.
	if cfg.EnrollmentToken == "" {
		generated, err := generateSecureSecret(24)
		if err != nil {
			log.Fatalf("failed to generate node enrollment token: %v", err)
		}
		cfg.EnrollmentToken = generated
		log.Printf("PIKPIK_ENROLLMENT_TOKEN not set — generated node enrollment token (save this, it will not be shown again): %s", generated)
	}

	if cfg.DataDir == "" {
		cfg.DataDir = dataDir
	}
	_ = os.MkdirAll(cfg.DataDir, 0755)

	if cfg.DBPath == "" {
		cfg.DBPath = filepath.Join(cfg.DataDir, "pikpik.db")
	}

	// Support port environment variable override
	if portStr := os.Getenv("PIKPIK_PORT"); portStr != "" {
		if p, err := strconv.Atoi(portStr); err == nil && p > 0 {
			cfg.ListenAddr = fmt.Sprintf(":%d", p)
		}
	}

	return cfg
}

func setupUnifiedServer(ctx context.Context, cfg ServerConfig) (*http.Server, func(), error) {
	var cleanups []func()

	// 1. SQLite Store (WAL mode)
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open sqlite store: %w", err)
	}
	cleanups = append(cleanups, func() { _ = st.Close() })

	// 2. Argon2 Hasher, Crypto Vault, Config Manager & Auth Service
	hasher := crypto.DefaultArgon2Hasher()
	authSvc := auth.NewAuthService(st, hasher)
	masterKey, err := loadOrCreateMasterKey(cfg.DataDir)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load/create AES master key: %w", err)
	}
	vault, err := crypto.NewAESVault(masterKey)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize AES vault: %w", err)
	}
	configMgr := config.NewConfigManager(st, vault)

	// Bootstrap default admin owner if needed
	if cfg.AdminEmail != "" && cfg.AdminPassword != "" {
		_, _ = authSvc.BootstrapAdmin(ctx, cfg.AdminEmail, cfg.AdminPassword)
	}

	// 3. Orchestration Engine
	var orch orchestration.Orchestrator
	var rawDockerCli client.CommonAPIClient
	orchClient, orchErr := orchestration.NewDockerEngineClient(ctx, cfg.DockerSocket)
	if orchErr == nil {
		orch = orchClient
		rawDockerCli = orchClient.RawClient()
		cleanups = append(cleanups, func() { _ = orch.Close() })
	} else {
		log.Printf("Docker socket unreachable (%v), operating in standalone mock orchestration mode.", orchErr)
	}

	// 4. Ingress Manager
	caddyCli := ingress.NewCaddyClient(cfg.CaddyAdminURL, 5*time.Second)
	ingressMgr := ingress.NewIngressManager(caddyCli, nil)

	// 5. Backup Engine & Cron Scheduler
	s3Cli, _ := s3.NewClient(s3.ClientOptions{
		Bucket:   "pikpik-backups",
		Provider: s3.ProviderAWS,
	})
	var execRunner backup.DockerExecRunner
	if rawDockerCli != nil {
		execRunner = backup.NewSocketDockerExecRunner(rawDockerCli)
	}
	backupEng := backup.NewBackupEngine(s3Cli, execRunner)
	cronScheduler := backup.NewCronScheduler(st, backupEng, s3Cli)
	if err := cronScheduler.Start(ctx); err != nil {
		log.Printf("Warning: failed to start backup cron scheduler: %v", err)
	} else {
		cleanups = append(cleanups, func() { cronScheduler.Stop() })
	}

	// 6. Registry Manager
	htpasswdPath := filepath.Join(cfg.DataDir, "htpasswd")
	regConfigPath := filepath.Join(cfg.DataDir, "registry_config.yml")
	regMgr := registry.NewRegistryManager(rawDockerCli, htpasswdPath, regConfigPath)

	// 7. Telemetry & API WebSocket Hubs + SSE Broadcaster
	apiWSHub := api.NewWebSocketHub()
	go apiWSHub.Run(ctx)
	sseBroadcaster := api.NewSSEBroadcaster()

	telemetryWSHub := telemetry.NewWebSocketHub()
	ringBuffers := make(map[string]telemetry.RingBuffer)

	// 8. Agent Server (Worker Node Multiplexer with Telemetry Bridge)
	agentServer := agent.NewAgentServer(agent.AgentServerOptions{
		EnrollmentToken: cfg.EnrollmentToken,
		WebSocketHub:    telemetryWSHub,
		RingBuffers:     ringBuffers,
		MachineStore:    st.Machines(),
		OnTelemetry: func(nodeID string, msg *telemetry.StreamMessage) {
			if msg != nil {
				apiWSHub.Broadcast(api.WSMessage{
					Channel:  msg.Channel,
					TargetID: msg.TargetID,
					Event:    msg.Type,
					Data:     msg.Payload,
					Time:     time.Unix(msg.Timestamp, 0).UTC(),
				})
				sseBroadcaster.Broadcast(msg.Channel, msg.TargetID, msg.Type, msg.Payload)
			}
		},
	})

	// Background Downsampler Ticker (Hourly Rollup)
	downsampler := telemetry.NewDownsampler(st.DB())
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case t := <-ticker.C:
				hourStart := t.Truncate(time.Hour).Unix()
				for key, buf := range agentServer.RingBufferSnapshot() {
					parts := strings.SplitN(key, ":", 2)
					if len(parts) == 2 {
						_ = downsampler.DownsampleAndSave(ctx, parts[0], parts[1], buf, hourStart)
					}
				}
				// Prune rollup metrics older than 30 days
				_, _ = downsampler.PruneExpiredMetrics(ctx, 30*24*time.Hour)
			}
		}
	}()

	// 9. Deploy Webhook Handler
	nudgeHandler := deploy.NewDeployWebhookHandler(deploy.HandlerOptions{
		RateLimitPerMin: 60,
		BurstLimit:      10,
	})

	// 10. API Gateway
	ctrl := api.NewDefaultController(api.ControllerDependencies{
		Store:          st,
		AuthService:    authSvc,
		Orchestrator:   orch,
		IngressManager: ingressMgr,
		BackupEngine:   backupEng,
		Registry:       regMgr,
		ConfigManager:  configMgr,
		Vault:          vault,
		WSHub:          apiWSHub,
		SSEBroadcaster: sseBroadcaster,
	})

	rateLimiter := api.NewRateLimiter(600, time.Minute)

	gateway := api.NewAPIGatewayWithOptions(api.APIGatewayOptions{
		Controller:     ctrl,
		Store:          st,
		AuthService:    authSvc,
		WebSocketHub:   apiWSHub,
		SSEBroadcaster: sseBroadcaster,
		DeployWebhook:  nudgeHandler,
		RateLimiter:    rateLimiter,
		DockerClient:   rawDockerCli,
		EnableCors:     true,
		AllowedOrigins: cfg.AllowedOrigins,
	})

	// Unified HTTP Router
	rootMux := http.NewServeMux()

	// Mount Worker Node Agent Connection Endpoint
	if asHandler, ok := agentServer.(http.Handler); ok {
		rootMux.Handle("/agent/connect", asHandler)
		rootMux.Handle("/api/v1/agent/connect", asHandler)
	} else {
		rootMux.HandleFunc("/agent/connect", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
	}

	// Mount Ingress health check
	rootMux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"healthy","version":"` + Version + `"}`))
	})

	// 11. Embedded SPA Static Handler
	staticHandler := NewStaticHandler()

	// Mount API Gateway & WebSockets
	rootMux.Handle("/api/", gateway)
	rootMux.Handle("/ws", gateway)
	rootMux.Handle("/ws/", gateway)

	// Mount Embedded Frontend SPA
	rootMux.Handle("/", staticHandler)

	server := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           rootMux,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	cleanupFn := func() {
		for _, fn := range cleanups {
			fn()
		}
	}

	return server, cleanupFn, nil
}

func getEnvOrDefault(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

// generateSecureSecret returns a cryptographically random, URL-safe secret
// string derived from nBytes of entropy.
func generateSecureSecret(nBytes int) (string, error) {
	raw := make([]byte, nBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("failed to read random bytes: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// masterKeyFileName is the filename (relative to DataDir) that persists the
// generated AES master encryption key across restarts.
const masterKeyFileName = "master.key"

// loadOrCreateMasterKey resolves the AES vault master key with no hardcoded
// fallback: PIKPIK_MASTER_KEY takes precedence, then a previously persisted
// key file in dataDir, then a freshly generated 32-byte key that is
// persisted to that file (mode 0600) so it survives restarts.
func loadOrCreateMasterKey(dataDir string) (string, error) {
	if key := os.Getenv("PIKPIK_MASTER_KEY"); key != "" {
		return key, nil
	}

	keyPath := filepath.Join(dataDir, masterKeyFileName)
	if data, err := os.ReadFile(keyPath); err == nil {
		if key := strings.TrimSpace(string(data)); key != "" {
			return key, nil
		}
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("failed to read master key file %s: %w", keyPath, err)
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("failed to generate master key: %w", err)
	}
	key := hex.EncodeToString(raw)

	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create data directory %s: %w", dataDir, err)
	}
	if err := os.WriteFile(keyPath, []byte(key), 0600); err != nil {
		return "", fmt.Errorf("failed to persist master key to %s: %w", keyPath, err)
	}

	log.Printf("Generated new AES-256 master encryption key on first boot and persisted it to %s (mode 0600). Back this file up — losing it makes all encrypted secrets-at-rest unrecoverable.", keyPath)

	return key, nil
}
