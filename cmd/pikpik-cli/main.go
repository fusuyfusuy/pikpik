package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/fusuycorp/pikpik/pkg/api"
	"github.com/gorilla/websocket"
	flag "github.com/spf13/pflag"
	"golang.org/x/term"
)

var (
	// Version is injected during compilation via -ldflags="-X main.Version=1.0.0"
	Version = "1.0.0"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "version", "--version", "-v":
		fmt.Printf("pikpik-cli version %s\n", Version)
	case "help", "--help", "-h":
		printUsage()
	case "init":
		runInit(args)
	case "login":
		runLogin(args)
	case "nodes":
		runNodes(args)
	case "deploy":
		runDeploy(args)
	case "logs":
		runLogs(args)
	case "stats":
		runStats(args)
	case "db":
		runDB(args)
	case "prune":
		runPrune(args)
	case "exec":
		runExec(args)
	case "context":
		runContext(args)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\nRun 'pikpik help' for available commands.\n", cmd)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`pikpik - Minimalist Next-Gen PaaS Standalone CLI

Usage:
  pikpik <command> [arguments]

Commands:
  init          Initialize a new project with .pikpik.yml
  login         Authenticate against a pikpik control plane instance
  nodes         Inspect cluster nodes, allocation, and availability
  deploy        Trigger rolling deployment for active project or image
  logs          Stream aggregated real-time container logs
  stats         Display real-time CPU, RAM, Network and IO metrics
  db            Manage databases, snapshots, and S3 restores
  prune         Garbage collect unused images, containers, and volumes
  exec          Open an interactive PTY shell inside a container
  context       Manage CLI contexts and active cluster connections
  version       Display CLI version

Flags:
  Run 'pikpik <command> --help' for details on each subcommand.`)
}

func getClient() (*APIClient, *Config, *ConfigManager) {
	cm, err := NewConfigManager()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing config manager: %v\n", err)
		os.Exit(1)
	}

	cfg, err := cm.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading configuration: %v\n", err)
		os.Exit(1)
	}

	activeCtx, ctxName, err := cm.GetActiveContext(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if activeCtx.ServerURL == "" {
		fmt.Fprintf(os.Stderr, "Context %q has no server_url set. Run 'pikpik login <url>' first.\n", ctxName)
		os.Exit(1)
	}

	client := NewAPIClient(*activeCtx)
	return client, cfg, cm
}

// 1. pikpik init
func runInit(args []string) {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	name := fs.StringP("name", "n", "", "Application name")
	port := fs.IntP("port", "p", 8080, "Container HTTP port")
	domain := fs.StringP("domain", "d", "", "Custom domain binding")
	image := fs.StringP("image", "i", "", "Docker image name or registry path")
	_ = fs.Parse(args)

	appName := *name
	if appName == "" {
		dir, err := os.Getwd()
		if err == nil {
			appName = strings.ToLower(filepath.Base(dir))
		} else {
			appName = "my-app"
		}
	}

	img := *image
	if img == "" {
		img = appName + ":latest"
	}

	manifest := fmt.Sprintf(`version: 1
name: %s
image: %s
port: %d
replicas: 1
domains:
  - %s
env:
  NODE_ENV: production
`, appName, img, *port, *domain)

	filePath := ".pikpik.yml"
	if _, err := os.Stat(filePath); err == nil {
		fmt.Printf(".pikpik.yml already exists in current directory.\n")
		return
	}

	if err := os.WriteFile(filePath, []byte(manifest), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to write .pikpik.yml: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Initialized %s successfully.\n", filePath)
}

// 2. pikpik login
func runLogin(args []string) {
	fs := flag.NewFlagSet("login", flag.ExitOnError)
	token := fs.StringP("token", "t", "", "API Token (pik_live_...) for non-interactive login")
	ctxName := fs.StringP("context", "c", "", "Context name (default: default)")
	insecure := fs.BoolP("insecure", "k", false, "Skip TLS certificate verification")
	_ = fs.Parse(args)

	positional := fs.Args()
	if len(positional) == 0 {
		fmt.Println("Usage: pikpik login <server-url> [-t|--token <token>] [-c|--context <name>] [-k|--insecure]")
		os.Exit(1)
	}

	serverURL := strings.TrimRight(positional[0], "/")
	if !strings.HasPrefix(serverURL, "http://") && !strings.HasPrefix(serverURL, "https://") {
		serverURL = "https://" + serverURL
	}

	contextKey := *ctxName
	if contextKey == "" {
		contextKey = "default"
	}

	cm, err := NewConfigManager()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Config init failed: %v\n", err)
		os.Exit(1)
	}

	cfg, _ := cm.Load()

	finalToken := *token
	if finalToken == "" {
		// Interactive password prompt
		reader := bufio.NewReader(os.Stdin)
		fmt.Print("Email: ")
		email, _ := reader.ReadString('\n')
		email = strings.TrimSpace(email)

		fmt.Print("Password: ")
		passBytes, err := term.ReadPassword(int(syscall.Stdin))
		fmt.Println()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Password read error: %v\n", err)
			os.Exit(1)
		}
		password := strings.TrimSpace(string(passBytes))

		client := NewAPIClient(Context{
			ServerURL:     serverURL,
			TLSSkipVerify: *insecure,
		})

		loginResp, err := client.Login(context.Background(), email, password)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Login failed: %v\n", err)
			os.Exit(1)
		}
		finalToken = loginResp.Token
		fmt.Printf("Authenticated successfully as %s.\n", loginResp.User.Email)
	} else {
		// Verify provided token
		client := NewAPIClient(Context{
			ServerURL:     serverURL,
			Token:         finalToken,
			TLSSkipVerify: *insecure,
		})
		_, err := client.GetMe(context.Background())
		if err != nil {
			fmt.Printf("Warning: Token verification returned: %v\n", err)
		} else {
			fmt.Println("API token verified successfully.")
		}
	}

	cfg.CurrentContext = contextKey
	cfg.Contexts[contextKey] = Context{
		ServerURL:      serverURL,
		Token:          finalToken,
		TLSSkipVerify:  *insecure,
		TimeoutSeconds: 30,
	}

	if err := cm.Save(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to save configuration: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Saved context %q (%s) to ~/.pikpik/config.json\n", contextKey, serverURL)
}

// 3. pikpik nodes
func runNodes(args []string) {
	if len(args) > 0 {
		sub := args[0]
		switch sub {
		case "drain":
			if len(args) < 2 {
				fmt.Println("Usage: pikpik nodes drain <node_id>")
				os.Exit(1)
			}
			client, _, _ := getClient()
			nodeID := args[1]
			if err := client.DrainNode(context.Background(), nodeID); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to drain node %s: %v\n", nodeID, err)
				os.Exit(1)
			}
			fmt.Printf("Node %s marked as drained.\n", nodeID)
			return
		case "join-tokens":
			client, _, _ := getClient()
			tokens, err := client.GetJoinTokens(context.Background())
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to get join tokens: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("Manager Join Token:\n  %s\n\nWorker Join Token:\n  %s\n", tokens.Manager, tokens.Worker)
			return
		}
	}

	client, _, _ := getClient()
	nodes, err := client.ListNodes(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to list nodes: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("%-16s %-18s %-10s %-8s %-12s %-8s %s\n", "ID", "HOSTNAME", "ROLE", "STATUS", "AVAILABILITY", "ENGINE", "LEADER")
	for _, n := range nodes {
		leaderMark := ""
		if n.Leader {
			leaderMark = "★ Yes"
		}
		fmt.Printf("%-16s %-18s %-10s %-8s %-12s %-8s %s\n",
			n.ID, n.Hostname, n.Role, n.Status, n.Availability, n.EngineVer, leaderMark)
	}
}

// 4. pikpik deploy
func runDeploy(args []string) {
	fs := flag.NewFlagSet("deploy", flag.ExitOnError)
	appFlag := fs.StringP("app", "a", "", "Application ID or name")
	imageFlag := fs.StringP("image", "i", "", "Container image tag to deploy")
	_ = fs.Parse(args)

	client, _, _ := getClient()

	targetApp := *appFlag
	if targetApp == "" {
		// Read from .pikpik.yml if present
		if data, err := os.ReadFile(".pikpik.yml"); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				if strings.HasPrefix(strings.TrimSpace(line), "name:") {
					targetApp = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "name:"))
					break
				}
			}
		}
	}

	if targetApp == "" {
		fmt.Println("Usage: pikpik deploy [-a|--app <app_name>] [-i|--image <image_tag>]")
		os.Exit(1)
	}

	fmt.Printf("Triggering rolling deployment for %q (image: %s)...\n", targetApp, *imageFlag)
	if err := client.DeployApp(context.Background(), targetApp, *imageFlag); err != nil {
		fmt.Fprintf(os.Stderr, "Deployment failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Deployment for %q dispatched successfully.\n", targetApp)
}

// 5. pikpik logs
func runLogs(args []string) {
	fs := flag.NewFlagSet("logs", flag.ExitOnError)
	follow := fs.BoolP("follow", "f", false, "Follow log stream")
	tail := fs.IntP("tail", "n", 100, "Number of historical lines")
	timestamps := fs.BoolP("timestamps", "t", false, "Show log timestamps")
	_ = fs.Parse(args)

	positional := fs.Args()
	if len(positional) == 0 {
		fmt.Println("Usage: pikpik logs <app_name> [-f|--follow] [-n|--tail <lines>] [-t|--timestamps]")
		os.Exit(1)
	}
	appID := positional[0]

	client, _, _ := getClient()
	wsURL := client.GetWebSocketURL(fmt.Sprintf("/ws/logs?target_id=%s&tail=%d", appID, *tail))

	dialer := client.WebSocketDialer()
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "WebSocket connection failed: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	// Send subscription
	sub := api.ClientAction{
		Action:   "subscribe",
		Channel:  "logs",
		TargetID: appID,
		Params: map[string]any{
			"follow":     *follow,
			"tail":       *tail,
			"timestamps": *timestamps,
		},
	}
	_ = conn.WriteJSON(sub)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var frame api.WSMessage
			if err := json.Unmarshal(msg, &frame); err == nil {
				if frame.Channel == "logs" {
					if line, ok := frame.Data.(string); ok {
						if *timestamps && !frame.Time.IsZero() {
							fmt.Printf("[%s] %s\n", frame.Time.Format(time.RFC3339), line)
						} else {
							fmt.Println(line)
						}
					} else if m, ok := frame.Data.(map[string]any); ok {
						fmt.Printf("%v\n", m["line"])
					}
				}
			} else {
				fmt.Println(string(msg))
			}
			if !*follow {
				break
			}
		}
	}()

	select {
	case <-sigChan:
		_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	case <-done:
	}
}

// 6. pikpik stats
func runStats(args []string) {
	client, _, _ := getClient()
	wsURL := client.GetWebSocketURL("/ws/stats")

	dialer := client.WebSocketDialer()
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "WebSocket stats dial failed: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	sub := api.ClientAction{
		Action:   "subscribe",
		Channel:  "stats",
		TargetID: "*",
	}
	_ = conn.WriteJSON(sub)

	fmt.Printf("%-20s %-12s %-15s %-15s\n", "TARGET", "CPU %", "MEMORY", "STATUS")
	fmt.Println(strings.Repeat("-", 65))

	for i := 0; i < 5; i++ {
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}
		var frame api.WSMessage
		if err := json.Unmarshal(msg, &frame); err == nil {
			fmt.Printf("%-20s %-12v %-15v Active\n", frame.TargetID, frame.Event, frame.Data)
		}
	}
}

// 7. pikpik db
func runDB(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: pikpik db [backup|backups|restore] <database_name>")
		os.Exit(1)
	}

	sub := args[0]
	client, _, _ := getClient()

	switch sub {
	case "backup":
		if len(args) < 2 {
			fmt.Println("Usage: pikpik db backup <database_name>")
			os.Exit(1)
		}
		dbName := args[1]
		bk, err := client.CreateBackup(context.Background(), dbName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Backup failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Snapshot created: %s (Key: %s, Size: %d bytes)\n", bk.ID, bk.S3Key, bk.CompressedBytes)

	case "backups":
		bks, err := client.ListBackups(context.Background())
		if err != nil {
			fmt.Fprintf(os.Stderr, "Listing backups failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("%-16s %-16s %-12s %-10s %s\n", "ID", "SERVICE", "SIZE", "STATUS", "CREATED AT")
		for _, b := range bks {
			fmt.Printf("%-16s %-16s %-12d %-10s %s\n", b.ID, b.ServiceID, b.CompressedBytes, b.Status, b.CreatedAt.Format(time.RFC3339))
		}

	case "restore":
		fs := flag.NewFlagSet("restore", flag.ExitOnError)
		snap := fs.StringP("snapshot", "s", "", "Snapshot ID to restore")
		_ = fs.Parse(args[1:])
		if *snap == "" {
			fmt.Println("Usage: pikpik db restore <database_name> [-s|--snapshot <snapshot_id>]")
			os.Exit(1)
		}
		target := ""
		if len(fs.Args()) > 0 {
			target = fs.Args()[0]
		}
		if err := client.RestoreBackup(context.Background(), *snap, target); err != nil {
			fmt.Fprintf(os.Stderr, "Restore failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Database snapshot %s restored successfully.\n", *snap)
	}
}

// 8. pikpik prune
func runPrune(args []string) {
	fs := flag.NewFlagSet("prune", flag.ExitOnError)
	all := fs.BoolP("all", "a", true, "Remove all unused images, not just dangling ones")
	volumes := fs.BoolP("volumes", "v", false, "Prune unused persistent volumes")
	_ = fs.Parse(args)

	client, _, _ := getClient()
	fmt.Println("Running cluster garbage collection...")
	res, err := client.PruneSystem(context.Background(), *all, *volumes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Prune failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Prune completed: Reclaimed %d MB\n", res.SpaceReclaimedBytes/(1024*1024))
}

// 9. pikpik exec
func runExec(args []string) {
	fs := flag.NewFlagSet("exec", flag.ExitOnError)
	interactive := fs.BoolP("interactive", "i", true, "Keep STDIN open")
	tty := fs.BoolP("tty", "t", true, "Allocate a pseudo-TTY")
	_ = fs.Parse(args)

	positional := fs.Args()
	if len(positional) == 0 {
		fmt.Println("Usage: pikpik exec [-i|--interactive] [-t|--tty] <app_or_container> -- /bin/sh")
		os.Exit(1)
	}

	containerID := positional[0]
	var cmdArgs []string
	if len(positional) > 1 {
		cmdArgs = positional[1:]
		if len(cmdArgs) > 0 && cmdArgs[0] == "--" {
			cmdArgs = cmdArgs[1:]
		}
	}
	cmdStr := strings.Join(cmdArgs, " ")
	if cmdStr == "" {
		cmdStr = "/bin/sh"
	}

	client, _, _ := getClient()
	ptyURL := client.GetWebSocketURL(fmt.Sprintf("/ws/pty?container_id=%s&cmd=%s", containerID, cmdStr))

	dialer := client.WebSocketDialer()
	conn, _, err := dialer.Dial(ptyURL, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to connect to PTY WebSocket: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	if (*interactive || *tty) && term.IsTerminal(int(os.Stdin.Fd())) {
		oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
		if err == nil {
			defer func() { _ = term.Restore(int(os.Stdin.Fd()), oldState) }()
		}

		// Send initial resize
		w, h, err := term.GetSize(int(os.Stdin.Fd()))
		if err == nil {
			resizeJSON, _ := json.Marshal(api.TermResizeMessage{Cols: uint(w), Rows: uint(h)})
			_ = conn.WriteMessage(websocket.BinaryMessage, append([]byte{0x01}, resizeJSON...))
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Read loop: WebSocket -> Stdout
	go func() {
		for {
			msgType, payload, err := conn.ReadMessage()
			if err != nil {
				cancel()
				return
			}
			if msgType == websocket.BinaryMessage && len(payload) > 0 {
				opcode := payload[0]
				data := payload[1:]
				if opcode == 0x00 {
					_, _ = os.Stdout.Write(data)
				} else if opcode == 0xFF {
					cancel()
					return
				}
			} else if msgType == websocket.TextMessage {
				_, _ = os.Stdout.Write(payload)
			}
		}
	}()

	// Stdin loop: Stdin -> WebSocket
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				frame := append([]byte{0x00}, buf[:n]...)
				_ = conn.WriteMessage(websocket.BinaryMessage, frame)
			}
			if err != nil {
				if err != io.EOF {
					cancel()
				}
				return
			}
		}
	}()

	<-ctx.Done()
}

// 10. pikpik context
func runContext(args []string) {
	if len(args) == 0 || args[0] == "list" {
		cm, err := NewConfigManager()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		cfg, _ := cm.Load()
		fmt.Printf("%-3s %-16s %s\n", "CUR", "NAME", "SERVER URL")
		for name, ctx := range cfg.Contexts {
			cur := " "
			if name == cfg.CurrentContext {
				cur = "*"
			}
			fmt.Printf("%-3s %-16s %s\n", cur, name, ctx.ServerURL)
		}
		return
	}

	if args[0] == "use" {
		if len(args) < 2 {
			fmt.Println("Usage: pikpik context use <context_name>")
			os.Exit(1)
		}
		name := args[1]
		cm, err := NewConfigManager()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		cfg, _ := cm.Load()
		if _, exists := cfg.Contexts[name]; !exists {
			fmt.Fprintf(os.Stderr, "Context %q does not exist.\n", name)
			os.Exit(1)
		}
		cfg.CurrentContext = name
		_ = cm.Save(cfg)
		fmt.Printf("Switched to context %q.\n", name)
	}
}
