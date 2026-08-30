package templates

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math/big"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	dockerTypes "github.com/docker/docker/api/types"
	"github.com/fusuycorp/pikpik/pkg/orchestration"
	"github.com/fusuycorp/pikpik/pkg/store"
)

const (
	DefaultVolumeRootPath = "/var/lib/pikpik/volumes"
	alphaNumericCharset   = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
)

var (
	varInterpolateRegex = regexp.MustCompile(`\$\{([a-zA-Z0-9_]+)(?::-([^}]*))?\}|\$([a-zA-Z0-9_]+)`)
)

// Deployer defines the contract for instantiating template applications.
type Deployer interface {
	Deploy(ctx context.Context, templateID string, req DeployTemplateRequest) (*DeployTemplateResponse, error)
}

// DefaultDeployer implements Deployer with variable evaluation, token auto-generation,
// isolated network setup, volume slugging, SQLite metadata storage, and container orchestration.
type DefaultDeployer struct {
	catalog    *Catalog
	st         store.Store
	orch       orchestration.Orchestrator
	volumeRoot string
}

// NewDeployer constructs a new DefaultDeployer instance.
func NewDeployer(catalog *Catalog, st store.Store, orch orchestration.Orchestrator) *DefaultDeployer {
	if catalog == nil {
		catalog = DefaultCatalog()
	}
	return &DefaultDeployer{
		catalog:    catalog,
		st:         st,
		orch:       orch,
		volumeRoot: DefaultVolumeRootPath,
	}
}

// SetVolumeRoot overrides the default volume base path (useful for testing).
func (d *DefaultDeployer) SetVolumeRoot(path string) {
	d.volumeRoot = path
}

// Deploy deploys a curated template stack into the pikpik engine.
func (d *DefaultDeployer) Deploy(ctx context.Context, templateID string, req DeployTemplateRequest) (*DeployTemplateResponse, error) {
	tpl, err := d.catalog.GetTemplate(templateID)
	if err != nil {
		return nil, err
	}

	appID := store.NewID("app")
	appName := strings.TrimSpace(req.Name)
	if appName == "" {
		appName = fmt.Sprintf("%s-%s", tpl.ID, appID[len(appID)-6:])
	}
	projectID := req.ProjectID
	if projectID == "" {
		projectID = "default"
	}
	stageID := req.StageID
	if stageID == "" {
		stageID = "production"
	}

	// 1. Evaluate & Auto-Generate Variables
	resolvedVars, err := d.evaluateVariables(tpl, req.Variables)
	if err != nil {
		return nil, fmt.Errorf("variable resolution failed: %w", err)
	}

	// Add built-in environment context variables
	resolvedVars["APP_ID"] = appID
	resolvedVars["APP_NAME"] = appName
	resolvedVars["PROJECT_ID"] = projectID
	resolvedVars["STAGE_ID"] = stageID
	if req.Domain != "" {
		resolvedVars["DOMAIN"] = req.Domain
	}

	// 2. Setup Isolated Bridge Network
	networkName := fmt.Sprintf("pikpik_net_%s", appID)
	if d.orch != nil && d.orch.RawClient() != nil {
		_, _ = d.orch.RawClient().NetworkCreate(ctx, networkName, dockerTypes.NetworkCreate{
			Driver: "bridge",
			Labels: map[string]string{
				"pikpik.app_id":      appID,
				"pikpik.template_id": tpl.ID,
				"pikpik.managed":     "true",
			},
		})
	}

	// 3. Process Volume Mounts with Slugging
	createdVolumes := make([]string, 0)
	var volumeRecords []*store.Volume

	for _, vol := range tpl.Volumes {
		hostPath := fmt.Sprintf("%s/%s_%s", strings.TrimRight(d.volumeRoot, "/"), appID, vol.Name)
		createdVolumes = append(createdVolumes, hostPath)

		volRecord := &store.Volume{
			ID:        store.NewID("vol"),
			ProjectID: projectID,
			ServiceID: appID,
			Name:      vol.Name,
			Slug:      fmt.Sprintf("%s_%s", appID, vol.Name),
			MountPath: vol.MountPath,
			HostPath:  hostPath,
			Type:      "bind",
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		}
		volumeRecords = append(volumeRecords, volRecord)
	}

	// 4. Register Metadata in SQLite Store
	deployedServices := make([]string, 0, len(tpl.Services))
	var domainList []string
	if req.Domain != "" {
		domainList = append(domainList, req.Domain)
	}

	if d.st != nil {
		d.ensureProjectAndStage(ctx, projectID, stageID)

		// Persist primary App Service
		primaryType := "app"
		if tpl.Category == CategoryDatabases {
			primaryType = "database"
		}

		primaryImage := ""
		if len(tpl.Services) > 0 {
			primaryImage = tpl.Services[0].Image
		}

		_ = d.st.Services().Create(ctx, &store.Service{
			ID:            appID,
			ProjectID:     projectID,
			StageID:       stageID,
			Name:          appName,
			Slug:          strings.ToLower(appName),
			Type:          primaryType,
			Image:         primaryImage,
			Replicas:      1,
			ContainerPort: tpl.DefaultPort,
			DomainNames:   domainList,
			Status:        "running",
			CreatedAt:     time.Now().UTC(),
			UpdatedAt:     time.Now().UTC(),
		})

		// Persist Environment Variables
		for k, v := range resolvedVars {
			isSecret := isSecretVar(tpl, k)
			_ = d.st.EnvVars().Set(ctx, &store.EnvVar{
				ID:             store.NewID("env"),
				ScopeTier:      store.TierService,
				ResourceID:     appID,
				Key:            k,
				ValueEncrypted: v,
				IsSecret:       isSecret,
				CreatedAt:      time.Now().UTC(),
				UpdatedAt:      time.Now().UTC(),
			})
		}

		// Persist Volumes
		for _, vr := range volumeRecords {
			_ = d.st.Volumes().Create(ctx, vr)
		}

		// Persist Deployment Record
		_ = d.st.Deployments().Create(ctx, &store.Deployment{
			ID:          store.NewID("dep"),
			ServiceID:   appID,
			ImageTag:    primaryImage,
			Status:      "healthy",
			InitiatedBy: "template:" + tpl.ID,
			StartedAt:   time.Now().UTC(),
		})
	}

	// 5. Deploy Containers via Orchestration Engine
	deployedContainers := make([]string, 0, len(tpl.Services))
	endpoints := make([]string, 0)

	orderedServices, err := d.resolveServiceOrder(tpl.Services)
	if err != nil {
		return nil, fmt.Errorf("failed to order services: %w", err)
	}

	for _, svc := range orderedServices {
		deployedServices = append(deployedServices, svc.Name)

		// Map Volumes
		var containerMounts []orchestration.VolumeMountSpec
		for _, vol := range svc.Mounts {
			hostPath := fmt.Sprintf("%s/%s_%s", strings.TrimRight(d.volumeRoot, "/"), appID, vol.Name)
			containerMounts = append(containerMounts, orchestration.VolumeMountSpec{
				Type:     "bind",
				Source:   hostPath,
				Target:   vol.MountPath,
				ReadOnly: vol.ReadOnly,
			})
		}

		// Interpolate Service Environment
		serviceEnv := make(map[string]string)
		for k, v := range resolvedVars {
			serviceEnv[k] = v
		}
		for k, v := range svc.Environment {
			serviceEnv[k] = interpolateString(v, resolvedVars)
		}

		// Interpolate Command & Entrypoint
		cmd := make([]string, len(svc.Command))
		for i, c := range svc.Command {
			cmd[i] = interpolateString(c, resolvedVars)
		}
		ep := make([]string, len(svc.Entrypoint))
		for i, e := range svc.Entrypoint {
			ep[i] = interpolateString(e, resolvedVars)
		}

		// Build Labels
		labels := make(map[string]string)
		for k, v := range svc.Labels {
			labels[k] = v
		}
		labels["pikpik.app_id"] = appID
		labels["pikpik.template_id"] = tpl.ID
		labels["pikpik.service_name"] = svc.Name
		labels["pikpik.managed"] = "true"

		containerSpec := orchestration.ContainerSpec{
			ID:            fmt.Sprintf("%s_%s", appID, svc.Name),
			Name:          fmt.Sprintf("%s_%s", appName, svc.Name),
			ProjectID:     projectID,
			Image:         svc.Image,
			Environment:   serviceEnv,
			Mounts:        containerMounts,
			Networks:      []string{networkName},
			ExposedPorts:  svc.Ports,
			Resources:     svc.Resources,
			HealthCheck:   svc.HealthCheck,
			RestartPolicy: svc.Restart,
			Labels:        labels,
			Command:       cmd,
			Entrypoint:    ep,
		}

		for _, p := range svc.Ports {
			portNum := p.HostPort
			if portNum == 0 {
				portNum = p.ContainerPort
			}
			endpoints = append(endpoints, fmt.Sprintf("%s://%s:%d", p.Protocol, appName, portNum))
		}

		if d.orch != nil {
			cid, err := d.orch.Containers().Create(ctx, containerSpec)
			if err != nil {
				d.rollback(ctx, appID, networkName, deployedContainers, volumeRecords, createdVolumes)
				return nil, fmt.Errorf("failed to create container for service '%s': %w", svc.Name, err)
			}
			deployedContainers = append(deployedContainers, cid)

			if err := d.orch.Containers().Start(ctx, cid); err != nil {
				d.rollback(ctx, appID, networkName, deployedContainers, volumeRecords, createdVolumes)
				return nil, fmt.Errorf("failed to start container for service '%s': %w", svc.Name, err)
			}
		} else {
			deployedContainers = append(deployedContainers, containerSpec.ID)
		}
	}

	if len(endpoints) == 0 && tpl.DefaultPort > 0 {
		endpoints = append(endpoints, fmt.Sprintf("http://%s:%d", appName, tpl.DefaultPort))
	}

	return &DeployTemplateResponse{
		AppID:             appID,
		Name:              appName,
		TemplateID:        tpl.ID,
		Category:          tpl.Category,
		Status:            "running",
		Services:          deployedServices,
		Containers:        deployedContainers,
		Volumes:           createdVolumes,
		Network:           networkName,
		Endpoints:         endpoints,
		ResolvedVariables: sanitizeResolvedVariables(resolvedVars, tpl),
		DeployedAt:        time.Now().UTC(),
		Message:           fmt.Sprintf("Template %s successfully deployed as application %s", tpl.Name, appName),
	}, nil
}

func (d *DefaultDeployer) ensureProjectAndStage(ctx context.Context, projectID, stageID string) {
	if d.st == nil {
		return
	}
	orgID := "org_default"
	if _, err := d.st.Organizations().GetByID(ctx, orgID); err != nil {
		_ = d.st.Organizations().Create(ctx, &store.Organization{
			ID:        orgID,
			Name:      "Default Organization",
			Slug:      "default",
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		})
	}
	if _, err := d.st.Projects().GetByID(ctx, projectID); err != nil {
		_ = d.st.Projects().Create(ctx, &store.Project{
			ID:          projectID,
			OrgID:       orgID,
			Name:        projectID,
			Slug:        strings.ToLower(projectID),
			Description: "Default project",
			CreatedAt:   time.Now().UTC(),
			UpdatedAt:   time.Now().UTC(),
		})
	}
	if _, err := d.st.Stages().GetByID(ctx, stageID); err != nil {
		_ = d.st.Stages().Create(ctx, &store.Stage{
			ID:        stageID,
			ProjectID: projectID,
			Name:      stageID,
			Slug:      strings.ToLower(stageID),
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		})
	}
}

// evaluateVariables processes user inputs, generates required tokens, and fills defaults.
func (d *DefaultDeployer) evaluateVariables(tpl *Template, userInputs map[string]string) (map[string]string, error) {
	resolved := make(map[string]string)

	for _, ev := range tpl.EnvVars {
		userVal, provided := userInputs[ev.Key]

		if provided && strings.TrimSpace(userVal) != "" {
			resolved[ev.Key] = strings.TrimSpace(userVal)
			continue
		}

		// Handle AutoGenerate
		if ev.AutoGenerate != "" {
			token, err := GenerateToken(ev.AutoGenerate)
			if err != nil {
				return nil, fmt.Errorf("failed to generate token for %s: %w", ev.Key, err)
			}
			resolved[ev.Key] = token
			continue
		}

		// Handle Default value
		if ev.Default != "" {
			resolved[ev.Key] = ev.Default
			continue
		}

		// Handle Required without default/autogen
		if ev.Required {
			return nil, fmt.Errorf("required environment variable '%s' (%s) was not provided", ev.Key, ev.Label)
		}
	}

	// Include any additional user overrides not in schema
	for k, v := range userInputs {
		if _, exists := resolved[k]; !exists {
			resolved[k] = v
		}
	}

	return resolved, nil
}

// GenerateToken generates cryptographically secure random tokens according to format specification.
func GenerateToken(format string) (string, error) {
	switch format {
	case "hex_32":
		// 16 bytes = 32 hex characters
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			return "", err
		}
		return hex.EncodeToString(b), nil

	case "pass_16":
		// 16 alphanumeric characters
		b := make([]byte, 16)
		charsetLen := big.NewInt(int64(len(alphaNumericCharset)))
		for i := 0; i < 16; i++ {
			idx, err := rand.Int(rand.Reader, charsetLen)
			if err != nil {
				return "", err
			}
			b[i] = alphaNumericCharset[idx.Int64()]
		}
		return string(b), nil

	case "base64_32":
		// 32 random bytes base64 standard encoded
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return "", err
		}
		return base64.StdEncoding.EncodeToString(b), nil

	default:
		// Fallback to 16 bytes hex (32 chars)
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			return "", err
		}
		return hex.EncodeToString(b), nil
	}
}

// resolveServiceOrder topologically sorts template services based on DependsOn dependencies.
func (d *DefaultDeployer) resolveServiceOrder(services []TemplateService) ([]TemplateService, error) {
	if len(services) <= 1 {
		return services, nil
	}

	serviceMap := make(map[string]TemplateService)
	inDegree := make(map[string]int)
	graph := make(map[string][]string)

	for _, svc := range services {
		serviceMap[svc.Name] = svc
		inDegree[svc.Name] = 0
		graph[svc.Name] = make([]string, 0)
	}

	for _, svc := range services {
		for _, dep := range svc.DependsOn {
			if _, exists := serviceMap[dep]; !exists {
				return nil, fmt.Errorf("service '%s' depends on unknown service '%s'", svc.Name, dep)
			}
			graph[dep] = append(graph[dep], svc.Name)
			inDegree[svc.Name]++
		}
	}

	var queue []string
	for name, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, name)
		}
	}
	sort.Strings(queue)

	var ordered []TemplateService
	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		ordered = append(ordered, serviceMap[curr])

		neighbors := graph[curr]
		sort.Strings(neighbors)
		for _, neighbor := range neighbors {
			inDegree[neighbor]--
			if inDegree[neighbor] == 0 {
				queue = append(queue, neighbor)
				sort.Strings(queue)
			}
		}
	}

	if len(ordered) != len(services) {
		return nil, fmt.Errorf("cyclic dependency detected among template services")
	}

	return ordered, nil
}

func interpolateString(s string, vars map[string]string) string {
	return varInterpolateRegex.ReplaceAllStringFunc(s, func(match string) string {
		sub := varInterpolateRegex.FindStringSubmatch(match)
		var varName string
		var defaultVal string

		if sub[1] != "" {
			varName = sub[1]
			defaultVal = sub[2]
		} else if sub[3] != "" {
			varName = sub[3]
		}

		if val, exists := vars[varName]; exists && val != "" {
			return val
		}
		return defaultVal
	})
}

func (d *DefaultDeployer) rollback(ctx context.Context, appID string, networkName string, containerIDs []string, volumeRecords []*store.Volume, createdVolumes []string) {
	// 1. Stop and remove any containers created during this deployment
	if d.orch != nil && d.orch.Containers() != nil {
		for _, cid := range containerIDs {
			_ = d.orch.Containers().Stop(context.Background(), cid, 5*time.Second)
			_ = d.orch.Containers().Remove(context.Background(), cid, true, true)
		}
	}

	// 2. Remove Docker bridge network
	if d.orch != nil && d.orch.RawClient() != nil && networkName != "" {
		_ = d.orch.RawClient().NetworkRemove(context.Background(), networkName)
	}

	// 3. Purge persisted store metadata for the app
	if d.st != nil && appID != "" {
		_ = d.st.Services().Delete(context.Background(), appID)
		for _, vr := range volumeRecords {
			_ = d.st.Volumes().Delete(context.Background(), vr.ID)
		}
	}

	// 4. Remove any volume directories created on disk
	for _, hostPath := range createdVolumes {
		_ = os.RemoveAll(hostPath)
	}
}

func isSecretVar(tpl *Template, key string) bool {
	if tpl != nil {
		for _, ev := range tpl.EnvVars {
			if ev.Key == key {
				return ev.IsSecret || ev.AutoGenerate != ""
			}
		}
	}
	k := strings.ToUpper(key)
	return strings.Contains(k, "PASSWORD") || strings.Contains(k, "SECRET") || strings.Contains(k, "KEY") || strings.Contains(k, "TOKEN")
}

func sanitizeResolvedVariables(vars map[string]string, tpl *Template) map[string]string {
	res := make(map[string]string, len(vars))
	for k, v := range vars {
		if isSecretVar(tpl, k) {
			res[k] = "[REDACTED]"
		} else {
			res[k] = v
		}
	}
	return res
}

