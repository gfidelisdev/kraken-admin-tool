package docker

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/kraken/admin-tool/backend/internal/models"
)

var safeStackNameRegex = regexp.MustCompile(`^[a-zA-Z0-9_\-]+$`)

// ValidateStackName checks that the stack name contains only safe identifier characters
func ValidateStackName(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return errors.New("stack name cannot be empty")
	}
	if !safeStackNameRegex.MatchString(trimmed) {
		return fmt.Errorf("invalid stack name '%s': only alphanumeric, hyphens, and underscores are allowed", name)
	}
	return nil
}

// ListComposeStacks returns all Docker Compose projects discovered on the target server
func (s *DockerService) ListComposeStacks(ctx context.Context, serverID uint) ([]models.ComposeStack, error) {
	cli, err := s.GetClient(ctx, serverID)
	if err != nil {
		return nil, err
	}

	containers, err := cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	// Group containers by compose project label
	stackMap := make(map[string]*models.ComposeStack)
	servicesMap := make(map[string]map[string]*models.ComposeService) // stackName -> serviceName -> service

	for _, c := range containers {
		projectName := strings.TrimSpace(c.Labels["com.docker.compose.project"])
		if projectName == "" {
			// Container does not belong to a Docker Compose stack
			continue
		}

		serviceName := strings.TrimSpace(c.Labels["com.docker.compose.service"])
		if serviceName == "" {
			serviceName = "unspecified"
		}

		stack, exists := stackMap[projectName]
		if !exists {
			stack = &models.ComposeStack{
				Name:        projectName,
				ConfigFiles: c.Labels["com.docker.compose.project.config_files"],
				WorkingDir:  c.Labels["com.docker.compose.project.working_dir"],
				Created:     c.Created,
				Services:    []models.ComposeService{},
			}
			stackMap[projectName] = stack
			servicesMap[projectName] = make(map[string]*models.ComposeService)
		}

		if c.Created < stack.Created || stack.Created == 0 {
			stack.Created = c.Created
		}

		stack.TotalContainers++
		isRunning := strings.EqualFold(c.State, "running")
		if isRunning {
			stack.RunningContainers++
		}

		// Process service
		svc, svcExists := servicesMap[projectName][serviceName]
		if !svcExists {
			svc = &models.ComposeService{
				Name:       serviceName,
				Image:      c.Image,
				Containers: []models.ContainerSummary{},
			}
			servicesMap[projectName][serviceName] = svc
		}

		svc.Replicas++
		if isRunning {
			svc.RunningReplicas++
		}

		// Convert ports
		var ports []models.PortSummary
		for _, p := range c.Ports {
			ports = append(ports, models.PortSummary{
				IP:          p.IP,
				PrivatePort: p.PrivatePort,
				PublicPort:  p.PublicPort,
				Type:        p.Type,
			})
		}

		svc.Containers = append(svc.Containers, models.ContainerSummary{
			ID:      c.ID,
			Names:   c.Names,
			Image:   c.Image,
			Command: c.Command,
			Created: c.Created,
			State:   c.State,
			Status:  c.Status,
			Ports:   ports,
		})
	}

	// Assemble final results and determine status
	var result []models.ComposeStack
	for stackName, stack := range stackMap {
		if stack.TotalContainers == 0 {
			stack.Status = "stopped"
		} else if stack.RunningContainers == stack.TotalContainers {
			stack.Status = "running"
		} else if stack.RunningContainers == 0 {
			stack.Status = "stopped"
		} else {
			stack.Status = "partial"
		}

		for _, svc := range servicesMap[stackName] {
			if svc.Replicas == 0 || svc.RunningReplicas == 0 {
				svc.Status = "stopped"
			} else if svc.RunningReplicas == svc.Replicas {
				svc.Status = "running"
			} else {
				svc.Status = "partial"
			}
			stack.Services = append(stack.Services, *svc)
		}

		sort.Slice(stack.Services, func(i, j int) bool {
			return stack.Services[i].Name < stack.Services[j].Name
		})

		result = append(result, *stack)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result, nil
}

// GetComposeStack returns details for a specific Compose project
func (s *DockerService) GetComposeStack(ctx context.Context, serverID uint, stackName string) (*models.ComposeStack, error) {
	if err := ValidateStackName(stackName); err != nil {
		return nil, err
	}

	stacks, err := s.ListComposeStacks(ctx, serverID)
	if err != nil {
		return nil, err
	}

	for _, st := range stacks {
		if strings.EqualFold(st.Name, stackName) {
			return &st, nil
		}
	}

	return nil, fmt.Errorf("compose stack '%s' not found", stackName)
}

// StartComposeStack starts all stopped containers in a Compose stack
func (s *DockerService) StartComposeStack(ctx context.Context, serverID uint, stackName string) error {
	if err := ValidateStackName(stackName); err != nil {
		return err
	}

	cli, err := s.GetClient(ctx, serverID)
	if err != nil {
		return err
	}

	containers, err := cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	found := false
	for _, c := range containers {
		if strings.EqualFold(c.Labels["com.docker.compose.project"], stackName) {
			found = true
			if !strings.EqualFold(c.State, "running") {
				if startErr := cli.ContainerStart(ctx, c.ID, container.StartOptions{}); startErr != nil {
					return fmt.Errorf("failed to start container %s: %w", c.ID[:12], startErr)
				}
			}
		}
	}

	if !found {
		return fmt.Errorf("compose stack '%s' not found", stackName)
	}

	return nil
}

// StopComposeStack stops all running containers in a Compose stack
func (s *DockerService) StopComposeStack(ctx context.Context, serverID uint, stackName string) error {
	if err := ValidateStackName(stackName); err != nil {
		return err
	}

	cli, err := s.GetClient(ctx, serverID)
	if err != nil {
		return err
	}

	containers, err := cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	found := false
	timeout := 10
	stopOpts := container.StopOptions{Timeout: &timeout}

	for _, c := range containers {
		if strings.EqualFold(c.Labels["com.docker.compose.project"], stackName) {
			found = true
			if strings.EqualFold(c.State, "running") {
				if stopErr := cli.ContainerStop(ctx, c.ID, stopOpts); stopErr != nil {
					return fmt.Errorf("failed to stop container %s: %w", c.ID[:12], stopErr)
				}
			}
		}
	}

	if !found {
		return fmt.Errorf("compose stack '%s' not found", stackName)
	}

	return nil
}

// RestartComposeStack restarts all containers in a Compose stack
func (s *DockerService) RestartComposeStack(ctx context.Context, serverID uint, stackName string) error {
	if err := ValidateStackName(stackName); err != nil {
		return err
	}

	cli, err := s.GetClient(ctx, serverID)
	if err != nil {
		return err
	}

	containers, err := cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	found := false
	timeout := 10
	restartOpts := container.StopOptions{Timeout: &timeout}

	for _, c := range containers {
		if strings.EqualFold(c.Labels["com.docker.compose.project"], stackName) {
			found = true
			if err := cli.ContainerRestart(ctx, c.ID, restartOpts); err != nil {
				return fmt.Errorf("failed to restart container %s: %w", c.ID[:12], err)
			}
		}
	}

	if !found {
		return fmt.Errorf("compose stack '%s' not found", stackName)
	}

	return nil
}

// RemoveComposeStack removes all containers in a Compose stack
func (s *DockerService) RemoveComposeStack(ctx context.Context, serverID uint, stackName string, removeVolumes bool) error {
	if err := ValidateStackName(stackName); err != nil {
		return err
	}

	cli, err := s.GetClient(ctx, serverID)
	if err != nil {
		return err
	}

	containers, err := cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	found := false
	rmOpts := container.RemoveOptions{
		RemoveVolumes: removeVolumes,
		Force:         true,
	}

	for _, c := range containers {
		if strings.EqualFold(c.Labels["com.docker.compose.project"], stackName) {
			found = true
			if rmErr := cli.ContainerRemove(ctx, c.ID, rmOpts); rmErr != nil {
				return fmt.Errorf("failed to remove container %s: %w", c.ID[:12], rmErr)
			}
		}
	}

	if !found {
		return fmt.Errorf("compose stack '%s' not found", stackName)
	}

	return nil
}

// DeployComposeStack launches a Docker Compose stack from YAML configuration
func (s *DockerService) DeployComposeStack(ctx context.Context, serverID uint, req models.DeployStackRequest) error {
	if err := ValidateStackName(req.Name); err != nil {
		return err
	}

	cleanYAML := strings.TrimSpace(req.ComposeYAML)
	if cleanYAML == "" {
		return errors.New("compose YAML content cannot be empty")
	}

	// Basic safety check on YAML content
	if len(cleanYAML) > 2<<20 { // 2MB limit
		return errors.New("compose file exceeds maximum permitted size of 2MB")
	}

	server, err := s.GetServer(serverID)
	if err != nil {
		return err
	}

	// If host is SSH, deploy through SSH session using docker compose CLI
	if IsSSHHost(server.HostURL) {
		sshClient, _, err := s.GetSSHClient(ctx, serverID)
		if err != nil {
			return fmt.Errorf("failed to connect via SSH: %w", err)
		}
		defer sshClient.Close()

		session, err := sshClient.NewSession()
		if err != nil {
			return fmt.Errorf("failed to create SSH session: %w", err)
		}
		defer session.Close()

		// Write compose file securely using base64 decoding on remote host
		remoteDir := fmt.Sprintf("/tmp/kraken-compose-%s", req.Name)
		encodedYAML := base64.StdEncoding.EncodeToString([]byte(cleanYAML))
		writeCmd := fmt.Sprintf("mkdir -p %s && echo '%s' | base64 -d > %s/docker-compose.yml && cd %s && docker compose -p %s up -d",
			remoteDir, encodedYAML, remoteDir, remoteDir, req.Name)

		output, err := session.CombinedOutput(writeCmd)
		if err != nil {
			return fmt.Errorf("docker compose deployment failed: %s %w", string(output), err)
		}
		return nil
	}

	// Local deployment: write to /tmp/kraken-stacks/<name>/docker-compose.yml and execute docker compose up -d
	tmpDir := filepath.Join(os.TempDir(), "kraken-stacks", req.Name)
	if err := os.MkdirAll(tmpDir, 0750); err != nil {
		return fmt.Errorf("failed to create temporary stack directory: %w", err)
	}

	composeFile := filepath.Join(tmpDir, "docker-compose.yml")
	if err := os.WriteFile(composeFile, []byte(cleanYAML), 0600); err != nil {
		return fmt.Errorf("failed to write compose file: %w", err)
	}

	// Run docker compose up -d
	cmdCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "docker", "compose", "-p", req.Name, "-f", composeFile, "up", "-d")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker compose up failed: %s %w", string(out), err)
	}

	return nil
}
