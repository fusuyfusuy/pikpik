package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	dockerclient "github.com/docker/docker/client"
	"github.com/fusuycorp/pikpik/pkg/auth"
	"github.com/fusuycorp/pikpik/pkg/build"
	"github.com/fusuycorp/pikpik/pkg/deploy"
	"github.com/fusuycorp/pikpik/pkg/store"
)

// APIGatewayOptions holds configuration for creating an APIGateway instance.
type APIGatewayOptions struct {
	Controller     Controller
	Store          store.Store
	AuthService    auth.AuthService
	WebSocketHub   *WebSocketHub
	SSEBroadcaster *SSEBroadcaster
	DockerClient   dockerclient.CommonAPIClient
	DeployWebhook  *deploy.DefaultDeployWebhookHandler
	RateLimiter    *RateLimiter
	BuildManager   *build.BuildManager
	EnableCors     bool
	AllowedOrigins []string
}

// APIGateway is the primary HTTP router and gateway engine for the pikpik control plane.
type APIGateway struct {
	ctrl           Controller
	wsHub          *WebSocketHub
	sseBroadcaster *SSEBroadcaster
	ptyHandler     *PTYHandler
	nudgeHandler   *deploy.DefaultDeployWebhookHandler
	rateLimiter    *RateLimiter
	buildMgr       *build.BuildManager
	router         *http.ServeMux
	handler        http.Handler
}

// NewAPIGateway creates and configures a new APIGateway.
func NewAPIGateway(ctrl Controller, wsHub *WebSocketHub, rl *RateLimiter) *APIGateway {
	return NewAPIGatewayWithOptions(APIGatewayOptions{
		Controller:   ctrl,
		WebSocketHub: wsHub,
		RateLimiter:  rl,
	})
}

// NewAPIGatewayWithOptions constructs an APIGateway with all optional subsystems injected.
func NewAPIGatewayWithOptions(opts APIGatewayOptions) *APIGateway {
	if opts.RateLimiter == nil {
		opts.RateLimiter = NewRateLimiter(600, time.Minute)
	}
	if opts.WebSocketHub == nil {
		opts.WebSocketHub = NewWebSocketHub()
	}
	if opts.SSEBroadcaster == nil {
		opts.SSEBroadcaster = NewSSEBroadcaster()
	}

	pty := NewPTYHandler(opts.DockerClient)

	mux := http.NewServeMux()
	RegisterRoutes(
		mux,
		opts.Controller,
		opts.AuthService,
		opts.Store,
		opts.WebSocketHub,
		opts.SSEBroadcaster,
		pty,
		opts.DeployWebhook,
		opts.RateLimiter,
		opts.BuildManager,
	)

	// Wrap mux with standard CORS, MaxBytesReader (10MB limit) and Request ID middleware
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if len(opts.AllowedOrigins) > 0 {
			for _, o := range opts.AllowedOrigins {
				if o == "*" {
					w.Header().Set("Access-Control-Allow-Origin", "*")
					break
				}
				if origin != "" && (o == origin || strings.EqualFold(o, origin)) {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Set("Access-Control-Allow-Credentials", "true")
					w.Header().Set("Vary", "Origin")
					break
				}
			}
		} else {
			// Default CORS behavior if no specific allowed origins list provided
			if origin != "" {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Vary", "Origin")
			} else {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			}
		}

		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Sec-WebSocket-Protocol, X-Request-ID, X-Node-ID, X-Node-Name, X-Node-Role")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// Enforce 10MB limit on incoming JSON/HTTP request bodies
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, 10*1024*1024)
		}

		mux.ServeHTTP(w, r)
	})

	return &APIGateway{
		ctrl:           opts.Controller,
		wsHub:          opts.WebSocketHub,
		sseBroadcaster: opts.SSEBroadcaster,
		ptyHandler:     pty,
		nudgeHandler:   opts.DeployWebhook,
		rateLimiter:    opts.RateLimiter,
		buildMgr:       opts.BuildManager,
		router:         mux,
		handler:        handler,
	}
}

// ServeHTTP satisfies the http.Handler interface.
func (gw *APIGateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	gw.handler.ServeHTTP(w, r)
}

// WebSocketHub returns the underlying WebSocketHub instance.
func (gw *APIGateway) WebSocketHub() *WebSocketHub {
	return gw.wsHub
}

// SSEBroadcaster returns the underlying SSEBroadcaster instance.
func (gw *APIGateway) SSEBroadcaster() *SSEBroadcaster {
	return gw.sseBroadcaster
}

// PTYHandler returns the underlying PTYHandler instance.
func (gw *APIGateway) PTYHandler() *PTYHandler {
	return gw.ptyHandler
}

// BuildManager returns the underlying BuildManager instance.
func (gw *APIGateway) BuildManager() *build.BuildManager {
	return gw.buildMgr
}

// RunBackground starts background workers (e.g. WebSocketHub run loop).
func (gw *APIGateway) RunBackground(ctx context.Context) {
	if gw.wsHub != nil {
		go gw.wsHub.Run(ctx)
	}
}
