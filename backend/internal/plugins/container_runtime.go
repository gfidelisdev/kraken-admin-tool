package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	dockerclient "github.com/docker/docker/client"
	"github.com/kraken/admin-tool/backend/internal/docker"
	"github.com/kraken/admin-tool/backend/internal/models"
	"github.com/kraken/admin-tool/backend/internal/plugins/sidecarauth"
)

type ContainerRuntimeManager struct {
	mu            sync.RWMutex
	dockerService *docker.DockerService
	transport     *sidecarauth.Transport
}

func NewContainerRuntimeManager(ds *docker.DockerService) *ContainerRuntimeManager {
	return &ContainerRuntimeManager{
		dockerService: ds,
		transport:     sidecarauth.NewTransport(20 * time.Second),
	}
}

func (m *ContainerRuntimeManager) getClient(ctx context.Context) (*dockerclient.Client, error) {
	if m.dockerService == nil {
		return nil, errors.New("docker service unavailable")
	}
	cli, err := m.dockerService.GetSystemClient()
	if err != nil || cli == nil {
		return nil, errors.New("docker client unavailable on default host")
	}
	return cli, nil
}

// StartContainer spawns or attaches to a sidecar container for the plugin
func (m *ContainerRuntimeManager) StartContainer(ctx context.Context, plugin *models.InstalledPlugin, manifest *models.PluginManifest) (string, error) {
	cli, err := m.getClient(ctx)
	if err != nil {
		return "", err
	}

	containerName := fmt.Sprintf("kraken-plugin-%s", sanitizeContainerName(plugin.PluginID))
	imageName := strings.TrimSpace(manifest.Runtime.Entrypoint)
	if imageName == "" {
		return "", errors.New("container entrypoint (image) cannot be empty")
	}

	// 1. Check if container already exists
	existing, err := cli.ContainerInspect(ctx, containerName)
	if err == nil {
		if existing.State.Running {
			return existing.ID, nil
		}
		if startErr := cli.ContainerStart(ctx, existing.ID, container.StartOptions{}); startErr == nil {
			return existing.ID, nil
		}
		// Remove stale container that failed to start (e.g., attached to a missing network) and recreate it
		_ = cli.ContainerRemove(ctx, existing.ID, container.RemoveOptions{Force: true})
	}

	// 2. Ensure image exists locally, build from Dockerfile if present, or attempt pull
	_, _, inspErr := cli.ImageInspectWithRaw(ctx, imageName)
	if inspErr != nil {
		if plugin.InstallPath != "" && (hasDockerfile(plugin.InstallPath) || hasDockerfile(filepath.Join(plugin.InstallPath, "backend"))) {
			if _, buildErr := BuildContainerPluginImage(ctx, m.dockerService, plugin.InstallPath, manifest); buildErr != nil {
				return "", fmt.Errorf("failed to build container image '%s' from plugin Dockerfile: %w", imageName, buildErr)
			}
		} else if strings.HasPrefix(imageName, "kraken-plugin-") {
			return "", fmt.Errorf("local plugin image '%s' does not exist on the Docker host and no Dockerfile was found in '%s'", imageName, plugin.InstallPath)
		} else {
			rc, pullErr := cli.ImagePull(ctx, imageName, image.PullOptions{})
			if pullErr != nil {
				return "", fmt.Errorf("failed to pull container image '%s': %w", imageName, pullErr)
			}
			_, _ = io.Copy(io.Discard, rc)
			rc.Close()
		}
	}

	// 3. Create container
	internalPort := manifest.Runtime.Port
	if internalPort <= 0 {
		internalPort = 8080
	}

	// Ensure cryptographically secure installation ID and instance secret exist
	if plugin.InstallationID == "" {
		plugin.InstallationID = sidecarauth.GenerateInstallationID()
	}
	if plugin.InstanceSecret == "" {
		sec, secErr := sidecarauth.GenerateInstanceSecret()
		if secErr == nil {
			plugin.InstanceSecret = sec
		}
	}

	cfg := &container.Config{
		Image: imageName,
		Labels: map[string]string{
			"com.kraken.plugin.id":              plugin.PluginID,
			"com.kraken.plugin.installation_id": plugin.InstallationID,
			"com.kraken.plugin.version":         plugin.Version,
		},
		Env: []string{
			fmt.Sprintf("KRAKEN_PLUGIN_ID=%s", plugin.PluginID),
			fmt.Sprintf("KRAKEN_PLUGIN_INSTALLATION_ID=%s", plugin.InstallationID),
			fmt.Sprintf("KRAKEN_PLUGIN_INSTANCE_SECRET=%s", plugin.InstanceSecret),
			fmt.Sprintf("PORT=%d", internalPort),
		},
	}

	hostCfg := &container.HostConfig{
		RestartPolicy: container.RestartPolicy{Name: "unless-stopped"},
	}

	var netCfg *network.NetworkingConfig
	if netName := resolveKrakenNetwork(ctx, cli); netName != "" {
		netCfg = &network.NetworkingConfig{
			EndpointsConfig: map[string]*network.EndpointSettings{
				netName: {},
			},
		}
	}

	resp, err := cli.ContainerCreate(ctx, cfg, hostCfg, netCfg, nil, containerName)
	if err != nil && netCfg != nil {
		// Fallback without custom network if creation failed
		resp, err = cli.ContainerCreate(ctx, cfg, hostCfg, nil, nil, containerName)
	}
	if err != nil {
		return "", fmt.Errorf("failed to create plugin container: %w", err)
	}

	if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		if netCfg != nil {
			// If start failed due to network attachment, recreate on default bridge network
			_ = cli.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
			if fallbackResp, createErr := cli.ContainerCreate(ctx, cfg, hostCfg, nil, nil, containerName); createErr == nil {
				if startErr := cli.ContainerStart(ctx, fallbackResp.ID, container.StartOptions{}); startErr == nil {
					return fallbackResp.ID, nil
				}
			}
		}
		return resp.ID, fmt.Errorf("failed to start plugin container: %w", err)
	}

	return resp.ID, nil
}

// resolveKrakenNetwork detects the Docker network that kraken-backend runs on so plugin sidecars can communicate via DNS/IP.
func resolveKrakenNetwork(ctx context.Context, cli *dockerclient.Client) string {
	// 1. Inspect self container (by hostname or well-known container name)
	candidates := []string{"kraken-backend"}
	if hn, err := os.Hostname(); err == nil && hn != "" {
		candidates = append([]string{hn}, candidates...)
	}
	for _, selfID := range candidates {
		if insp, err := cli.ContainerInspect(ctx, selfID); err == nil && insp.NetworkSettings != nil {
			for netName := range insp.NetworkSettings.Networks {
				if netName != "" && netName != "bridge" && netName != "host" && netName != "none" {
					return netName
				}
			}
		}
	}

	// 2. Check well-known compose network names
	for _, netName := range []string{"kraken-admin-tool_kraken-network", "kraken-network"} {
		if _, err := cli.NetworkInspect(ctx, netName, network.InspectOptions{}); err == nil {
			return netName
		}
	}
	return ""
}

// StopContainer stops and removes the plugin container
func (m *ContainerRuntimeManager) StopContainer(ctx context.Context, plugin *models.InstalledPlugin) error {
	cli, err := m.getClient(ctx)
	if err != nil {
		return nil
	}

	containerName := fmt.Sprintf("kraken-plugin-%s", sanitizeContainerName(plugin.PluginID))
	timeoutSec := 5
	stopOpts := container.StopOptions{Timeout: &timeoutSec}

	_ = cli.ContainerStop(ctx, containerName, stopOpts)
	_ = cli.ContainerRemove(ctx, containerName, container.RemoveOptions{Force: true})
	return nil
}

// ExecuteCall proxies an authenticated, signed action call to the running container sidecar
func (m *ContainerRuntimeManager) ExecuteCall(
	ctx context.Context,
	plugin *models.InstalledPlugin,
	manifest *models.PluginManifest,
	action string,
	payload interface{},
	username string,
	userRole string,
	userID uint,
	sessionID string,
	grantedPerms []string,
) (interface{}, error) {
	cli, err := m.getClient(ctx)
	if err != nil {
		return nil, err
	}

	containerName := fmt.Sprintf("kraken-plugin-%s", sanitizeContainerName(plugin.PluginID))
	insp, err := cli.ContainerInspect(ctx, containerName)
	if err != nil || !insp.State.Running {
		return nil, fmt.Errorf("plugin container is not running: %v", err)
	}

	internalPort := manifest.Runtime.Port
	if internalPort <= 0 {
		internalPort = 8080
	}

	// Ensure instance credentials exist
	if plugin.InstallationID == "" {
		plugin.InstallationID = sidecarauth.GenerateInstallationID()
	}
	if plugin.InstanceSecret == "" {
		sec, secErr := sidecarauth.GenerateInstanceSecret()
		if secErr == nil {
			plugin.InstanceSecret = sec
		}
	}

	targetURL := fmt.Sprintf("http://%s:%d/call", containerName, internalPort)

	reqBody := map[string]interface{}{
		"action":    action,
		"payload":   payload,
		"plugin_id": plugin.PluginID,
	}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize request body: %w", err)
	}

	callParams := sidecarauth.CallParams{
		TargetURL:      targetURL,
		Path:           "/call",
		PluginID:       plugin.PluginID,
		InstallationID: plugin.InstallationID,
		InstanceSecret: plugin.InstanceSecret,
		UserID:         userID,
		Username:       username,
		SessionID:      sessionID,
		Roles:          []string{userRole},
		Permissions:    sidecarauth.MapRoleToPermissions(userRole, grantedPerms),
		Action:         action,
		RequestBody:    data,
	}

	resp, err := m.transport.Call(ctx, callParams)
	if err != nil {
		// Fallback to IP if containerName DNS fails within Docker network
		ip := insp.NetworkSettings.IPAddress
		if ip == "" && insp.NetworkSettings != nil {
			for _, netEndpoint := range insp.NetworkSettings.Networks {
				if netEndpoint != nil && netEndpoint.IPAddress != "" {
					ip = netEndpoint.IPAddress
					break
				}
			}
		}
		if ip != "" {
			callParams.TargetURL = fmt.Sprintf("http://%s:%d/call", ip, internalPort)
			resp, err = m.transport.Call(ctx, callParams)
		}
		if err != nil {
			return nil, fmt.Errorf("failed to communicate with plugin container: %w", err)
		}
	}

	return resp, nil
}

// NotifySessionEnded sends a signed lifecycle event to the sidecar when a Kraken session expires or logs out
func (m *ContainerRuntimeManager) NotifySessionEnded(ctx context.Context, plugin *models.InstalledPlugin, manifest *models.PluginManifest, rawSessionID string) error {
	eventPayload := map[string]interface{}{
		"event":             "kraken.session.ended",
		"kraken_session_id": sidecarauth.ComputeKrakenSessionID(rawSessionID),
		"occurred_at":       time.Now().UTC().Format(time.RFC3339),
	}
	data, _ := json.Marshal(eventPayload)

	containerName := fmt.Sprintf("kraken-plugin-%s", sanitizeContainerName(plugin.PluginID))
	internalPort := manifest.Runtime.Port
	if internalPort <= 0 {
		internalPort = 8080
	}
	targetURL := fmt.Sprintf("http://%s:%d/v1/internal/session-events", containerName, internalPort)

	callParams := sidecarauth.CallParams{
		TargetURL:      targetURL,
		Path:           "/v1/internal/session-events",
		PluginID:       plugin.PluginID,
		InstallationID: plugin.InstallationID,
		InstanceSecret: plugin.InstanceSecret,
		UserID:         0,
		Username:       "system",
		SessionID:      rawSessionID,
		Roles:          []string{"system"},
		Action:         "internal.session.event",
		RequestBody:    data,
	}

	_, err := m.transport.Call(ctx, callParams)
	return err
}

// NotifyUserDisabled sends a signed lifecycle event when a user is deactivated in Kraken
func (m *ContainerRuntimeManager) NotifyUserDisabled(ctx context.Context, plugin *models.InstalledPlugin, manifest *models.PluginManifest, userID uint) error {
	eventPayload := map[string]interface{}{
		"event":          "kraken.user.disabled",
		"kraken_user_id": userID,
		"occurred_at":    time.Now().UTC().Format(time.RFC3339),
	}
	data, _ := json.Marshal(eventPayload)

	containerName := fmt.Sprintf("kraken-plugin-%s", sanitizeContainerName(plugin.PluginID))
	internalPort := manifest.Runtime.Port
	if internalPort <= 0 {
		internalPort = 8080
	}
	targetURL := fmt.Sprintf("http://%s:%d/v1/internal/session-events", containerName, internalPort)

	callParams := sidecarauth.CallParams{
		TargetURL:      targetURL,
		Path:           "/v1/internal/session-events",
		PluginID:       plugin.PluginID,
		InstallationID: plugin.InstallationID,
		InstanceSecret: plugin.InstanceSecret,
		UserID:         userID,
		Username:       "system",
		Roles:          []string{"system"},
		Action:         "internal.session.event",
		RequestBody:    data,
	}

	_, err := m.transport.Call(ctx, callParams)
	return err
}

// NotifyPluginRevoked notifies and revokes the sidecar upon plugin uninstall or status revocation
func (m *ContainerRuntimeManager) NotifyPluginRevoked(ctx context.Context, plugin *models.InstalledPlugin, manifest *models.PluginManifest) error {
	eventPayload := map[string]interface{}{
		"event":           "plugin.installation.revoked",
		"installation_id": plugin.InstallationID,
		"occurred_at":     time.Now().UTC().Format(time.RFC3339),
	}
	data, _ := json.Marshal(eventPayload)

	containerName := fmt.Sprintf("kraken-plugin-%s", sanitizeContainerName(plugin.PluginID))
	internalPort := manifest.Runtime.Port
	if internalPort <= 0 {
		internalPort = 8080
	}
	targetURL := fmt.Sprintf("http://%s:%d/v1/internal/session-events", containerName, internalPort)

	callParams := sidecarauth.CallParams{
		TargetURL:      targetURL,
		Path:           "/v1/internal/session-events",
		PluginID:       plugin.PluginID,
		InstallationID: plugin.InstallationID,
		InstanceSecret: plugin.InstanceSecret,
		UserID:         0,
		Username:       "system",
		Roles:          []string{"system"},
		Action:         "internal.session.event",
		RequestBody:    data,
	}

	_, _ = m.transport.Call(ctx, callParams)
	return m.StopContainer(ctx, plugin)
}

func sanitizeContainerName(s string) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			sb.WriteRune(r)
		} else {
			sb.WriteRune('-')
		}
	}
	res := strings.Trim(sb.String(), "-")
	if res == "" {
		return "plugin"
	}
	return res
}

