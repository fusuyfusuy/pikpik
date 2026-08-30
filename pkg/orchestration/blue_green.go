package orchestration

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/fusuycorp/pikpik/pkg/ingress"
)

var (
	ErrGreenHealthCheckFailed = errors.New("green container health check failed")
	ErrBlueGreenDeployTimeout = errors.New("blue-green deployment timed out")
	ErrGreenStartFailed       = errors.New("failed to start green container")
)

// BlueGreenConfig defines parameters for executing a zero-downtime blue-green deployment.
type BlueGreenConfig struct {
	AppID           string               `json:"app_id"`
	ProjectID       string               `json:"project_id,omitempty"`
	Name            string               `json:"name,omitempty"`
	Domain          string               `json:"domain"`
	Image           string               `json:"image"`
	ContainerPort   uint32               `json:"container_port"`
	Environment     map[string]string    `json:"environment,omitempty"`
	HealthCheckPath string               `json:"health_check_path,omitempty"` // Default "/healthz"
	ProbeInterval   time.Duration        `json:"probe_interval,omitempty"`   // Default 250ms
	ProbeTimeout    time.Duration        `json:"probe_timeout,omitempty"`    // Default 30s
	DrainPeriod     time.Duration        `json:"drain_period,omitempty"`     // Default 5s
	CanarySteps     []int                `json:"canary_steps,omitempty"`     // Optional step-wise weight shifting e.g. [10, 50, 100]
	StepDelay       time.Duration        `json:"step_delay,omitempty"`       // Delay between canary steps
	Networks        []string             `json:"networks,omitempty"`
	Mounts          []VolumeMountSpec    `json:"mounts,omitempty"`
	Resources       ResourceRequirements `json:"resources,omitempty"`
	Labels          map[string]string    `json:"labels,omitempty"`
}

// BlueGreenResult summarizes the outcome of a blue-green deployment execution.
type BlueGreenResult struct {
	AppID             string        `json:"app_id"`
	BlueContainerID   string        `json:"blue_container_id,omitempty"`
	GreenContainerID  string        `json:"green_container_id"`
	ActiveContainerID string        `json:"active_container_id"`
	Domain            string        `json:"domain"`
	Status            string        `json:"status"` // "success", "failed"
	Duration          time.Duration `json:"duration"`
	SwappedAt         time.Time     `json:"swapped_at"`
}

// BlueGreenDeployer defines the contract for coordinating blue-green cutovers with ingress traffic shifting.
type BlueGreenDeployer interface {
	Deploy(ctx context.Context, cfg BlueGreenConfig) (*BlueGreenResult, error)
}

// ProbeFunc is a function type for performing health probes against a container endpoint.
type ProbeFunc func(ctx context.Context, probeURL string) (bool, error)

// DefaultBlueGreenDeployer implements BlueGreenDeployer.
type DefaultBlueGreenDeployer struct {
	containers ContainerManager
	ingress    ingress.TrafficSplitter
	httpClient *http.Client
	probeFn    ProbeFunc
}

// BlueGreenOption allows custom configuration of DefaultBlueGreenDeployer.
type BlueGreenOption func(*DefaultBlueGreenDeployer)

// WithProbeFunc sets a custom probing function (ideal for unit testing).
func WithProbeFunc(fn ProbeFunc) BlueGreenOption {
	return func(d *DefaultBlueGreenDeployer) {
		d.probeFn = fn
	}
}

// WithHTTPClient sets a custom HTTP client for health probes.
func WithHTTPClient(client *http.Client) BlueGreenOption {
	return func(d *DefaultBlueGreenDeployer) {
		d.httpClient = client
	}
}

// NewBlueGreenDeployer constructs a new DefaultBlueGreenDeployer.
func NewBlueGreenDeployer(containers ContainerManager, ingressSplitter ingress.TrafficSplitter, opts ...BlueGreenOption) *DefaultBlueGreenDeployer {
	deployer := &DefaultBlueGreenDeployer{
		containers: containers,
		ingress:    ingressSplitter,
		httpClient: &http.Client{
			Timeout: 2 * time.Second,
		},
	}
	for _, opt := range opts {
		opt(deployer)
	}
	return deployer
}

// Deploy orchestrates a full Blue-Green container rollout with probe verification and ingress cutover.
func (d *DefaultBlueGreenDeployer) Deploy(ctx context.Context, cfg BlueGreenConfig) (*BlueGreenResult, error) {
	startTime := time.Now()

	// 1. Validation & Default resolution
	if cfg.AppID == "" && cfg.Name == "" {
		return nil, errors.New("app_id or name is required for blue-green deployment")
	}
	if cfg.Image == "" {
		return nil, errors.New("image is required for blue-green deployment")
	}
	if cfg.ContainerPort == 0 {
		cfg.ContainerPort = 80
	}
	if cfg.HealthCheckPath == "" {
		cfg.HealthCheckPath = "/healthz"
	}
	if !strings.HasPrefix(cfg.HealthCheckPath, "/") {
		cfg.HealthCheckPath = "/" + cfg.HealthCheckPath
	}
	if cfg.ProbeInterval == 0 {
		cfg.ProbeInterval = 250 * time.Millisecond
	}
	if cfg.ProbeTimeout == 0 {
		cfg.ProbeTimeout = 30 * time.Second
	}

	baseName := cfg.Name
	if baseName == "" {
		baseName = cfg.AppID
	}

	// 2. Discover Existing Active Blue Container
	var blueContainerID string
	var blueIP string
	existingContainers, err := d.containers.List(ctx, ListOptions{
		ProjectID: cfg.ProjectID,
		All:       false,
	})
	if err == nil {
		for _, c := range existingContainers {
			if c.State != "running" {
				continue
			}
			matched := false
			if c.Labels != nil {
				if c.Labels["pikpik.name"] == baseName || c.Labels["pikpik.app_id"] == cfg.AppID {
					matched = true
				}
			}
			if !matched && (c.Name == baseName || strings.HasPrefix(c.Name, baseName+"_")) {
				matched = true
			}
			if matched {
				blueContainerID = c.ID
				blueIP = c.IPAddress
				break
			}
		}
	}

	if blueIP == "" {
		blueIP = baseName + "_blue"
	}

	// 3. Create Green Container Spec
	greenName := fmt.Sprintf("%s_green_%d", baseName, time.Now().UnixNano())
	greenLabels := make(map[string]string)
	for k, v := range cfg.Labels {
		greenLabels[k] = v
	}
	greenLabels["pikpik.managed"] = "true"
	greenLabels["pikpik.name"] = baseName
	greenLabels["pikpik.slot"] = "green"
	if cfg.AppID != "" {
		greenLabels["pikpik.app_id"] = cfg.AppID
	}
	if cfg.ProjectID != "" {
		greenLabels["pikpik.project_id"] = cfg.ProjectID
	}

	greenSpec := ContainerSpec{
		Name:          greenName,
		ProjectID:     cfg.ProjectID,
		Image:         cfg.Image,
		Environment:   cfg.Environment,
		Mounts:        cfg.Mounts,
		Networks:      cfg.Networks,
		ExposedPorts:  []PortMappingSpec{{ContainerPort: cfg.ContainerPort, Protocol: "tcp"}},
		Resources:     cfg.Resources,
		Labels:        greenLabels,
		RestartPolicy: "unless-stopped",
		StopTimeout:   30 * time.Second,
	}

	// 4. Create and Start Green Container
	greenContainerID, err := d.containers.Create(ctx, greenSpec)
	if err != nil {
		return nil, fmt.Errorf("blue-green create failed: %w", err)
	}

	if err := d.containers.Start(ctx, greenContainerID); err != nil {
		_ = d.containers.Remove(ctx, greenContainerID, true, true)
		return nil, fmt.Errorf("%w: %v", ErrGreenStartFailed, err)
	}

	// 5. Inspect Green Container
	greenStatus, err := d.containers.Inspect(ctx, greenContainerID)
	if err != nil {
		_ = d.containers.Remove(ctx, greenContainerID, true, true)
		return nil, fmt.Errorf("blue-green inspect failed: %w", err)
	}

	greenIP := greenStatus.IPAddress
	if greenIP == "" {
		greenIP = "127.0.0.1"
	}

	// 6. Health Check Probe Loop
	probeURL := fmt.Sprintf("http://%s:%d%s", greenIP, cfg.ContainerPort, cfg.HealthCheckPath)
	probeCtx, probeCancel := context.WithTimeout(ctx, cfg.ProbeTimeout)
	defer probeCancel()

	ticker := time.NewTicker(cfg.ProbeInterval)
	defer ticker.Stop()

	healthy := false
	var lastProbeErr error

	for {
		// Verify container is still running
		st, inspErr := d.containers.Inspect(probeCtx, greenContainerID)
		if inspErr == nil && (st.State == "exited" || st.State == "dead") {
			_ = d.containers.Remove(ctx, greenContainerID, true, true)
			return nil, fmt.Errorf("%w: container exited with status %q", ErrGreenHealthCheckFailed, st.State)
		}

		// Perform probe
		if d.probeFn != nil {
			var ok bool
			ok, lastProbeErr = d.probeFn(probeCtx, probeURL)
			if ok {
				healthy = true
				break
			}
		} else {
			req, reqErr := http.NewRequestWithContext(probeCtx, http.MethodGet, probeURL, nil)
			if reqErr == nil {
				resp, respErr := d.httpClient.Do(req)
				if respErr == nil {
					_ = resp.Body.Close()
					if resp.StatusCode >= 200 && resp.StatusCode < 300 {
						healthy = true
						break
					}
					lastProbeErr = fmt.Errorf("health check returned status %d", resp.StatusCode)
				} else {
					lastProbeErr = respErr
				}
			} else {
				lastProbeErr = reqErr
			}
		}

		select {
		case <-probeCtx.Done():
			// Teardown Green container on failure; Blue is untouched!
			_ = d.containers.Remove(ctx, greenContainerID, true, true)
			if lastProbeErr != nil {
				return nil, fmt.Errorf("%w: probe %s timed out after %v: %v", ErrGreenHealthCheckFailed, probeURL, cfg.ProbeTimeout, lastProbeErr)
			}
			return nil, fmt.Errorf("%w: probe %s timed out after %v", ErrGreenHealthCheckFailed, probeURL, cfg.ProbeTimeout)
		case <-ticker.C:
			// Next probe iteration
		}
	}

	if !healthy {
		_ = d.containers.Remove(ctx, greenContainerID, true, true)
		return nil, fmt.Errorf("%w: probe failed", ErrGreenHealthCheckFailed)
	}

	// 7. Success: Traffic Shifting
	greenDial := fmt.Sprintf("%s:%d", greenIP, cfg.ContainerPort)
	blueDial := fmt.Sprintf("%s:%d", blueIP, cfg.ContainerPort)

	if d.ingress != nil && cfg.Domain != "" {
		// Optional step-wise canary transition
		if len(cfg.CanarySteps) > 0 && blueContainerID != "" {
			for _, pct := range cfg.CanarySteps {
				if pct > 0 && pct < 100 {
					stepCfg := ingress.TrafficSplitConfig{
						Domain:         cfg.Domain,
						StableUpstream: blueDial,
						CanaryUpstream: greenDial,
						CanaryPercent:  pct,
					}
					_ = d.ingress.SetTrafficSplit(ctx, cfg.Domain, stepCfg)
					if cfg.StepDelay > 0 {
						time.Sleep(cfg.StepDelay)
					}
				}
			}
		}

		// Cutover 100% traffic to Green
		finalCfg := ingress.TrafficSplitConfig{
			Domain:         cfg.Domain,
			StableUpstream: greenDial,
			CanaryPercent:  0,
		}
		if err := d.ingress.SetTrafficSplit(ctx, cfg.Domain, finalCfg); err != nil {
			// Rollback traffic if ingress switch fails
			_ = d.containers.Remove(ctx, greenContainerID, true, true)
			return nil, fmt.Errorf("blue-green ingress cutover failed: %w", err)
		}
	}

	swappedAt := time.Now().UTC()

	// 8. Drain Period and Blue Teardown
	if blueContainerID != "" && blueContainerID != greenContainerID {
		if cfg.DrainPeriod > 0 {
			time.Sleep(cfg.DrainPeriod)
		}
		_ = d.containers.Stop(ctx, blueContainerID, 30*time.Second)
		_ = d.containers.Remove(ctx, blueContainerID, true, false)
	}

	return &BlueGreenResult{
		AppID:             cfg.AppID,
		BlueContainerID:   blueContainerID,
		GreenContainerID:  greenContainerID,
		ActiveContainerID: greenContainerID,
		Domain:            cfg.Domain,
		Status:            "success",
		Duration:          time.Since(startTime),
		SwappedAt:         swappedAt,
	}, nil
}
