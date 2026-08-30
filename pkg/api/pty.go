package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/docker/docker/api/types/container"
	dockerclient "github.com/docker/docker/client"
	"github.com/gorilla/websocket"
)

// PTYHandler handles bidirectional interactive terminal sessions between WebSocket and Docker exec.
type PTYHandler struct {
	dockerCli dockerclient.CommonAPIClient
}

// NewPTYHandler creates a new PTYHandler instance.
func NewPTYHandler(dockerCli dockerclient.CommonAPIClient) *PTYHandler {
	return &PTYHandler{dockerCli: dockerCli}
}

// ServeHTTP handles /ws/pty WebSocket connections.
func (h *PTYHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	containerID := r.URL.Query().Get("container_id")
	if containerID == "" {
		containerID = r.URL.Query().Get("app_id")
	}
	if containerID == "" {
		http.Error(w, "missing container_id", http.StatusBadRequest)
		return
	}

	cmdStr := r.URL.Query().Get("cmd")
	var cmd []string
	if cmdStr != "" {
		cmd = strings.Fields(cmdStr)
	}
	if len(cmd) == 0 {
		cmd = []string{"/bin/sh"}
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

	if h.dockerCli == nil {
		_ = conn.WriteMessage(websocket.BinaryMessage, append([]byte{0xFF}, []byte(`{"error":"docker client unavailable"}`)...))
		return
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// 1. Create Docker Exec Configuration
	execCfg := container.ExecOptions{
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          true,
		Cmd:          cmd,
	}

	execResp, err := h.dockerCli.ContainerExecCreate(ctx, containerID, execCfg)
	if err != nil {
		_ = conn.WriteMessage(websocket.BinaryMessage, append([]byte{0xFF}, []byte(`{"error":"`+err.Error()+`"}`)...))
		return
	}

	// 2. Attach Hijacked Connection
	attachResp, err := h.dockerCli.ContainerExecAttach(ctx, execResp.ID, container.ExecStartOptions{Tty: true})
	if err != nil {
		_ = conn.WriteMessage(websocket.BinaryMessage, append([]byte{0xFF}, []byte(`{"error":"`+err.Error()+`"}`)...))
		return
	}
	defer attachResp.Close()

	// 3. Stdout Goroutine: Docker -> WebSocket
	go func() {
		buf := make([]byte, 4096)
		for {
			n, readErr := attachResp.Reader.Read(buf)
			if n > 0 {
				frame := append([]byte{0x00}, buf[:n]...)
				if wsErr := conn.WriteMessage(websocket.BinaryMessage, frame); wsErr != nil {
					cancel()
					return
				}
			}
			if readErr != nil {
				// Container process exited
				exitFrame := []byte{0xFF}
				exitJSON, _ := json.Marshal(TermExitMessage{ExitCode: 0})
				_ = conn.WriteMessage(websocket.BinaryMessage, append(exitFrame, exitJSON...))
				cancel()
				return
			}
		}
	}()

	// 4. Stdin Loop: WebSocket -> Docker
	for {
		msgType, payload, wsErr := conn.ReadMessage()
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
				if err := json.Unmarshal(data, &resize); err == nil {
					_ = h.dockerCli.ContainerExecResize(ctx, execResp.ID, container.ResizeOptions{
						Height: resize.Rows,
						Width:  resize.Cols,
					})
				}
			case 0x02: // Interruption Signal
				// SIGINT (0x03 byte into stdin)
				_, _ = attachResp.Conn.Write([]byte{0x03})
			}
		} else if msgType == websocket.TextMessage {
			// Allow text JSON resize commands as well
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
