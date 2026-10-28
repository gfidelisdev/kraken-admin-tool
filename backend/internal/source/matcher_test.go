package source

import (
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	dockerimage "github.com/docker/docker/api/types/image"
	"github.com/docker/go-connections/nat"

	"github.com/kraken/admin-tool/backend/internal/models"
)

func TestParseDockerfile(t *testing.T) {
	content := `
FROM node:20-alpine AS builder
WORKDIR /app
COPY package*.json ./
RUN npm install \
    && npm cache clean --force
ENV NODE_ENV=production \
    PORT=3000
EXPOSE 3000/tcp 8080
ENTRYPOINT ["node", "server.js"]
CMD ["--verbose"]
LABEL version="1.0.0"
`
	parsed := ParseDockerfile(content)

	if len(parsed.BaseImages) == 0 || parsed.BaseImages[0] != "node:20-alpine" {
		t.Errorf("expected base image node:20-alpine, got %v", parsed.BaseImages)
	}
	if parsed.Envs["NODE_ENV"] != "production" || parsed.Envs["PORT"] != "3000" {
		t.Errorf("unexpected envs: %v", parsed.Envs)
	}
	if len(parsed.ExposedPorts) < 2 {
		t.Errorf("expected 2 exposed ports, got %v", parsed.ExposedPorts)
	}
	if len(parsed.Workdirs) == 0 || parsed.Workdirs[0] != "/app" {
		t.Errorf("unexpected workdir: %v", parsed.Workdirs)
	}
	if len(parsed.Entrypoints) != 2 || parsed.Entrypoints[0] != "node" {
		t.Errorf("unexpected entrypoint: %v", parsed.Entrypoints)
	}
	if len(parsed.Cmds) != 1 || parsed.Cmds[0] != "--verbose" {
		t.Errorf("unexpected cmd: %v", parsed.Cmds)
	}
	if parsed.Labels["version"] != "1.0.0" {
		t.Errorf("unexpected labels: %v", parsed.Labels)
	}
}

func TestMatchImageWithCandidate(t *testing.T) {
	dfContent := `
FROM redis:7.0-alpine
WORKDIR /data
ENV MAX_MEMORY=256mb
EXPOSE 6379
CMD ["redis-server"]
`
	candidate := CandidateFile{
		Path:    "/opt/redis/Dockerfile",
		Type:    CandidateDockerfile,
		Content: dfContent,
	}

	inspect := types.ImageInspect{
		ID:       "sha256:1234567890abcdef",
		RepoTags: []string{"redis:7.0-alpine"},
		Config: &container.Config{
			WorkingDir: "/data",
			Env:        []string{"MAX_MEMORY=256mb", "PATH=/usr/local/bin"},
			ExposedPorts: nat.PortSet{
				"6379/tcp": struct{}{},
			},
			Cmd: []string{"redis-server"},
		},
	}

	history := []dockerimage.HistoryResponseItem{
		{CreatedBy: "/bin/sh -c #(nop) FROM redis:7.0-alpine"},
		{CreatedBy: "/bin/sh -c #(nop) WORKDIR /data"},
	}

	score := MatchImageWithCandidate(inspect, history, candidate)
	if score < 0.85 {
		t.Errorf("expected high match score (>= 0.85), got %f", score)
	}

	// Completely unrelated candidate
	unrelatedCandidate := CandidateFile{
		Path:    "/opt/java/Dockerfile",
		Type:    CandidateDockerfile,
		Content: "FROM openjdk:17\nWORKDIR /java\nEXPOSE 8080\nCMD [\"java\", \"-jar\", \"app.jar\"]",
	}

	unrelatedScore := MatchImageWithCandidate(inspect, history, unrelatedCandidate)
	if unrelatedScore >= 0.6 {
		t.Errorf("expected low match score for unrelated candidate (< 0.6), got %f", unrelatedScore)
	}
}

func TestMatchStackWithCandidate(t *testing.T) {
	composeContent := `
version: '3.8'
services:
  web:
    image: nginx:alpine
    ports:
      - "80:80"
  db:
    image: postgres:15
`
	candidate := CandidateFile{
		Path:    "/opt/my-app/docker-compose.yml",
		Type:    CandidateCompose,
		Content: composeContent,
	}

	stack := models.ComposeStack{
		Name:       "my-app",
		WorkingDir: "/opt/my-app",
		Services: []models.ComposeService{
			{Name: "web", Image: "nginx:alpine"},
			{Name: "db", Image: "postgres:15"},
		},
	}

	score := MatchStackWithCandidate(stack, candidate)
	if score < 0.9 {
		t.Errorf("expected high match score for matching stack, got %f", score)
	}
}
