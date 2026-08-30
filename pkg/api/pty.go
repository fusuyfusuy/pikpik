package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	dockerclient "github.com/docker/docker/client"
	"github.com/gorilla/websocket"
	"golang.org/x/sys/unix"
)

// PTYHandler handles bidirectional interactive terminal sessions over WebSocket
// across Docker containers, Swarm tasks, and Host Machine operating systems.
type PTYHandler struct {
	dockerCli dockerclient.CommonAPIClient
}

// NewPTYHandler creates a new PTYHandler instance.
func NewPTYHandler(dockerCli dockerclient.CommonAPIClient) *PTYHandler {
	return &PTYHandler{dockerCli: dockerCli}
}

type safeWSConn struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (s *safeWSConn) WriteBinary(data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conn.WriteMessage(websocket.BinaryMessage, data)
}

func (s *safeWSConn) WriteExit(code int, errMsg string) {
	exitJSON, _ := json.Marshal(TermExitMessage{ExitCode: code, Error: errMsg})
	_ = s.WriteBinary(append([]byte{0xFF}, exitJSON...))
}

// ServeHTTP handles /ws/pty WebSocket connections with multi-target dispatching.
func (h *PTYHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	targetType := strings.ToLower(r.URL.Query().Get("target_type"))
	targetID := r.URL.Query().Get("target_id")
	if targetID == "" {
		targetID = r.URL.Query().Get("container_id")
	}
	if targetID == "" {
		targetID = r.URL.Query().Get("app_id")
	}
	if targetID == "" {
		targetID = r.URL.Query().Get("task_id")
	}

	if targetType == "" {
		if targetID != "" {
			targetType = "container"
		} else {
			targetType = "host_machine"
		}
	}

	cmdStr := r.URL.Query().Get("cmd")
	var cmd []string
	if cmdStr != "" {
		cmd = strings.Fields(cmdStr)
	}

	responseHeader := make(http.Header)
	for _, p := range websocket.Subprotocols(r) {
		if strings.HasPrefix(p, "pikpik-auth") {
			responseHeader.Set("Sec-WebSocket-Protocol", "pikpik-auth")
			break
		}
	}

	conn, err := upgrader.Upgrade(w, r, responseHeader)
	if err != nil {
		return
	}
	defer conn.Close()

	safeConn := &safeWSConn{conn: conn}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	switch targetType {
	case "container":
		if targetID == "" {
			safeConn.WriteExit(1, "missing target_id or container_id")
			return
		}
		if len(cmd) == 0 {
			cmd = []string{"/bin/sh"}
		}
		h.handleContainerPTY(ctx, cancel, safeConn, targetID, cmd)

	case "swarm_task":
		if targetID == "" {
			safeConn.WriteExit(1, "missing target_id or task_id")
			return
		}
		if len(cmd) == 0 {
			cmd = []string{"/bin/sh"}
		}
		h.handleSwarmTaskPTY(ctx, cancel, safeConn, targetID, cmd)

	case "host_machine", "host", "node":
		role := GetRoleFromContext(r.Context())
		if role != RoleAdmin && role != RoleOwner {
			safeConn.WriteExit(1, "forbidden: host machine terminal requires admin or owner role")
			return
		}
		if len(cmd) == 0 {
			if _, err := os.Stat("/bin/bash"); err == nil {
				cmd = []string{"/bin/bash"}
			} else {
				cmd = []string{"/bin/sh"}
			}
		}
		h.handleHostPTY(ctx, cancel, safeConn, cmd)

	default:
		safeConn.WriteExit(1, fmt.Sprintf("unsupported target_type: %s", targetType))
	}
}

func (h *PTYHandler) handleContainerPTY(ctx context.Context, cancel context.CancelFunc, sConn *safeWSConn, containerID string, cmd []string) {
	if h.dockerCli == nil {
		sConn.WriteExit(1, "docker client unavailable")
		return
	}

	execCfg := container.ExecOptions{
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          true,
		Cmd:          cmd,
	}

	execResp, err := h.dockerCli.ContainerExecCreate(ctx, containerID, execCfg)
	if err != nil {
		sConn.WriteExit(1, err.Error())
		return
	}

	attachResp, err := h.dockerCli.ContainerExecAttach(ctx, execResp.ID, container.ExecStartOptions{Tty: true})
	if err != nil {
		sConn.WriteExit(1, err.Error())
		return
	}
	defer attachResp.Close()

	// Stdout Goroutine: Docker -> WebSocket
	go func() {
		buf := make([]byte, 4096)
		for {
			n, readErr := attachResp.Reader.Read(buf)
			if n > 0 {
				frame := append([]byte{0x00}, buf[:n]...)
				if wsErr := sConn.WriteBinary(frame); wsErr != nil {
					cancel()
					return
				}
			}
			if readErr != nil {
				sConn.WriteExit(0, "")
				cancel()
				return
			}
		}
	}()

	// Stdin Loop: WebSocket -> Docker
	for {
		msgType, payload, wsErr := sConn.conn.ReadMessage()
		if wsErr != nil || len(payload) == 0 {
			break
		}

		if msgType == websocket.BinaryMessage {
			opcode := payload[0]
			data := payload[1:]

			switch opcode {
			case 0x00: // Raw Stdin Bytes
				if _, wErr := attachResp.Conn.Write(data); wErr != nil {
					return
				}
			case 0x01: // Terminal Window Resize
				var resize TermResizeMessage
				if err := json.Unmarshal(data, &resize); err == nil && resize.Cols > 0 && resize.Rows > 0 {
					_ = h.dockerCli.ContainerExecResize(ctx, execResp.ID, container.ResizeOptions{
						Height: resize.Rows,
						Width:  resize.Cols,
					})
				}
			case 0x02: // Interruption Signal (SIGINT)
				_, _ = attachResp.Conn.Write([]byte{0x03})
			case 0xFF: // Exit
				cancel()
				return
			}
		} else if msgType == websocket.TextMessage {
			var resize TermResizeMessage
			if err := json.Unmarshal(payload, &resize); err == nil && resize.Cols > 0 {
				_ = h.dockerCli.ContainerExecResize(ctx, execResp.ID, container.ResizeOptions{
					Height: resize.Rows,
					Width:  resize.Cols,
				})
			}
		}
	}
}

func (h *PTYHandler) handleSwarmTaskPTY(ctx context.Context, cancel context.CancelFunc, sConn *safeWSConn, taskID string, cmd []string) {
	if h.dockerCli == nil {
		sConn.WriteExit(1, "docker client unavailable")
		return
	}

	containerID := ""

	// 1. Inspect direct Swarm Task
	task, _, err := h.dockerCli.TaskInspectWithRaw(ctx, taskID)
	if err == nil && task.Status.ContainerStatus != nil && task.Status.ContainerStatus.ContainerID != "" {
		containerID = task.Status.ContainerStatus.ContainerID
	}

	// 2. Fallback: query task list by service or task filter
	if containerID == "" {
		taskList, err := h.dockerCli.TaskList(ctx, types.TaskListOptions{
			Filters: filters.NewArgs(
				filters.Arg("id", taskID),
				filters.Arg("service", taskID),
			),
		})
		if err == nil {
			for _, t := range taskList {
				if t.Status.ContainerStatus != nil && t.Status.ContainerStatus.ContainerID != "" {
					containerID = t.Status.ContainerStatus.ContainerID
					break
				}
			}
		}
	}

	// 3. Fallback: treat taskID directly as container ID
	if containerID == "" {
		containerID = taskID
	}

	h.handleContainerPTY(ctx, cancel, sConn, containerID, cmd)
}

type hostSession struct {
	cmd    *exec.Cmd
	ptmx   *os.File
	stdin  io.WriteCloser
	stdout io.Reader
	isPTY  bool
}

func (s *hostSession) Resize(cols, rows uint) {
	if s.isPTY && s.ptmx != nil && cols > 0 && rows > 0 {
		_ = unix.IoctlSetWinsize(int(s.ptmx.Fd()), unix.TIOCSWINSZ, &unix.Winsize{
			Row: uint16(rows),
			Col: uint16(cols),
		})
	}
}

func (s *hostSession) Write(p []byte) (int, error) {
	if s.isPTY && s.ptmx != nil {
		return s.ptmx.Write(p)
	}
	if s.stdin != nil {
		return s.stdin.Write(p)
	}
	return 0, io.EOF
}

func (s *hostSession) Close() {
	if s.ptmx != nil {
		_ = s.ptmx.Close()
	}
	if s.stdin != nil {
		_ = s.stdin.Close()
	}
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
}

func startHostSession(ctx context.Context, cmdArgs []string) (*hostSession, error) {
	if len(cmdArgs) == 0 {
		cmdArgs = []string{"/bin/sh"}
	}

	cmd := exec.CommandContext(ctx, cmdArgs[0], cmdArgs[1:]...)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	// Attempt Linux PTY master/slave allocation via /dev/ptmx
	ptmx, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err == nil && ptmx != nil {
		ptn, ptnErr := unix.IoctlGetInt(int(ptmx.Fd()), unix.TIOCGPTN)
		if ptnErr == nil {
			var unlock int
			if unlockErr := unix.IoctlSetPointerInt(int(ptmx.Fd()), unix.TIOCSPTLCK, unlock); unlockErr == nil {
				ptsPath := fmt.Sprintf("/dev/pts/%d", ptn)
				pts, ptsErr := os.OpenFile(ptsPath, os.O_RDWR|unix.O_NOCTTY, 0)
				if ptsErr == nil {
					cmd.Stdin = pts
					cmd.Stdout = pts
					cmd.Stderr = pts
					cmd.SysProcAttr = &syscall.SysProcAttr{
						Setsid:  true,
						Setctty: true,
					}
					if startErr := cmd.Start(); startErr == nil {
						_ = pts.Close()
						return &hostSession{
							cmd:   cmd,
							ptmx:  ptmx,
							isPTY: true,
						}, nil
					}
					_ = pts.Close()
				}
			}
		}
		_ = ptmx.Close()
	}

	// Fallback to standard pipe redirection
	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to open stdin pipe: %w", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdinPipe.Close()
		return nil, fmt.Errorf("failed to open stdout pipe: %w", err)
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		_ = stdinPipe.Close()
		return nil, fmt.Errorf("failed to start host command: %w", err)
	}

	return &hostSession{
		cmd:    cmd,
		stdin:  stdinPipe,
		stdout: stdoutPipe,
		isPTY:  false,
	}, nil
}

func (h *PTYHandler) handleHostPTY(ctx context.Context, cancel context.CancelFunc, sConn *safeWSConn, cmd []string) {
	session, err := startHostSession(ctx, cmd)
	if err != nil {
		sConn.WriteExit(1, err.Error())
		return
	}
	defer session.Close()

	var reader io.Reader
	if session.isPTY {
		reader = session.ptmx
	} else {
		reader = session.stdout
	}

	// Output goroutine: Host -> WebSocket
	go func() {
		buf := make([]byte, 4096)
		for {
			n, readErr := reader.Read(buf)
			if n > 0 {
				frame := append([]byte{0x00}, buf[:n]...)
				if wsErr := sConn.WriteBinary(frame); wsErr != nil {
					cancel()
					return
				}
			}
			if readErr != nil {
				exitCode := 0
				if waitErr := session.cmd.Wait(); waitErr != nil {
					if exitErr, ok := waitErr.(*exec.ExitError); ok {
						exitCode = exitErr.ExitCode()
					} else {
						exitCode = 1
					}
				}
				sConn.WriteExit(exitCode, "")
				cancel()
				return
			}
		}
	}()

	// Input Loop: WebSocket -> Host
	for {
		msgType, payload, wsErr := sConn.conn.ReadMessage()
		if wsErr != nil || len(payload) == 0 {
			break
		}

		if msgType == websocket.BinaryMessage {
			opcode := payload[0]
			data := payload[1:]

			switch opcode {
			case 0x00: // Raw Stdin
				if _, wErr := session.Write(data); wErr != nil {
					return
				}
			case 0x01: // Terminal Window Resize
				var resize TermResizeMessage
				if err := json.Unmarshal(data, &resize); err == nil {
					session.Resize(resize.Cols, resize.Rows)
				}
			case 0x02: // Interruption Signal (SIGINT)
				if session.isPTY {
					_, _ = session.Write([]byte{0x03})
				} else if session.cmd.Process != nil {
					_ = session.cmd.Process.Signal(syscall.SIGINT)
				}
			case 0xFF: // Exit
				cancel()
				return
			}
		} else if msgType == websocket.TextMessage {
			var resize TermResizeMessage
			if err := json.Unmarshal(payload, &resize); err == nil && resize.Cols > 0 {
				session.Resize(resize.Cols, resize.Rows)
			}
		}
	}
}
