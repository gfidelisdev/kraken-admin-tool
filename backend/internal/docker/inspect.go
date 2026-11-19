package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
)

// InspectContainerOnServer returns the raw `docker inspect` JSON document of a container
func (s *DockerService) InspectContainerOnServer(ctx context.Context, serverID uint, containerID string) (json.RawMessage, error) {
	containerID = strings.TrimSpace(containerID)
	if containerID == "" || len(containerID) > 128 {
		return nil, errors.New("invalid container id")
	}
	cli, err := s.GetClient(ctx, serverID)
	if err != nil {
		return nil, err
	}
	_, raw, err := cli.ContainerInspectWithRaw(ctx, containerID, true)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(raw), nil
}

// ComposeStackInspect aggregates the low-level docker objects that make up a compose project
type ComposeStackInspect struct {
	Project    string            `json:"project"`
	Containers []json.RawMessage `json:"containers"`
	Networks   []json.RawMessage `json:"networks"`
	Volumes    []*volume.Volume  `json:"volumes"`
}

// InspectComposeStackOnServer inspects every container, network and volume labelled with the compose project
func (s *DockerService) InspectComposeStackOnServer(ctx context.Context, serverID uint, stackName string) (*ComposeStackInspect, error) {
	if !safeStackNameRegex.MatchString(stackName) {
		return nil, fmt.Errorf("invalid stack name: %q", stackName)
	}
	cli, err := s.GetClient(ctx, serverID)
	if err != nil {
		return nil, err
	}

	label := filters.NewArgs(filters.Arg("label", "com.docker.compose.project="+stackName))
	result := &ComposeStackInspect{
		Project:    stackName,
		Containers: []json.RawMessage{},
		Networks:   []json.RawMessage{},
		Volumes:    []*volume.Volume{},
	}

	containers, err := cli.ContainerList(ctx, container.ListOptions{All: true, Filters: label})
	if err != nil {
		return nil, err
	}
	if len(containers) == 0 {
		return nil, fmt.Errorf("no containers found for stack %s", stackName)
	}
	for _, c := range containers {
		_, raw, err := cli.ContainerInspectWithRaw(ctx, c.ID, false)
		if err != nil {
			continue
		}
		result.Containers = append(result.Containers, json.RawMessage(raw))
	}

	if networks, err := cli.NetworkList(ctx, network.ListOptions{Filters: label}); err == nil {
		for _, n := range networks {
			if _, raw, err := cli.NetworkInspectWithRaw(ctx, n.ID, network.InspectOptions{}); err == nil {
				result.Networks = append(result.Networks, json.RawMessage(raw))
			}
		}
	}

	if vols, err := cli.VolumeList(ctx, volume.ListOptions{Filters: label}); err == nil {
		result.Volumes = append(result.Volumes, vols.Volumes...)
	}

	return result, nil
}

// TerminalSize is a terminal window size in character cells
type TerminalSize struct {
	Cols uint16
	Rows uint16
}

// TerminalStreams connects an interactive TTY process to a client (e.g. a WebSocket)
type TerminalStreams struct {
	Stdin  io.Reader
	Stdout io.Writer
	Resize <-chan TerminalSize
}

// AutoShellCommand starts bash when available, falling back to POSIX sh
var AutoShellCommand = []string{"/bin/sh", "-c", "if command -v bash >/dev/null 2>&1; then exec bash; else exec sh; fi"}

// ResolveShellCommand maps a user-selected shell to an exec command, rejecting anything outside the allow-list
func ResolveShellCommand(shell string) ([]string, error) {
	switch strings.TrimSpace(shell) {
	case "", "auto":
		return AutoShellCommand, nil
	case "sh", "/bin/sh":
		return []string{"/bin/sh"}, nil
	case "bash", "/bin/bash":
		return []string{"/bin/bash"}, nil
	case "ash", "/bin/ash":
		return []string{"/bin/ash"}, nil
	case "zsh", "/bin/zsh":
		return []string{"/bin/zsh"}, nil
	default:
		return nil, fmt.Errorf("unsupported shell %q", shell)
	}
}

// ExecTTYOnServer runs an interactive TTY command inside a container until it exits or ctx is cancelled.
// It returns the process exit code.
func (s *DockerService) ExecTTYOnServer(ctx context.Context, serverID uint, containerID string, cmd []string, streams TerminalStreams) (int, error) {
	containerID = strings.TrimSpace(containerID)
	if containerID == "" || len(containerID) > 128 {
		return -1, errors.New("invalid container id")
	}
	cli, err := s.GetClient(ctx, serverID)
	if err != nil {
		return -1, err
	}

	created, err := cli.ContainerExecCreate(ctx, containerID, container.ExecOptions{
		Tty:          true,
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		Env:          []string{"TERM=xterm-256color"},
		Cmd:          cmd,
	})
	if err != nil {
		return -1, err
	}

	attach, err := cli.ContainerExecAttach(ctx, created.ID, container.ExecAttachOptions{Tty: true})
	if err != nil {
		return -1, err
	}
	defer attach.Close()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case size, ok := <-streams.Resize:
				if !ok {
					return
				}
				_ = cli.ContainerExecResize(ctx, created.ID, container.ResizeOptions{Height: uint(size.Rows), Width: uint(size.Cols)})
			}
		}
	}()

	go func() {
		_, _ = io.Copy(attach.Conn, streams.Stdin)
		_ = attach.CloseWrite()
	}()

	outDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(streams.Stdout, attach.Reader)
		close(outDone)
	}()

	select {
	case <-ctx.Done():
		return -1, ctx.Err()
	case <-outDone:
	}

	inspectCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	info, err := cli.ContainerExecInspect(inspectCtx, created.ID)
	if err != nil {
		return -1, nil
	}
	return info.ExitCode, nil
}
