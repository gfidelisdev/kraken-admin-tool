package docker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"

	"github.com/kraken/admin-tool/backend/internal/models"
)

// UpdateImageResult captures the outcome of an image upgrade and container recreation
type UpdateImageResult struct {
	TargetTag           string   `json:"target_tag"`
	OldImageID          string   `json:"old_image_id"`
	NewImageID          string   `json:"new_image_id"`
	RecreatedContainers []string `json:"recreated_containers"`
	RolledBack          bool     `json:"rolled_back"`
	ErrorMessage        string   `json:"error_message,omitempty"`
}

// UpdateImageOnServer pulls the target image and optionally recreates associated containers with rollback safety
func (s *DockerService) UpdateImageOnServer(ctx context.Context, req models.UpdateImageRequest) (*UpdateImageResult, error) {
	serverID := req.ServerID
	targetTag := strings.TrimSpace(req.TargetTag)
	if targetTag == "" {
		return nil, errors.New("target image tag cannot be empty")
	}

	cli, err := s.GetClient(ctx, serverID)
	if err != nil {
		return nil, err
	}

	// 1. Pull target image
	if err := s.PullImageOnServer(ctx, serverID, targetTag); err != nil {
		return nil, fmt.Errorf("failed to pull image '%s': %w", targetTag, err)
	}

	// 2. Inspect new image to get its ID
	newInspect, _, err := cli.ImageInspectWithRaw(ctx, targetTag)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect newly pulled image '%s': %w", targetTag, err)
	}

	result := &UpdateImageResult{
		TargetTag:  targetTag,
		OldImageID: req.ImageID,
		NewImageID: newInspect.ID,
	}

	if !req.RecreateContainers {
		return result, nil
	}

	// 3. Find containers running or referencing the old image
	cleanOldID := strings.TrimPrefix(req.ImageID, "sha256:")
	containers, err := cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return result, fmt.Errorf("failed to list containers: %w", err)
	}

	var targetContainers []string
	for _, c := range containers {
		cImgID := strings.TrimPrefix(c.ImageID, "sha256:")
		if cImgID == cleanOldID || strings.HasPrefix(cImgID, cleanOldID) || strings.HasPrefix(cleanOldID, cImgID) {
			targetContainers = append(targetContainers, c.ID)
		}
	}

	// 4. Recreate each container with automatic rollback on failure
	for _, cID := range targetContainers {
		inspectData, err := cli.ContainerInspect(ctx, cID)
		if err != nil {
			continue
		}

		origName := strings.TrimPrefix(inspectData.Name, "/")
		wasRunning := inspectData.State != nil && inspectData.State.Running
		backupName := fmt.Sprintf("%s-kraken-backup-%d", origName, time.Now().Unix())

		// Stop if currently running
		if wasRunning {
			stopTimeout := 15
			_ = cli.ContainerStop(ctx, cID, container.StopOptions{Timeout: &stopTimeout})
		}

		// Rename old container to backup name
		if err := cli.ContainerRename(ctx, cID, backupName); err != nil {
			result.ErrorMessage = fmt.Sprintf("failed to rename container %s to backup: %v", origName, err)
			continue
		}

		// Clone config with updated image
		newCfg := *inspectData.Config
		newCfg.Image = targetTag

		var netCfg *network.NetworkingConfig
		if inspectData.NetworkSettings != nil && len(inspectData.NetworkSettings.Networks) > 0 {
			netCfg = &network.NetworkingConfig{
				EndpointsConfig: inspectData.NetworkSettings.Networks,
			}
		}

		created, createErr := cli.ContainerCreate(ctx, &newCfg, inspectData.HostConfig, netCfg, nil, origName)
		if createErr != nil {
			// Rollback: rename backup back to origName and restart if was running
			_ = cli.ContainerRename(ctx, cID, origName)
			if wasRunning {
				_ = cli.ContainerStart(ctx, cID, container.StartOptions{})
			}
			result.RolledBack = true
			result.ErrorMessage = fmt.Sprintf("recreation failed for %s (rolled back): %v", origName, createErr)
			return result, nil
		}

		if wasRunning {
			startErr := cli.ContainerStart(ctx, created.ID, container.StartOptions{})
			if startErr != nil {
				// Rollback: remove broken container, rename backup back and restart
				_ = cli.ContainerRemove(ctx, created.ID, container.RemoveOptions{Force: true})
				_ = cli.ContainerRename(ctx, cID, origName)
				_ = cli.ContainerStart(ctx, cID, container.StartOptions{})
				result.RolledBack = true
				result.ErrorMessage = fmt.Sprintf("start failed for %s (rolled back): %v", origName, startErr)
				return result, nil
			}
		}

		result.RecreatedContainers = append(result.RecreatedContainers, origName)

		// If success and remove old requested, cleanup the backup container
		if req.RemoveOldImage {
			_ = cli.ContainerRemove(ctx, cID, container.RemoveOptions{Force: true})
		}
	}

	// If remove old requested and all containers recreated successfully, attempt to remove old image
	if req.RemoveOldImage && !result.RolledBack && len(result.RecreatedContainers) > 0 {
		_, _ = cli.ImageRemove(ctx, req.ImageID, image.RemoveOptions{PruneChildren: true})
	}

	return result, nil
}
