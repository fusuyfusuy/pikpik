package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

var validContainerIDRegex = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)

func validateContainerID(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("missing container id")
	}
	if strings.Contains(id, "/") || strings.Contains(id, "\\") || strings.Contains(id, "..") || strings.Contains(id, "%") || !validContainerIDRegex.MatchString(id) {
		return fmt.Errorf("invalid container id %q: path traversal or invalid characters detected", id)
	}
	return nil
}

func sanitizeTail(tail string) string {
	tail = strings.TrimSpace(tail)
	if tail == "" || tail == "all" {
		if tail == "all" {
			return "all"
		}
		return "100"
	}
	for _, r := range tail {
		if r < '0' || r > '9' {
			return "100"
		}
	}
	return tail
}

// CommandHandlerFunc is the signature for handling specific agent commands.
type CommandHandlerFunc func(ctx context.Context, cmd *CommandPayload) (interface{}, error)

// CommandDispatcher coordinates local command execution on the worker node.
type CommandDispatcher interface {
	Dispatch(ctx context.Context, cmd *CommandPayload) (*CommandResult, error)
	RegisterHandler(command string, handler CommandHandlerFunc)
}

type defaultDispatcher struct {
	mu           sync.RWMutex
	handlers     map[string]CommandHandlerFunc
	dockerClient *http.Client
	dockerSocket string
}

// NewCommandDispatcher creates a new dispatcher with built-in system and Docker handlers.
func NewCommandDispatcher(dockerSocket string) CommandDispatcher {
	if dockerSocket == "" {
		dockerSocket = "/var/run/docker.sock"
	}

	tr := &http.Transport{
		DialContext: func(ctx context.Context, proto, addr string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", dockerSocket)
		},
		DisableKeepAlives: false,
		IdleConnTimeout:   30 * time.Second,
	}

	d := &defaultDispatcher{
		handlers:     make(map[string]CommandHandlerFunc),
		dockerClient: &http.Client{Transport: tr, Timeout: 30 * time.Second},
		dockerSocket: dockerSocket,
	}

	// Register built-in handlers
	d.RegisterHandler("ping", d.handlePing)
	d.RegisterHandler("host.info", d.handleHostInfo)
	d.RegisterHandler("docker.ps", d.handleDockerPS)
	d.RegisterHandler("docker.inspect", d.handleDockerInspect)
	d.RegisterHandler("docker.logs", d.handleDockerLogs)

	return d
}

// RegisterHandler binds a command name to a handler function.
func (d *defaultDispatcher) RegisterHandler(command string, handler CommandHandlerFunc) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.handlers[command] = handler
}

// Dispatch executes the requested command payload and produces a structured CommandResult.
func (d *defaultDispatcher) Dispatch(ctx context.Context, cmd *CommandPayload) (*CommandResult, error) {
	if cmd == nil {
		return nil, fmt.Errorf("agent: nil command payload")
	}

	d.mu.RLock()
	handler, exists := d.handlers[cmd.Command]
	d.mu.RUnlock()

	if !exists {
		return &CommandResult{
			ID:      cmd.ID,
			Success: false,
			Error:   fmt.Sprintf("unknown command '%s'", cmd.Command),
		}, nil
	}

	data, err := handler(ctx, cmd)
	if err != nil {
		return &CommandResult{
			ID:      cmd.ID,
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	return &CommandResult{
		ID:      cmd.ID,
		Success: true,
		Data:    data,
	}, nil
}

func (d *defaultDispatcher) handlePing(ctx context.Context, cmd *CommandPayload) (interface{}, error) {
	return map[string]interface{}{
		"pong":      true,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func (d *defaultDispatcher) handleHostInfo(ctx context.Context, cmd *CommandPayload) (interface{}, error) {
	hostname, _ := os.Hostname()
	return map[string]interface{}{
		"os":         runtime.GOOS,
		"arch":       runtime.GOARCH,
		"hostname":   hostname,
		"num_cpu":    runtime.NumCPU(),
		"go_version": runtime.Version(),
	}, nil
}

func (d *defaultDispatcher) handleDockerPS(ctx context.Context, cmd *CommandPayload) (interface{}, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "http://localhost/containers/json?all=true", nil)
	if err != nil {
		return nil, err
	}

	resp, err := d.dockerClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("docker ps error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("docker ps status code %d", resp.StatusCode)
	}

	var raw json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func (d *defaultDispatcher) handleDockerInspect(ctx context.Context, cmd *CommandPayload) (interface{}, error) {
	var targetID string
	if len(cmd.Args) > 0 {
		targetID = cmd.Args[0]
	} else if val, ok := cmd.Params["id"].(string); ok {
		targetID = val
	}

	if err := validateContainerID(targetID); err != nil {
		return nil, err
	}

	url := fmt.Sprintf("http://localhost/containers/%s/json", url.PathEscape(targetID))
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := d.dockerClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("docker inspect error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("docker inspect returned %d", resp.StatusCode)
	}

	var raw json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func (d *defaultDispatcher) handleDockerLogs(ctx context.Context, cmd *CommandPayload) (interface{}, error) {
	var targetID string
	tail := "100"

	if len(cmd.Args) > 0 {
		targetID = cmd.Args[0]
		if len(cmd.Args) > 1 {
			tail = cmd.Args[1]
		}
	} else {
		if val, ok := cmd.Params["id"].(string); ok {
			targetID = val
		}
		if val, ok := cmd.Params["tail"].(string); ok {
			tail = val
		}
	}

	if err := validateContainerID(targetID); err != nil {
		return nil, err
	}
	tail = sanitizeTail(tail)

	url := fmt.Sprintf("http://localhost/containers/%s/logs?stdout=true&stderr=true&tail=%s", url.PathEscape(targetID), url.QueryEscape(tail))
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := d.dockerClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("docker logs error: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Clean binary multiplex prefix (8 bytes per frame in docker raw logs stream)
	lines := strings.Split(string(body), "\n")
	cleaned := make([]string, 0, len(lines))
	for _, l := range lines {
		if len(l) > 8 {
			cleaned = append(cleaned, l[8:])
		} else if len(l) > 0 {
			cleaned = append(cleaned, l)
		}
	}

	return map[string]interface{}{
		"container_id": targetID,
		"logs":         cleaned,
	}, nil
}
