package plugins

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
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
