package plugins

import (
	"context"
	"os"
	"path/filepath"
	"testing"
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
