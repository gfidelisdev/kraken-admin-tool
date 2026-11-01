package plugins

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"gorm.io/gorm"

	"github.com/kraken/admin-tool/backend/internal/models"
)

// WasmHostBridge provides the services and permission checks available to a WASM plugin instance
type WasmHostBridge struct {
	PluginID     string
	Permissions  map[string]bool
	DB           *gorm.DB
	ServiceProxy func(ctx context.Context, action string, params json.RawMessage) (interface{}, error)
}

// WasmRuntimeManager compiles and executes wasm binaries securely
type WasmRuntimeManager struct {
	mu          sync.RWMutex
	compiledMod map[string]wasmCompiledEntry
	runtime     wazero.Runtime
}

type wasmCompiledEntry struct {
	mod     wazero.CompiledModule
	modTime time.Time
	size    int64
}

func NewWasmRuntimeManager(ctx context.Context) *WasmRuntimeManager {
	r := wazero.NewRuntime(ctx)
	wasi_snapshot_preview1.MustInstantiate(ctx, r)

	return &WasmRuntimeManager{
		compiledMod: make(map[string]wasmCompiledEntry),
		runtime:     r,
	}
}

func (m *WasmRuntimeManager) Close(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.runtime.Close(ctx)
}

// Execute invokes an action on a WASM plugin module with strict timeouts and memory isolation
func (m *WasmRuntimeManager) Execute(
	ctx context.Context,
	wasmPath string,
	action string,
	payload interface{},
	bridge *WasmHostBridge,
) (interface{}, error) {
	fi, err := os.Stat(wasmPath)
	if err != nil {
		return nil, fmt.Errorf("wasm entrypoint file not found: %w", err)
	}

	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read wasm binary: %w", err)
	}

	execCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	// Compile or get cached module (auto-recompiles if file on disk was modified)
	m.mu.Lock()
	cached, exists := m.compiledMod[wasmPath]
	var compiled wazero.CompiledModule
	if !exists || !cached.modTime.Equal(fi.ModTime()) || cached.size != fi.Size() {
		var cErr error
		compiled, cErr = m.runtime.CompileModule(execCtx, wasmBytes)
		if cErr != nil {
			m.mu.Unlock()
			return nil, fmt.Errorf("failed to compile wasm module: %w", cErr)
		}
		m.compiledMod[wasmPath] = wasmCompiledEntry{
			mod:     compiled,
			modTime: fi.ModTime(),
			size:    fi.Size(),
		}
	} else {
		compiled = cached.mod
	}
	m.mu.Unlock()

	// Prepare payload bytes
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		payloadBytes = []byte("{}")
	}

	requestObj := map[string]interface{}{
		"action":    action,
		"payload":   json.RawMessage(payloadBytes),
		"plugin_id": bridge.PluginID,
	}
	reqData, _ := json.Marshal(requestObj)

	var stdoutBuf bytes.Buffer
	var stderrBuf bytes.Buffer

	// Build Host Functions module if not yet created
	_, _ = m.runtime.NewHostModuleBuilder("kraken").
		NewFunctionBuilder().
		WithFunc(func(ctx context.Context, mod api.Module, ptr, size uint32) {
			msgBytes, ok := mod.Memory().Read(ptr, size)
			if ok {
				log.Printf("[PLUGIN:%s] %s", bridge.PluginID, string(msgBytes))
			}
		}).
		Export("kraken_log").
		NewFunctionBuilder().
		WithFunc(func(ctx context.Context, mod api.Module, keyPtr, keyLen, outPtr, maxOutLen uint32) uint32 {
			if !bridge.Permissions["storage:kv"] || bridge.DB == nil {
				return 0
			}
			keyBytes, ok := mod.Memory().Read(keyPtr, keyLen)
			if !ok {
				return 0
			}
			var kv models.PluginKVStore
			if err := bridge.DB.Where("plugin_id = ? AND key = ?", bridge.PluginID, string(keyBytes)).First(&kv).Error; err != nil {
				return 0
			}
			valBytes := []byte(kv.Value)
			if uint32(len(valBytes)) > maxOutLen {
				valBytes = valBytes[:maxOutLen]
			}
			mod.Memory().Write(outPtr, valBytes)
			return uint32(len(valBytes))
		}).
		Export("kraken_kv_get").
		NewFunctionBuilder().
		WithFunc(func(ctx context.Context, mod api.Module, keyPtr, keyLen, valPtr, valLen uint32) uint32 {
			if !bridge.Permissions["storage:kv"] || bridge.DB == nil {
				return 1 // error: permission denied
			}
			kBytes, ok1 := mod.Memory().Read(keyPtr, keyLen)
			vBytes, ok2 := mod.Memory().Read(valPtr, valLen)
			if !ok1 || !ok2 {
				return 2 // memory read error
			}
			key := string(kBytes)
			val := string(vBytes)

			var kv models.PluginKVStore
			err := bridge.DB.Where("plugin_id = ? AND key = ?", bridge.PluginID, key).First(&kv).Error
			if err != nil {
				kv = models.PluginKVStore{
					PluginID:  bridge.PluginID,
					Key:       key,
					Value:     val,
					UpdatedAt: time.Now(),
				}
				bridge.DB.Create(&kv)
			} else {
				bridge.DB.Model(&kv).Updates(map[string]interface{}{
					"value":      val,
					"updated_at": time.Now(),
				})
			}
			return 0 // success
		}).
		Export("kraken_kv_set").
		Instantiate(execCtx)

	modConfig := wazero.NewModuleConfig().
		WithName(fmt.Sprintf("%s-%d", bridge.PluginID, time.Now().UnixNano())).
		WithStdin(bytes.NewReader(reqData)).
		WithStdout(&stdoutBuf).
		WithStderr(&stderrBuf).
		WithArgs("plugin", action).
		WithEnv("PLUGIN_ID", bridge.PluginID).
		WithEnv("ACTION", action)

	instance, err := m.runtime.InstantiateModule(execCtx, compiled, modConfig)
	if err != nil {
		if stderrBuf.Len() > 0 {
			return nil, fmt.Errorf("wasm execution failed: %s (%w)", stderrBuf.String(), err)
		}
		return nil, fmt.Errorf("failed to instantiate wasm module: %w", err)
	}
	defer instance.Close(execCtx)

	// Check if handle_request function is exported directly
	handleFn := instance.ExportedFunction("handle_request")
	if handleFn != nil {
		// Module uses in-memory exported function
		mallocFn := instance.ExportedFunction("malloc")
		if mallocFn != nil {
			allocRes, aErr := mallocFn.Call(execCtx, uint64(len(reqData)))
			if aErr == nil && len(allocRes) > 0 {
				inPtr := uint32(allocRes[0])
				instance.Memory().Write(inPtr, reqData)

				outBufSize := uint32(64 * 1024) // 64 KB max return buffer
				outAlloc, oErr := mallocFn.Call(execCtx, uint64(outBufSize))
				if oErr == nil && len(outAlloc) > 0 {
					outPtr := uint32(outAlloc[0])
					callRes, cErr := handleFn.Call(execCtx, uint64(inPtr), uint64(len(reqData)), uint64(outPtr), uint64(outBufSize))
					if cErr == nil && len(callRes) > 0 {
						writtenLen := uint32(callRes[0])
						if writtenLen > 0 {
							outBytes, ok := instance.Memory().Read(outPtr, writtenLen)
							if ok {
								var parsed interface{}
								if err := json.Unmarshal(outBytes, &parsed); err == nil {
									return parsed, nil
								}
								return string(outBytes), nil
							}
						}
					}
				}
			}
		}
	}

	// Read result from stdout
	output := stdoutBuf.Bytes()
	if len(output) == 0 {
		if stderrBuf.Len() > 0 {
			return nil, fmt.Errorf("plugin error: %s", stderrBuf.String())
		}
		return map[string]interface{}{"status": "ok"}, nil
	}

	var parsedResult interface{}
	if err := json.Unmarshal(output, &parsedResult); err == nil {
		return parsedResult, nil
	}

	return string(output), nil
}

// InvalidateCache clears the compiled WASM cache for a plugin path or all if empty
func (m *WasmRuntimeManager) InvalidateCache(wasmPath string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if wasmPath == "" {
		m.compiledMod = make(map[string]wasmCompiledEntry)
		return
	}
	clean := filepath.Clean(wasmPath)
	delete(m.compiledMod, clean)
	delete(m.compiledMod, wasmPath)
}
