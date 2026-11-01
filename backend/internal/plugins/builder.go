package plugins

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/kraken/admin-tool/backend/internal/docker"
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

// hasDockerfile checks if dir contains a file named Dockerfile or dockerfile
func hasDockerfile(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && (strings.EqualFold(e.Name(), "dockerfile") || strings.HasSuffix(strings.ToLower(e.Name()), ".dockerfile")) {
			return true
		}
	}
	return false
}

func tarDirectoryToBuffer(srcDir string) (*bytes.Buffer, error) {
	buf := new(bytes.Buffer)
	tw := tar.NewWriter(buf)

	err := filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() && !info.Mode().IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		if relPath == "." {
			return nil
		}

		// Skip VCS directories
		if strings.HasPrefix(relPath, ".git") || strings.HasPrefix(relPath, ".svn") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		header, err := tar.FileInfoHeader(info, info.Name())
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(relPath)

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		if info.Mode().IsRegular() {
			f, openErr := os.Open(path)
			if openErr != nil {
				return openErr
			}
			_, copyErr := io.Copy(tw, f)
			_ = f.Close()
			if copyErr != nil {
				return copyErr
			}
		}
		return nil
	})

	if err != nil {
		_ = tw.Close()
		return nil, fmt.Errorf("failed to create tar archive of '%s': %w", srcDir, err)
	}

	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("failed to finalize tar archive: %w", err)
	}

	return buf, nil
}

// BuildContainerPluginImage builds a Docker image from a plugin's Dockerfile and context files.
// It tags the resulting image with manifest.Runtime.Entrypoint (or a generated tag).
func BuildContainerPluginImage(ctx context.Context, dockerService *docker.DockerService, targetDir string, manifest *models.PluginManifest) (*BuildResult, error) {
	start := time.Now()

	imageTag := strings.TrimSpace(manifest.Runtime.Entrypoint)
	if imageTag == "" {
		imageTag = fmt.Sprintf("kraken-plugin-%s:%s", sanitizeContainerName(manifest.ID), manifest.Version)
		manifest.Runtime.Entrypoint = imageTag
	}

	// 1. Locate Dockerfile and resolve build context directory
	var dockerfileRelPath string

	if manifest.Runtime.Dockerfile != "" {
		dfCandidate := filepath.Join(targetDir, filepath.FromSlash(manifest.Runtime.Dockerfile))
		if _, err := os.Stat(dfCandidate); err == nil {
			dockerfileRelPath = manifest.Runtime.Dockerfile
		}
	}

	if dockerfileRelPath == "" {
		candidates := []string{
			"Dockerfile",
			"dockerfile",
			"backend/Dockerfile",
			"backend/dockerfile",
		}
		for _, cand := range candidates {
			p := filepath.Join(targetDir, filepath.FromSlash(cand))
			if _, err := os.Stat(p); err == nil {
				dockerfileRelPath = cand
				break
			}
		}
	}

	if dockerfileRelPath == "" {
		// Recursive search for any Dockerfile within targetDir
		_ = filepath.Walk(targetDir, func(p string, fi os.FileInfo, err error) error {
			if err == nil && !fi.IsDir() && strings.EqualFold(fi.Name(), "dockerfile") {
				rel, relErr := filepath.Rel(targetDir, p)
				if relErr == nil {
					dockerfileRelPath = filepath.ToSlash(rel)
					return filepath.SkipAll
				}
			}
			return nil
		})
	}

	if dockerfileRelPath == "" {
		return nil, fmt.Errorf("no Dockerfile found in plugin '%s' directory to build container image", manifest.ID)
	}

	manifest.Runtime.Dockerfile = dockerfileRelPath
	fullDockerfilePath := filepath.Join(targetDir, filepath.FromSlash(dockerfileRelPath))

	contextDir := targetDir
	relDF := dockerfileRelPath

	// If Dockerfile is inside a subfolder (e.g. backend/Dockerfile), inspect whether it expects that subfolder as context
	if dfRaw, readErr := os.ReadFile(fullDockerfilePath); readErr == nil {
		patched := sanitizeDockerfileContent(dfRaw)
		if !bytes.Equal(patched, dfRaw) {
			_ = os.WriteFile(fullDockerfilePath, patched, 0644)
			dfRaw = patched
		}
		if strings.Contains(dockerfileRelPath, "/") {
			dfText := string(dfRaw)
			subDir := filepath.Dir(dockerfileRelPath)
			// If Dockerfile does not reference its own subfolder in COPY/ADD, context is that subfolder
			if !strings.Contains(dfText, "COPY "+subDir) && !strings.Contains(dfText, "ADD "+subDir) {
				contextDir = filepath.Join(targetDir, filepath.FromSlash(subDir))
				relDF = filepath.Base(dockerfileRelPath)
			}
		}
	}

	// Decouple Docker image build from HTTP request cancellation so transient browser disconnects
	// or link-state changes do not abort the host Docker build mid-flight.
	buildCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	var compilerUsed string
	var imageSize int64

	// Strategy A: Direct Docker Engine API via dockerService
	var apiErr error
	if dockerService != nil {
		cli, err := dockerService.GetClient(buildCtx, 0)
		if err == nil && cli != nil {
			tarBuf, tarErr := tarDirectoryToBuffer(contextDir)
			if tarErr == nil {
				opts := types.ImageBuildOptions{
					Tags:        []string{imageTag},
					Dockerfile:  filepath.ToSlash(relDF),
					Remove:      true,
					ForceRemove: true,
					// Use host network during image build to avoid creating/destroying transient veth
					// interfaces on docker0, which causes carrier flapping and NetworkManager link events
					// that can abort in-flight browser fetch() calls.
					NetworkMode: "host",
				}

				resp, buildErr := cli.ImageBuild(buildCtx, tarBuf, opts)
				if buildErr == nil {
					defer resp.Body.Close()

					dec := json.NewDecoder(resp.Body)
					var buildLogs bytes.Buffer
					var buildError string
					var streamDecodeErr error

					for {
						var msg struct {
							Stream      string `json:"stream"`
							Error       string `json:"error"`
							ErrorDetail struct {
								Message string `json:"message"`
							} `json:"errorDetail"`
						}
						if dErr := dec.Decode(&msg); dErr != nil {
							if !errors.Is(dErr, io.EOF) {
								streamDecodeErr = dErr
							}
							break
						}
						if msg.Stream != "" {
							buildLogs.WriteString(msg.Stream)
						}
						if msg.Error != "" {
							buildError = msg.Error
						} else if msg.ErrorDetail.Message != "" {
							buildError = msg.ErrorDetail.Message
						}
					}

					if buildError != "" {
						apiErr = fmt.Errorf("docker engine build failed: %s\nLogs:\n%s", buildError, buildLogs.String())
					} else if streamDecodeErr != nil {
						apiErr = fmt.Errorf("docker engine build stream interrupted: %w\nLogs:\n%s", streamDecodeErr, buildLogs.String())
					} else {
						insp, _, inspErr := cli.ImageInspectWithRaw(buildCtx, imageTag)
						if inspErr == nil {
							compilerUsed = "docker-engine-api"
							imageSize = insp.Size
						} else {
							apiErr = fmt.Errorf("image '%s' was not found on Docker host after build: %w\nLogs:\n%s", imageTag, inspErr, buildLogs.String())
						}
					}
				} else {
					apiErr = buildErr
				}
			} else {
				apiErr = tarErr
			}
		} else {
			apiErr = errors.New("docker service client unavailable on host 0")
		}
	} else {
		apiErr = errors.New("docker service is nil")
	}

	// Strategy B: Fallback to host docker CLI ONLY if API client was unavailable
	// If the Dockerfile itself failed compilation, do not re-run the same failing build on CLI.
	if compilerUsed == "" && apiErr != nil && strings.Contains(apiErr.Error(), "unavailable") {
		if dockerBin, err := exec.LookPath("docker"); err == nil {
			cmd := exec.CommandContext(buildCtx, dockerBin, "build", "--network=host", "-t", imageTag, "-f", fullDockerfilePath, contextDir)
			var outBuf bytes.Buffer
			cmd.Stdout = &outBuf
			cmd.Stderr = &outBuf
			if runErr := cmd.Run(); runErr != nil {
				return nil, fmt.Errorf("failed to build container image for '%s': %v\nOutput: %s",
					manifest.ID, runErr, outBuf.String())
			}
			compilerUsed = "docker-cli"
			inspectCmd := exec.CommandContext(buildCtx, dockerBin, "inspect", "--format={{.Size}}", imageTag)
			if out, err := inspectCmd.Output(); err == nil {
				var sz int64
				fmt.Sscanf(strings.TrimSpace(string(out)), "%d", &sz)
				imageSize = sz
			}
		} else {
			return nil, fmt.Errorf("failed to build container image for plugin '%s': %w", manifest.ID, apiErr)
		}
	} else if compilerUsed == "" {
		return nil, apiErr
	}

	duration := time.Since(start)
	log.Printf("[BUILDER] Successfully built container image '%s' for plugin '%s' via %s in %v (size: %d bytes)",
		imageTag, manifest.ID, compilerUsed, duration, imageSize)

	return &BuildResult{
		Compiler:   compilerUsed,
		Entrypoint: imageTag,
		SizeBytes:  imageSize,
		DurationMs: duration.Milliseconds(),
	}, nil
}

// sanitizeDockerfileContent applies backward-compatible fixes to known breaking upstream module changes
// in plugin Dockerfiles (e.g., PSWSMan v3.0+ replacing Install-WSMan with Enable-PSWSMan -Force).
func sanitizeDockerfileContent(raw []byte) []byte {
	text := string(raw)
	if strings.Contains(text, "Install-WSMan") && !strings.Contains(text, "Enable-PSWSMan") {
		compat := "if (Get-Command Install-WSMan -ErrorAction SilentlyContinue) { Install-WSMan } elseif (Get-Command Enable-PSWSMan -ErrorAction SilentlyContinue) { Enable-PSWSMan -Force }"
		text = strings.ReplaceAll(text, "Install-WSMan", compat)
		return []byte(text)
	}
	return raw
}

