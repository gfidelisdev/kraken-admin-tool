package plugins

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kraken/admin-tool/backend/internal/models"
)

func TestWasmRuntime_Minimal(t *testing.T) {
	ctx := context.Background()
	mgr := NewWasmRuntimeManager(ctx)
	defer mgr.Close(ctx)

	// Minimal empty WASM binary (header: 0x00 0x61 0x73 0x6D 0x01 0x00 0x00 0x00)
	minimalWasm := []byte{0x00, 0x61, 0x73, 0x6D, 0x01, 0x00, 0x00, 0x00}

	tmpDir := t.TempDir()
	wasmFile := filepath.Join(tmpDir, "test.wasm")
	if err := os.WriteFile(wasmFile, minimalWasm, 0644); err != nil {
		t.Fatalf("failed to write test wasm: %v", err)
	}

	bridge := &WasmHostBridge{
		PluginID:    "test.minimal",
		Permissions: map[string]bool{"storage:kv": true},
	}

	res, err := mgr.Execute(ctx, wasmFile, "ping", nil, bridge)
	if err != nil {
		t.Fatalf("unexpected execution error: %v", err)
	}

	// Should return default ok or output
	if res == nil {
		t.Errorf("expected non-nil response")
	}
}

func TestWasmRuntime_KrakenAuthGetToken(t *testing.T) {
	ctx := context.Background()
	mgr := NewWasmRuntimeManager(ctx)
	defer mgr.Close(ctx)

	r := mgr.runtime

	// Verify kraken module is exported
	krakenMod := r.Module("kraken")
	if krakenMod == nil {
		t.Fatalf("expected 'kraken' host module to be instantiated")
	}

	// Compile a small Go WASM plugin that imports kraken_auth_get_token
	tmpDir := t.TempDir()
	sourceDir := filepath.Join(tmpDir, "backend")
	_ = os.MkdirAll(sourceDir, 0755)

	goModContent := "module test.token.plugin\n\ngo 1.22\n"
	_ = os.WriteFile(filepath.Join(sourceDir, "go.mod"), []byte(goModContent), 0644)

	mainContent := `package main

import (
	"encoding/json"
	"os"
	"unsafe"
)

//go:wasmimport kraken kraken_auth_get_token
func krakenAuthGetToken(scopePtr, scopeLen, outPtr, maxOutLen uint32) uint32

func main() {
	scope := "https://vault.azure.net/.default"
	scopeBytes := []byte(scope)
	outBuf := make([]byte, 1024)

	scopePtr := uint32(uintptr(unsafe.Pointer(&scopeBytes[0])))
	scopeLen := uint32(len(scopeBytes))
	outPtr := uint32(uintptr(unsafe.Pointer(&outBuf[0])))
	maxLen := uint32(len(outBuf))

	n := krakenAuthGetToken(scopePtr, scopeLen, outPtr, maxLen)
	if n == 0 {
		resp, _ := json.Marshal(map[string]interface{}{"status": "error", "bytes": 0})
		os.Stdout.Write(resp)
		return
	}

	var parsed map[string]interface{}
	_ = json.Unmarshal(outBuf[:n], &parsed)
	resp, _ := json.Marshal(map[string]interface{}{
		"status": "ok",
		"token":  parsed["access_token"],
	})
	os.Stdout.Write(resp)
}
`
	_ = os.WriteFile(filepath.Join(sourceDir, "main.go"), []byte(mainContent), 0644)

	manifest := &models.PluginManifest{
		ID: "test.token.plugin",
		Runtime: models.PluginRuntimeConfig{
			Type:       "wasm",
			Entrypoint: "backend/plugin.wasm",
		},
	}

	buildRes, err := BuildWasmPlugin(ctx, tmpDir, manifest)
	if err != nil {
		t.Fatalf("BuildWasmPlugin failed: %v", err)
	}

	wasmFile := filepath.Join(tmpDir, "backend", "plugin.wasm")
	if _, err := os.Stat(wasmFile); err != nil {
		t.Fatalf("generated wasm not found: %v", err)
	}

	// 1. With bridge & credentials:resolve permission
	calledScope := ""
	bridge := &WasmHostBridge{
		PluginID: "test.token.plugin",
		Permissions: map[string]bool{
			"credentials:resolve": true,
		},
		GetMicrosoftToken: func(ctx context.Context, scope string) (string, *models.MicrosoftSessionAccount, error) {
			calledScope = scope
			return "mock-ms-token-xyz", &models.MicrosoftSessionAccount{
				ID:       "acc-1",
				Email:    "operator@contoso.com",
				Source:   "kraken_session",
				TenantID: "mock-tenant",
			}, nil
		},
	}

	res, err := mgr.Execute(ctx, wasmFile, "get_token", nil, bridge)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	resMap, ok := res.(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected res type: %T (%v)", res, res)
	}
	if resMap["status"] != "ok" {
		t.Errorf("expected status 'ok', got %v", resMap)
	}
	if resMap["token"] != "mock-ms-token-xyz" {
		t.Errorf("expected token 'mock-ms-token-xyz', got %v", resMap["token"])
	}
	if calledScope != "https://vault.azure.net/.default" {
		t.Errorf("expected calledScope 'https://vault.azure.net/.default', got %q", calledScope)
	}
	_ = buildRes
}
