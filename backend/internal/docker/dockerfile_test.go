package docker

import (
	"context"
	"strings"
	"testing"

	"github.com/docker/docker/api/types"
	dockercontainer "github.com/docker/docker/api/types/container"
	dockerimage "github.com/docker/docker/api/types/image"
	"github.com/kraken/admin-tool/backend/internal/models"
)

func TestReconstructDockerfile_Layers(t *testing.T) {
	history := []dockerimage.HistoryResponseItem{
		{
			CreatedBy: "/bin/sh -c #(nop) CMD [\"npm\" \"start\"]",
		},
		{
			CreatedBy: "/bin/sh -c npm install --production",
		},
		{
			CreatedBy: "/bin/sh -c #(nop) WORKDIR /app",
		},
		{
			CreatedBy: "/bin/sh -c #(nop) ENV NODE_ENV=production PORT=3000",
		},
		{
			CreatedBy: "/bin/sh -c #(nop) ADD file:abc in /",
		},
	}

	inspect := types.ImageInspect{
		ID: "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
		Config: &dockercontainer.Config{
			Image:      "node:20-alpine",
			WorkingDir: "/app",
			Env:        []string{"NODE_ENV=production", "PORT=3000"},
			Cmd:        []string{"npm", "start"},
		},
		RepoTags: []string{"my-app:v1"},
	}

	res := reconstructFromHistoryAndInspect(history, inspect)
	if !strings.Contains(res, "WORKDIR /app") {
		t.Errorf("expected WORKDIR in reconstructed Dockerfile, got: %s", res)
	}
	if !strings.Contains(res, "RUN npm install --production") {
		t.Errorf("expected RUN instruction in reconstructed Dockerfile, got: %s", res)
	}
	if !strings.Contains(res, "CMD [\"npm\" \"start\"]") {
		t.Errorf("expected CMD instruction in reconstructed Dockerfile, got: %s", res)
	}
}

func TestBuildImage_Validation(t *testing.T) {
	svc := NewDockerService("", nil)
	ctx := context.Background()

	t.Run("empty tag rejected", func(t *testing.T) {
		_, err := svc.BuildImageStreamOnServer(ctx, 1, models.BuildImageRequest{
			Tag:        "",
			Dockerfile: "FROM alpine\nRUN echo hello",
		})
		if err == nil {
			t.Error("expected error for empty tag, got nil")
		}
	})

	t.Run("invalid tag characters rejected", func(t *testing.T) {
		_, err := svc.BuildImageStreamOnServer(ctx, 1, models.BuildImageRequest{
			Tag:        "my_tag; rm -rf /",
			Dockerfile: "FROM alpine",
		})
		if err == nil {
			t.Error("expected error for invalid tag characters, got nil")
		}
	})

	t.Run("empty dockerfile rejected", func(t *testing.T) {
		_, err := svc.BuildImageStreamOnServer(ctx, 1, models.BuildImageRequest{
			Tag:        "my-image:latest",
			Dockerfile: "   ",
		})
		if err == nil {
			t.Error("expected error for empty dockerfile, got nil")
		}
	})
}
