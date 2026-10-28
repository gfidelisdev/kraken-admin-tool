package plugins

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/kraken/admin-tool/backend/internal/models"
)

// BuildResult contains details about an auto-build operation
type BuildResult struct {
	Compiler   string `json:"compiler"`
	Entrypoint string `json:"entrypoint"`
	SizeBytes  int64  `json:"size_bytes"`
	DurationMs int64  `json:"duration_ms"`
}

// BuildWasmPlugin compiles Go source code within targetDir into the declared WASI WebAssembly binary.
// It checks in order:
//  1. tinygo (if present in PATH) -> produces compact WASI binaries
//  2. go (if present in PATH) -> produces official Go wasip1 WASM binaries
//  3. docker (if available) -> executes ephemeral container using golang:alpine
func BuildWasmPlugin(ctx context.Context, targetDir string, manifest *models.PluginManifest) (*BuildResult, error) {
	start := time.Now()

	entrypointRel := strings.TrimSpace(manifest.Runtime.Entrypoint)
	if entrypointRel == "" {
		entrypointRel = "backend/plugin.wasm"
	}
	outPath := filepath.Join(targetDir, filepath.FromSlash(entrypointRel))

	// Ensure destination directory exists
	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create destination directory for wasm: %w", err)
	}

	// Detect Go source directory
	var sourceDir string
	candidates := []string{
		filepath.Join(targetDir, "backend"),
		targetDir,
	}

	for _, cand := range candidates {
		if hasGoSourceFiles(cand) {
			sourceDir = cand
			break
		}
	}

	if sourceDir == "" {
		return nil, fmt.Errorf("no Go source code found in '%s' to compile wasm", targetDir)
	}

	// Auto-generate minimal go.mod if absent so go build works in module mode
	goModPath := filepath.Join(sourceDir, "go.mod")
	if _, err := os.Stat(goModPath); os.IsNotExist(err) {
		modContent := fmt.Sprintf("module %s\n\ngo 1.22\n", manifest.ID)
		_ = os.WriteFile(goModPath, []byte(modContent), 0644)
	}

	buildCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	var compilerUsed string
	var buildErr error

	// 1. Try tinygo
	if tinygoBin, err := exec.LookPath("tinygo"); err == nil {
		compilerUsed = "tinygo"
		cmd := exec.CommandContext(buildCtx, tinygoBin, "build", "-target=wasi", "-no-debug", "-o", outPath, ".")
		cmd.Dir = sourceDir
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if runErr := cmd.Run(); runErr != nil {
			buildErr = fmt.Errorf("tinygo build error: %v (stderr: %s)", runErr, stderr.String())
			log.Printf("[BUILDER] %v, falling back to standard go", buildErr)
		} else {
			buildErr = nil
		}
	}

	// 2. Try standard go (GOOS=wasip1 GOARCH=wasm)
	if buildErr != nil || compilerUsed == "" {
		if goBin, err := exec.LookPath("go"); err == nil {
			compilerUsed = "go-wasip1"
			cmd := exec.CommandContext(buildCtx, goBin, "build", "-ldflags=-s -w", "-o", outPath, ".")
			cmd.Dir = sourceDir
			cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=wasip1", "GOARCH=wasm")
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			if runErr := cmd.Run(); runErr != nil {
				buildErr = fmt.Errorf("go wasip1 build error: %v (stderr: %s)", runErr, stderr.String())
				log.Printf("[BUILDER] %v, falling back to docker build if available", buildErr)
			} else {
				buildErr = nil
			}
		}
	}

	// 3. Try Docker container fallback
	if buildErr != nil || compilerUsed == "" {
		if dockerBin, err := exec.LookPath("docker"); err == nil {
			compilerUsed = "docker-golang-wasip1"
			// Map relative path for the source dir
			relSource, _ := filepath.Rel(targetDir, sourceDir)
			if relSource == "" {
				relSource = "."
			} else {
				relSource = "./" + filepath.ToSlash(relSource)
			}
			relOut := filepath.ToSlash(entrypointRel)

			shCmd := fmt.Sprintf("cd /src && CGO_ENABLED=0 GOOS=wasip1 GOARCH=wasm go build -ldflags='-s -w' -o %s %s", relOut, relSource)
			cmd := exec.CommandContext(buildCtx, dockerBin, "run", "--rm",
				"-v", targetDir+":/src",
				"-w", "/src",
				"golang:alpine",
				"sh", "-c", shCmd,
			)
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			if runErr := cmd.Run(); runErr != nil {
				buildErr = fmt.Errorf("docker builder error: %v (stderr: %s)", runErr, stderr.String())
			} else {
				buildErr = nil
			}
		} else {
			if buildErr == nil {
				buildErr = fmt.Errorf("no Go compiler found in PATH ('tinygo' or 'go') and Docker is unavailable")
			}
		}
	}

	if buildErr != nil {
		return nil, fmt.Errorf("failed to compile WASM for plugin '%s': %w", manifest.ID, buildErr)
	}

	fi, statErr := os.Stat(outPath)
	if statErr != nil || fi.Size() == 0 {
		return nil, fmt.Errorf("wasm compilation succeeded but generated binary was not found or is empty at %s", outPath)
	}

	duration := time.Since(start)
	log.Printf("[BUILDER] Successfully built plugin '%s' wasm binary via %s in %v (size: %d bytes)",
		manifest.ID, compilerUsed, duration, fi.Size())

	return &BuildResult{
		Compiler:   compilerUsed,
		Entrypoint: entrypointRel,
		SizeBytes:  fi.Size(),
		DurationMs: duration.Milliseconds(),
	}, nil
}

// hasGoSourceFiles checks if directory contains any .go files
func hasGoSourceFiles(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".go") {
			return true
		}
	}
	return false
}
