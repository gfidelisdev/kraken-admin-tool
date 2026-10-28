package docker

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	dockerimage "github.com/docker/docker/api/types/image"
	"github.com/kraken/admin-tool/backend/internal/models"
)

// GetImageDockerfile retrieves the Dockerfile for an image or reconstructs it
func (s *DockerService) GetImageDockerfile(ctx context.Context, serverID uint, imageIDOrTag string) (*models.ImageDockerfileResponse, error) {
	imageRef := strings.TrimSpace(imageIDOrTag)
	if imageRef == "" {
		return nil, errors.New("image identifier is required")
	}

	cli, err := s.GetClient(ctx, serverID)
	if err != nil {
		return nil, err
	}

	inspect, _, err := cli.ImageInspectWithRaw(ctx, imageRef)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect image '%s': %w", imageRef, err)
	}

	cleanImageID := strings.TrimPrefix(inspect.ID, "sha256:")
	tags := inspect.RepoTags
	if len(tags) == 0 {
		tags = []string{imageRef}
	}

	// 1. Check database for previously saved/built Dockerfile
	if s.db != nil {
		var cfg models.ImageDockerfileConfig
		query := s.db.Where("server_id = ? AND (image_id = ? OR image_id = ?)", serverID, inspect.ID, cleanImageID)
		for _, tag := range tags {
			query = query.Or("server_id = ? AND image_tag = ?", serverID, tag)
		}
		if err := query.Order("updated_at desc").First(&cfg).Error; err == nil && strings.TrimSpace(cfg.Dockerfile) != "" {
			resp := buildResponseFromInspect(inspect, cfg.Dockerfile, "database")
			resp.ContextDir = cfg.ContextDir
			resp.IsReadOnly = false
			return resp, nil
		}

		// 2. Check source_links table for reverse-engineered source files on host
		var link models.SourceLink
		linkQuery := s.db.Where("server_id = ? AND kind = 'image' AND status != 'user_rejected' AND (target_ref = ? OR target_ref = ?)", serverID, inspect.ID, cleanImageID)
		for _, tag := range tags {
			linkQuery = linkQuery.Or("server_id = ? AND kind = 'image' AND status != 'user_rejected' AND target_ref = ?", serverID, tag)
		}
		if err := linkQuery.Order("match_score desc, updated_at desc").First(&link).Error; err == nil {
			content, readErr := s.ReadFileFromHost(ctx, serverID, link.FilePath)
			if readErr == nil && strings.TrimSpace(content) != "" {
				resp := buildResponseFromInspect(inspect, content, "source_file")
				resp.FilePath = link.FilePath
				resp.MatchScore = link.MatchScore
				resp.IsReadOnly = true
				resp.ContextDir = filepath.Dir(link.FilePath)
				return resp, nil
			}
		}
	}

	// 3. Check host filesystem if image labels point to build context
	server, _ := s.GetServer(serverID)
	if server != nil && !IsSSHHost(server.HostURL) {
		candidates := []string{}
		if dir := inspect.Config.Labels["com.docker.compose.project.working_dir"]; dir != "" {
			candidates = append(candidates, filepath.Join(dir, "Dockerfile"), filepath.Join(dir, "dockerfile"))
		}
		for _, cand := range candidates {
			if data, err := os.ReadFile(cand); err == nil && len(strings.TrimSpace(string(data))) > 0 {
				content := string(data)
				s.persistDockerfileConfig(serverID, tags[0], inspect.ID, content, filepath.Dir(cand))
				resp := buildResponseFromInspect(inspect, content, "host_file")
				resp.FilePath = cand
				resp.IsReadOnly = true
				return resp, nil
			}
		}
	}

	// 4. Reconstruct Dockerfile from history layers and inspect config
	history, err := cli.ImageHistory(ctx, inspect.ID)
	if err != nil {
		// Fallback to basic reconstruction from inspect config only
		reconstructed := reconstructFromInspectOnly(inspect)
		return buildResponseFromInspect(inspect, reconstructed, "reconstructed"), nil
	}

	reconstructed := reconstructFromHistoryAndInspect(history, inspect)
	return buildResponseFromInspect(inspect, reconstructed, "reconstructed"), nil
}

// buildResponseFromInspect populates standard metadata and generates quick extend dockerfile
func buildResponseFromInspect(inspect types.ImageInspect, dockerfile string, source string) *models.ImageDockerfileResponse {
	var envs []string
	if inspect.Config != nil {
		for _, e := range inspect.Config.Env {
			if !strings.HasPrefix(e, "PATH=") && !strings.HasPrefix(e, "HOSTNAME=") {
				envs = append(envs, e)
			}
		}
	}

	var exposed []string
	if inspect.Config != nil && inspect.Config.ExposedPorts != nil {
		for p := range inspect.Config.ExposedPorts {
			exposed = append(exposed, string(p))
		}
		sort.Strings(exposed)
	}

	workingDir := ""
	var entrypoint []string
	var cmd []string
	if inspect.Config != nil {
		workingDir = inspect.Config.WorkingDir
		entrypoint = inspect.Config.Entrypoint
		cmd = inspect.Config.Cmd
	}

	primaryTag := "<image-id>"
	if len(inspect.RepoTags) > 0 && inspect.RepoTags[0] != "<none>:<none>" {
		primaryTag = inspect.RepoTags[0]
	} else if len(inspect.ID) > 12 {
		primaryTag = inspect.ID[:12]
	}

	// Generate Quick Extend Dockerfile
	var extendBuilder strings.Builder
	extendBuilder.WriteString(fmt.Sprintf("# Quick Override Dockerfile for %s\n", primaryTag))
	extendBuilder.WriteString(fmt.Sprintf("# Built on top of the existing image without needing original source files.\n\n"))
	extendBuilder.WriteString(fmt.Sprintf("FROM %s\n\n", primaryTag))

	if workingDir != "" {
		extendBuilder.WriteString(fmt.Sprintf("WORKDIR %s\n", workingDir))
	}
	for _, env := range envs {
		extendBuilder.WriteString(fmt.Sprintf("ENV %s\n", env))
	}
	for _, p := range exposed {
		extendBuilder.WriteString(fmt.Sprintf("EXPOSE %s\n", p))
	}
	if len(entrypoint) > 0 {
		var quoted []string
		for _, ep := range entrypoint {
			quoted = append(quoted, fmt.Sprintf("%q", ep))
		}
		extendBuilder.WriteString(fmt.Sprintf("ENTRYPOINT [%s]\n", strings.Join(quoted, ", ")))
	}
	if len(cmd) > 0 {
		var quoted []string
		for _, c := range cmd {
			quoted = append(quoted, fmt.Sprintf("%q", c))
		}
		extendBuilder.WriteString(fmt.Sprintf("CMD [%s]\n", strings.Join(quoted, ", ")))
	}

	return &models.ImageDockerfileResponse{
		ImageID:          inspect.ID,
		RepoTags:         inspect.RepoTags,
		Dockerfile:       dockerfile,
		ExtendDockerfile: extendBuilder.String(),
		Source:           source,
		Env:              envs,
		WorkingDir:       workingDir,
		ExposedPorts:     exposed,
		Entrypoint:       entrypoint,
		Cmd:              cmd,
	}
}

// reconstructFromHistoryAndInspect attempts reverse engineering the Dockerfile steps
func reconstructFromHistoryAndInspect(history []dockerimage.HistoryResponseItem, inspect types.ImageInspect) string {
	var lines []string
	baseImageDetermined := false

	// Image history is delivered latest-layer-first; reverse it to read bottom-up
	var reversed []dockerimage.HistoryResponseItem
	for i := len(history) - 1; i >= 0; i-- {
		reversed = append(reversed, history[i])
	}

	for _, h := range reversed {
		cmdStr := strings.TrimSpace(h.CreatedBy)
		if cmdStr == "" {
			continue
		}

		// Filter out standard nop directives
		if strings.HasPrefix(cmdStr, "/bin/sh -c #(nop)") {
			instruction := strings.TrimSpace(strings.TrimPrefix(cmdStr, "/bin/sh -c #(nop)"))
			if instruction != "" {
				lines = append(lines, instruction)
			}
			continue
		}

		if strings.HasPrefix(cmdStr, "/bin/sh -c ") {
			runCmd := strings.TrimSpace(strings.TrimPrefix(cmdStr, "/bin/sh -c "))
			if runCmd != "" {
				lines = append(lines, "RUN "+runCmd)
			}
			continue
		}

		if strings.HasPrefix(cmdStr, "#(nop) ") {
			instruction := strings.TrimSpace(strings.TrimPrefix(cmdStr, "#(nop) "))
			if instruction != "" {
				lines = append(lines, instruction)
			}
			continue
		}

		// Unrecognized format
		lines = append(lines, "# "+cmdStr)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Reconstructed Dockerfile from image layers (%s)\n", inspect.ID[:12]))
	sb.WriteString("# Note: Files added via COPY/ADD are embedded in image layers.\n# You can modify environment, commands, or append instructions below.\n\n")

	// Determine base image if known or default to scratch/base
	for _, l := range lines {
		if strings.HasPrefix(strings.ToUpper(l), "FROM ") {
			baseImageDetermined = true
			break
		}
	}
	if !baseImageDetermined {
		sb.WriteString("FROM scratch\n\n")
	}

	for _, l := range lines {
		sb.WriteString(l)
		sb.WriteString("\n")
	}

	return sb.String()
}

func reconstructFromInspectOnly(inspect types.ImageInspect) string {
	var sb strings.Builder
	sb.WriteString("# Reconstructed Dockerfile from image configuration\n\n")
	sb.WriteString("FROM scratch\n\n")

	if inspect.Config != nil {
		if inspect.Config.WorkingDir != "" {
			sb.WriteString(fmt.Sprintf("WORKDIR %s\n", inspect.Config.WorkingDir))
		}
		for _, e := range inspect.Config.Env {
			sb.WriteString(fmt.Sprintf("ENV %s\n", e))
		}
		for p := range inspect.Config.ExposedPorts {
			sb.WriteString(fmt.Sprintf("EXPOSE %s\n", p))
		}
		if len(inspect.Config.Entrypoint) > 0 {
			var quoted []string
			for _, ep := range inspect.Config.Entrypoint {
				quoted = append(quoted, fmt.Sprintf("%q", ep))
			}
			sb.WriteString(fmt.Sprintf("ENTRYPOINT [%s]\n", strings.Join(quoted, ", ")))
		}
		if len(inspect.Config.Cmd) > 0 {
			var quoted []string
			for _, c := range inspect.Config.Cmd {
				quoted = append(quoted, fmt.Sprintf("%q", c))
			}
			sb.WriteString(fmt.Sprintf("CMD [%s]\n", strings.Join(quoted, ", ")))
		}
	}

	return sb.String()
}

// persistDockerfileConfig saves Dockerfile content to DB
func (s *DockerService) persistDockerfileConfig(serverID uint, tag, imageID, dockerfile, contextDir string) {
	if s.db == nil {
		return
	}
	var existing models.ImageDockerfileConfig
	err := s.db.Where("server_id = ? AND image_tag = ?", serverID, tag).First(&existing).Error
	if err == nil {
		s.db.Model(&existing).Updates(map[string]interface{}{
			"dockerfile":  dockerfile,
			"image_id":    imageID,
			"context_dir": contextDir,
			"updated_at":  time.Now(),
		})
	} else {
		newCfg := models.ImageDockerfileConfig{
			ServerID:   serverID,
			ImageTag:   tag,
			ImageID:    imageID,
			Dockerfile: dockerfile,
			ContextDir: contextDir,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		}
		s.db.Create(&newCfg)
	}
}

// SaveImageDockerfile stores or updates the Dockerfile for an image in the database
func (s *DockerService) SaveImageDockerfile(ctx context.Context, serverID uint, tag, imageID, dockerfile string) error {
	cleanTag := strings.TrimSpace(tag)
	if cleanTag == "" {
		return errors.New("image tag cannot be empty")
	}
	cleanDockerfile := strings.TrimSpace(dockerfile)
	if cleanDockerfile == "" {
		return errors.New("dockerfile content cannot be empty")
	}

	s.persistDockerfileConfig(serverID, cleanTag, imageID, cleanDockerfile, "")
	return nil
}

// ReadFileFromHost safely reads up to 512KB of a file from a target host in read-only mode
func (s *DockerService) ReadFileFromHost(ctx context.Context, serverID uint, path string) (string, error) {
	cleanPath := filepath.Clean(strings.TrimSpace(path))
	if cleanPath == "" {
		return "", errors.New("path cannot be empty")
	}

	server, err := s.GetServer(serverID)
	if err != nil {
		return "", err
	}

	if IsSSHHost(server.HostURL) {
		client, _, err := s.GetSSHClient(ctx, serverID)
		if err != nil {
			return "", err
		}
		session, err := client.NewSession()
		if err != nil {
			return "", err
		}
		defer session.Close()

		quoted := "'" + strings.ReplaceAll(cleanPath, "'", "'\\''") + "'"
		out, err := session.Output("head -c 524288 " + quoted + " 2>/dev/null")
		if err != nil {
			return "", err
		}
		return string(out), nil
	}

	// Local host read (resolving against /host mount if running containerized)
	actualPath := cleanPath
	if _, err := os.Stat("/.dockerenv"); err == nil {
		if fi, err := os.Stat("/host"); err == nil && fi.IsDir() {
			if !strings.HasPrefix(cleanPath, "/host") {
				actualPath = filepath.Clean("/host/" + strings.TrimPrefix(cleanPath, "/"))
			}
		}
	}

	data, err := os.ReadFile(actualPath)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// BuildImageStreamOnServer builds a Docker image from a Dockerfile string and streams build events
func (s *DockerService) BuildImageStreamOnServer(ctx context.Context, serverID uint, req models.BuildImageRequest) (io.ReadCloser, error) {
	tag := strings.TrimSpace(req.Tag)
	if err := validateImageRef(tag); err != nil {
		return nil, fmt.Errorf("invalid image tag: %w", err)
	}

	dockerfile := strings.TrimSpace(req.Dockerfile)
	if dockerfile == "" {
		return nil, errors.New("dockerfile content cannot be empty")
	}
	if len(dockerfile) > 2<<20 {
		return nil, errors.New("dockerfile exceeds 2MB size limit")
	}

	cli, err := s.GetClient(ctx, serverID)
	if err != nil {
		return nil, err
	}

	// Pack Dockerfile into in-memory tar archive
	tarBuffer := new(bytes.Buffer)
	tarWriter := tar.NewWriter(tarBuffer)

	dfBytes := []byte(dockerfile)
	header := &tar.Header{
		Name:     "Dockerfile",
		Mode:     0644,
		Size:     int64(len(dfBytes)),
		ModTime:  time.Now(),
		Typeflag: tar.TypeReg,
	}

	if err := tarWriter.WriteHeader(header); err != nil {
		return nil, fmt.Errorf("failed to write tar header: %w", err)
	}
	if _, err := tarWriter.Write(dfBytes); err != nil {
		return nil, fmt.Errorf("failed to write Dockerfile to tar: %w", err)
	}
	if err := tarWriter.Close(); err != nil {
		return nil, fmt.Errorf("failed to close tar archive: %w", err)
	}

	buildArgs := make(map[string]*string)
	for k, v := range req.BuildArgs {
		val := v
		buildArgs[k] = &val
	}

	opts := types.ImageBuildOptions{
		Tags:        []string{tag},
		Dockerfile:  "Dockerfile",
		NoCache:     req.NoCache,
		PullParent:  req.PullParent,
		Remove:      true,
		ForceRemove: true,
		BuildArgs:   buildArgs,
	}

	resp, err := cli.ImageBuild(ctx, tarBuffer, opts)
	if err != nil {
		return nil, fmt.Errorf("docker image build failed to initiate: %w", err)
	}

	// Save Dockerfile configuration for this tag in DB
	s.persistDockerfileConfig(serverID, tag, "", dockerfile, "")

	return resp.Body, nil
}
