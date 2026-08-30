package main

import (
	"context"
	"flag"
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
	"github.com/docker/docker/client"
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
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Printf("pikpik unified control plane v%s\n", Version)
		os.Exit(0)
	}

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
	_ = os.MkdirAll(dataDir, 0755)

	fs := flag.NewFlagSet("pikpik", flag.ContinueOnError)
	fs.StringVar(&cfg.ListenAddr, "listen", getEnvOrDefault("PIKPIK_LISTEN_ADDR", ":8080"), "HTTP listen address")
	fs.StringVar(&cfg.DBPath, "db", getEnvOrDefault("PIKPIK_DB_PATH", filepath.Join(dataDir, "pikpik.db")), "SQLite state database path")
	fs.StringVar(&cfg.DockerSocket, "docker-socket", getEnvOrDefault("PIKPIK_DOCKER_SOCKET", "/var/run/docker.sock"), "Docker Engine Unix domain socket")
	fs.StringVar(&cfg.CaddyAdminURL, "caddy-admin-url", getEnvOrDefault("PIKPIK_CADDY_ADMIN_URL", "http://127.0.0.1:2019"), "Caddy Admin API URL")
	fs.StringVar(&cfg.EnrollmentToken, "enrollment-token", getEnvOrDefault("PIKPIK_ENROLLMENT_TOKEN", "pik_node_enrollment_secret_token"), "Worker node agent enrollment token")
	fs.StringVar(&cfg.AdminEmail, "admin-email", getEnvOrDefault("PIKPIK_ADMIN_EMAIL", "admin@pikpik.local"), "Initial bootstrap owner email")
	fs.StringVar(&cfg.AdminPassword, "admin-password", getEnvOrDefault("PIKPIK_ADMIN_PASSWORD", "pikpikAdmin123!"), "Initial bootstrap owner password")
	cfg.DataDir = dataDir

	_ = fs.Parse(args)

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
	vault, _ := crypto.NewAESVault("pikpik_system_master_secret_32b!")
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

	// 5. Backup Engine
	s3Cli, _ := s3.NewClient(s3.ClientOptions{
		Bucket:   "pikpik-backups",
		Provider: s3.ProviderAWS,
	})
	var execRunner backup.DockerExecRunner
	if rawDockerCli != nil {
		execRunner = backup.NewSocketDockerExecRunner(rawDockerCli)
	}
	backupEng := backup.NewBackupEngine(s3Cli, execRunner)

	// 6. Registry Manager
	htpasswdPath := filepath.Join(cfg.DataDir, "htpasswd")
	regConfigPath := filepath.Join(cfg.DataDir, "registry_config.yml")
	regMgr := registry.NewRegistryManager(rawDockerCli, htpasswdPath, regConfigPath)

	// 7. Telemetry & API WebSocket Hubs
	apiWSHub := api.NewWebSocketHub()
	go apiWSHub.Run(ctx)

	telemetryWSHub := telemetry.NewWebSocketHub()
	ringBuffers := make(map[string]telemetry.RingBuffer)

	// 8. Agent Server (Worker Node Multiplexer with Telemetry Bridge)
	agentServer := agent.NewAgentServer(agent.AgentServerOptions{
		EnrollmentToken: cfg.EnrollmentToken,
		WebSocketHub:    telemetryWSHub,
		RingBuffers:     ringBuffers,
		OnTelemetry: func(nodeID string, msg *telemetry.StreamMessage) {
			if msg != nil {
				apiWSHub.Broadcast(api.WSMessage{
					Channel:  msg.Channel,
					TargetID: msg.TargetID,
					Event:    msg.Type,
					Data:     msg.Payload,
					Time:     time.Unix(msg.Timestamp, 0).UTC(),
				})
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
				for key, buf := range ringBuffers {
					parts := strings.SplitN(key, ":", 2)
					if len(parts) == 2 {
						_ = downsampler.DownsampleAndSave(ctx, parts[0], parts[1], buf, hourStart)
					}
				}
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
		WSHub:          apiWSHub,
	})

	rateLimiter := api.NewRateLimiter(600, time.Minute)

	gateway := api.NewAPIGatewayWithOptions(api.APIGatewayOptions{
		Controller:    ctrl,
		Store:         st,
		AuthService:   authSvc,
		WebSocketHub:  apiWSHub,
		DeployWebhook: nudgeHandler,
		RateLimiter:   rateLimiter,
		DockerClient:  rawDockerCli,
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

	// Mount API Gateway & WebSockets
	rootMux.Handle("/", gateway)

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
