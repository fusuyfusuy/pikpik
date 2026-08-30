package api_test

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fusuycorp/pikpik/pkg/api"
	"github.com/gorilla/websocket"
)

// 1. Test Host Machine Interactive PTY Session (Echo, Resize & Clean Exit)
func TestPTY_HostMachine_Interactive(t *testing.T) {
	ptyHandler := api.NewPTYHandler(nil)
	server := httptest.NewServer(ptyHandler)
	defer server.Close()

	wsURL := "ws" + server.URL[4:] + "?target_type=host_machine&cmd=/bin/sh"
	dialer := websocket.Dialer{}
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to dial pty websocket: %v", err)
	}
	defer conn.Close()

	// 1. Send stdin command: echo hello_pty_test
	inputCmd := []byte("echo hello_pty_test\n")
	stdinFrame := append([]byte{0x00}, inputCmd...)
	if err := conn.WriteMessage(websocket.BinaryMessage, stdinFrame); err != nil {
		t.Fatalf("failed to write stdin frame: %v", err)
	}

	// 2. Read stdout frame
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	foundOutput := false
	for i := 0; i < 10; i++ {
		msgType, payload, err := conn.ReadMessage()
		if err != nil {
			break
		}
		if msgType == websocket.BinaryMessage && len(payload) > 1 && payload[0] == 0x00 {
			if strings.Contains(string(payload[1:]), "hello_pty_test") {
				foundOutput = true
				break
			}
		}
	}
	if !foundOutput {
		t.Fatalf("expected stdout frame containing hello_pty_test")
	}

	// 3. Send Window Resize Frame: 0x01 + JSON
	resizePayload, _ := json.Marshal(api.TermResizeMessage{Cols: 120, Rows: 34})
	resizeFrame := append([]byte{0x01}, resizePayload...)
	if err := conn.WriteMessage(websocket.BinaryMessage, resizeFrame); err != nil {
		t.Fatalf("failed to write resize frame: %v", err)
	}

	// 4. Send Exit Command via stdin or 0xFF
	exitCmd := append([]byte{0x00}, []byte("exit 0\n")...)
	_ = conn.WriteMessage(websocket.BinaryMessage, exitCmd)

	// 5. Verify Exit Frame (0xFF)
	receivedExit := false
	for i := 0; i < 10; i++ {
		msgType, payload, err := conn.ReadMessage()
		if err != nil {
			break
		}
		if msgType == websocket.BinaryMessage && len(payload) > 0 && payload[0] == 0xFF {
			var exitMsg api.TermExitMessage
			if len(payload) > 1 {
				_ = json.Unmarshal(payload[1:], &exitMsg)
			}
			receivedExit = true
			break
		}
	}
	if !receivedExit {
		t.Fatalf("expected 0xFF exit frame upon process termination")
	}
}

// 2. Test Container PTY when Docker Engine is Unavailable
func TestPTY_Container_MissingDockerClient(t *testing.T) {
	ptyHandler := api.NewPTYHandler(nil)
	server := httptest.NewServer(ptyHandler)
	defer server.Close()

	wsURL := "ws" + server.URL[4:] + "?target_type=container&target_id=cnt_mock_01"
	dialer := websocket.Dialer{}
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to dial websocket: %v", err)
	}
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	msgType, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("expected message from pty handler, got: %v", err)
	}

	if msgType != websocket.BinaryMessage || len(payload) == 0 || payload[0] != 0xFF {
		t.Fatalf("expected 0xFF exit error frame, got msgType=%d, payload=%q", msgType, string(payload))
	}

	var exitMsg api.TermExitMessage
	_ = json.Unmarshal(payload[1:], &exitMsg)
	if !strings.Contains(exitMsg.Error, "docker client unavailable") {
		t.Fatalf("expected 'docker client unavailable' error, got: %s", exitMsg.Error)
	}
}

// 3. Test Swarm Task PTY when Docker Engine is Unavailable
func TestPTY_SwarmTask_MissingDockerClient(t *testing.T) {
	ptyHandler := api.NewPTYHandler(nil)
	server := httptest.NewServer(ptyHandler)
	defer server.Close()

	wsURL := "ws" + server.URL[4:] + "?target_type=swarm_task&target_id=task_mock_01"
	dialer := websocket.Dialer{}
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to dial websocket: %v", err)
	}
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	msgType, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("expected message from pty handler, got: %v", err)
	}

	if msgType != websocket.BinaryMessage || len(payload) == 0 || payload[0] != 0xFF {
		t.Fatalf("expected 0xFF exit error frame, got msgType=%d, payload=%q", msgType, string(payload))
	}

	var exitMsg api.TermExitMessage
	_ = json.Unmarshal(payload[1:], &exitMsg)
	if !strings.Contains(exitMsg.Error, "docker client unavailable") {
		t.Fatalf("expected 'docker client unavailable' error, got: %s", exitMsg.Error)
	}
}

// 4. Test Missing Target ID for Container
func TestPTY_MissingTargetID(t *testing.T) {
	ptyHandler := api.NewPTYHandler(nil)
	server := httptest.NewServer(ptyHandler)
	defer server.Close()

	wsURL := "ws" + server.URL[4:] + "?target_type=container"
	dialer := websocket.Dialer{}
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to dial websocket: %v", err)
	}
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("expected error frame: %v", err)
	}
	if len(payload) == 0 || payload[0] != 0xFF {
		t.Fatalf("expected 0xFF frame, got %v", payload)
	}
}

// 5. Test Unsupported Target Type
func TestPTY_UnsupportedTargetType(t *testing.T) {
	ptyHandler := api.NewPTYHandler(nil)
	server := httptest.NewServer(ptyHandler)
	defer server.Close()

	wsURL := "ws" + server.URL[4:] + "?target_type=invalid_quantum_mode"
	dialer := websocket.Dialer{}
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to dial websocket: %v", err)
	}
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("expected error frame: %v", err)
	}
	if len(payload) == 0 || payload[0] != 0xFF {
		t.Fatalf("expected 0xFF frame, got %v", payload)
	}
}

// 6. Test Host PTY Signals & Interruption (0x02 SIGINT Frame)
func TestPTY_HostMachine_SIGINT(t *testing.T) {
	ptyHandler := api.NewPTYHandler(nil)
	server := httptest.NewServer(ptyHandler)
	defer server.Close()

	wsURL := "ws" + server.URL[4:] + "?target_type=host_machine&cmd=/bin/sh+-i"
	dialer := websocket.Dialer{}
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to dial pty: %v", err)
	}
	defer conn.Close()

	// Send SIGINT frame 0x02
	_ = conn.WriteMessage(websocket.BinaryMessage, []byte{0x02})

	// Follow up with echo test
	_ = conn.WriteMessage(websocket.BinaryMessage, append([]byte{0x00}, []byte("echo after_interrupt\n")...))

	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	found := false
	for i := 0; i < 10; i++ {
		msgType, payload, err := conn.ReadMessage()
		if err != nil {
			break
		}
		if msgType == websocket.BinaryMessage && len(payload) > 1 && payload[0] == 0x00 {
			if strings.Contains(string(payload[1:]), "after_interrupt") {
				found = true
				break
			}
		}
	}

	if !found {
		t.Fatalf("expected output after SIGINT interruption")
	}

	// Exit session
	_ = conn.WriteMessage(websocket.BinaryMessage, []byte{0xFF})
}

// 7. Test Concurrent Host PTY Sessions (Race-Free)
func TestPTY_ConcurrentHostSessions_RaceFree(t *testing.T) {
	ptyHandler := api.NewPTYHandler(nil)
	server := httptest.NewServer(ptyHandler)
	defer server.Close()

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			wsURL := "ws" + server.URL[4:] + "?target_type=host_machine&cmd=/bin/sh"
			dialer := websocket.Dialer{}
			conn, _, err := dialer.Dial(wsURL, nil)
			if err != nil {
				return
			}
			defer conn.Close()

			_ = conn.WriteMessage(websocket.BinaryMessage, append([]byte{0x00}, []byte("echo ping_concurrent\n")...))
			_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))

			for j := 0; j < 5; j++ {
				_, _, rErr := conn.ReadMessage()
				if rErr != nil {
					break
				}
			}

			_ = conn.WriteMessage(websocket.BinaryMessage, []byte{0xFF})
		}(i)
	}

	wg.Wait()
}
