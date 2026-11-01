package plugins

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kraken/admin-tool/backend/internal/models"
)

func TestBuilder_InspectAndCompile(t *testing.T) {
	// Create an in-memory zip archive containing manifest, worker.js and backend/main.go (NO wasm)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	manifestContent := `{
		"id": "com.test.autobuild",
		"name": "Auto Build Test",
		"version": "1.0.0",
		"runtime": {
			"type": "wasm",
			"entrypoint": "backend/plugin.wasm"
		},
		"frontend": {
			"entrypoint": "frontend/worker.js"
		}
	}`
	wManifest, _ := zw.Create("plugin.json")
	_, _ = wManifest.Write([]byte(manifestContent))

	wWorker, _ := zw.Create("frontend/worker.js")
	_, _ = wWorker.Write([]byte("console.log('worker');"))

	goSource := `package main

import (
	"encoding/json"
	"os"
)

func main() {
	resp := map[string]string{"status": "compiled_ok"}
	out, _ := json.Marshal(resp)
	os.Stdout.Write(out)
}
`
	wGo, _ := zw.Create("backend/main.go")
	_, _ = wGo.Write([]byte(goSource))

	zw.Close()

	// 1. Inspect archive
	insp, err := InspectArchive("test.zip", buf.Bytes(), nil)
	if err != nil {
		t.Fatalf("InspectArchive failed: %v", err)
	}

	if !insp.NeedsCompilation {
		t.Errorf("expected NeedsCompilation=true, got false")
	}
	if !insp.SourceDetected {
		t.Errorf("expected SourceDetected=true, got false")
	}
	if len(insp.SourceFiles) == 0 {
		t.Errorf("expected source files to be detected")
	}

	// 2. Extract into temp dir
	targetDir := t.TempDir()
	if err := ExtractArchive(buf.Bytes(), ArchiveTypeZip, targetDir); err != nil {
		t.Fatalf("ExtractArchive failed: %v", err)
	}

	// 3. Build WASM
	manifest := &models.PluginManifest{
		ID: "com.test.autobuild",
		Runtime: models.PluginRuntimeConfig{
			Type:       "wasm",
			Entrypoint: "backend/plugin.wasm",
		},
	}

	ctx := context.Background()
	buildRes, err := BuildWasmPlugin(ctx, targetDir, manifest)
	if err != nil {
		t.Fatalf("BuildWasmPlugin failed: %v", err)
	}

	if buildRes.SizeBytes == 0 {
		t.Errorf("expected generated wasm to have positive size")
	}
	t.Logf("Compiled with: %s, size: %d bytes, in %dms", buildRes.Compiler, buildRes.SizeBytes, buildRes.DurationMs)

	// 4. Verify generated binary in wazero runtime
	wasmPath := filepath.Join(targetDir, "backend/plugin.wasm")
	if _, err := os.Stat(wasmPath); err != nil {
		t.Fatalf("compiled wasm file missing at %s: %v", wasmPath, err)
	}

	wasmRuntime := NewWasmRuntimeManager(ctx)
	defer wasmRuntime.Close(ctx)

	bridge := &WasmHostBridge{PluginID: "com.test.autobuild"}
	res, err := wasmRuntime.Execute(ctx, wasmPath, "test", nil, bridge)
	if err != nil {
		t.Fatalf("wazero execute failed: %v", err)
	}
	t.Logf("Execution output: %+v", res)
}

func TestInspect_ContainerPluginWithDockerfile(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	manifestContent := `{
		"id": "com.test.dockerplugin",
		"name": "Docker Plugin Test",
		"version": "1.0.0",
		"runtime": {
			"type": "container"
		},
		"frontend": {
			"entrypoint": "frontend/worker.js"
		}
	}`
	wManifest, _ := zw.Create("plugin.json")
	_, _ = wManifest.Write([]byte(manifestContent))

	dockerfileContent := `FROM alpine:latest
WORKDIR /app
COPY . .
CMD ["echo", "running"]
`
	wDF, _ := zw.Create("Dockerfile")
	_, _ = wDF.Write([]byte(dockerfileContent))

	wWorker, _ := zw.Create("frontend/worker.js")
	_, _ = wWorker.Write([]byte("console.log('worker');"))

	zw.Close()

	insp, err := InspectArchive("plugin.zip", buf.Bytes(), nil)
	if err != nil {
		t.Fatalf("InspectArchive failed: %v", err)
	}

	if !insp.NeedsCompilation {
		t.Errorf("expected NeedsCompilation=true for container plugin with Dockerfile")
	}
	if !insp.SourceDetected {
		t.Errorf("expected SourceDetected=true for container plugin with Dockerfile")
	}
	if insp.Manifest.Runtime.Dockerfile != "Dockerfile" {
		t.Errorf("expected manifest Runtime.Dockerfile='Dockerfile', got '%s'", insp.Manifest.Runtime.Dockerfile)
	}
	if insp.Manifest.Runtime.Entrypoint != "kraken-plugin-com-test-dockerplugin:1.0.0" {
		t.Errorf("expected auto-generated entrypoint tag, got '%s'", insp.Manifest.Runtime.Entrypoint)
	}
}

func TestInspect_ContainerPlugin_MissingBoth_FailsHelpfully(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	manifestContent := `{
		"id": "com.test.emptycontainer",
		"name": "Empty Container Test",
		"version": "1.0.0",
		"runtime": {
			"type": "container"
		}
	}`
	wManifest, _ := zw.Create("plugin.json")
	_, _ = wManifest.Write([]byte(manifestContent))
	zw.Close()

	_, err := InspectArchive("plugin.zip", buf.Bytes(), nil)
	if err == nil {
		t.Fatalf("expected error when neither Dockerfile nor entrypoint is provided, got nil")
	}
	if !strings.Contains(err.Error(), "requires a 'Dockerfile' in the plugin archive or an 'entrypoint'") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestTarDirectoryToBuffer(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpDir, "Dockerfile"), []byte("FROM alpine\n"), 0644)
	_ = os.Mkdir(filepath.Join(tmpDir, "backend"), 0755)
	_ = os.WriteFile(filepath.Join(tmpDir, "backend", "app.py"), []byte("print('hello')\n"), 0644)

	buf, err := tarDirectoryToBuffer(tmpDir)
	if err != nil {
		t.Fatalf("tarDirectoryToBuffer failed: %v", err)
	}

	if buf.Len() == 0 {
		t.Fatalf("expected non-empty tar buffer")
	}
}

func TestSanitizeDockerfileContent(t *testing.T) {
	in := []byte("FROM mcr.microsoft.com/powershell:lts-ubuntu-22.04\nRUN pwsh -Command \"Install-Module -Name PSWSMan -Force; Install-WSMan\"\n")
	out := string(sanitizeDockerfileContent(in))
	if !strings.Contains(out, "Enable-PSWSMan -Force") {
		t.Errorf("expected Enable-PSWSMan -Force fallback in sanitized Dockerfile, got: %s", out)
	}
}

