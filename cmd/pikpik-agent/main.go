package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/fusuycorp/pikpik/pkg/agent"
	"github.com/fusuycorp/pikpik/pkg/telemetry"
	flag "github.com/spf13/pflag"
)

var (
	// Version is injected during compilation via -ldflags="-X main.Version=1.0.0"
	Version = "1.0.0"
)

func main() {
	cfg := parseFlagsAndEnv(os.Args[1:])

	if cfg.ControlPlaneURL == "" {
		log.Println("WARNING: PIKPIK_CONTROL_PLANE_URL is not set. Running agent in standalone offline mode.")
	}

	log.Printf("Starting pikpik-agent v%s (Node: %s, Role: %s)", Version, cfg.NodeID, cfg.NodeRole)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)

	procReader := telemetry.NewProcReaderWithRoot(cfg.ProcRoot)
	dockerCollector := telemetry.NewDockerCollector(cfg.DockerSocket, cfg.NodeID)
	dispatcher := agent.NewCommandDispatcher(cfg.DockerSocket)

	client, err := agent.NewAgentClient(cfg, procReader, dockerCollector, dispatcher)
	if err != nil {
		log.Fatalf("Failed to initialize agent client: %v", err)
	}

	if cfg.ControlPlaneURL != "" {
		if err := client.Start(ctx); err != nil {
			log.Fatalf("Failed to start agent client loop: %v", err)
		}
	}

	// Wait for OS termination signal
	sig := <-sigChan
	log.Printf("Received signal %s, initiating graceful shutdown...", sig)

	cancel()
	_ = client.Close()
	log.Println("pikpik-agent stopped cleanly.")
}

func parseFlagsAndEnv(args []string) agent.AgentConfig {
	cfg := agent.DefaultAgentConfig()

	fs := flag.NewFlagSet("pikpik-agent", flag.ContinueOnError)

	var configFile string
	var hostSec, containerSec int
	var insecure, showVersion bool

	fs.StringVarP(&configFile, "config", "c", os.Getenv("PIKPIK_CONFIG_FILE"), "Path to agent.env configuration file")
	fs.StringVarP(&cfg.NodeID, "node-id", "i", os.Getenv("PIKPIK_NODE_ID"), "Unique worker node identifier")
	fs.StringVarP(&cfg.NodeName, "node-name", "n", os.Getenv("PIKPIK_NODE_NAME"), "Node hostname or friendly label")
	fs.StringVarP(&cfg.NodeRole, "node-role", "r", getEnvOrDefault("PIKPIK_NODE_ROLE", "worker"), "Node role (e.g. worker, manager)")
	fs.StringVarP(&cfg.ControlPlaneURL, "control-plane-url", "u", os.Getenv("PIKPIK_CONTROL_PLANE_URL"), "Control plane WebSocket endpoint (e.g. wss://cp.example.com/agent/connect)")
	fs.StringVarP(&cfg.EnrollmentToken, "token", "t", os.Getenv("PIKPIK_ENROLLMENT_TOKEN"), "Worker node authentication token")
	fs.StringVar(&cfg.TLSCertFile, "tls-cert", os.Getenv("PIKPIK_TLS_CERT_FILE"), "Client TLS certificate file path")
	fs.StringVar(&cfg.TLSKeyFile, "tls-key", os.Getenv("PIKPIK_TLS_KEY_FILE"), "Client TLS private key file path")
	fs.StringVar(&cfg.TLSCAFile, "tls-ca", os.Getenv("PIKPIK_TLS_CA_FILE"), "Control plane CA certificate file path")
	fs.BoolVarP(&insecure, "insecure-skip-verify", "k", os.Getenv("PIKPIK_INSECURE_SKIP_VERIFY") == "true", "Skip TLS certificate verification")
	fs.IntVar(&hostSec, "host-interval", 5, "Host metric collection cadence in seconds")
	fs.IntVar(&containerSec, "container-interval", 10, "Container stats collection cadence in seconds")
	fs.StringVarP(&cfg.DockerSocket, "docker-socket", "s", getEnvOrDefault("PIKPIK_DOCKER_SOCKET", "/var/run/docker.sock"), "Docker daemon Unix domain socket")
	fs.StringVar(&cfg.ProcRoot, "proc-root", getEnvOrDefault("PIKPIK_PROC_ROOT", "/proc"), "Linux /proc filesystem root path")
	fs.BoolVarP(&showVersion, "version", "v", false, "Display agent version")

	// Skip first arg if it is "run" or "start"
	filteredArgs := args
	if len(filteredArgs) > 0 && (filteredArgs[0] == "run" || filteredArgs[0] == "start") {
		filteredArgs = filteredArgs[1:]
	}
	_ = fs.Parse(filteredArgs)

	if showVersion || (len(args) > 0 && args[0] == "version") {
		fmt.Printf("pikpik-agent version %s\n", Version)
		os.Exit(0)
	}

	// Load .env file if specified
	if configFile != "" {
		loadEnvFile(configFile, &cfg)
	}

	if envHost := os.Getenv("PIKPIK_HOST_METRIC_INTERVAL_SEC"); envHost != "" {
		if v, err := strconv.Atoi(envHost); err == nil && v > 0 {
			hostSec = v
		}
	}
	if envContainer := os.Getenv("PIKPIK_CONTAINER_METRIC_INTERVAL_SEC"); envContainer != "" {
		if v, err := strconv.Atoi(envContainer); err == nil && v > 0 {
			containerSec = v
		}
	}
	if envDocker := os.Getenv("PIKPIK_DOCKER_SOCKET"); envDocker != "" {
		cfg.DockerSocket = envDocker
	}

	cfg.InsecureSkipVerify = insecure
	cfg.HostMetricInterval = time.Duration(hostSec) * time.Second
	cfg.ContainerMetricInterval = time.Duration(containerSec) * time.Second

	if cfg.NodeID == "" {
		hostname, _ := os.Hostname()
		if hostname != "" {
			cfg.NodeID = "node_" + hostname
		} else {
			cfg.NodeID = "node_worker_" + fmt.Sprintf("%x", time.Now().UnixNano()%100000)
		}
	}
	if cfg.NodeName == "" {
		hostname, _ := os.Hostname()
		cfg.NodeName = hostname
	}
	if cfg.NodeRole == "" {
		cfg.NodeRole = "worker"
	}

	return cfg
}

func loadEnvFile(path string, cfg *agent.AgentConfig) {
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		val = strings.Trim(val, `"'`)

		switch key {
		case "PIKPIK_NODE_ID":
			cfg.NodeID = val
		case "PIKPIK_NODE_NAME":
			cfg.NodeName = val
		case "PIKPIK_NODE_ROLE":
			cfg.NodeRole = val
		case "PIKPIK_CONTROL_PLANE_URL":
			cfg.ControlPlaneURL = val
		case "PIKPIK_ENROLLMENT_TOKEN":
			cfg.EnrollmentToken = val
		case "PIKPIK_TLS_CERT_FILE":
			cfg.TLSCertFile = val
		case "PIKPIK_TLS_KEY_FILE":
			cfg.TLSKeyFile = val
		case "PIKPIK_TLS_CA_FILE":
			cfg.TLSCAFile = val
		case "PIKPIK_INSECURE_SKIP_VERIFY":
			cfg.InsecureSkipVerify = val == "true" || val == "1"
		case "PIKPIK_DOCKER_SOCKET":
			cfg.DockerSocket = val
		case "PIKPIK_HOST_METRIC_INTERVAL_SEC":
			if v, err := strconv.Atoi(val); err == nil && v > 0 {
				cfg.HostMetricInterval = time.Duration(v) * time.Second
			}
		case "PIKPIK_CONTAINER_METRIC_INTERVAL_SEC":
			if v, err := strconv.Atoi(val); err == nil && v > 0 {
				cfg.ContainerMetricInterval = time.Duration(v) * time.Second
			}
		}
	}
}

func getEnvOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
