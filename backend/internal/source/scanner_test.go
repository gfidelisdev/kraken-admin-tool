package source

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"github.com/kraken/admin-tool/backend/internal/models"
)

type mockExecutor struct {
	outputs map[string]string
}

func (m *mockExecutor) Execute(ctx context.Context, cmd string) (string, string, int, error) {
	for k, v := range m.outputs {
		if strings.Contains(cmd, k) {
			return v, "", 0, nil
		}
	}
	return "", "", 0, nil
}

func (m *mockExecutor) ExecuteStream(ctx context.Context, cmd string, onChunk func(string)) (string, string, int, error) {
	return m.Execute(ctx, cmd)
}

func (m *mockExecutor) ExecuteSudo(ctx context.Context, cmd string, sudoPassword string) (string, string, int, error) {
	return m.Execute(ctx, cmd)
}

func (m *mockExecutor) ExecuteSudoStream(ctx context.Context, cmd string, sudoPassword string, onChunk func(string)) (string, string, int, error) {
	return m.Execute(ctx, cmd)
}

func TestValidateSearchPath(t *testing.T) {
	tests := []struct {
		path    string
		wantErr bool
	}{
		{"", true},
		{"   ", true},
		{"relative/path", true},
		{"/", true},
		{"/proc", true},
		{"/sys", true},
		{"/dev", true},
		{"/opt/app", false},
		{"/var/www/docker", false},
		{"/home/ubuntu/projects", false},
	}

	for _, tt := range tests {
		err := ValidateSearchPath(tt.path)
		if (err != nil) != tt.wantErr {
			t.Errorf("ValidateSearchPath(%q) error = %v, wantErr %v", tt.path, err, tt.wantErr)
		}
	}
}

func TestClassifyContentHeuristically(t *testing.T) {
	dockerfileContent := `
# Build stage
FROM golang:1.22-alpine
WORKDIR /app
COPY . .
RUN go build -o server
EXPOSE 8080
CMD ["./server"]
`
	composeContent := `
version: '3.8'
services:
  web:
    image: nginx:alpine
    ports:
      - "80:80"
`
	unknownContent := `
Hello world! This is a plain text file without container directives.
`

	if c := classifyContentHeuristically("Dockerfile.custom", dockerfileContent); c != CandidateDockerfile {
		t.Errorf("expected CandidateDockerfile, got %v", c)
	}
	if c := classifyContentHeuristically("custom.yaml", composeContent); c != CandidateCompose {
		t.Errorf("expected CandidateCompose, got %v", c)
	}
	if c := classifyContentHeuristically("plain.txt", unknownContent); c != "" {
		t.Errorf("expected empty classification, got %v", c)
	}
	if c := classifyContentHeuristically("Dockerfile.dockerignore", dockerfileContent); c != "" {
		t.Errorf("expected dockerignore file to be ignored, got %v", c)
	}
}

func TestScannerScanDirectory(t *testing.T) {
	sampleDF := "FROM alpine:3.18\nRUN apk add curl\nCMD [\"sh\"]"
	b64DF := base64.StdEncoding.EncodeToString([]byte(sampleDF))

	mock := &mockExecutor{
		outputs: map[string]string{
			"-iname 'dockerfile*'": "/opt/app/Dockerfile\n",
			"head -c 524288 '/opt/app/Dockerfile'": fmt.Sprintf("%s\n---MTIME---\n1700000000", b64DF),
		},
	}

	scanner := NewScanner(mock)
	dir := models.SourceSearchDir{
		Path:      "/opt/app",
		Recursive: true,
		MaxDepth:  3,
	}

	candidates, err := scanner.ScanDirectory(context.Background(), dir, "")
	if err != nil {
		t.Fatalf("ScanDirectory failed: %v", err)
	}

	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}

	cand := candidates[0]
	if cand.Path != "/opt/app/Dockerfile" {
		t.Errorf("unexpected path: %s", cand.Path)
	}
	if cand.Type != CandidateDockerfile {
		t.Errorf("unexpected type: %v", cand.Type)
	}
	if cand.Content != sampleDF {
		t.Errorf("unexpected content: %s", cand.Content)
	}
	if cand.MTime != 1700000000 {
		t.Errorf("unexpected mtime: %d", cand.MTime)
	}
}
