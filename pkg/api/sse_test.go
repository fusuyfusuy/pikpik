package api_test

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fusuycorp/pikpik/pkg/api"
)

// 1. Test FormatSSE serialization and multiline handling
func TestFormatSSE(t *testing.T) {
	// Simple event
	frame := api.FormatSSE("1001", "ping", "pong")
	expected := "id: 1001\nevent: ping\ndata: pong\n\n"
	if string(frame) != expected {
		t.Fatalf("expected:\n%q\ngot:\n%q", expected, string(frame))
	}

	// Multiline event
	multiline := api.FormatSSE("1002", "logs", "line 1\nline 2\nline 3")
	expectedMulti := "id: 1002\nevent: logs\ndata: line 1\ndata: line 2\ndata: line 3\n\n"
	if string(multiline) != expectedMulti {
		t.Fatalf("expected:\n%q\ngot:\n%q", expectedMulti, string(multiline))
	}

	// JSON object payload
	obj := map[string]string{"status": "ready", "env": "prod"}
	jsonFrame := api.FormatSSE("", "status_change", obj)
	if !strings.Contains(string(jsonFrame), "event: status_change\n") ||
		!strings.Contains(string(jsonFrame), `"status":"ready"`) {
		t.Fatalf("unexpected json frame output: %s", string(jsonFrame))
	}
}

// 2. Test Broadcaster Registration, Filtering & Slow Consumer Frame Dropping
func TestSSEBroadcaster_RegisterAndFiltering(t *testing.T) {
	b := api.NewSSEBroadcaster()
	defer b.Close()

	if count := b.ClientCount(); count != 0 {
		t.Fatalf("expected 0 clients, got %d", count)
	}

	c1 := b.Register("events", "app_1")
	c2 := b.Register("logs", "app_1")
	c3 := b.Register("events", "*")
	c4 := b.Register("*", "*")

	if count := b.ClientCount(); count != 4 {
		t.Fatalf("expected 4 clients, got %d", count)
	}

	// Broadcast matching events:app_1 -> c1, c3, c4 should receive
	b.Broadcast("events", "app_1", "deploy", "started")

	select {
	case msg := <-c1.SendChan():
		if !strings.Contains(string(msg), "started") {
			t.Fatalf("c1 unexpected message: %s", string(msg))
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timeout waiting for c1")
	}

	select {
	case <-c2.SendChan():
		t.Fatalf("c2 should not receive event for different channel (logs vs events)")
	default:
	}

	select {
	case msg := <-c3.SendChan():
		if !strings.Contains(string(msg), "started") {
			t.Fatalf("c3 unexpected message: %s", string(msg))
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timeout waiting for c3")
	}

	select {
	case msg := <-c4.SendChan():
		if !strings.Contains(string(msg), "started") {
			t.Fatalf("c4 unexpected message: %s", string(msg))
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timeout waiting for c4")
	}

	// Unregister clients
	b.Unregister(c1)
	b.Unregister(c2)
	b.Unregister(c3)
	b.Unregister(c4)

	if count := b.ClientCount(); count != 0 {
		t.Fatalf("expected 0 clients after unregister, got %d", count)
	}
}

// 3. Test HTTP SSE Streaming Endpoints with Headers and Event Delivery
func TestSSE_StreamingEndpoints(t *testing.T) {
	sseB := api.NewSSEBroadcaster()
	defer sseB.Close()

	ctrl := api.NewDefaultController(api.ControllerDependencies{
		SSEBroadcaster: sseB,
	})
	gw := api.NewAPIGatewayWithOptions(api.APIGatewayOptions{
		Controller:     ctrl,
		SSEBroadcaster: sseB,
	})
	server := httptest.NewServer(gw)
	defer server.Close()

	token := "pik_live_teststreamtoken123"

	// A. Events Stream: GET /api/v1/events/stream
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", server.URL+"/api/v1/events/stream?target_id=app_test", nil)
	if err != nil {
		t.Fatalf("failed to build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("events stream connection failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("expected Content-Type text/event-stream, got %s", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-cache" {
		t.Fatalf("expected Cache-Control no-cache, got %s", cc)
	}
	if xab := resp.Header.Get("X-Accel-Buffering"); xab != "no" {
		t.Fatalf("expected X-Accel-Buffering no, got %s", xab)
	}

	reader := bufio.NewReader(resp.Body)

	// Broadcast an event
	time.Sleep(30 * time.Millisecond)
	sseB.Broadcast("events", "app_test", "service_updated", `{"version":"v2"}`)

	lineChan := make(chan string, 10)
	go func() {
		for {
			line, rErr := reader.ReadString('\n')
			if rErr != nil {
				return
			}
			lineChan <- strings.TrimSpace(line)
		}
	}()

	foundEvent := false
	foundData := false
	deadline := time.After(2 * time.Second)

	for !foundEvent || !foundData {
		select {
		case line := <-lineChan:
			if strings.HasPrefix(line, "event: service_updated") {
				foundEvent = true
			}
			if strings.Contains(line, `{"version":"v2"}`) {
				foundData = true
			}
		case <-deadline:
			t.Fatalf("timed out waiting for event stream data; foundEvent=%v, foundData=%v", foundEvent, foundData)
		}
	}

	// B. Logs Stream: GET /api/v1/logs/stream?target_id=app_test&follow=true
	logReq, _ := http.NewRequestWithContext(ctx, "GET", server.URL+"/api/v1/logs/stream?target_id=app_test&follow=true", nil)
	logReq.Header.Set("Authorization", "Bearer "+token)
	logResp, err := http.DefaultClient.Do(logReq)
	if err != nil {
		t.Fatalf("logs stream connection failed: %v", err)
	}
	defer logResp.Body.Close()

	logReader := bufio.NewReader(logResp.Body)
	time.Sleep(30 * time.Millisecond)
	sseB.Broadcast("logs", "app_test", "log_entry", "Application initialized successfully")

	logLineChan := make(chan string, 10)
	go func() {
		for {
			l, rErr := logReader.ReadString('\n')
			if rErr != nil {
				return
			}
			logLineChan <- strings.TrimSpace(l)
		}
	}()

	foundLog := false
	logDeadline := time.After(2 * time.Second)
	for !foundLog {
		select {
		case l := <-logLineChan:
			if strings.Contains(l, "Application initialized successfully") {
				foundLog = true
			}
		case <-logDeadline:
			t.Fatalf("timed out waiting for logs stream data")
		}
	}

	// C. Stats Stream: GET /api/v1/stats/stream?target_id=node_01
	statsReq, _ := http.NewRequestWithContext(ctx, "GET", server.URL+"/api/v1/stats/stream?target_id=node_01", nil)
	statsReq.Header.Set("Authorization", "Bearer "+token)
	statsResp, err := http.DefaultClient.Do(statsReq)
	if err != nil {
		t.Fatalf("stats stream connection failed: %v", err)
	}
	defer statsResp.Body.Close()

	statsReader := bufio.NewReader(statsResp.Body)
	time.Sleep(30 * time.Millisecond)
	sseB.Broadcast("stats", "node_01", "cpu_usage", `{"usage_pct":42.5}`)

	statsLineChan := make(chan string, 10)
	go func() {
		for {
			l, rErr := statsReader.ReadString('\n')
			if rErr != nil {
				return
			}
			statsLineChan <- strings.TrimSpace(l)
		}
	}()

	foundStats := false
	statsDeadline := time.After(2 * time.Second)
	for !foundStats {
		select {
		case l := <-statsLineChan:
			if strings.Contains(l, `42.5`) {
				foundStats = true
			}
		case <-statsDeadline:
			t.Fatalf("timed out waiting for stats stream data")
		}
	}

	// D. Non-following logs request: GET /api/v1/logs/stream?target_id=app_test&follow=false
	noFollowReq, _ := http.NewRequest("GET", server.URL+"/api/v1/logs/stream?target_id=app_test&follow=false", nil)
	noFollowReq.Header.Set("Authorization", "Bearer "+token)
	noFollowResp, err := http.DefaultClient.Do(noFollowReq)
	if err != nil || noFollowResp.StatusCode != http.StatusOK {
		t.Fatalf("non-following log request failed: status %d, err %v", noFollowResp.StatusCode, err)
	}
	_ = noFollowResp.Body.Close()
}

// 4. Test Keep-Alive Ping Heartbeat
func TestSSE_KeepAlivePing(t *testing.T) {
	b := api.NewSSEBroadcaster()
	defer b.Close()
	b.SetHeartbeat(30 * time.Millisecond) // fast ping for test

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.ServeStream(w, r, "events", "*")
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", server.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)
	pingChan := make(chan string, 5)
	go func() {
		for {
			l, rErr := reader.ReadString('\n')
			if rErr != nil {
				return
			}
			pingChan <- strings.TrimSpace(l)
		}
	}()

	receivedPing := false
	deadline := time.After(500 * time.Millisecond)
	for !receivedPing {
		select {
		case l := <-pingChan:
			if l == ": ping" {
				receivedPing = true
			}
		case <-deadline:
			t.Fatalf("did not receive keep-alive : ping before deadline")
		}
	}
}

// 5. Test Client Disconnect Cleanup
func TestSSE_ClientDisconnectCleanup(t *testing.T) {
	b := api.NewSSEBroadcaster()
	defer b.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.ServeStream(w, r, "events", "*")
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", server.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	time.Sleep(30 * time.Millisecond)
	if count := b.ClientCount(); count != 1 {
		t.Fatalf("expected 1 active client, got %d", count)
	}

	// Cancel client context
	cancel()
	_ = resp.Body.Close()

	// Broadcaster should clean up client
	time.Sleep(50 * time.Millisecond)
	if count := b.ClientCount(); count != 0 {
		t.Fatalf("expected 0 active clients after disconnect, got %d", count)
	}
}

// 6. Test Concurrent Register, Unregister, and Broadcast (Race-Free)
func TestSSE_ConcurrentRaceFree(t *testing.T) {
	b := api.NewSSEBroadcaster()
	defer b.Close()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			client := b.Register("channel_test", "*")
			time.Sleep(5 * time.Millisecond)
			b.Broadcast("channel_test", "*", "tick", map[string]int{"id": id})
			time.Sleep(5 * time.Millisecond)
			b.Unregister(client)
		}(i)
	}

	wg.Wait()
}
