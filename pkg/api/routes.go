package api

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/fusuycorp/pikpik/pkg/auth"
	"github.com/fusuycorp/pikpik/pkg/build"
	"github.com/fusuycorp/pikpik/pkg/deploy"
	"github.com/fusuycorp/pikpik/pkg/ingress"
	"github.com/fusuycorp/pikpik/pkg/store"
	"github.com/fusuycorp/pikpik/pkg/templates"
)

// WriteJSON sends a standardized JSON success response envelope.
func WriteJSON[T any](w http.ResponseWriter, statusCode int, data T, requestID string) {
	if requestID == "" {
		requestID = GenerateRequestID()
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(Response[T]{
		Success: true,
		Data:    data,
		Meta: MetaInfo{
			RequestID: requestID,
			Timestamp: time.Now().UTC(),
		},
	})
}

// WriteError sends an RFC 7807 compliant error envelope.
func WriteError(w http.ResponseWriter, statusCode int, code, message string, details map[string]any, requestID string) {
	if requestID == "" {
		requestID = GenerateRequestID()
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(ErrorResponse{
		Success: false,
		Error: APIError{
			Code:      code,
			Message:   message,
			Details:   details,
			RequestID: requestID,
			DocsURL:   "https://pikpik.dev/docs/errors#" + code,
		},
	})
}

// getPathParam retrieves a URL path parameter via strict route matching.
func getPathParam(r *http.Request, key string) string {
	return r.PathValue(key)
}

// writeServiceError formats validation vs internal server errors.
func writeServiceError(w http.ResponseWriter, err error, reqID string) {
	if strings.Contains(err.Error(), "cannot be empty") || strings.Contains(err.Error(), "invalid") || strings.Contains(err.Error(), "already exists") {
		WriteError(w, http.StatusBadRequest, ErrCodeValidationFailed, err.Error(), nil, reqID)
	} else {
		WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, reqID)
	}
}

// RegisterRoutes mounts all REST and WebSocket routes on the mux.
func RegisterRoutes(
	mux *http.ServeMux,
	ctrl Controller,
	authSvc auth.AuthService,
	st store.Store,
	wsHub *WebSocketHub,
	sseBroadcaster *SSEBroadcaster,
	ptyHandler *PTYHandler,
	nudgeHandler *deploy.DefaultDeployWebhookHandler,
	rateLimiter *RateLimiter,
	buildMgr *build.BuildManager,
) {
	// Dedicated login rate limiter: 5 attempts/min per client IP, to mitigate
	// credential brute-forcing against the unauthenticated login endpoint.
	loginLimiter := NewRateLimiter(5, time.Minute)

	authWrap := func(role string, h http.HandlerFunc) http.Handler {
		mw := AuthMiddleware(authSvc, st, role)
		return mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if rateLimiter != nil {
				key := ExtractToken(r)
				if key == "" {
					key = ExtractClientIP(r)
				}
				allowed, rem, retry := rateLimiter.Allow(key)
				SetRateLimitHeaders(w, 600, rem, retry)
				if !allowed {
					WriteError(w, http.StatusTooManyRequests, ErrCodeRateLimited, "Rate limit exceeded", nil, GetRequestID(r.Context()))
					return
				}
			}
			h(w, r)
		}))
	}

	// --- 1. Auth Endpoints ---
	mux.HandleFunc("POST /api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		reqID := GenerateRequestID()
		clientIP := ExtractClientIP(r)
		allowed, rem, retry := loginLimiter.Allow(clientIP)
		SetRateLimitHeaders(w, 5, rem, retry)
		if !allowed {
			WriteError(w, http.StatusTooManyRequests, ErrCodeRateLimited, "Too many login attempts, please try again later", nil, reqID)
			return
		}
		var req LoginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, ErrCodeValidationFailed, "Malformed login request", nil, reqID)
			return
		}
		resp, err := ctrl.Login(r.Context(), req.Email, req.Password)
		if err != nil {
			WriteError(w, http.StatusUnauthorized, ErrCodeUnauthorized, err.Error(), nil, reqID)
			return
		}
		// Set cookie
		http.SetCookie(w, &http.Cookie{
			Name:     "pikpik_session",
			Value:    resp.Token,
			Path:     "/",
			Expires:  resp.ExpiresAt,
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteLaxMode,
		})
		WriteJSON(w, http.StatusOK, resp, reqID)
	})

	mux.Handle("POST /api/v1/auth/logout", authWrap(RoleViewer, func(w http.ResponseWriter, r *http.Request) {
		token := ExtractToken(r)
		_ = ctrl.Logout(r.Context(), token)
		http.SetCookie(w, &http.Cookie{
			Name:     "pikpik_session",
			Value:    "",
			Path:     "/",
			Expires:  time.Unix(0, 0),
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteLaxMode,
		})
		WriteJSON(w, http.StatusOK, map[string]string{"message": "logged out"}, GetRequestID(r.Context()))
	}))

	mux.Handle("GET /api/v1/auth/me", authWrap(RoleViewer, func(w http.ResponseWriter, r *http.Request) {
		user, err := ctrl.GetCurrentUser(r.Context())
		if err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, user, GetRequestID(r.Context()))
	}))

	mux.Handle("GET /api/v1/auth/tokens", authWrap(RoleDeveloper, func(w http.ResponseWriter, r *http.Request) {
		user, _ := ctrl.GetCurrentUser(r.Context())
		userID := "usr_current"
		if user != nil {
			userID = user.ID
		}
		tokens, err := ctrl.ListAPITokens(r.Context(), userID)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, tokens, GetRequestID(r.Context()))
	}))

	mux.Handle("POST /api/v1/auth/tokens", authWrap(RoleAdmin, func(w http.ResponseWriter, r *http.Request) {
		var req CreateTokenRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, ErrCodeValidationFailed, "Malformed request", nil, GetRequestID(r.Context()))
			return
		}
		user, _ := ctrl.GetCurrentUser(r.Context())
		userID := "usr_current"
		if user != nil {
			userID = user.ID
		}
		tok, err := ctrl.CreateAPIToken(r.Context(), userID, req.Name, req.Scopes, req.ExpiresAt)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusCreated, tok, GetRequestID(r.Context()))
	}))

	mux.Handle("DELETE /api/v1/auth/tokens/{id}", authWrap(RoleAdmin, func(w http.ResponseWriter, r *http.Request) {
		id := getPathParam(r, "id")
		if err := ctrl.DeleteAPIToken(r.Context(), id); err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, map[string]string{"message": "token deleted"}, GetRequestID(r.Context()))
	}))

	// --- 1b. Organizations & Projects Endpoints ---
	mux.Handle("GET /api/v1/orgs", authWrap(RoleViewer, func(w http.ResponseWriter, r *http.Request) {
		orgs, err := ctrl.ListOrganizations(r.Context())
		if err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, orgs, GetRequestID(r.Context()))
	}))

	mux.Handle("POST /api/v1/orgs", authWrap(RoleAdmin, func(w http.ResponseWriter, r *http.Request) {
		var req CreateOrgRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, ErrCodeValidationFailed, "Invalid organization payload", nil, GetRequestID(r.Context()))
			return
		}
		org, err := ctrl.CreateOrganization(r.Context(), &req)
		if err != nil {
			WriteError(w, http.StatusBadRequest, ErrCodeValidationFailed, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusCreated, org, GetRequestID(r.Context()))
	}))

	mux.Handle("GET /api/v1/projects", authWrap(RoleViewer, func(w http.ResponseWriter, r *http.Request) {
		orgID := r.URL.Query().Get("org_id")
		tagFilter := r.URL.Query().Get("tag")
		prjs, err := ctrl.ListProjects(r.Context(), orgID)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		if tagFilter != "" {
			var filtered []ProjectDTO
			for _, p := range prjs {
				for _, t := range p.Tags {
					if strings.EqualFold(t, tagFilter) {
						filtered = append(filtered, p)
						break
					}
				}
			}
			prjs = filtered
		}
		WriteJSON(w, http.StatusOK, prjs, GetRequestID(r.Context()))
	}))

	mux.Handle("POST /api/v1/projects", authWrap(RoleDeveloper, func(w http.ResponseWriter, r *http.Request) {
		var req CreateProjectRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, ErrCodeValidationFailed, "Invalid project payload", nil, GetRequestID(r.Context()))
			return
		}
		prj, err := ctrl.CreateProject(r.Context(), &req)
		if err != nil {
			WriteError(w, http.StatusBadRequest, ErrCodeValidationFailed, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusCreated, prj, GetRequestID(r.Context()))
	}))

	mux.Handle("GET /api/v1/projects/{id}", authWrap(RoleViewer, func(w http.ResponseWriter, r *http.Request) {
		id := getPathParam(r, "id")
		prj, err := ctrl.GetProject(r.Context(), id)
		if err != nil {
			WriteError(w, http.StatusNotFound, ErrCodeNotFound, "Project not found", nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, prj, GetRequestID(r.Context()))
	}))

	mux.Handle("PATCH /api/v1/projects/{id}", authWrap(RoleDeveloper, func(w http.ResponseWriter, r *http.Request) {
		id := getPathParam(r, "id")
		var req UpdateProjectRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, ErrCodeValidationFailed, "Invalid project patch payload", nil, GetRequestID(r.Context()))
			return
		}
		prj, err := ctrl.UpdateProject(r.Context(), id, &req)
		if err != nil {
			WriteError(w, http.StatusNotFound, ErrCodeNotFound, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, prj, GetRequestID(r.Context()))
	}))

	mux.Handle("DELETE /api/v1/projects/{id}", authWrap(RoleAdmin, func(w http.ResponseWriter, r *http.Request) {
		id := getPathParam(r, "id")
		if err := ctrl.DeleteProject(r.Context(), id); err != nil {
			WriteError(w, http.StatusBadRequest, ErrCodeValidationFailed, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, map[string]string{"message": "project deleted"}, GetRequestID(r.Context()))
	}))

	mux.Handle("GET /api/v1/tags", authWrap(RoleViewer, func(w http.ResponseWriter, r *http.Request) {
		tags, err := ctrl.ListTags(r.Context())
		if err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, tags, GetRequestID(r.Context()))
	}))

	// --- 2. Apps Endpoints ---
	mux.Handle("GET /api/v1/apps", authWrap(RoleViewer, func(w http.ResponseWriter, r *http.Request) {
		apps, err := ctrl.ListApps(r.Context())
		if err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}

		projectID := r.URL.Query().Get("project_id")
		tagFilter := r.URL.Query().Get("tag")
		search := r.URL.Query().Get("search")

		if projectID != "" || tagFilter != "" || search != "" {
			var filtered []App
			for _, a := range apps {
				if projectID != "" && a.ProjectID != projectID {
					continue
				}
				if tagFilter != "" {
					tagMatch := false
					for _, t := range a.Tags {
						if strings.EqualFold(t, tagFilter) {
							tagMatch = true
							break
						}
					}
					if !tagMatch {
						continue
					}
				}
				if search != "" && !strings.Contains(strings.ToLower(a.Name), strings.ToLower(search)) {
					continue
				}
				filtered = append(filtered, a)
			}
			apps = filtered
		}

		WriteJSON(w, http.StatusOK, apps, GetRequestID(r.Context()))
	}))

	mux.Handle("POST /api/v1/apps", authWrap(RoleDeveloper, func(w http.ResponseWriter, r *http.Request) {
		var req CreateAppRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, ErrCodeValidationFailed, "Invalid app payload", nil, GetRequestID(r.Context()))
			return
		}
		app, err := ctrl.CreateApp(r.Context(), &req)
		if err != nil {
			writeServiceError(w, err, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusCreated, app, GetRequestID(r.Context()))
	}))

	mux.Handle("POST /api/v1/apps/inspect-compose", authWrap(RoleViewer, func(w http.ResponseWriter, r *http.Request) {
		var req InspectComposeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, ErrCodeValidationFailed, "Invalid inspect compose payload", nil, GetRequestID(r.Context()))
			return
		}
		res, err := ctrl.InspectCompose(r.Context(), req.ComposeYAML)
		if err != nil {
			WriteError(w, http.StatusBadRequest, ErrCodeValidationFailed, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, res, GetRequestID(r.Context()))
	}))

	mux.Handle("GET /api/v1/apps/{id}", authWrap(RoleViewer, func(w http.ResponseWriter, r *http.Request) {
		id := getPathParam(r, "id")
		app, err := ctrl.GetApp(r.Context(), id)
		if err != nil {
			WriteError(w, http.StatusNotFound, ErrCodeNotFound, "App not found", nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, app, GetRequestID(r.Context()))
	}))

	mux.Handle("PATCH /api/v1/apps/{id}", authWrap(RoleDeveloper, func(w http.ResponseWriter, r *http.Request) {
		id := getPathParam(r, "id")
		var req UpdateAppRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, ErrCodeValidationFailed, "Invalid patch payload", nil, GetRequestID(r.Context()))
			return
		}
		app, err := ctrl.UpdateApp(r.Context(), id, &req)
		if err != nil {
			WriteError(w, http.StatusNotFound, ErrCodeNotFound, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, app, GetRequestID(r.Context()))
	}))

	mux.Handle("DELETE /api/v1/apps/{id}", authWrap(RoleAdmin, func(w http.ResponseWriter, r *http.Request) {
		id := getPathParam(r, "id")
		if err := ctrl.DeleteApp(r.Context(), id); err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, map[string]string{"message": "app deleted"}, GetRequestID(r.Context()))
	}))

	mux.Handle("POST /api/v1/apps/{id}/deploy", authWrap(RoleDeveloper, func(w http.ResponseWriter, r *http.Request) {
		id := getPathParam(r, "id")
		var req DeployAppRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if err := ctrl.DeployApp(r.Context(), id, req.Image); err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, map[string]string{"status": "deploying", "app_id": id}, GetRequestID(r.Context()))
	}))

	mux.Handle("POST /api/v1/apps/{id}/restart", authWrap(RoleDeveloper, func(w http.ResponseWriter, r *http.Request) {
		id := getPathParam(r, "id")
		if err := ctrl.RestartApp(r.Context(), id); err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, map[string]string{"status": "restarting", "app_id": id}, GetRequestID(r.Context()))
	}))

	mux.Handle("POST /api/v1/apps/{id}/stop", authWrap(RoleDeveloper, func(w http.ResponseWriter, r *http.Request) {
		id := getPathParam(r, "id")
		if err := ctrl.StopApp(r.Context(), id); err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, map[string]string{"status": "stopped", "app_id": id}, GetRequestID(r.Context()))
	}))

	mux.Handle("POST /api/v1/apps/{id}/start", authWrap(RoleDeveloper, func(w http.ResponseWriter, r *http.Request) {
		id := getPathParam(r, "id")
		if err := ctrl.StartApp(r.Context(), id); err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, map[string]string{"status": "started", "app_id": id}, GetRequestID(r.Context()))
	}))

	mux.Handle("GET /api/v1/apps/{id}/env", authWrap(RoleDeveloper, func(w http.ResponseWriter, r *http.Request) {
		id := getPathParam(r, "id")
		env, err := ctrl.GetAppEnv(r.Context(), id)
		if err != nil {
			WriteError(w, http.StatusNotFound, ErrCodeNotFound, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, env, GetRequestID(r.Context()))
	}))

	mux.Handle("PUT /api/v1/apps/{id}/env", authWrap(RoleDeveloper, func(w http.ResponseWriter, r *http.Request) {
		id := getPathParam(r, "id")
		var env map[string]string
		if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
			WriteError(w, http.StatusBadRequest, ErrCodeValidationFailed, "Invalid env payload", nil, GetRequestID(r.Context()))
			return
		}
		if err := ctrl.SetAppEnv(r.Context(), id, env); err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, env, GetRequestID(r.Context()))
	}))

	mux.Handle("GET /api/v1/apps/{id}/traffic", authWrap(RoleViewer, func(w http.ResponseWriter, r *http.Request) {
		id := getPathParam(r, "id")
		res, err := ctrl.GetAppTraffic(r.Context(), id)
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				WriteError(w, http.StatusNotFound, ErrCodeNotFound, err.Error(), nil, GetRequestID(r.Context()))
				return
			}
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, res, GetRequestID(r.Context()))
	}))

	mux.Handle("POST /api/v1/apps/{id}/traffic", authWrap(RoleDeveloper, func(w http.ResponseWriter, r *http.Request) {
		id := getPathParam(r, "id")
		var req SetTrafficSplitRequest
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			WriteError(w, http.StatusBadRequest, ErrCodeValidationFailed, "Failed to read request body", nil, GetRequestID(r.Context()))
			return
		}
		if len(bodyBytes) > 0 {
			if err := json.Unmarshal(bodyBytes, &req); err != nil || (len(req.Splits) == 0 && len(req.Upstreams) == 0 && !req.Reset) {
				var splits []UpstreamWeight
				if err2 := json.Unmarshal(bodyBytes, &splits); err2 == nil && len(splits) > 0 {
					req.Splits = splits
				}
			}
		}

		res, err := ctrl.SetAppTraffic(r.Context(), id, &req)
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				WriteError(w, http.StatusNotFound, ErrCodeNotFound, err.Error(), nil, GetRequestID(r.Context()))
				return
			}
			if strings.Contains(err.Error(), "cannot be empty") || strings.Contains(err.Error(), "cannot be negative") || strings.Contains(err.Error(), "greater than 0") {
				WriteError(w, http.StatusBadRequest, ErrCodeValidationFailed, err.Error(), nil, GetRequestID(r.Context()))
				return
			}
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, res, GetRequestID(r.Context()))
	}))


	// Nudge Webhook endpoint
	if nudgeHandler != nil {
		mux.Handle("POST /api/deploy/nudge/", nudgeHandler)
		mux.Handle("POST /api/deploy/nudge/{token}", nudgeHandler)
	} else {
		mux.HandleFunc("POST /api/deploy/nudge/{token}", func(w http.ResponseWriter, r *http.Request) {
			token := getPathParam(r, "token")
			var req map[string]string
			_ = json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&req)
			WriteJSON(w, http.StatusAccepted, map[string]any{
				"status":        "UPDATING",
				"deployment_id": "dep_nudge_" + token,
				"image":         req["image"],
			}, GenerateRequestID())
		})
	}

	// --- 3. Stacks Endpoints ---
	mux.Handle("GET /api/v1/stacks", authWrap(RoleViewer, func(w http.ResponseWriter, r *http.Request) {
		stacks, err := ctrl.ListStacks(r.Context())
		if err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, stacks, GetRequestID(r.Context()))
	}))

	mux.Handle("POST /api/v1/stacks", authWrap(RoleDeveloper, func(w http.ResponseWriter, r *http.Request) {
		var req CreateStackRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, ErrCodeValidationFailed, "Malformed payload", nil, GetRequestID(r.Context()))
			return
		}
		stack, err := ctrl.CreateStack(r.Context(), &req)
		if err != nil {
			writeServiceError(w, err, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusCreated, stack, GetRequestID(r.Context()))
	}))

	mux.Handle("GET /api/v1/stacks/{id}", authWrap(RoleViewer, func(w http.ResponseWriter, r *http.Request) {
		id := getPathParam(r, "id")
		stack, err := ctrl.GetStack(r.Context(), id)
		if err != nil {
			WriteError(w, http.StatusNotFound, ErrCodeNotFound, "Stack not found", nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, stack, GetRequestID(r.Context()))
	}))

	mux.Handle("PUT /api/v1/stacks/{id}", authWrap(RoleDeveloper, func(w http.ResponseWriter, r *http.Request) {
		id := getPathParam(r, "id")
		var req CreateStackRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, ErrCodeValidationFailed, "Malformed payload", nil, GetRequestID(r.Context()))
			return
		}
		stack, err := ctrl.UpdateStack(r.Context(), id, req.ComposeYAML)
		if err != nil {
			WriteError(w, http.StatusNotFound, ErrCodeNotFound, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, stack, GetRequestID(r.Context()))
	}))

	mux.Handle("POST /api/v1/stacks/{id}/deploy", authWrap(RoleDeveloper, func(w http.ResponseWriter, r *http.Request) {
		id := getPathParam(r, "id")
		if err := ctrl.DeployStack(r.Context(), id); err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, map[string]string{"status": "deploying", "stack_id": id}, GetRequestID(r.Context()))
	}))

	mux.Handle("POST /api/v1/stacks/{id}/restart", authWrap(RoleDeveloper, func(w http.ResponseWriter, r *http.Request) {
		id := getPathParam(r, "id")
		if err := ctrl.RestartStack(r.Context(), id); err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, map[string]string{"status": "restarted", "stack_id": id}, GetRequestID(r.Context()))
	}))

	mux.Handle("POST /api/v1/stacks/{id}/stop", authWrap(RoleDeveloper, func(w http.ResponseWriter, r *http.Request) {
		id := getPathParam(r, "id")
		if err := ctrl.StopStack(r.Context(), id); err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, map[string]string{"status": "stopped", "stack_id": id}, GetRequestID(r.Context()))
	}))

	mux.Handle("GET /api/v1/stacks/{id}/logs", authWrap(RoleViewer, func(w http.ResponseWriter, r *http.Request) {
		id := getPathParam(r, "id")
		stack, err := ctrl.GetStack(r.Context(), id)
		if err != nil {
			WriteError(w, http.StatusNotFound, ErrCodeNotFound, "Stack not found", nil, GetRequestID(r.Context()))
			return
		}

		if r.URL.Query().Get("follow") == "true" && sseBroadcaster != nil {
			sseBroadcaster.ServeLogsStream(w, r, stack.Name)
			return
		}

		WriteJSON(w, http.StatusOK, map[string]any{
			"stack_id":   stack.ID,
			"stack_name": stack.Name,
			"status":     stack.Status,
			"containers": stack.Containers,
		}, GetRequestID(r.Context()))
	}))

	mux.Handle("DELETE /api/v1/stacks/{id}", authWrap(RoleAdmin, func(w http.ResponseWriter, r *http.Request) {
		id := getPathParam(r, "id")
		if err := ctrl.DeleteStack(r.Context(), id); err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, map[string]string{"message": "stack deleted"}, GetRequestID(r.Context()))
	}))

	// --- 3b. Networks Endpoints ---
	mux.Handle("GET /api/v1/networks", authWrap(RoleViewer, func(w http.ResponseWriter, r *http.Request) {
		projID := r.URL.Query().Get("project_id")
		nets, err := ctrl.ListNetworks(r.Context(), projID)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, nets, GetRequestID(r.Context()))
	}))

	mux.Handle("POST /api/v1/networks", authWrap(RoleDeveloper, func(w http.ResponseWriter, r *http.Request) {
		var req CreateNetworkRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, ErrCodeValidationFailed, "Malformed payload", nil, GetRequestID(r.Context()))
			return
		}
		net, err := ctrl.CreateNetwork(r.Context(), &req)
		if err != nil {
			writeServiceError(w, err, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusCreated, net, GetRequestID(r.Context()))
	}))

	mux.Handle("POST /api/v1/networks/prune", authWrap(RoleAdmin, func(w http.ResponseWriter, r *http.Request) {
		projID := r.URL.Query().Get("project_id")
		res, err := ctrl.PruneNetworks(r.Context(), projID)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, res, GetRequestID(r.Context()))
	}))

	mux.Handle("GET /api/v1/networks/{id}", authWrap(RoleViewer, func(w http.ResponseWriter, r *http.Request) {
		id := getPathParam(r, "id")
		net, err := ctrl.GetNetwork(r.Context(), id)
		if err != nil {
			WriteError(w, http.StatusNotFound, ErrCodeNotFound, "Network not found", nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, net, GetRequestID(r.Context()))
	}))

	mux.Handle("DELETE /api/v1/networks/{id}", authWrap(RoleAdmin, func(w http.ResponseWriter, r *http.Request) {
		id := getPathParam(r, "id")
		if err := ctrl.DeleteNetwork(r.Context(), id); err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, map[string]string{"message": "network deleted"}, GetRequestID(r.Context()))
	}))

	// --- 3c. Volumes Endpoints ---
	mux.Handle("GET /api/v1/volumes", authWrap(RoleViewer, func(w http.ResponseWriter, r *http.Request) {
		projID := r.URL.Query().Get("project_id")
		vols, err := ctrl.ListVolumes(r.Context(), projID)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, vols, GetRequestID(r.Context()))
	}))

	mux.Handle("POST /api/v1/volumes", authWrap(RoleDeveloper, func(w http.ResponseWriter, r *http.Request) {
		var req CreateVolumeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, ErrCodeValidationFailed, "Malformed payload", nil, GetRequestID(r.Context()))
			return
		}
		vol, err := ctrl.CreateVolume(r.Context(), &req)
		if err != nil {
			writeServiceError(w, err, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusCreated, vol, GetRequestID(r.Context()))
	}))

	mux.Handle("POST /api/v1/volumes/prune", authWrap(RoleAdmin, func(w http.ResponseWriter, r *http.Request) {
		projID := r.URL.Query().Get("project_id")
		res, err := ctrl.PruneVolumes(r.Context(), projID)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, res, GetRequestID(r.Context()))
	}))

	mux.Handle("GET /api/v1/volumes/{id}", authWrap(RoleViewer, func(w http.ResponseWriter, r *http.Request) {
		id := getPathParam(r, "id")
		vol, err := ctrl.GetVolume(r.Context(), id)
		if err != nil {
			WriteError(w, http.StatusNotFound, ErrCodeNotFound, "Volume not found", nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, vol, GetRequestID(r.Context()))
	}))

	mux.Handle("DELETE /api/v1/volumes/{id}", authWrap(RoleAdmin, func(w http.ResponseWriter, r *http.Request) {
		id := getPathParam(r, "id")
		if err := ctrl.DeleteVolume(r.Context(), id); err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, map[string]string{"message": "volume deleted"}, GetRequestID(r.Context()))
	}))

	// --- 4. Nodes Endpoints ---
	mux.Handle("GET /api/v1/nodes", authWrap(RoleViewer, func(w http.ResponseWriter, r *http.Request) {
		nodes, err := ctrl.ListNodes(r.Context())
		if err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, nodes, GetRequestID(r.Context()))
	}))

	mux.Handle("GET /api/v1/nodes/{id}", authWrap(RoleViewer, func(w http.ResponseWriter, r *http.Request) {
		id := getPathParam(r, "id")
		node, err := ctrl.GetNode(r.Context(), id)
		if err != nil {
			WriteError(w, http.StatusNotFound, ErrCodeNotFound, "Node not found", nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, node, GetRequestID(r.Context()))
	}))

	mux.Handle("PATCH /api/v1/nodes/{id}", authWrap(RoleAdmin, func(w http.ResponseWriter, r *http.Request) {
		id := getPathParam(r, "id")
		var req UpdateNodeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, ErrCodeValidationFailed, "Malformed payload", nil, GetRequestID(r.Context()))
			return
		}
		if err := ctrl.UpdateNodeAvailability(r.Context(), id, req.Availability); err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, map[string]string{"status": "updated", "node_id": id, "availability": req.Availability}, GetRequestID(r.Context()))
	}))

	mux.Handle("DELETE /api/v1/nodes/{id}", authWrap(RoleAdmin, func(w http.ResponseWriter, r *http.Request) {
		id := getPathParam(r, "id")
		if err := ctrl.DeleteNode(r.Context(), id); err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, map[string]string{"message": "node deleted"}, GetRequestID(r.Context()))
	}))

	mux.Handle("GET /api/v1/nodes/join-tokens", authWrap(RoleAdmin, func(w http.ResponseWriter, r *http.Request) {
		tokens, err := ctrl.GetJoinTokens(r.Context())
		if err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, tokens, GetRequestID(r.Context()))
	}))

	// --- 4.1 Remote Managed Machines Endpoints ---
	mux.Handle("GET /api/v1/machines", authWrap(RoleViewer, func(w http.ResponseWriter, r *http.Request) {
		machines, err := ctrl.ListMachines(r.Context())
		if err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, machines, GetRequestID(r.Context()))
	}))

	mux.Handle("GET /api/v1/machines/enroll", authWrap(RoleAdmin, func(w http.ResponseWriter, r *http.Request) {
		serverURL := r.URL.Query().Get("server_url")
		if serverURL == "" {
			scheme := "http"
			if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
				scheme = "https"
			}
			serverURL = scheme + "://" + r.Host
		}
		resp, err := ctrl.GetMachineEnrollCommand(r.Context(), serverURL)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, resp, GetRequestID(r.Context()))
	}))

	mux.Handle("GET /api/v1/machines/{id}", authWrap(RoleViewer, func(w http.ResponseWriter, r *http.Request) {
		id := getPathParam(r, "id")
		machine, err := ctrl.GetMachine(r.Context(), id)
		if err != nil {
			WriteError(w, http.StatusNotFound, ErrCodeNotFound, "Machine not found", nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, machine, GetRequestID(r.Context()))
	}))

	mux.Handle("DELETE /api/v1/machines/{id}", authWrap(RoleAdmin, func(w http.ResponseWriter, r *http.Request) {
		id := getPathParam(r, "id")
		if err := ctrl.DeleteMachine(r.Context(), id); err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, map[string]string{"message": "machine deleted", "id": id}, GetRequestID(r.Context()))
	}))

	mux.Handle("GET /api/v1/machines/{id}/metrics", authWrap(RoleViewer, func(w http.ResponseWriter, r *http.Request) {
		id := getPathParam(r, "id")
		metrics, err := ctrl.GetMachineMetrics(r.Context(), id)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, metrics, GetRequestID(r.Context()))
	}))

	mux.Handle("POST /api/v1/machines/{id}/join-swarm", authWrap(RoleAdmin, func(w http.ResponseWriter, r *http.Request) {
		id := getPathParam(r, "id")
		var req JoinSwarmRequest
		if r.ContentLength > 0 {
			_ = json.NewDecoder(r.Body).Decode(&req)
		}
		node, err := ctrl.JoinSwarmCluster(r.Context(), id, &req)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, node, GetRequestID(r.Context()))
	}))

	// --- 5. Databases Endpoints ---
	mux.Handle("GET /api/v1/databases", authWrap(RoleViewer, func(w http.ResponseWriter, r *http.Request) {
		dbs, err := ctrl.ListDatabases(r.Context())
		if err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, dbs, GetRequestID(r.Context()))
	}))

	mux.Handle("POST /api/v1/databases", authWrap(RoleDeveloper, func(w http.ResponseWriter, r *http.Request) {
		var req CreateDatabaseRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, ErrCodeValidationFailed, "Malformed payload", nil, GetRequestID(r.Context()))
			return
		}
		db, err := ctrl.CreateDatabase(r.Context(), &req)
		if err != nil {
			writeServiceError(w, err, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusCreated, db, GetRequestID(r.Context()))
	}))

	mux.Handle("GET /api/v1/databases/{id}", authWrap(RoleViewer, func(w http.ResponseWriter, r *http.Request) {
		id := getPathParam(r, "id")
		db, err := ctrl.GetDatabase(r.Context(), id)
		if err != nil {
			WriteError(w, http.StatusNotFound, ErrCodeNotFound, "Database not found", nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, db, GetRequestID(r.Context()))
	}))

	mux.Handle("PATCH /api/v1/databases/{id}", authWrap(RoleDeveloper, func(w http.ResponseWriter, r *http.Request) {
		id := getPathParam(r, "id")
		var req UpdateDatabaseRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, ErrCodeValidationFailed, "Malformed payload", nil, GetRequestID(r.Context()))
			return
		}
		db, err := ctrl.UpdateDatabase(r.Context(), id, &req)
		if err != nil {
			WriteError(w, http.StatusNotFound, ErrCodeNotFound, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, db, GetRequestID(r.Context()))
	}))

	mux.Handle("POST /api/v1/databases/{id}/restart", authWrap(RoleDeveloper, func(w http.ResponseWriter, r *http.Request) {
		id := getPathParam(r, "id")
		if err := ctrl.RestartDatabase(r.Context(), id); err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, map[string]string{"status": "restarted", "db_id": id}, GetRequestID(r.Context()))
	}))

	mux.Handle("DELETE /api/v1/databases/{id}", authWrap(RoleOwner, func(w http.ResponseWriter, r *http.Request) {
		id := getPathParam(r, "id")
		if err := ctrl.DeleteDatabase(r.Context(), id); err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, map[string]string{"message": "database deleted"}, GetRequestID(r.Context()))
	}))

	// --- 6. Backups Endpoints ---
	mux.Handle("GET /api/v1/backups", authWrap(RoleDeveloper, func(w http.ResponseWriter, r *http.Request) {
		bks, err := ctrl.ListBackups(r.Context())
		if err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, bks, GetRequestID(r.Context()))
	}))

	mux.Handle("POST /api/v1/backups", authWrap(RoleDeveloper, func(w http.ResponseWriter, r *http.Request) {
		var req CreateBackupRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, ErrCodeValidationFailed, "Malformed payload", nil, GetRequestID(r.Context()))
			return
		}
		bk, err := ctrl.CreateBackup(r.Context(), req.ServiceID)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusCreated, bk, GetRequestID(r.Context()))
	}))

	mux.Handle("GET /api/v1/backups/{id}", authWrap(RoleDeveloper, func(w http.ResponseWriter, r *http.Request) {
		id := getPathParam(r, "id")
		bk, err := ctrl.GetBackup(r.Context(), id)
		if err != nil {
			WriteError(w, http.StatusNotFound, ErrCodeNotFound, "Backup not found", nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, bk, GetRequestID(r.Context()))
	}))

	mux.Handle("POST /api/v1/backups/{id}/restore", authWrap(RoleAdmin, func(w http.ResponseWriter, r *http.Request) {
		id := getPathParam(r, "id")
		var req RestoreBackupRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if err := ctrl.RestoreBackup(r.Context(), id, req.TargetServiceID); err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, map[string]string{"status": "restored", "backup_id": id}, GetRequestID(r.Context()))
	}))

	mux.Handle("DELETE /api/v1/backups/{id}", authWrap(RoleAdmin, func(w http.ResponseWriter, r *http.Request) {
		id := getPathParam(r, "id")
		if err := ctrl.DeleteBackup(r.Context(), id); err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, map[string]string{"message": "backup deleted"}, GetRequestID(r.Context()))
	}))

	mux.Handle("GET /api/v1/backups/destinations", authWrap(RoleAdmin, func(w http.ResponseWriter, r *http.Request) {
		dests, err := ctrl.ListBackupDestinations(r.Context())
		if err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, dests, GetRequestID(r.Context()))
	}))

	mux.Handle("POST /api/v1/backups/destinations", authWrap(RoleAdmin, func(w http.ResponseWriter, r *http.Request) {
		var dest BackupDestination
		if err := json.NewDecoder(r.Body).Decode(&dest); err != nil {
			WriteError(w, http.StatusBadRequest, ErrCodeValidationFailed, "Malformed payload", nil, GetRequestID(r.Context()))
			return
		}
		saved, err := ctrl.CreateBackupDestination(r.Context(), &dest)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusCreated, saved, GetRequestID(r.Context()))
	}))

	// Backup Schedules Endpoints
	mux.Handle("GET /api/v1/backups/schedules", authWrap(RoleDeveloper, func(w http.ResponseWriter, r *http.Request) {
		serviceID := r.URL.Query().Get("service_id")
		schedules, err := ctrl.ListBackupSchedules(r.Context(), serviceID)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		if schedules == nil {
			schedules = []*store.BackupSchedule{}
		}
		WriteJSON(w, http.StatusOK, schedules, GetRequestID(r.Context()))
	}))

	mux.Handle("POST /api/v1/backups/schedules", authWrap(RoleAdmin, func(w http.ResponseWriter, r *http.Request) {
		var req CreateBackupScheduleRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, ErrCodeValidationFailed, "Malformed payload", nil, GetRequestID(r.Context()))
			return
		}
		saved, err := ctrl.CreateBackupSchedule(r.Context(), &req)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusCreated, saved, GetRequestID(r.Context()))
	}))

	mux.Handle("GET /api/v1/backups/schedules/{id}", authWrap(RoleDeveloper, func(w http.ResponseWriter, r *http.Request) {
		id := getPathParam(r, "id")
		sch, err := ctrl.GetBackupSchedule(r.Context(), id)
		if err != nil {
			WriteError(w, http.StatusNotFound, ErrCodeNotFound, "Backup schedule not found", nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, sch, GetRequestID(r.Context()))
	}))

	mux.Handle("PATCH /api/v1/backups/schedules/{id}", authWrap(RoleAdmin, func(w http.ResponseWriter, r *http.Request) {
		id := getPathParam(r, "id")
		var req UpdateBackupScheduleRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, ErrCodeValidationFailed, "Malformed payload", nil, GetRequestID(r.Context()))
			return
		}
		updated, err := ctrl.UpdateBackupSchedule(r.Context(), id, &req)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, updated, GetRequestID(r.Context()))
	}))

	mux.Handle("PUT /api/v1/backups/schedules/{id}", authWrap(RoleAdmin, func(w http.ResponseWriter, r *http.Request) {
		id := getPathParam(r, "id")
		var req UpdateBackupScheduleRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, ErrCodeValidationFailed, "Malformed payload", nil, GetRequestID(r.Context()))
			return
		}
		updated, err := ctrl.UpdateBackupSchedule(r.Context(), id, &req)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, updated, GetRequestID(r.Context()))
	}))

	mux.Handle("DELETE /api/v1/backups/schedules/{id}", authWrap(RoleAdmin, func(w http.ResponseWriter, r *http.Request) {
		id := getPathParam(r, "id")
		if err := ctrl.DeleteBackupSchedule(r.Context(), id); err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, map[string]string{"message": "schedule deleted"}, GetRequestID(r.Context()))
	}))

	// --- 7. Ingress Endpoints ---
	mux.Handle("GET /api/v1/ingress/domains", authWrap(RoleViewer, func(w http.ResponseWriter, r *http.Request) {
		domains, err := ctrl.ListDomains(r.Context())
		if err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, domains, GetRequestID(r.Context()))
	}))

	mux.Handle("POST /api/v1/ingress/domains", authWrap(RoleDeveloper, func(w http.ResponseWriter, r *http.Request) {
		var req BindDomainRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, ErrCodeValidationFailed, "Malformed payload", nil, GetRequestID(r.Context()))
			return
		}
		dom, err := ctrl.BindDomain(r.Context(), req.AppID, req.Domain, req.AutoTLS)
		if err != nil {
			if strings.Contains(err.Error(), "already bound") {
				WriteError(w, http.StatusConflict, ErrCodeResourceConflict, err.Error(), map[string]any{"domain": req.Domain}, GetRequestID(r.Context()))
				return
			}
			WriteError(w, http.StatusBadRequest, ErrCodeValidationFailed, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusCreated, dom, GetRequestID(r.Context()))
	}))

	mux.Handle("DELETE /api/v1/ingress/domains/{id}", authWrap(RoleDeveloper, func(w http.ResponseWriter, r *http.Request) {
		id := getPathParam(r, "id")
		if err := ctrl.DeleteDomain(r.Context(), id); err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, map[string]string{"message": "domain removed"}, GetRequestID(r.Context()))
	}))

	mux.Handle("POST /api/v1/ingress/certificates", authWrap(RoleAdmin, func(w http.ResponseWriter, r *http.Request) {
		var req CertificateUploadRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, ErrCodeValidationFailed, "Malformed certificate payload", nil, GetRequestID(r.Context()))
			return
		}
		if err := ctrl.UploadCertificate(r.Context(), &req); err != nil {
			WriteError(w, http.StatusBadRequest, ErrCodeValidationFailed, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, map[string]string{"status": "uploaded", "domain": req.Domain}, GetRequestID(r.Context()))
	}))

	mux.Handle("POST /api/v1/ingress/reconcile", authWrap(RoleAdmin, func(w http.ResponseWriter, r *http.Request) {
		if err := ctrl.ReconcileIngress(r.Context()); err != nil {
			WriteError(w, http.StatusBadGateway, ErrCodeIngressReconcileFailed, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, map[string]string{"status": "reconciled"}, GetRequestID(r.Context()))
	}))

	mux.Handle("GET /api/v1/ingress/caddy/config", authWrap(RoleAdmin, func(w http.ResponseWriter, r *http.Request) {
		diag, err := ctrl.GetCaddyConfig(r.Context())
		if err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, diag, GetRequestID(r.Context()))
	}))

	mux.HandleFunc("GET /api/v1/ingress/ask", func(w http.ResponseWriter, r *http.Request) {
		domain := strings.TrimSpace(r.URL.Query().Get("domain"))
		if domain == "" {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		if st != nil && st.DB() != nil {
			validator := ingress.NewStoreDomainValidator(st)
			allowed, err := validator.VerifyDomain(r.Context(), domain)
			if err != nil || !allowed {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// --- 8. Registry Endpoints ---
	mux.Handle("GET /api/v1/registry/status", authWrap(RoleViewer, func(w http.ResponseWriter, r *http.Request) {
		st, err := ctrl.GetRegistryStatus(r.Context())
		if err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, st, GetRequestID(r.Context()))
	}))

	mux.Handle("GET /api/v1/registry/repositories", authWrap(RoleViewer, func(w http.ResponseWriter, r *http.Request) {
		catalog, err := ctrl.ListRepositories(r.Context())
		if err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, catalog, GetRequestID(r.Context()))
	}))

	mux.Handle("GET /api/v1/registry/credentials", authWrap(RoleDeveloper, func(w http.ResponseWriter, r *http.Request) {
		proj := r.URL.Query().Get("project_id")
		creds, err := ctrl.GetRegistryCredentials(r.Context(), proj)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, creds, GetRequestID(r.Context()))
	}))

	mux.Handle("POST /api/v1/registry/credentials/rotate", authWrap(RoleAdmin, func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		rotated, err := ctrl.RotateRegistryCredentials(r.Context(), id)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, rotated, GetRequestID(r.Context()))
	}))

	mux.Handle("POST /api/v1/registry/garbage-collect", authWrap(RoleAdmin, func(w http.ResponseWriter, r *http.Request) {
		if err := ctrl.GarbageCollectRegistry(r.Context()); err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, map[string]string{"status": "gc_completed"}, GetRequestID(r.Context()))
	}))

	// --- 9. System Endpoints ---
	mux.Handle("GET /api/v1/system/info", authWrap(RoleViewer, func(w http.ResponseWriter, r *http.Request) {
		info, err := ctrl.GetSystemInfo(r.Context())
		if err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, info, GetRequestID(r.Context()))
	}))

	mux.Handle("GET /api/v1/system/disk", authWrap(RoleViewer, func(w http.ResponseWriter, r *http.Request) {
		disk, err := ctrl.GetDiskUsage(r.Context())
		if err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, disk, GetRequestID(r.Context()))
	}))

	mux.Handle("POST /api/v1/system/prune", authWrap(RoleAdmin, func(w http.ResponseWriter, r *http.Request) {
		var req PruneRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		res, err := ctrl.PruneSystem(r.Context(), &req)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, res, GetRequestID(r.Context()))
	}))

	// --- 10. WebSocket Endpoints (Authenticated) ---
	if wsHub != nil {
		mux.Handle("GET /ws", authWrap(RoleViewer, func(w http.ResponseWriter, r *http.Request) {
			wsHub.ServeWebSocket(w, r, "multiplex")
		}))
		mux.Handle("GET /ws/events", authWrap(RoleViewer, func(w http.ResponseWriter, r *http.Request) {
			wsHub.ServeWebSocket(w, r, "events")
		}))
		mux.Handle("GET /ws/logs", authWrap(RoleViewer, func(w http.ResponseWriter, r *http.Request) {
			wsHub.ServeWebSocket(w, r, "logs")
		}))
		mux.Handle("GET /ws/stats", authWrap(RoleViewer, func(w http.ResponseWriter, r *http.Request) {
			wsHub.ServeWebSocket(w, r, "stats")
		}))
	}

	if ptyHandler != nil {
		mux.Handle("GET /ws/pty", authWrap(RoleDeveloper, ptyHandler.ServeHTTP))
	}

	// --- 11. Server-Sent Events (SSE) Streaming Endpoints ---
	if sseBroadcaster != nil {
		mux.Handle("GET /api/v1/events/stream", authWrap(RoleViewer, func(w http.ResponseWriter, r *http.Request) {
			sseBroadcaster.ServeEventsStream(w, r)
		}))

		mux.Handle("GET /api/v1/logs/stream", authWrap(RoleViewer, func(w http.ResponseWriter, r *http.Request) {
			sseBroadcaster.ServeLogsStream(w, r, "")
		}))

		mux.Handle("GET /api/v1/projects/{pid}/services/{sid}/logs/stream", authWrap(RoleViewer, func(w http.ResponseWriter, r *http.Request) {
			sid := getPathParam(r, "sid")
			sseBroadcaster.ServeLogsStream(w, r, sid)
		}))

		mux.Handle("GET /api/v1/apps/{id}/logs/stream", authWrap(RoleViewer, func(w http.ResponseWriter, r *http.Request) {
			id := getPathParam(r, "id")
			sseBroadcaster.ServeLogsStream(w, r, id)
		}))

		mux.Handle("GET /api/v1/stats/stream", authWrap(RoleViewer, func(w http.ResponseWriter, r *http.Request) {
			sseBroadcaster.ServeStatsStream(w, r)
		}))
	}

	// --- 12. Build & Webhook Endpoints ---
	// POST /api/v1/webhooks/github (Public HMAC verified webhook receiver)
	mux.HandleFunc("POST /api/v1/webhooks/github", func(w http.ResponseWriter, r *http.Request) {
		reqID := GenerateRequestID()
		sig := r.Header.Get("X-Hub-Signature-256")
		if sig == "" {
			sig = r.Header.Get("X-Hub-Signature")
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, 5*1024*1024))
		if err != nil {
			WriteError(w, http.StatusBadRequest, ErrCodeValidationFailed, "Failed to read webhook payload", nil, reqID)
			return
		}

		secret := os.Getenv("GITHUB_WEBHOOK_SECRET")
		bld, err := ctrl.HandleGitHubWebhook(r.Context(), secret, sig, body)
		if err != nil {
			if strings.Contains(err.Error(), "signature") || strings.Contains(err.Error(), "unauthorized") {
				WriteError(w, http.StatusUnauthorized, ErrCodeUnauthorized, err.Error(), nil, reqID)
				return
			}
			WriteError(w, http.StatusBadRequest, ErrCodeValidationFailed, err.Error(), nil, reqID)
			return
		}
		if bld == nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ignored","reason":"branch mismatch"}`))
			return
		}
		WriteJSON(w, http.StatusAccepted, bld, reqID)
	})

	// POST /api/v1/webhooks/git/{app_id} (Generic token verified webhook)
	mux.HandleFunc("POST /api/v1/webhooks/git/{app_id}", func(w http.ResponseWriter, r *http.Request) {
		reqID := GenerateRequestID()
		appID := getPathParam(r, "app_id")
		token := r.URL.Query().Get("token")
		if token == "" {
			token = r.Header.Get("X-Webhook-Token")
		}
		if token == "" {
			token = ExtractToken(r)
		}

		bld, err := ctrl.HandleGenericGitWebhook(r.Context(), appID, token, r)
		if err != nil {
			if strings.Contains(err.Error(), "unauthorized") {
				WriteError(w, http.StatusUnauthorized, ErrCodeUnauthorized, err.Error(), nil, reqID)
				return
			}
			WriteError(w, http.StatusBadRequest, ErrCodeValidationFailed, err.Error(), nil, reqID)
			return
		}
		if bld == nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ignored","reason":"branch mismatch"}`))
			return
		}
		WriteJSON(w, http.StatusAccepted, bld, reqID)
	})

	// GET /api/v1/apps/{app_id}/builds (List app builds)
	mux.Handle("GET /api/v1/apps/{app_id}/builds", authWrap(RoleViewer, func(w http.ResponseWriter, r *http.Request) {
		appID := getPathParam(r, "app_id")
		builds, err := ctrl.ListAppBuilds(r.Context(), appID, 50)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, builds, GetRequestID(r.Context()))
	}))

	// GET /api/v1/builds/{build_id} (Get build details)
	mux.Handle("GET /api/v1/builds/{build_id}", authWrap(RoleViewer, func(w http.ResponseWriter, r *http.Request) {
		buildID := getPathParam(r, "build_id")
		bld, err := ctrl.GetBuild(r.Context(), buildID)
		if err != nil {
			WriteError(w, http.StatusNotFound, ErrCodeNotFound, "Build not found", nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, bld, GetRequestID(r.Context()))
	}))

	// POST /api/v1/builds/{build_id}/rebuild (Trigger manual rebuild)
	mux.Handle("POST /api/v1/builds/{build_id}/rebuild", authWrap(RoleDeveloper, func(w http.ResponseWriter, r *http.Request) {
		buildID := getPathParam(r, "build_id")
		bld, err := ctrl.Rebuild(r.Context(), buildID)
		if err != nil {
			WriteError(w, http.StatusNotFound, ErrCodeNotFound, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusAccepted, bld, GetRequestID(r.Context()))
	}))

	// GET /api/v1/builds/{build_id}/stream (SSE live build log stream via SSEBroadcaster)
	if sseBroadcaster != nil {
		mux.Handle("GET /api/v1/builds/{build_id}/stream", authWrap(RoleViewer, func(w http.ResponseWriter, r *http.Request) {
			buildID := getPathParam(r, "build_id")
			sseBroadcaster.ServeLogsStream(w, r, buildID)
		}))
	}

	// --- 13. Template Marketplace Endpoints ---
	// GET /api/v1/templates (List templates with optional ?category= and ?search=)
	mux.Handle("GET /api/v1/templates", authWrap(RoleViewer, func(w http.ResponseWriter, r *http.Request) {
		category := r.URL.Query().Get("category")
		search := r.URL.Query().Get("search")
		if search == "" {
			search = r.URL.Query().Get("q")
		}
		tpls, err := ctrl.ListTemplates(r.Context(), category, search)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, tpls, GetRequestID(r.Context()))
	}))

	// GET /api/v1/templates/{id} (Get template details & variable schema)
	mux.Handle("GET /api/v1/templates/{id}", authWrap(RoleViewer, func(w http.ResponseWriter, r *http.Request) {
		id := getPathParam(r, "id")
		tpl, err := ctrl.GetTemplate(r.Context(), id)
		if err != nil {
			WriteError(w, http.StatusNotFound, ErrCodeNotFound, "Template not found", nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusOK, tpl, GetRequestID(r.Context()))
	}))

	// POST /api/v1/templates/{id}/deploy (Deploy template stack)
	mux.Handle("POST /api/v1/templates/{id}/deploy", authWrap(RoleDeveloper, func(w http.ResponseWriter, r *http.Request) {
		id := getPathParam(r, "id")
		var req templates.DeployTemplateRequest
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&req)
		}
		resp, err := ctrl.DeployTemplate(r.Context(), id, &req)
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				WriteError(w, http.StatusNotFound, ErrCodeNotFound, err.Error(), nil, GetRequestID(r.Context()))
				return
			}
			WriteError(w, http.StatusBadRequest, ErrCodeValidationFailed, err.Error(), nil, GetRequestID(r.Context()))
			return
		}
		WriteJSON(w, http.StatusCreated, resp, GetRequestID(r.Context()))
	}))
}

