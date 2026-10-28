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
	"gopkg.in/yaml.v3"
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

	// Validate YAML syntax
	var yamlCheck interface{}
	if err := yaml.Unmarshal([]byte(cleanYAML), &yamlCheck); err != nil {
		return fmt.Errorf("invalid YAML syntax: %w", err)
	}

	server, err := s.GetServer(serverID)
	if err != nil {
		return err
	}

	upFlags := "up -d --remove-orphans"
	if req.ForceRecreate {
		upFlags = "up -d --force-recreate --remove-orphans"
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
		writeCmd := fmt.Sprintf("mkdir -p %s && echo '%s' | base64 -d > %s/docker-compose.yml && cd %s && docker compose -p %s %s",
			remoteDir, encodedYAML, remoteDir, remoteDir, req.Name, upFlags)

		output, err := session.CombinedOutput(writeCmd)
		if err != nil {
			return fmt.Errorf("docker compose deployment failed: %s %w", string(output), err)
		}

		s.persistStackConfig(serverID, req.Name, cleanYAML, remoteDir, remoteDir+"/docker-compose.yml")
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

	cmdArgs := []string{"compose", "-p", req.Name, "-f", composeFile, "up", "-d", "--remove-orphans"}
	if req.ForceRecreate {
		cmdArgs = []string{"compose", "-p", req.Name, "-f", composeFile, "up", "-d", "--force-recreate", "--remove-orphans"}
	}

	cmd := exec.CommandContext(cmdCtx, "docker", cmdArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker compose up failed: %s %w", string(out), err)
	}

	s.persistStackConfig(serverID, req.Name, cleanYAML, tmpDir, composeFile)
	return nil
}

// persistStackConfig saves or updates ComposeStackConfig in DB
func (s *DockerService) persistStackConfig(serverID uint, stackName, composeYAML, workingDir, configFile string) {
	if s.db == nil {
		return
	}
	var existing models.ComposeStackConfig
	err := s.db.Where("server_id = ? AND stack_name = ?", serverID, stackName).First(&existing).Error
	if err == nil {
		s.db.Model(&existing).Updates(map[string]interface{}{
			"compose_yaml": composeYAML,
			"working_dir":  workingDir,
			"config_file":  configFile,
			"updated_at":   time.Now(),
		})
	} else {
		newCfg := models.ComposeStackConfig{
			ServerID:    serverID,
			StackName:   stackName,
			ComposeYAML: composeYAML,
			WorkingDir:  workingDir,
			ConfigFile:  configFile,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}
		s.db.Create(&newCfg)
	}
}

// GetComposeStackYAML retrieves the docker-compose.yaml for a stack from DB, host filesystem, or synthesizes it
func (s *DockerService) GetComposeStackYAML(ctx context.Context, serverID uint, stackName string) (*models.ComposeStackYAMLResponse, error) {
	if err := ValidateStackName(stackName); err != nil {
		return nil, err
	}

	stack, err := s.GetComposeStack(ctx, serverID, stackName)
	if err != nil {
		return nil, err
	}

	// 1. Check database first (user customized override)
	if s.db != nil {
		var cfg models.ComposeStackConfig
		if err := s.db.Where("server_id = ? AND stack_name = ?", serverID, stackName).First(&cfg).Error; err == nil {
			if strings.TrimSpace(cfg.ComposeYAML) != "" {
				return &models.ComposeStackYAMLResponse{
					Name:        stackName,
					ComposeYAML: cfg.ComposeYAML,
					Source:      "database",
					ConfigFile:  cfg.ConfigFile,
					WorkingDir:  cfg.WorkingDir,
					IsReadOnly:  false,
				}, nil
			}
		}

		// 2. Check source_links table for reverse-engineered source files on host
		var link models.SourceLink
		if err := s.db.Where("server_id = ? AND kind = 'stack' AND target_ref = ? AND status != 'user_rejected'", serverID, stackName).
			Order("match_score desc, updated_at desc").First(&link).Error; err == nil {
			content, readErr := s.ReadFileFromHost(ctx, serverID, link.FilePath)
			if readErr == nil && strings.TrimSpace(content) != "" {
				return &models.ComposeStackYAMLResponse{
					Name:        stackName,
					ComposeYAML: content,
					Source:      "source_file",
					ConfigFile:  link.FilePath,
					FilePath:    link.FilePath,
					WorkingDir:  stack.WorkingDir,
					IsReadOnly:  true,
					MatchScore:  link.MatchScore,
				}, nil
			}
		}
	}

	server, serverErr := s.GetServer(serverID)
	firstConfigFile := ""
	if stack.ConfigFiles != "" {
		parts := strings.Split(stack.ConfigFiles, ",")
		if len(parts) > 0 {
			firstConfigFile = strings.TrimSpace(parts[0])
		}
	}

	// 3. Check conventional host filesystem paths (local file or SSH)
	if serverErr == nil && server != nil {
		if IsSSHHost(server.HostURL) && firstConfigFile != "" {
			sshClient, _, sshErr := s.GetSSHClient(ctx, serverID)
			if sshErr == nil {
				defer sshClient.Close()
				session, sessErr := sshClient.NewSession()
				if sessErr == nil {
					defer session.Close()
					catCmd := fmt.Sprintf("cat %s", firstConfigFile)
					content, catErr := session.Output(catCmd)
					if catErr == nil && len(strings.TrimSpace(string(content))) > 0 {
						cleanContent := string(content)
						s.persistStackConfig(serverID, stackName, cleanContent, stack.WorkingDir, firstConfigFile)
						return &models.ComposeStackYAMLResponse{
							Name:        stackName,
							ComposeYAML: cleanContent,
							Source:      "host_file",
							ConfigFile:  firstConfigFile,
							WorkingDir:  stack.WorkingDir,
						}, nil
					}
				}
			}
		} else if !IsSSHHost(server.HostURL) {
			// Local host check
			candidates := []string{}
			if firstConfigFile != "" {
				candidates = append(candidates, firstConfigFile)
			}
			if stack.WorkingDir != "" {
				candidates = append(candidates, filepath.Join(stack.WorkingDir, "docker-compose.yml"))
				candidates = append(candidates, filepath.Join(stack.WorkingDir, "docker-compose.yaml"))
				candidates = append(candidates, filepath.Join(stack.WorkingDir, "compose.yml"))
				candidates = append(candidates, filepath.Join(stack.WorkingDir, "compose.yaml"))
			}
			candidates = append(candidates, filepath.Join(os.TempDir(), "kraken-stacks", stackName, "docker-compose.yml"))

			for _, candidate := range candidates {
				if data, readErr := os.ReadFile(candidate); readErr == nil && len(strings.TrimSpace(string(data))) > 0 {
					cleanContent := string(data)
					s.persistStackConfig(serverID, stackName, cleanContent, stack.WorkingDir, candidate)
					return &models.ComposeStackYAMLResponse{
						Name:        stackName,
						ComposeYAML: cleanContent,
						Source:      "host_file",
						ConfigFile:  candidate,
						WorkingDir:  stack.WorkingDir,
					}, nil
				}
			}
		}
	}

	// 3. Fallback: synthesize compose YAML from running containers
	reconstructedYAML, err := s.SynthesizeComposeYAML(ctx, serverID, stackName)
	if err != nil {
		return nil, fmt.Errorf("failed to reconstruct compose YAML: %w", err)
	}

	return &models.ComposeStackYAMLResponse{
		Name:        stackName,
		ComposeYAML: reconstructedYAML,
		Source:      "reconstructed",
		ConfigFile:  firstConfigFile,
		WorkingDir:  stack.WorkingDir,
	}, nil
}

// UpdateComposeStackYAML updates the stack YAML configuration and optionally redeploys
func (s *DockerService) UpdateComposeStackYAML(ctx context.Context, serverID uint, stackName string, req models.UpdateStackYAMLRequest) error {
	if err := ValidateStackName(stackName); err != nil {
		return err
	}

	cleanYAML := strings.TrimSpace(req.ComposeYAML)
	if cleanYAML == "" {
		return errors.New("compose YAML content cannot be empty")
	}

	var yamlCheck interface{}
	if err := yaml.Unmarshal([]byte(cleanYAML), &yamlCheck); err != nil {
		return fmt.Errorf("invalid YAML syntax: %w", err)
	}

	// Update DB record
	stack, _ := s.GetComposeStack(ctx, serverID, stackName)
	workingDir := ""
	configFile := ""
	if stack != nil {
		workingDir = stack.WorkingDir
		configFile = stack.ConfigFiles
	}
	s.persistStackConfig(serverID, stackName, cleanYAML, workingDir, configFile)

	// If redeploy requested, run deploy workflow
	if req.Redeploy {
		deployReq := models.DeployStackRequest{
			Name:          stackName,
			ComposeYAML:   cleanYAML,
			ForceRecreate: req.ForceRecreate,
		}
		if err := s.DeployComposeStack(ctx, serverID, deployReq); err != nil {
			return fmt.Errorf("saved configuration, but redeploy failed: %w", err)
		}
	}

	return nil
}

// SynthesizeComposeYAML builds a docker-compose.yaml equivalent by inspecting containers in the stack
func (s *DockerService) SynthesizeComposeYAML(ctx context.Context, serverID uint, stackName string) (string, error) {
	cli, err := s.GetClient(ctx, serverID)
	if err != nil {
		return "", err
	}

	containers, err := cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return "", fmt.Errorf("failed to list containers: %w", err)
	}

	services := make(map[string]interface{})

	for _, c := range containers {
		if !strings.EqualFold(c.Labels["com.docker.compose.project"], stackName) {
			continue
		}

		serviceName := strings.TrimSpace(c.Labels["com.docker.compose.service"])
		if serviceName == "" {
			serviceName = "service-" + c.ID[:8]
		}

		if _, exists := services[serviceName]; exists {
			continue // Already processed a replica
		}

		inspect, inspectErr := cli.ContainerInspect(ctx, c.ID)
		if inspectErr != nil {
			// Fallback to basic summary
			services[serviceName] = map[string]interface{}{
				"image": c.Image,
			}
			continue
		}

		svcMap := make(map[string]interface{})
		svcMap["image"] = inspect.Config.Image

		// Restart policy
		if inspect.HostConfig != nil && inspect.HostConfig.RestartPolicy.Name != "" {
			svcMap["restart"] = inspect.HostConfig.RestartPolicy.Name
		}

		// Environment variables (exclude standard container envs)
		var envList []string
		for _, env := range inspect.Config.Env {
			if strings.HasPrefix(env, "PATH=") || strings.HasPrefix(env, "HOSTNAME=") || strings.HasPrefix(env, "HOME=") {
				continue
			}
			envList = append(envList, env)
		}
		if len(envList) > 0 {
			sort.Strings(envList)
			svcMap["environment"] = envList
		}

		// Ports
		if inspect.HostConfig != nil && inspect.HostConfig.PortBindings != nil {
			var portList []string
			for portProto, bindings := range inspect.HostConfig.PortBindings {
				for _, b := range bindings {
					hostPort := b.HostPort
					containerPort := string(portProto)
					if b.HostIP != "" && b.HostIP != "0.0.0.0" {
						portList = append(portList, fmt.Sprintf("%s:%s:%s", b.HostIP, hostPort, containerPort))
					} else {
						portList = append(portList, fmt.Sprintf("%s:%s", hostPort, containerPort))
					}
				}
			}
			if len(portList) > 0 {
				sort.Strings(portList)
				svcMap["ports"] = portList
			}
		}

		// Volumes / Binds
		if inspect.HostConfig != nil && len(inspect.HostConfig.Binds) > 0 {
			var volList []string
			for _, b := range inspect.HostConfig.Binds {
				volList = append(volList, b)
			}
			sort.Strings(volList)
			svcMap["volumes"] = volList
		}

		// Command & Entrypoint
		if len(inspect.Config.Cmd) > 0 {
			svcMap["command"] = strings.Join(inspect.Config.Cmd, " ")
		}
		if len(inspect.Config.Entrypoint) > 0 {
			svcMap["entrypoint"] = strings.Join(inspect.Config.Entrypoint, " ")
		}

		services[serviceName] = svcMap
	}

	if len(services) == 0 {
		return "", fmt.Errorf("no containers found for compose stack '%s'", stackName)
	}

	composeDoc := map[string]interface{}{
		"version":  "3.8",
		"services": services,
	}

	out, err := yaml.Marshal(composeDoc)
	if err != nil {
		return "", err
	}

	header := fmt.Sprintf("# Generated by Kraken Admin Tool from running containers of stack '%s'\n# You can edit, add services, and click 'Save & Redeploy' to apply.\n\n", stackName)
	return header + string(out), nil
}
