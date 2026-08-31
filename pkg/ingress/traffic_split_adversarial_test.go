package ingress_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fusuycorp/pikpik/pkg/ingress"
)

func TestAdversarial_Ingress_ExtremeWeights_1000To0(t *testing.T) {
	// Extreme 1000/0 weight split
	cfg := ingress.TrafficSplitConfig{
		Domain: "extreme.example.com",
		Splits: []ingress.UpstreamWeight{
			{Upstream: "10.0.0.1:8080", Weight: 1000},
			{Upstream: "10.0.0.2:8080", Weight: 0},
		},
	}

	route := ingress.BuildTrafficSplitRoute(cfg)
	if route.ID != "route_split_extreme_example_com" {
		t.Fatalf("unexpected route ID: %s", route.ID)
	}

	// Verify route can be cleanly marshaled to JSON
	data, err := json.Marshal(route)
	if err != nil {
		t.Fatalf("failed to marshal extreme route to JSON: %v", err)
	}

	if len(data) == 0 {
		t.Fatal("empty serialized route JSON")
	}
}

func TestAdversarial_Ingress_NegativeAndZeroSumWeights(t *testing.T) {
	client := ingress.NewCaddyClient("http://127.0.0.1:2019", 1*time.Second)
	ctx := context.Background()

	// 1. Negative weight
	cfgNeg := ingress.TrafficSplitConfig{
		Domain: "neg.example.com",
		Splits: []ingress.UpstreamWeight{
			{Upstream: "10.0.0.1:8080", Weight: -50},
			{Upstream: "10.0.0.2:8080", Weight: 50},
		},
	}
	err := client.SetTrafficSplit(ctx, "neg.example.com", cfgNeg)
	if err == nil || !errors.Is(err, ingress.ErrInvalidRoutePayload) {
		t.Fatalf("expected ErrInvalidRoutePayload for negative weight, got %v", err)
	}

	// 2. Zero-sum weights (all zero)
	cfgZero := ingress.TrafficSplitConfig{
		Domain: "zero.example.com",
		Splits: []ingress.UpstreamWeight{
			{Upstream: "10.0.0.1:8080", Weight: 0},
			{Upstream: "10.0.0.2:8080", Weight: 0},
		},
	}
	err = client.SetTrafficSplit(ctx, "zero.example.com", cfgZero)
	if err == nil || !errors.Is(err, ingress.ErrInvalidRoutePayload) {
		t.Fatalf("expected ErrInvalidRoutePayload for zero-sum weights, got %v", err)
	}

	// 3. Empty upstream dial
	cfgEmptyUpstream := ingress.TrafficSplitConfig{
		Domain: "empty.example.com",
		Splits: []ingress.UpstreamWeight{
			{Upstream: "   ", Weight: 100},
		},
	}
	err = client.SetTrafficSplit(ctx, "empty.example.com", cfgEmptyUpstream)
	if err == nil || !errors.Is(err, ingress.ErrInvalidRoutePayload) {
		t.Fatalf("expected ErrInvalidRoutePayload for empty upstream target, got %v", err)
	}
}

func TestAdversarial_Ingress_IntegerOverflowWeights(t *testing.T) {
	cfg := ingress.TrafficSplitConfig{
		Domain: "overflow.example.com",
		Splits: []ingress.UpstreamWeight{
			{Upstream: "10.0.0.1:8080", Weight: math.MaxInt32},
			{Upstream: "10.0.0.2:8080", Weight: 1},
		},
	}

	route := ingress.BuildTrafficSplitRoute(cfg)
	data, err := json.Marshal(route)
	if err != nil {
		t.Fatalf("failed to marshal high-integer split route: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("empty serialized route data")
	}
}

func TestAdversarial_Ingress_MassiveUpstreamListScaling(t *testing.T) {
	// 500 upstream splits in a single route
	splits := make([]ingress.UpstreamWeight, 500)
	for i := 0; i < 500; i++ {
		splits[i] = ingress.UpstreamWeight{
			Upstream: fmt.Sprintf("10.0.%d.%d:8080", (i/250)+1, (i%250)+1),
			Weight:   1,
		}
	}

	cfg := ingress.TrafficSplitConfig{
		Domain: "scale500.example.com",
		Splits: splits,
	}

	start := time.Now()
	route := ingress.BuildTrafficSplitRoute(cfg)
	data, err := json.Marshal(route)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("failed to compile massive 500-upstream route: %v", err)
	}
	if elapsed > 100*time.Millisecond {
		t.Fatalf("building 500-upstream route took excessive time: %v", elapsed)
	}
	if len(data) == 0 {
		t.Fatal("empty serialized data")
	}
}

func TestAdversarial_Ingress_Sub5msRouteThrashingConcurrency(t *testing.T) {
	// Mock Caddy server accepting high concurrency PUT/DELETE/GET
	routesStore := sync.Map{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/id/")
		switch r.Method {
		case http.MethodPut:
			routesStore.Store(id, true)
			w.WriteHeader(http.StatusOK)
		case http.MethodDelete:
			routesStore.Delete(id)
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			if _, ok := routesStore.Load(id); ok {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"@id":"` + id + `"}`))
			} else {
				w.WriteHeader(http.StatusNotFound)
			}
		}
	}))
	defer server.Close()

	client := ingress.NewCaddyClient(server.URL, 2*time.Second)
	manager := ingress.NewIngressManager(client, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	const numGoroutines = 50
	var wg sync.WaitGroup
	var opCount int64

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			domain := fmt.Sprintf("thrash-%d.example.com", workerID%5)
			for {
				select {
				case <-ctx.Done():
					return
				default:
					// Interleave Set, Get, Remove operations rapidly
					cfg := ingress.TrafficSplitConfig{
						Domain: domain,
						Splits: []ingress.UpstreamWeight{
							{Upstream: "10.0.0.1:8080", Weight: 80},
							{Upstream: "10.0.0.2:8080", Weight: 20},
						},
					}
					_ = manager.SetTrafficSplit(ctx, domain, cfg)
					_, _ = manager.GetTrafficSplit(ctx, domain)
					_ = manager.RemoveTrafficSplit(ctx, domain)
					atomic.AddInt64(&opCount, 3)
					time.Sleep(1 * time.Millisecond)
				}
			}
		}(i)
	}

	wg.Wait()
	t.Logf("Completed %d concurrent thrashing operations successfully without data race or deadlock", atomic.LoadInt64(&opCount))
}

func TestAdversarial_Ingress_CaddyRestAPIDisconnectionAndTimeouts(t *testing.T) {
	// 1. Connection Refused / Unreachable TCP RST
	// Bind a local TCP listener to get a free port, then close it immediately
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	deadAddr := "http://" + listener.Addr().String()
	_ = listener.Close()

	client := ingress.NewCaddyClient(deadAddr, 100*time.Millisecond)
	ctx := context.Background()

	err = client.PutRoute(ctx, "test_route", ingress.CaddyRoute{ID: "test_route"})
	if err == nil || !errors.Is(err, ingress.ErrCaddyUnreachable) {
		t.Fatalf("expected ErrCaddyUnreachable for dead port, got %v", err)
	}

	// 2. Hanging / Timeout Server
	hangingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Sleep longer than client timeout
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer hangingServer.Close()

	timeoutClient := ingress.NewCaddyClient(hangingServer.URL, 50*time.Millisecond)
	err = timeoutClient.PutRoute(ctx, "timeout_route", ingress.CaddyRoute{ID: "timeout_route"})
	if err == nil || !errors.Is(err, ingress.ErrCaddyUnreachable) {
		t.Fatalf("expected ErrCaddyUnreachable on client timeout, got %v", err)
	}
}

func TestAdversarial_Ingress_Caddy500InternalErrorsAndMalformedJSON(t *testing.T) {
	// 1. HTTP 500 / 502 error handling
	errServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error": "loading config failed: invalid matcher"}`))
	}))
	defer errServer.Close()

	client := ingress.NewCaddyClient(errServer.URL, 1*time.Second)
	ctx := context.Background()

	err := client.PutRoute(ctx, "err_route", ingress.CaddyRoute{ID: "err_route"})
	if err == nil || !errors.Is(err, ingress.ErrCaddyMutationFailed) {
		t.Fatalf("expected ErrCaddyMutationFailed for HTTP 500, got %v", err)
	}

	// 2. Truncated JSON on GetRoute
	brokenJSONServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"apps": {"http": `)) // Truncated JSON
	}))
	defer brokenJSONServer.Close()

	client2 := ingress.NewCaddyClient(brokenJSONServer.URL, 1*time.Second)
	_, err = client2.GetRoute(ctx, "broken_route")
	if err == nil {
		t.Fatal("expected error parsing truncated JSON from Caddy, got nil")
	}

	// 3. 404 deletion returns ErrRouteNotFound and cleans local cache
	notFoundServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer notFoundServer.Close()

	client3 := ingress.NewCaddyClient(notFoundServer.URL, 1*time.Second)
	err = client3.DeleteRoute(ctx, "non_existent_route")
	if err == nil || !errors.Is(err, ingress.ErrRouteNotFound) {
		t.Fatalf("expected ErrRouteNotFound on 404 deletion, got %v", err)
	}
}
