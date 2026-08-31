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
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/fusuycorp/pikpik/pkg/api"
	"github.com/fusuycorp/pikpik/pkg/templates"
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
	case "projects":
		runProjects(args)
	case "app", "apps":
		runApps(args)
	case "tags":
		runTags(args)
	case "nodes":
		runNodes(args)
	case "machine", "machines":
		runMachine(args)
	case "deploy":
		runDeploy(args)
	case "logs":
		runLogs(args)
	case "stats":
		runStats(args)
	case "domain", "domains", "ingress":
		runDomain(args)
	case "registry":
		runRegistry(args)
	case "template", "templates", "marketplace":
		runTemplate(args)
	case "schedule", "schedules":
		runSchedule(args)
	case "notify", "notification", "notifications":
		runNotify(args)
	case "stack", "stacks":
		runStack(args)
	case "network", "networks":
		runNetwork(args)
	case "volume", "volumes":
		runVolume(args)
	case "db":
		runDB(args)
	case "prune":
		runPrune(args)
	case "exec":
		runExec(args)
	case "context":
		runContext(args)
	case "user", "users":
		runUser(args)
	case "integration", "integrations":
		runIntegration(args)
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
  projects      Manage project workspaces and grouping
  apps, app     Manage deployed applications and weighted traffic splits
  tags          List aggregated tag taxonomy and counts
  nodes         Inspect cluster nodes, allocation, and availability
  machine       Manage remote agent hosts, telemetry, and swarm enrollment
  deploy        Trigger rolling deployment for active project or image
  domain        Manage custom ingress domains, ACME TLS, and Caddy routing
  registry      Manage private OCI container registry and robot credentials
  template      Discover and deploy 1-click marketplace template stacks
  schedule      Manage automated cron database backup schedules
  stack         Manage multi-container Compose v2 application stacks
  network       Manage bridge, project-mesh, and overlay virtual networks
  volume        Manage persistent data storage volumes
  logs          Stream aggregated real-time container logs
  stats         Display real-time CPU, RAM, Network and IO metrics
  db            Manage databases, snapshots, schedules, and S3 restores
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

	cfg, err := cm.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Config load failed: %v\n", err)
		os.Exit(1)
	}

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

// 2b. pikpik projects
func runProjects(args []string) {
	if len(args) > 0 {
		switch args[0] {
		case "create":
			fs := flag.NewFlagSet("projects create", flag.ExitOnError)
			name := fs.StringP("name", "n", "", "Project name")
			desc := fs.StringP("desc", "d", "", "Project description")
			tags := fs.StringP("tags", "t", "", "Comma-separated tags")
			_ = fs.Parse(args[1:])

			if *name == "" {
				fmt.Println("Usage: pikpik projects create -n <name> [-d <description>] [-t <tag1,tag2>]")
				os.Exit(1)
			}

			var tagList []string
			if *tags != "" {
				for _, t := range strings.Split(*tags, ",") {
					if trimmed := strings.TrimSpace(t); trimmed != "" {
						tagList = append(tagList, trimmed)
					}
				}
			}

			client, _, _ := getClient()
			p, err := client.CreateProject(context.Background(), api.CreateProjectRequest{
				Name:        *name,
				Description: *desc,
				Tags:        tagList,
			})
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to create project: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("Project %q (%s) created successfully.\n", p.Name, p.ID)
			return

		case "rm", "delete":
			if len(args) < 2 {
				fmt.Println("Usage: pikpik projects rm <project_id>")
				os.Exit(1)
			}
			client, _, _ := getClient()
			if err := client.DeleteProject(context.Background(), args[1]); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to delete project: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("Project %s deleted.\n", args[1])
			return
		}
	}

	fs := flag.NewFlagSet("projects list", flag.ExitOnError)
	tag := fs.StringP("tag", "t", "", "Filter by tag")
	_ = fs.Parse(args)

	client, _, _ := getClient()
	prjs, err := client.ListProjects(context.Background(), "", *tag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to list projects: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("%-16s %-20s %-8s %-24s %s\n", "ID", "NAME", "APPS", "TAGS", "DESCRIPTION")
	for _, p := range prjs {
		tagStr := strings.Join(p.Tags, ", ")
		if len(tagStr) > 22 {
			tagStr = tagStr[:20] + ".."
		}
		fmt.Printf("%-16s %-20s %-8d %-24s %s\n", p.ID, p.Name, p.AppCount, tagStr, p.Description)
	}
}

// 2c. pikpik app / apps
func runApps(args []string) {
	if len(args) > 0 {
		sub := args[0]
		switch sub {
		case "traffic":
			runAppTraffic(args[1:])
			return
		case "list", "ls":
			args = args[1:]
		}
	}

	fs := flag.NewFlagSet("apps", flag.ExitOnError)
	tag := fs.StringP("tag", "t", "", "Filter by tag")
	_ = fs.Parse(args)

	client, _, _ := getClient()
	apps, err := client.ListApps(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to list apps: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("%-20s %-16s %-18s %-10s %-20s %s\n", "ID", "NAME", "PROJECT", "STATUS", "TAGS", "IMAGE")
	for _, a := range apps {
		if *tag != "" {
			matched := false
			for _, t := range a.Tags {
				if strings.EqualFold(t, *tag) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		tagStr := strings.Join(a.Tags, ", ")
		if len(tagStr) > 18 {
			tagStr = tagStr[:16] + ".."
		}
		projName := a.ProjectName
		if projName == "" {
			projName = a.ProjectID
		}
		fmt.Printf("%-20s %-16s %-18s %-10s %-20s %s\n", a.ID, a.Name, projName, a.Status, tagStr, a.Image)
	}
}

// runAppTraffic manages weighted traffic shifting and canary deployments for an app.
func runAppTraffic(args []string) {
	fs := flag.NewFlagSet("app traffic", flag.ExitOnError)
	upstreams := fs.StringArrayP("upstream", "u", nil, "Upstream target and weight (e.g. <target:weight> or <target=weight>)")
	reset := fs.Bool("reset", false, "Reset traffic distribution to 100% stable upstream")
	_ = fs.Parse(args)

	pos := fs.Args()
	if len(pos) < 1 {
		fmt.Println("Usage: pikpik app traffic <app_id> [--upstream <target:weight>...] [--reset]")
		os.Exit(1)
	}
	appID := pos[0]

	client, _, _ := getClient()
	ctx := context.Background()

	if *reset {
		resp, err := client.SetAppTraffic(ctx, appID, api.SetTrafficSplitRequest{
			Reset: true,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to reset traffic: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Traffic split reset to 100%% stable upstream for app %q:\n\n", appID)
		printTrafficDistributionTable(resp)
		return
	}

	if upstreams != nil && len(*upstreams) > 0 {
		var splits []api.UpstreamWeight
		for _, u := range *upstreams {
			parts := strings.Split(u, ",")
			for _, part := range parts {
				part = strings.TrimSpace(part)
				if part == "" {
					continue
				}
				target, weight, err := parseUpstreamWeight(part)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Invalid upstream specification %q: %v\n", part, err)
					os.Exit(1)
				}
				splits = append(splits, api.UpstreamWeight{
					Upstream: target,
					Weight:   weight,
				})
			}
		}

		resp, err := client.SetAppTraffic(ctx, appID, api.SetTrafficSplitRequest{
			Splits: splits,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to update traffic split: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Traffic split updated successfully for app %q:\n\n", appID)
		printTrafficDistributionTable(resp)
		return
	}

	// Read-only GET active traffic distribution
	resp, err := client.GetAppTraffic(ctx, appID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to retrieve traffic split: %v\n", err)
		os.Exit(1)
	}
	printTrafficDistributionTable(resp)
}

func parseUpstreamWeight(s string) (string, int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", 0, fmt.Errorf("empty upstream specification")
	}

	if idx := strings.LastIndex(s, "="); idx != -1 {
		target := strings.TrimSpace(s[:idx])
		weightStr := strings.TrimSpace(s[idx+1:])
		w, err := strconv.Atoi(weightStr)
		if err != nil {
			return "", 0, fmt.Errorf("invalid weight %q: %w", weightStr, err)
		}
		if target == "" {
			return "", 0, fmt.Errorf("target cannot be empty")
		}
		if w < 0 {
			return "", 0, fmt.Errorf("weight cannot be negative: %d", w)
		}
		return target, w, nil
	}

	if idx := strings.LastIndex(s, ":"); idx != -1 {
		target := strings.TrimSpace(s[:idx])
		weightStr := strings.TrimSpace(s[idx+1:])
		w, err := strconv.Atoi(weightStr)
		if err == nil && target != "" {
			if w < 0 {
				return "", 0, fmt.Errorf("weight cannot be negative: %d", w)
			}
			return target, w, nil
		}
	}

	return "", 0, fmt.Errorf("invalid upstream format %q: expected <target:weight> or <target=weight>", s)
}

func printTrafficDistributionTable(resp *api.TrafficSplitResponse) {
	if resp == nil {
		return
	}
	totalWeight := 0
	for _, s := range resp.Splits {
		totalWeight += s.Weight
	}

	fmt.Printf("App ID:  %s\n", resp.AppID)
	if resp.Domain != "" {
		fmt.Printf("Domain:  %s\n", resp.Domain)
	}
	fmt.Println()

	fmt.Printf("%-32s %-12s %s\n", "UPSTREAM", "WEIGHT", "PERCENTAGE")
	fmt.Println(strings.Repeat("-", 56))

	for _, s := range resp.Splits {
		pctStr := "0.0%"
		if totalWeight > 0 {
			pct := float64(s.Weight) / float64(totalWeight) * 100.0
			pctStr = fmt.Sprintf("%.1f%%", pct)
		}
		fmt.Printf("%-32s %-12d %s\n", s.Upstream, s.Weight, pctStr)
	}
}

// 2d. pikpik tags
func runTags(args []string) {
	client, _, _ := getClient()
	tags, err := client.ListTags(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to list tags: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("%-24s %s\n", "TAG", "COUNT")
	for _, t := range tags {
		fmt.Printf("%-24s %d\n", t.Tag, t.Count)
	}
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
	fileFlag := fs.StringP("file", "f", "", "Path to docker-compose.yml or stack file")
	projFlag := fs.StringP("project", "p", "", "Project ID (default: prj_default)")
	swarmFlag := fs.Bool("swarm", false, "Force deployment to Swarm cluster")
	standaloneFlag := fs.Bool("standalone", false, "Force deployment to Standalone engine")
	tagsFlag := fs.StringP("tags", "t", "", "Comma-separated tags")
	_ = fs.Parse(args)

	client, _, _ := getClient()

	if *fileFlag != "" {
		data, err := os.ReadFile(*fileFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to read compose file %q: %v\n", *fileFlag, err)
			os.Exit(1)
		}

		fmt.Printf("Analyzing blueprint %q...\n", *fileFlag)
		inspect, err := client.InspectCompose(context.Background(), string(data))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Compose inspection failed: %v\n", err)
			os.Exit(1)
		}

		runtimeMode := inspect.SuggestedRuntime
		if *swarmFlag {
			runtimeMode = "swarm"
		} else if *standaloneFlag {
			runtimeMode = "standalone"
		}

		var svcNames []string
		for _, s := range inspect.Services {
			svcNames = append(svcNames, s.Name)
		}

		fmt.Printf("Blueprint verified: %d services (%s) | Runtime: %s | Exposed Ports: %v\n",
			len(inspect.Services), strings.Join(svcNames, ", "), strings.ToUpper(runtimeMode), inspect.ExposedPorts)

		appName := *appFlag
		if appName == "" && len(inspect.Services) > 0 {
			appName = inspect.Services[0].Name
		}
		if appName == "" {
			appName = "stack-app"
		}

		var tagList []string
		if *tagsFlag != "" {
			for _, t := range strings.Split(*tagsFlag, ",") {
				if trimmed := strings.TrimSpace(t); trimmed != "" {
					tagList = append(tagList, trimmed)
				}
			}
		}

		primaryImage := ""
		if len(inspect.Services) > 0 {
			primaryImage = inspect.Services[0].Image
		}

		createdApp, err := client.CreateApp(context.Background(), api.CreateAppRequest{
			ProjectID:   *projFlag,
			Name:        appName,
			Image:       primaryImage,
			Replicas:    1,
			Tags:        tagList,
			RuntimeMode: runtimeMode,
			ComposeYAML: string(data),
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to deploy application: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Application %q (%s) deployed successfully in [%s] mode.\n", createdApp.Name, createdApp.ID, strings.ToUpper(runtimeMode))
		return
	}

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
		fmt.Println("Usage:")
		fmt.Println("  pikpik deploy -a <app_name> [-i <image_tag>]")
		fmt.Println("  pikpik deploy -f <compose.yml> [-p <project_id>] [--swarm|--standalone] [-t <tags>]")
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
		if !*follow {
			_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		}
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if !*follow {
				_ = conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
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
						if line, ok := m["line"].(string); ok {
							if *timestamps && !frame.Time.IsZero() {
								fmt.Printf("[%s] %s\n", frame.Time.Format(time.RFC3339), line)
							} else {
								fmt.Println(line)
							}
						} else {
							fmt.Printf("%v\n", m["line"])
						}
					}
				}
			} else {
				fmt.Println(string(msg))
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

	fmt.Printf("%-24s %-12s %-22s %-12s\n", "TARGET", "CPU %", "MEMORY", "STATUS")
	fmt.Println(strings.Repeat("-", 74))

	for i := 0; i < 5; i++ {
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}
		var frame api.WSMessage
		if err := json.Unmarshal(msg, &frame); err == nil {
			target := frame.TargetID
			if target == "" || target == "*" {
				target = frame.Event
			}
			if target == "" {
				target = "system"
			}
			cpuStr := "0.00%"
			memStr := "0 B"
			statusStr := "Active"

			if m, ok := frame.Data.(map[string]any); ok {
				if cpu, ok := m["cpu_percent"].(float64); ok {
					cpuStr = fmt.Sprintf("%.2f%%", cpu)
				} else if cpu, ok := m["cpu"].(float64); ok {
					cpuStr = fmt.Sprintf("%.2f%%", cpu)
				}

				if memUsed, ok := m["memory_used_bytes"].(float64); ok {
					if memLimit, ok := m["memory_limit_bytes"].(float64); ok && memLimit > 0 {
						memStr = fmt.Sprintf("%s / %s", formatBytesInt(int64(memUsed)), formatBytesInt(int64(memLimit)))
					} else {
						memStr = formatBytesInt(int64(memUsed))
					}
				} else if memUsed, ok := m["mem_used_bytes"].(float64); ok {
					memStr = formatBytesInt(int64(memUsed))
				} else if memPct, ok := m["memory_percent"].(float64); ok {
					memStr = fmt.Sprintf("%.2f%%", memPct)
				}

				if st, ok := m["status"].(string); ok && st != "" {
					statusStr = st
				}
			}

			fmt.Printf("%-24s %-12s %-22s %-12s\n", target, cpuStr, memStr, statusStr)
		}
	}
}

// 7. pikpik db
func runDB(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: pikpik db [backup|backups|restore|schedule] <database_name>")
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

	case "schedule", "schedules":
		runSchedule(args[1:])
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
		cfg, err := cm.Load()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Config load failed: %v\n", err)
			os.Exit(1)
		}
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
		cfg, err := cm.Load()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Config load failed: %v\n", err)
			os.Exit(1)
		}
		if _, exists := cfg.Contexts[name]; !exists {
			fmt.Fprintf(os.Stderr, "Context %q does not exist.\n", name)
			os.Exit(1)
		}
		cfg.CurrentContext = name
		if err := cm.Save(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "Config save failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Switched to context %q.\n", name)
	}
}

// 11. pikpik stack
func runStack(args []string) {
	if len(args) == 0 {
		args = []string{"list"}
	}

	switch args[0] {
	case "deploy":
		fs := flag.NewFlagSet("stack deploy", flag.ExitOnError)
		name := fs.StringP("name", "n", "", "Stack name")
		project := fs.StringP("project", "p", "", "Project ID")
		_ = fs.Parse(args[1:])

		target := ""
		if len(fs.Args()) > 0 {
			target = fs.Args()[0]
		}

		if target == "" && *name == "" {
			fmt.Println("Usage: pikpik stack deploy <compose-file.yml | stack-name> [-n name] [-p project]")
			os.Exit(1)
		}

		client, _, _ := getClient()
		ctx := context.Background()

		// Check if target is a file on disk
		if target != "" {
			if data, err := os.ReadFile(target); err == nil {
				stackName := *name
				if stackName == "" {
					base := filepath.Base(target)
					stackName = strings.TrimSuffix(base, filepath.Ext(base))
					if stackName == "docker-compose" || stackName == "compose" {
						dir, _ := os.Getwd()
						stackName = filepath.Base(dir)
					}
				}

				stk, err := client.CreateStack(ctx, api.CreateStackRequest{
					ProjectID:   *project,
					Name:        stackName,
					ComposeYAML: string(data),
				})
				if err != nil {
					fmt.Fprintf(os.Stderr, "Failed to create/update stack: %v\n", err)
					os.Exit(1)
				}
				target = stk.ID
			}
		} else if *name != "" {
			target = *name
		}

		fmt.Printf("Deploying stack %q...\n", target)
		if err := client.DeployStack(ctx, target); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to deploy stack: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Stack %q deployed successfully.\n", target)

	case "list", "ls":
		fs := flag.NewFlagSet("stack list", flag.ExitOnError)
		jsonOut := fs.Bool("json", false, "Output JSON format")
		_ = fs.Parse(args[1:])

		client, _, _ := getClient()
		stacks, err := client.ListStacks(context.Background())
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to list stacks: %v\n", err)
			os.Exit(1)
		}

		if *jsonOut {
			b, _ := json.MarshalIndent(stacks, "", "  ")
			fmt.Println(string(b))
			return
		}

		fmt.Printf("%-20s %-16s %-16s %-12s %-20s %s\n", "ID", "NAME", "PROJECT", "STATUS", "SERVICES", "CREATED")
		for _, s := range stacks {
			svcStr := strings.Join(s.Services, ", ")
			if len(svcStr) > 18 {
				svcStr = svcStr[:16] + ".."
			}
			proj := s.ProjectID
			if proj == "" {
				proj = "default"
			}
			fmt.Printf("%-20s %-16s %-16s %-12s %-20s %s\n", s.ID, s.Name, proj, s.Status, svcStr, s.CreatedAt.Format("2006-01-02 15:04"))
		}

	case "inspect":
		if len(args) < 2 {
			fmt.Println("Usage: pikpik stack inspect <stack_id|name> [--json]")
			os.Exit(1)
		}
		fs := flag.NewFlagSet("stack inspect", flag.ExitOnError)
		jsonOut := fs.Bool("json", false, "Output JSON format")
		_ = fs.Parse(args[2:])

		client, _, _ := getClient()
		stk, err := client.GetStack(context.Background(), args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to inspect stack: %v\n", err)
			os.Exit(1)
		}

		if *jsonOut {
			b, _ := json.MarshalIndent(stk, "", "  ")
			fmt.Println(string(b))
			return
		}

		fmt.Printf("Stack: %s (%s)\n", stk.Name, stk.ID)
		fmt.Printf("Project:  %s\n", stk.ProjectID)
		fmt.Printf("Status:   %s\n", stk.Status)
		fmt.Printf("Services: %s\n", strings.Join(stk.Services, ", "))
		fmt.Printf("Created:  %s\n\n", stk.CreatedAt.Format(time.RFC3339))
		fmt.Println("Compose YAML:")
		fmt.Println("-------------")
		fmt.Println(stk.ComposeYAML)

	case "stop":
		if len(args) < 2 {
			fmt.Println("Usage: pikpik stack stop <stack_id|name>")
			os.Exit(1)
		}
		client, _, _ := getClient()
		if err := client.StopStack(context.Background(), args[1]); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to stop stack: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Stack %q stopped.\n", args[1])

	case "restart":
		if len(args) < 2 {
			fmt.Println("Usage: pikpik stack restart <stack_id|name>")
			os.Exit(1)
		}
		client, _, _ := getClient()
		if err := client.RestartStack(context.Background(), args[1]); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to restart stack: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Stack %q restarted.\n", args[1])

	case "rm", "delete":
		if len(args) < 2 {
			fmt.Println("Usage: pikpik stack rm <stack_id|name>")
			os.Exit(1)
		}
		client, _, _ := getClient()
		if err := client.DeleteStack(context.Background(), args[1]); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to delete stack: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Stack %q removed.\n", args[1])

	case "logs":
		if len(args) < 2 {
			fmt.Println("Usage: pikpik stack logs <stack_id|name>")
			os.Exit(1)
		}
		client, _, _ := getClient()
		logs, err := client.GetStackLogs(context.Background(), args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to fetch stack logs: %v\n", err)
			os.Exit(1)
		}
		b, _ := json.MarshalIndent(logs, "", "  ")
		fmt.Println(string(b))

	case "help", "--help", "-h":
		fmt.Println(`Usage: pikpik stack <command> [arguments]

Commands:
  deploy    Deploy a stack from compose file or name
  list      List all application stacks
  inspect   Show detailed stack blueprint and containers
  stop      Stop running stack containers
  restart   Restart stack containers
  logs      Show runtime logs and status for stack
  rm        Remove stack and associated containers`)
		return

	default:
		fmt.Printf("Unknown stack command: %s. Use deploy, list, inspect, stop, restart, rm, logs\n", args[0])
		os.Exit(1)
	}
}

// 12. pikpik network
func runNetwork(args []string) {
	if len(args) == 0 {
		args = []string{"list"}
	}

	switch args[0] {
	case "help", "--help", "-h":
		fmt.Println(`Usage: pikpik network <command> [arguments]

Commands:
  list      List managed networks
  inspect   Show network details
  create    Create a virtual bridge/overlay network
  rm        Remove a managed network
  prune     Prune unused networks`)
		return
	case "list", "ls":
		fs := flag.NewFlagSet("network list", flag.ExitOnError)
		project := fs.StringP("project", "p", "", "Filter by project ID")
		jsonOut := fs.Bool("json", false, "Output JSON format")
		_ = fs.Parse(args[1:])

		client, _, _ := getClient()
		nets, err := client.ListNetworks(context.Background(), *project)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to list networks: %v\n", err)
			os.Exit(1)
		}

		if *jsonOut {
			b, _ := json.MarshalIndent(nets, "", "  ")
			fmt.Println(string(b))
			return
		}

		fmt.Printf("%-20s %-24s %-16s %-10s %-10s %s\n", "ID", "NAME", "PROJECT", "DRIVER", "SCOPE", "EXTERNAL")
		for _, n := range nets {
			fmt.Printf("%-20s %-24s %-16s %-10s %-10s %v\n", n.ID, n.Name, n.ProjectID, n.Driver, n.Scope, n.IsExternal)
		}

	case "inspect":
		if len(args) < 2 {
			fmt.Println("Usage: pikpik network inspect <network_id>")
			os.Exit(1)
		}
		client, _, _ := getClient()
		net, err := client.GetNetwork(context.Background(), args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to inspect network: %v\n", err)
			os.Exit(1)
		}
		b, _ := json.MarshalIndent(net, "", "  ")
		fmt.Println(string(b))

	case "create":
		fs := flag.NewFlagSet("network create", flag.ExitOnError)
		driver := fs.StringP("driver", "d", "bridge", "Network driver (bridge/overlay)")
		scope := fs.StringP("scope", "s", "project", "Network scope (project/stack/custom)")
		project := fs.StringP("project", "p", "", "Project ID")
		_ = fs.Parse(args[1:])

		if len(fs.Args()) == 0 {
			fmt.Println("Usage: pikpik network create <name> [-d driver] [-s scope] [-p project]")
			os.Exit(1)
		}
		name := fs.Args()[0]

		client, _, _ := getClient()
		net, err := client.CreateNetwork(context.Background(), api.CreateNetworkRequest{
			ProjectID: *project,
			Name:      name,
			Driver:    *driver,
			Scope:     *scope,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create network: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Network %q (%s) created.\n", net.Name, net.ID)

	case "rm", "delete":
		if len(args) < 2 {
			fmt.Println("Usage: pikpik network rm <network_id>")
			os.Exit(1)
		}
		client, _, _ := getClient()
		if err := client.DeleteNetwork(context.Background(), args[1]); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to delete network: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Network %q removed.\n", args[1])

	case "prune":
		fs := flag.NewFlagSet("network prune", flag.ExitOnError)
		project := fs.StringP("project", "p", "", "Project ID")
		_ = fs.Parse(args[1:])

		client, _, _ := getClient()
		res, err := client.PruneNetworks(context.Background(), *project)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to prune networks: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Pruned %d unused networks.\n", len(res.Deleted))
		for _, name := range res.Deleted {
			fmt.Printf(" - %s\n", name)
		}

	default:
		fmt.Printf("Unknown network command: %s. Use list, inspect, create, rm, prune\n", args[0])
		os.Exit(1)
	}
}

// 13. pikpik volume
func runVolume(args []string) {
	if len(args) == 0 {
		args = []string{"list"}
	}

	switch args[0] {
	case "help", "--help", "-h":
		fmt.Println(`Usage: pikpik volume <command> [arguments]

Commands:
  list      List persistent storage volumes
  inspect   Show volume details
  create    Create a managed volume
  rm        Remove a managed volume
  prune     Prune unused volumes`)
		return
	case "list", "ls":
		fs := flag.NewFlagSet("volume list", flag.ExitOnError)
		project := fs.StringP("project", "p", "", "Filter by project ID")
		jsonOut := fs.Bool("json", false, "Output JSON format")
		_ = fs.Parse(args[1:])

		client, _, _ := getClient()
		vols, err := client.ListVolumes(context.Background(), *project)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to list volumes: %v\n", err)
			os.Exit(1)
		}

		if *jsonOut {
			b, _ := json.MarshalIndent(vols, "", "  ")
			fmt.Println(string(b))
			return
		}

		fmt.Printf("%-20s %-28s %-16s %-10s %s\n", "ID", "NAME", "PROJECT", "DRIVER", "CREATED")
		for _, v := range vols {
			fmt.Printf("%-20s %-28s %-16s %-10s %s\n", v.ID, v.Name, v.ProjectID, v.Driver, v.CreatedAt.Format("2006-01-02 15:04"))
		}

	case "inspect":
		if len(args) < 2 {
			fmt.Println("Usage: pikpik volume inspect <volume_id>")
			os.Exit(1)
		}
		client, _, _ := getClient()
		vol, err := client.GetVolume(context.Background(), args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to inspect volume: %v\n", err)
			os.Exit(1)
		}
		b, _ := json.MarshalIndent(vol, "", "  ")
		fmt.Println(string(b))

	case "create":
		fs := flag.NewFlagSet("volume create", flag.ExitOnError)
		driver := fs.StringP("driver", "d", "local", "Volume driver")
		project := fs.StringP("project", "p", "", "Project ID")
		_ = fs.Parse(args[1:])

		if len(fs.Args()) == 0 {
			fmt.Println("Usage: pikpik volume create <name> [-d driver] [-p project]")
			os.Exit(1)
		}
		name := fs.Args()[0]

		client, _, _ := getClient()
		vol, err := client.CreateVolume(context.Background(), api.CreateVolumeRequest{
			ProjectID: *project,
			Name:      name,
			Driver:    *driver,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create volume: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Volume %q (%s) created.\n", vol.Name, vol.ID)

	case "rm", "delete":
		if len(args) < 2 {
			fmt.Println("Usage: pikpik volume rm <volume_id>")
			os.Exit(1)
		}
		client, _, _ := getClient()
		if err := client.DeleteVolume(context.Background(), args[1]); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to delete volume: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Volume %q removed.\n", args[1])

	case "prune":
		fs := flag.NewFlagSet("volume prune", flag.ExitOnError)
		project := fs.StringP("project", "p", "", "Project ID")
		_ = fs.Parse(args[1:])

		client, _, _ := getClient()
		res, err := client.PruneVolumes(context.Background(), *project)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to prune volumes: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Pruned %d unused volumes (Reclaimed: %d bytes).\n", len(res.Deleted), res.SpaceReclaimed)
		for _, name := range res.Deleted {
			fmt.Printf(" - %s\n", name)
		}

	default:
		fmt.Printf("Unknown volume command: %s. Use list, inspect, create, rm, prune\n", args[0])
		os.Exit(1)
	}
}

// 12. pikpik machine
func runMachine(args []string) {
	if len(args) == 0 || args[0] == "list" || strings.HasPrefix(args[0], "-") {
		subArgs := args
		if len(args) > 0 && args[0] == "list" {
			subArgs = args[1:]
		}
		fs := flag.NewFlagSet("machine list", flag.ExitOnError)
		jsonOut := fs.Bool("json", false, "Output in JSON format")
		_ = fs.Parse(subArgs)

		client, _, _ := getClient()
		machines, err := client.ListMachines(context.Background())
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to list machines: %v\n", err)
			os.Exit(1)
		}

		if *jsonOut {
			b, _ := json.MarshalIndent(machines, "", "  ")
			fmt.Println(string(b))
			return
		}

		fmt.Printf("%-18s %-20s %-10s %-10s %-16s %-12s %-10s %s\n",
			"ID", "HOSTNAME", "ROLE", "STATUS", "PUBLIC IP", "OS/ARCH", "DOCKER", "LAST SEEN")
		for _, m := range machines {
			osArch := m.OSKernel
			if m.CPUArch != "" {
				osArch = m.CPUArch
			}
			lastSeenStr := "Never"
			if m.LastSeen != nil {
				lastSeenStr = m.LastSeen.Format(time.RFC3339)
			}
			fmt.Printf("%-18s %-20s %-10s %-10s %-16s %-12s %-10s %s\n",
				m.ID, m.Hostname, m.Role, m.Status, m.PublicIP, osArch, m.DockerVersion, lastSeenStr)
		}
		return
	}

	sub := args[0]
	client, _, _ := getClient()

	switch sub {
	case "inspect":
		if len(args) < 2 {
			fmt.Println("Usage: pikpik machine inspect <machine_id> [--json]")
			os.Exit(1)
		}
		fs := flag.NewFlagSet("machine inspect", flag.ExitOnError)
		jsonOut := fs.Bool("json", false, "Output in JSON format")
		_ = fs.Parse(args[2:])

		m, err := client.GetMachine(context.Background(), args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to inspect machine: %v\n", err)
			os.Exit(1)
		}

		if *jsonOut {
			b, _ := json.MarshalIndent(m, "", "  ")
			fmt.Println(string(b))
			return
		}

		fmt.Printf("Machine ID:      %s\n", m.ID)
		fmt.Printf("Hostname:        %s\n", m.Hostname)
		fmt.Printf("Role:            %s\n", m.Role)
		fmt.Printf("Status:          %s\n", m.Status)
		fmt.Printf("Public IP:       %s\n", m.PublicIP)
		fmt.Printf("Private IP:      %s\n", m.PrivateIP)
		fmt.Printf("OS Kernel:       %s\n", m.OSKernel)
		fmt.Printf("CPU Arch:        %s\n", m.CPUArch)
		fmt.Printf("Docker Version:  %s\n", m.DockerVersion)
		fmt.Printf("Agent Version:   %s\n", m.AgentVersion)
		if m.LastSeen != nil {
			fmt.Printf("Last Seen:       %s\n", m.LastSeen.Format(time.RFC3339))
		}
		if m.Metrics != nil {
			fmt.Printf("CPU Usage:       %.2f%% (%d cores)\n", m.Metrics.CPUPercent, m.Metrics.CPUCores)
			fmt.Printf("Memory Usage:    %.2f%% (%d / %d bytes)\n", m.Metrics.MemPercent, m.Metrics.MemUsedBytes, m.Metrics.MemTotalBytes)
		}

	case "rm", "delete":
		if len(args) < 2 {
			fmt.Println("Usage: pikpik machine rm <machine_id>")
			os.Exit(1)
		}
		if err := client.DeleteMachine(context.Background(), args[1]); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to remove machine: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Machine %q removed successfully.\n", args[1])

	case "join-swarm":
		if len(args) < 2 {
			fmt.Println("Usage: pikpik machine join-swarm <machine_id> [--role worker|manager] [--token <token>]")
			os.Exit(1)
		}
		machineID := args[1]
		fs := flag.NewFlagSet("machine join-swarm", flag.ExitOnError)
		role := fs.StringP("role", "r", "worker", "Node role (worker or manager)")
		token := fs.StringP("token", "t", "", "Swarm join token override")
		_ = fs.Parse(args[2:])

		node, err := client.JoinSwarmMachine(context.Background(), machineID, api.JoinSwarmRequest{
			Role:      *role,
			JoinToken: *token,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to join machine to Swarm cluster: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Machine %s joined Swarm cluster as %s (Status: %s, Availability: %s).\n",
			node.ID, node.Role, node.Status, node.Availability)

	case "enroll":
		resp, err := client.GetMachineEnrollCommand(context.Background())
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to get machine enrollment command: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("=== Remote Host Machine Enrollment ===")
		fmt.Printf("Enrollment Token: %s\n", resp.Token)
		fmt.Printf("Control Plane:    %s\n\n", resp.ServerURL)
		fmt.Println("Run this command on your remote server to install pikpik-agent:")
		fmt.Printf("  %s\n", resp.Command)

	case "metrics":
		if len(args) < 2 {
			fmt.Println("Usage: pikpik machine metrics <machine_id>")
			os.Exit(1)
		}
		m, err := client.GetMachineMetrics(context.Background(), args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to get machine metrics: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("=== Host Metrics (%s) ===\n", m.NodeID)
		fmt.Printf("CPU:     %.2f%% (%d cores)\n", m.CPUPercent, m.CPUCores)
		fmt.Printf("Memory:  %.2f%% (Used: %d bytes, Total: %d bytes)\n", m.MemPercent, m.MemUsedBytes, m.MemTotalBytes)
		fmt.Printf("Net RX:  %d B/s | TX: %d B/s\n", m.NetRxBps, m.NetTxBps)
		fmt.Printf("Disk RD: %d B/s | WR: %d B/s\n", m.DiskReadBps, m.DiskWriteBps)

	default:
		fmt.Printf("Unknown machine command: %s. Available: list, inspect, rm, join-swarm, enroll, metrics\n", sub)
		os.Exit(1)
	}
}

// 12. pikpik domain / ingress
func runDomain(args []string) {
	if len(args) == 0 {
		args = []string{"list"}
	}

	sub := args[0]
	client, _, _ := getClient()

	switch sub {
	case "list", "ls":
		domains, err := client.ListDomains(context.Background())
		if err != nil {
			fmt.Fprintf(os.Stderr, "Listing domains failed: %v\n", err)
			os.Exit(1)
		}
		if len(domains) == 0 {
			fmt.Println("No custom domain bindings found.")
			return
		}
		fmt.Printf("%-28s %-20s %-10s %s\n", "DOMAIN", "APP ID", "AUTO TLS", "CREATED AT")
		fmt.Println(strings.Repeat("-", 75))
		for _, d := range domains {
			tlsStatus := "Disabled"
			if d.AutoTLS {
				tlsStatus = "Enabled"
			}
			fmt.Printf("%-28s %-20s %-10s %s\n", d.Domain, d.AppID, tlsStatus, d.CreatedAt.Format(time.RFC3339))
		}

	case "bind", "add":
		fs := flag.NewFlagSet("domain bind", flag.ExitOnError)
		noTLS := fs.Bool("no-tls", false, "Disable automatic ACME TLS issuance")
		_ = fs.Parse(args[1:])

		pos := fs.Args()
		if len(pos) < 2 {
			fmt.Println("Usage: pikpik domain bind <app_id> <domain> [--no-tls]")
			os.Exit(1)
		}
		appID := pos[0]
		domainName := pos[1]

		binding, err := client.BindDomain(context.Background(), api.BindDomainRequest{
			AppID:   appID,
			Domain:  domainName,
			AutoTLS: !*noTLS,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Domain binding failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Domain %q successfully bound to app %q (ID: %s, TLS: %t)\n", binding.Domain, binding.AppID, binding.ID, binding.AutoTLS)

	case "rm", "delete", "remove":
		if len(args) < 2 {
			fmt.Println("Usage: pikpik domain rm <domain_id_or_binding_id>")
			os.Exit(1)
		}
		id := args[1]
		if err := client.DeleteDomain(context.Background(), id); err != nil {
			fmt.Fprintf(os.Stderr, "Deleting domain failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Domain binding %q removed.\n", id)

	case "cert", "upload-cert":
		if len(args) < 4 {
			fmt.Println("Usage: pikpik domain cert <domain> <cert_file_path> <key_file_path>")
			os.Exit(1)
		}
		domainName := args[1]
		certPath := args[2]
		keyPath := args[3]

		certPEM, err := os.ReadFile(certPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed reading cert file: %v\n", err)
			os.Exit(1)
		}
		keyPEM, err := os.ReadFile(keyPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed reading key file: %v\n", err)
			os.Exit(1)
		}

		err = client.UploadCertificate(context.Background(), api.CertificateUploadRequest{
			Domain:  domainName,
			CertPEM: string(certPEM),
			KeyPEM:  string(keyPEM),
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Uploading certificate failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Certificate uploaded successfully for %s.\n", domainName)

	case "reconcile", "sync":
		if err := client.ReconcileIngress(context.Background()); err != nil {
			fmt.Fprintf(os.Stderr, "Ingress reconciliation failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Ingress routes and TLS state successfully reconciled with Caddy.")

	case "caddy", "diagnostics":
		cfg, err := client.GetCaddyConfig(context.Background())
		if err != nil {
			fmt.Fprintf(os.Stderr, "Retrieving Caddy diagnostics failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Caddy Status: %s (Latency: %dms, Active Routes: %d)\n", cfg.Status, cfg.LatencyMs, cfg.ActiveRoutes)
		if len(cfg.Config) > 0 {
			fmt.Println(string(cfg.Config))
		}

	default:
		fmt.Printf("Unknown domain subcommand: %s\nUsage: pikpik domain [list|bind|rm|cert|reconcile|caddy]\n", sub)
		os.Exit(1)
	}
}

// 13. pikpik registry
func runRegistry(args []string) {
	if len(args) == 0 {
		args = []string{"status"}
	}

	sub := args[0]
	client, _, _ := getClient()

	switch sub {
	case "status":
		st, err := client.GetRegistryStatus(context.Background())
		if err != nil {
			fmt.Fprintf(os.Stderr, "Retrieving registry status failed: %v\n", err)
			os.Exit(1)
		}
		statusStr := "Stopped"
		if st.IsRunning {
			statusStr = "Online (OCI v2.8)"
		}
		fmt.Printf("Registry Status:  %s\n", statusStr)
		fmt.Printf("Storage Footprint: %s\n", formatBytesInt(st.StorageBytes))
		fmt.Printf("Repositories:     %d\n", st.Repositories)
		if st.ContainerID != "" {
			fmt.Printf("Container ID:     %s\n", st.ContainerID)
		}

	case "catalog", "ls", "list":
		cat, err := client.ListRepositories(context.Background())
		if err != nil {
			fmt.Fprintf(os.Stderr, "Listing registry catalog failed: %v\n", err)
			os.Exit(1)
		}
		if len(cat.Repositories) == 0 {
			fmt.Println("No container images in local registry.")
			return
		}
		fmt.Printf("%-32s %s\n", "REPOSITORY", "TAGS")
		fmt.Println(strings.Repeat("-", 60))
		for _, repo := range cat.Repositories {
			tags := cat.Tags[repo]
			if len(tags) == 0 {
				tags = []string{"latest"}
			}
			fmt.Printf("%-32s %s\n", repo, strings.Join(tags, ", "))
		}

	case "creds", "credentials":
		fs := flag.NewFlagSet("registry creds", flag.ExitOnError)
		project := fs.StringP("project", "p", "", "Project ID filter")
		_ = fs.Parse(args[1:])

		creds, err := client.GetRegistryCredentials(context.Background(), *project)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Retrieving registry credentials failed: %v\n", err)
			os.Exit(1)
		}
		if len(creds) == 0 {
			fmt.Println("Default robot account active.")
			return
		}
		fmt.Printf("%-24s %-20s %s\n", "ROBOT USERNAME", "PROJECT", "CREATED AT")
		fmt.Println(strings.Repeat("-", 65))
		for _, c := range creds {
			proj := c.ProjectID
			if proj == "" {
				proj = "cluster-global"
			}
			fmt.Printf("%-24s %-20s %s\n", c.Username, proj, c.CreatedAt.Format(time.RFC3339))
		}

	case "rotate":
		id := ""
		if len(args) > 1 {
			id = args[1]
		}
		rotated, err := client.RotateRegistryCredentials(context.Background(), id)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Rotating registry credentials failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Robot credentials rotated for %s (New Secret Token: %s)\n", rotated.Username, rotated.SecretToken)

	case "gc", "garbage-collect":
		if err := client.GarbageCollectRegistry(context.Background()); err != nil {
			fmt.Fprintf(os.Stderr, "Registry garbage collection failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Registry garbage collection completed. Unreferenced blobs pruned.")

	default:
		fmt.Printf("Unknown registry subcommand: %s\nUsage: pikpik registry [status|catalog|creds|rotate|gc]\n", sub)
		os.Exit(1)
	}
}

// 14. pikpik template
func runTemplate(args []string) {
	if len(args) == 0 {
		args = []string{"list"}
	}

	sub := args[0]
	client, _, _ := getClient()

	switch sub {
	case "list", "ls":
		fs := flag.NewFlagSet("template list", flag.ExitOnError)
		category := fs.StringP("category", "c", "", "Category filter (e.g. Databases, Storage, Analytics, Productivity, CMS)")
		search := fs.StringP("search", "s", "", "Search query")
		_ = fs.Parse(args[1:])

		tpls, err := client.ListTemplates(context.Background(), *category, *search)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Listing templates failed: %v\n", err)
			os.Exit(1)
		}
		if len(tpls) == 0 {
			fmt.Println("No templates found.")
			return
		}
		fmt.Printf("%-18s %-24s %-16s %-10s %s\n", "ID", "NAME", "CATEGORY", "SERVICES", "TAGS")
		fmt.Println(strings.Repeat("-", 80))
		for _, t := range tpls {
			svcCount := fmt.Sprintf("%d", len(t.Services))
			tags := strings.Join(t.Tags, ", ")
			fmt.Printf("%-18s %-24s %-16s %-10s %s\n", t.ID, t.Name, t.Category, svcCount, tags)
		}

	case "info", "inspect":
		if len(args) < 2 {
			fmt.Println("Usage: pikpik template info <template_id>")
			os.Exit(1)
		}
		id := args[1]
		tpl, err := client.GetTemplate(context.Background(), id)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Retrieving template failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("ID:          %s\n", tpl.ID)
		fmt.Printf("Name:        %s (v%s)\n", tpl.Name, tpl.Version)
		fmt.Printf("Category:    %s\n", tpl.Category)
		fmt.Printf("Description: %s\n", tpl.Description)
		if tpl.DocumentationURL != "" {
			fmt.Printf("Docs:        %s\n", tpl.DocumentationURL)
		}
		if tpl.DefaultPort > 0 {
			fmt.Printf("DefaultPort: %d\n", tpl.DefaultPort)
		}
		if len(tpl.Tags) > 0 {
			fmt.Printf("Tags:        %s\n", strings.Join(tpl.Tags, ", "))
		}
		if len(tpl.Volumes) > 0 {
			fmt.Println("\nVolumes:")
			for _, v := range tpl.Volumes {
				roStr := ""
				if v.ReadOnly {
					roStr = " (read-only)"
				}
				fmt.Printf("  - %s -> %s%s\n", v.Name, v.MountPath, roStr)
			}
		}
		if len(tpl.Services) > 0 {
			fmt.Println("\nServices:")
			for _, s := range tpl.Services {
				portStrs := make([]string, 0, len(s.Ports))
				for _, p := range s.Ports {
					portStrs = append(portStrs, fmt.Sprintf("%d:%d/%s", p.HostPort, p.ContainerPort, p.Protocol))
				}
				portsDesc := ""
				if len(portStrs) > 0 {
					portsDesc = fmt.Sprintf(", Ports: %s", strings.Join(portStrs, ", "))
				}
				fmt.Printf("  - %s (Image: %s%s)\n", s.Name, s.Image, portsDesc)
			}
		}
		if len(tpl.EnvVars) > 0 {
			fmt.Println("\nEnvironment Variables Schema:")
			for _, ev := range tpl.EnvVars {
				reqStr := "optional"
				if ev.Required {
					reqStr = "required"
				}
				var details []string
				details = append(details, reqStr)
				if ev.Default != "" {
					details = append(details, fmt.Sprintf("default: %q", ev.Default))
				}
				if ev.IsSecret {
					details = append(details, "secret")
				}
				if ev.AutoGenerate != "" {
					details = append(details, fmt.Sprintf("auto: %s", ev.AutoGenerate))
				}
				fmt.Printf("  - %-32s %s (%s)\n", ev.Key, ev.Description, strings.Join(details, ", "))
			}
		}

	case "deploy":
		fs := flag.NewFlagSet("template deploy", flag.ExitOnError)
		name := fs.StringP("name", "n", "", "Target application/stack name (required)")
		project := fs.StringP("project", "p", "", "Project ID")
		domain := fs.StringP("domain", "d", "", "Custom domain binding")
		envFlags := fs.StringArrayP("env", "e", nil, "Set environment variables (KEY=VALUE)")
		_ = fs.Parse(args[1:])

		pos := fs.Args()
		if len(pos) == 0 || *name == "" {
			fmt.Println("Usage: pikpik template deploy <template_id> --name <app_name> [-p|--project <prj>] [-d|--domain <domain>] [-e KEY=VAL]")
			os.Exit(1)
		}
		templateID := pos[0]

		varMap := make(map[string]string)
		for _, e := range *envFlags {
			parts := strings.SplitN(e, "=", 2)
			if len(parts) == 2 {
				varMap[parts[0]] = parts[1]
			}
		}

		res, err := client.DeployTemplate(context.Background(), templateID, templates.DeployTemplateRequest{
			Name:                *name,
			ProjectID:           *project,
			Domain:              *domain,
			Variables:           varMap,
			AutoGenerateMissing: true,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Template deployment failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Template %q deployed successfully! Stack/App: %s (Services: %d)\n", templateID, res.Name, len(res.Services))

	default:
		fmt.Printf("Unknown template subcommand: %s\nUsage: pikpik template [list|info|deploy]\n", sub)
		os.Exit(1)
	}
}

// 15. pikpik schedule
func runSchedule(args []string) {
	if len(args) == 0 {
		args = []string{"list"}
	}

	sub := args[0]
	client, _, _ := getClient()

	switch sub {
	case "list", "ls":
		fs := flag.NewFlagSet("schedule list", flag.ExitOnError)
		service := fs.StringP("service", "s", "", "Service ID filter")
		_ = fs.Parse(args[1:])

		schedules, err := client.ListBackupSchedules(context.Background(), *service)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Listing backup schedules failed: %v\n", err)
			os.Exit(1)
		}
		if len(schedules) == 0 {
			fmt.Println("No automated backup schedules found.")
			return
		}
		fmt.Printf("%-24s %-20s %-12s %-16s %-10s %s\n", "SCHEDULE ID", "SERVICE", "ENGINE", "CRON EXPR", "RETENTION", "STATUS")
		fmt.Println(strings.Repeat("-", 90))
		for _, s := range schedules {
			statusStr := "Disabled"
			if s.IsEnabled {
				statusStr = "Active"
			}
			ret := fmt.Sprintf("%dd", s.RetentionDaily)
			if ret == "0d" {
				ret = fmt.Sprintf("%dd", s.RetentionWeekly*7)
			}
			fmt.Printf("%-24s %-20s %-12s %-16s %-10s %s\n", s.ID, s.ServiceID, s.Engine, s.CronExpr, ret, statusStr)
		}

	case "create", "add":
		fs := flag.NewFlagSet("schedule create", flag.ExitOnError)
		service := fs.StringP("service", "s", "", "Service / Database name (required)")
		engine := fs.StringP("engine", "e", "postgres", "Database engine (postgres|mysql|redis|mongodb)")
		cron := fs.StringP("cron", "c", "0 0 * * *", "Cron expression (standard 5-part)")
		retention := fs.IntP("retention", "r", 14, "Retention period in days")
		_ = fs.Parse(args[1:])

		if *service == "" {
			fmt.Println("Usage: pikpik schedule create --service <db_name> [--engine <postgres>] [--cron \"0 0 * * * \"] [--retention 14]")
			os.Exit(1)
		}

		enabled := true
		sch, err := client.CreateBackupSchedule(context.Background(), api.CreateBackupScheduleRequest{
			ServiceID:      *service,
			DatabaseType:   *engine,
			Engine:         *engine,
			CronExpr:       *cron,
			CronExpression: *cron,
			RetentionDays:  *retention,
			RetentionDaily: *retention,
			IsEnabled:      &enabled,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Creating backup schedule failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Automated backup schedule created (ID: %s, Cron: %q, Retention: %dd)\n", sch.ID, sch.CronExpr, sch.RetentionDaily)

	case "rm", "delete", "remove":
		if len(args) < 2 {
			fmt.Println("Usage: pikpik schedule rm <schedule_id>")
			os.Exit(1)
		}
		id := args[1]
		if err := client.DeleteBackupSchedule(context.Background(), id); err != nil {
			fmt.Fprintf(os.Stderr, "Deleting backup schedule failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Backup schedule %q deleted.\n", id)

	default:
		fmt.Printf("Unknown schedule subcommand: %s\nUsage: pikpik schedule [list|create|rm]\n", sub)
		os.Exit(1)
	}
}

func runNotify(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: pikpik notify [list|add|test|rm] [flags]")
		return
	}

	client, _, _ := getClient()
	sub := args[0]

	switch sub {
	case "list", "ls":
		fs := flag.NewFlagSet("notify list", flag.ExitOnError)
		project := fs.String("project", "", "Filter channels by project ID")
		jsonOutput := fs.Bool("json", false, "Output in raw JSON format")
		_ = fs.Parse(args[1:])

		channels, err := client.ListNotificationChannels(context.Background(), *project)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Listing notification channels failed: %v\n", err)
			os.Exit(1)
		}

		if *jsonOutput {
			data, _ := json.MarshalIndent(channels, "", "  ")
			fmt.Println(string(data))
			return
		}

		if len(channels) == 0 {
			fmt.Println("No notification channels configured.")
			return
		}

		fmt.Printf("%-24s %-20s %-10s %-8s %-30s %s\n", "ID", "NAME", "TYPE", "ENABLED", "EVENTS", "TARGET URL")
		fmt.Println(strings.Repeat("-", 110))
		for _, ch := range channels {
			eventsStr := strings.Join(ch.Events, ",")
			if len(eventsStr) > 28 {
				eventsStr = eventsStr[:25] + "..."
			}
			urlStr := ch.TargetURL
			if len(urlStr) > 35 {
				urlStr = urlStr[:32] + "..."
			}
			enabledStr := "yes"
			if !ch.Enabled {
				enabledStr = "no"
			}
			fmt.Printf("%-24s %-20s %-10s %-8s %-30s %s\n", ch.ID, ch.Name, ch.Type, enabledStr, eventsStr, urlStr)
		}

	case "add", "create":
		fs := flag.NewFlagSet("notify add", flag.ExitOnError)
		name := fs.String("name", "", "Display name for the channel (required)")
		chType := fs.String("type", "webhook", "Channel type: webhook, discord, slack, telegram")
		targetURL := fs.String("url", "", "Target webhook URL or API endpoint (required)")
		project := fs.String("project", "", "Optional project ID scope")
		authToken := fs.String("auth-token", "", "Optional Bearer auth token for generic webhook")
		events := fs.String("events", "deploy:failure,deploy:success,backup:failure,backup:success", "Comma-separated event triggers")
		_ = fs.Parse(args[1:])

		if *name == "" || *targetURL == "" {
			fmt.Println("Usage: pikpik notify add --name <name> --type <webhook|discord|slack|telegram> --url <url> [--events <e1,e2>]")
			os.Exit(1)
		}

		eventList := strings.Split(*events, ",")
		for i := range eventList {
			eventList[i] = strings.TrimSpace(eventList[i])
		}

		ch, err := client.CreateNotificationChannel(context.Background(), api.CreateNotificationChannelRequest{
			Name:      *name,
			ProjectID: *project,
			Type:      *chType,
			TargetURL: *targetURL,
			AuthToken: *authToken,
			Events:    eventList,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Creating notification channel failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Notification channel %q created successfully (ID: %s, Type: %s)\n", ch.Name, ch.ID, ch.Type)

	case "test", "ping":
		if len(args) < 2 {
			fmt.Println("Usage: pikpik notify test <channel_id>")
			os.Exit(1)
		}
		id := args[1]
		if err := client.TestNotificationChannel(context.Background(), id); err != nil {
			fmt.Fprintf(os.Stderr, "Testing notification channel failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Test notification ping sent successfully to channel %q.\n", id)

	case "rm", "delete", "remove":
		if len(args) < 2 {
			fmt.Println("Usage: pikpik notify rm <channel_id>")
			os.Exit(1)
		}
		id := args[1]
		if err := client.DeleteNotificationChannel(context.Background(), id); err != nil {
			fmt.Fprintf(os.Stderr, "Deleting notification channel failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Notification channel %q deleted successfully.\n", id)

	default:
		fmt.Printf("Unknown notification subcommand: %s\nUsage: pikpik notify [list|add|test|rm]\n", sub)
		os.Exit(1)
	}
}

// formatBytesInt formats byte counts into human-readable string (KiB, MiB, GiB).
func formatBytesInt(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}

func runUser(args []string) {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Println(`Manage cluster team members, roles, invitations, and passwords

Usage:
  pikpik user <command> [arguments]

Commands:
  list              List all users and assigned roles (--json for JSON output)
  invite            Generate a shareable team invitation link
  role              Update a user's role (owner, admin, developer, viewer)
  passwd            Reset a user's password and invalidate active sessions
  rm                Remove a user from the organization

Flags for 'invite':
  --email string    Invited user email (required)
  --role string     Role to assign: owner, admin, developer, viewer (default: developer)
  --days int        Invitation expiry in days (default: 7)

Flags for 'role':
  --role string     New role: owner, admin, developer, viewer (required)

Flags for 'passwd':
  --password string New password (min 8 chars) (required)`)
		return
	}

	client, _, _ := getClient()
	sub := args[0]

	switch sub {
	case "list", "ls":
		fs := flag.NewFlagSet("user list", flag.ExitOnError)
		jsonFlag := fs.Bool("json", false, "Output in JSON format")
		_ = fs.Parse(args[1:])

		users, err := client.ListUsers(context.Background())
		if err != nil {
			fmt.Fprintf(os.Stderr, "Listing users failed: %v\n", err)
			os.Exit(1)
		}

		if *jsonFlag {
			data, _ := json.MarshalIndent(users, "", "  ")
			fmt.Println(string(data))
			return
		}

		if len(users) == 0 {
			fmt.Println("No users registered.")
			return
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
		fmt.Fprintln(w, "USER ID\tEMAIL\tROLE\t2FA\tCREATED")
		for _, u := range users {
			totpStr := "disabled"
			if u.TOTPEnabled {
				totpStr = "enabled"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
				u.ID, u.Email, strings.ToUpper(u.Role), totpStr, u.CreatedAt.Format("2006-01-02 15:04"),
			)
		}
		w.Flush()

	case "invite":
		fs := flag.NewFlagSet("user invite", flag.ExitOnError)
		email := fs.String("email", "", "Invited email address")
		role := fs.String("role", "developer", "Role: owner, admin, developer, viewer")
		days := fs.Int("days", 7, "Expiration in days")
		_ = fs.Parse(args[1:])

		if *email == "" {
			fmt.Fprintln(os.Stderr, "Error: --email is required")
			os.Exit(1)
		}

		inv, err := client.InviteUser(context.Background(), *email, *role, *days)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Inviting user failed: %v\n", err)
			os.Exit(1)
		}

		fmt.Println("Team invitation created successfully!")
		fmt.Printf("Email:       %s\n", inv.Email)
		fmt.Printf("Role:        %s\n", strings.ToUpper(inv.Role))
		fmt.Printf("Expires:     %s\n", inv.ExpiresAt.Format("2006-01-02 15:04:05 UTC"))
		fmt.Printf("Invite URL:  %s\n", inv.InviteURL)

	case "role":
		if len(args) < 2 {
			fmt.Println("Usage: pikpik user role <user_id> --role <role>")
			os.Exit(1)
		}
		userID := args[1]
		fs := flag.NewFlagSet("user role", flag.ExitOnError)
		role := fs.String("role", "", "New role: owner, admin, developer, viewer")
		_ = fs.Parse(args[2:])

		if *role == "" {
			fmt.Fprintln(os.Stderr, "Error: --role is required")
			os.Exit(1)
		}

		if err := client.UpdateUserRole(context.Background(), userID, *role); err != nil {
			fmt.Fprintf(os.Stderr, "Updating role failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Role for user %q updated to %q.\n", userID, strings.ToUpper(*role))

	case "passwd":
		if len(args) < 2 {
			fmt.Println("Usage: pikpik user passwd <user_id> --password <new_password>")
			os.Exit(1)
		}
		userID := args[1]
		fs := flag.NewFlagSet("user passwd", flag.ExitOnError)
		pwd := fs.String("password", "", "New password (min 8 characters)")
		_ = fs.Parse(args[2:])

		if len(*pwd) < 8 {
			fmt.Fprintln(os.Stderr, "Error: --password must be at least 8 characters")
			os.Exit(1)
		}

		if err := client.ResetUserPassword(context.Background(), userID, *pwd); err != nil {
			fmt.Fprintf(os.Stderr, "Password reset failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Password for user %q reset successfully and active sessions invalidated.\n", userID)

	case "rm", "delete":
		if len(args) < 2 {
			fmt.Println("Usage: pikpik user rm <user_id>")
			os.Exit(1)
		}
		userID := args[1]
		if err := client.DeleteUser(context.Background(), userID); err != nil {
			fmt.Fprintf(os.Stderr, "Deleting user failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("User %q removed successfully.\n", userID)

	default:
		fmt.Printf("Unknown user subcommand: %s\nRun 'pikpik user --help' for available commands.\n", sub)
		os.Exit(1)
	}
}

func runIntegration(args []string) {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Println(`Manage external Git providers, Container Registries, and S3 Storage destinations

Usage:
  pikpik integration <command> [arguments]

Commands:
  list              List all registered integrations (--json for JSON output)
  add               Register a new external provider integration
  test              Perform a live connectivity test
  rm                Delete an integration

Flags for 'add':
  --name string     Display name (required)
  --type string     Type: git_github, git_gitlab, git_gitea, registry_dockerhub, registry_ghcr, storage_s3 (required)
  --credentials str Secret token, API key, or registry password (required)
  --config string   Optional JSON configuration string`)
		return
	}

	client, _, _ := getClient()
	sub := args[0]

	switch sub {
	case "list", "ls":
		fs := flag.NewFlagSet("integration list", flag.ExitOnError)
		orgFlag := fs.String("org", "org_default", "Organization ID")
		jsonFlag := fs.Bool("json", false, "Output in JSON format")
		_ = fs.Parse(args[1:])

		items, err := client.ListIntegrations(context.Background(), *orgFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Listing integrations failed: %v\n", err)
			os.Exit(1)
		}

		if *jsonFlag {
			data, _ := json.MarshalIndent(items, "", "  ")
			fmt.Println(string(data))
			return
		}

		if len(items) == 0 {
			fmt.Println("No integrations registered. Add one using 'pikpik integration add'.")
			return
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
		fmt.Fprintln(w, "ID\tNAME\tTYPE\tSTATUS\tCREATED")
		for _, it := range items {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
				it.ID, it.Name, it.Type, strings.ToUpper(it.Status), it.CreatedAt.Format("2006-01-02 15:04"),
			)
		}
		w.Flush()

	case "add", "create":
		fs := flag.NewFlagSet("integration add", flag.ExitOnError)
		orgFlag := fs.String("org", "org_default", "Organization ID")
		name := fs.String("name", "", "Integration name")
		intType := fs.String("type", "", "Type: git_github, git_gitlab, registry_dockerhub, registry_ghcr, storage_s3")
		creds := fs.String("credentials", "", "API token or secret key")
		cfg := fs.String("config", "{}", "JSON configuration")
		_ = fs.Parse(args[1:])

		if *name == "" || *intType == "" || *creds == "" {
			fmt.Fprintln(os.Stderr, "Error: --name, --type, and --credentials are required")
			os.Exit(1)
		}

		it, err := client.CreateIntegration(context.Background(), *orgFlag, api.CreateIntegrationRequest{
			Name:        *name,
			Type:        *intType,
			Credentials: *creds,
			ConfigJSON:  *cfg,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Creating integration failed: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Integration %q (%s) registered successfully [ID: %s]\n", it.Name, it.Type, it.ID)

	case "test":
		if len(args) < 2 {
			fmt.Println("Usage: pikpik integration test <integration_id>")
			os.Exit(1)
		}
		id := args[1]
		res, err := client.TestIntegration(context.Background(), id)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Testing integration failed: %v\n", err)
			os.Exit(1)
		}

		status := "FAILED"
		if res.Success {
			status = "SUCCESS"
		}
		fmt.Printf("[%s] %s (latency: %dms)\n", status, res.Message, res.LatencyMS)

	case "rm", "delete":
		if len(args) < 2 {
			fmt.Println("Usage: pikpik integration rm <integration_id>")
			os.Exit(1)
		}
		id := args[1]
		if err := client.DeleteIntegration(context.Background(), id); err != nil {
			fmt.Fprintf(os.Stderr, "Deleting integration failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Integration %q deleted successfully.\n", id)

	default:
		fmt.Printf("Unknown integration subcommand: %s\nRun 'pikpik integration --help' for available commands.\n", sub)
		os.Exit(1)
	}
}




