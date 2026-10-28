package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/kraken/admin-tool/backend/internal/audit"
	"github.com/kraken/admin-tool/backend/internal/docker"
	"github.com/kraken/admin-tool/backend/internal/models"
)

type Manager struct {
	mu            sync.RWMutex
	db            *gorm.DB
	dockerService *docker.DockerService
	pluginsDir    string
	wasm          *WasmRuntimeManager
	container     *ContainerRuntimeManager
	store         *StoreService
}

func NewManager(ctx context.Context, db *gorm.DB, dockerService *docker.DockerService, pluginsDir string) (*Manager, error) {
	if pluginsDir == "" {
		pluginsDir = "./data/plugins"
	}
	if err := os.MkdirAll(pluginsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to initialize plugins directory '%s': %w", pluginsDir, err)
	}

	wasmManager := NewWasmRuntimeManager(ctx)
	containerManager := NewContainerRuntimeManager(dockerService)
	storeService := NewStoreService(db)

	m := &Manager{
		db:            db,
		dockerService: dockerService,
		pluginsDir:    pluginsDir,
		wasm:          wasmManager,
		container:     containerManager,
		store:         storeService,
	}

	// Auto-start active container plugins if any
	go m.initActivePlugins(ctx)

	return m, nil
}

func (m *Manager) initActivePlugins(ctx context.Context) {
	var plugins []models.InstalledPlugin
	if err := m.db.Where("status = ?", "enabled").Find(&plugins).Error; err != nil {
		return
	}

	for _, p := range plugins {
		if p.RuntimeType == "container" {
			var manifest models.PluginManifest
			if err := json.Unmarshal([]byte(p.ManifestJSON), &manifest); err == nil {
				_, startErr := m.container.StartContainer(ctx, &p, &manifest)
				if startErr != nil {
					log.Printf("[PLUGINS] Warning: failed to restart plugin container '%s': %v", p.PluginID, startErr)
				}
			}
		}
	}
}

// GetStoreService returns the store/catalog helper
func (m *Manager) GetStoreService() *StoreService {
	return m.store
}

// GetTrustedKeys returns all registered trusted public keys from DB
func (m *Manager) GetTrustedKeys() ([]models.PluginTrustedKey, error) {
	var keys []models.PluginTrustedKey
	err := m.db.Find(&keys).Error
	return keys, err
}

// InspectUpload inspects an uploaded archive without installing
func (m *Manager) InspectUpload(filename string, data []byte) (*ExtractedInspection, error) {
	keys, _ := m.GetTrustedKeys()
	return InspectArchive(filename, data, keys)
}

// InspectURL downloads and inspects a plugin from a remote URL without installing
func (m *Manager) InspectURL(ctx context.Context, downloadURL string) (*ExtractedInspection, error) {
	data, _, err := m.store.DownloadArchive(ctx, downloadURL)
	if err != nil {
		return nil, err
	}
	filename := ExtractFilenameFromURL(downloadURL)
	keys, _ := m.GetTrustedKeys()
	return InspectArchive(filename, data, keys)
}

// InstallFromArchive extracts, verifies and registers a plugin from archive bytes
func (m *Manager) InstallFromArchive(
	ctx context.Context,
	filename string,
	data []byte,
	expectedSHA256 string,
	grantedPerms []string,
	enableImmediately bool,
	installedBy string,
	sourceURL string,
	userID uint,
) (*models.InstalledPlugin, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	keys, _ := m.GetTrustedKeys()
	insp, err := InspectArchive(filename, data, keys)
	if err != nil {
		return nil, err
	}

	if expectedSHA256 != "" && !strings.EqualFold(expectedSHA256, insp.SHA256) {
		return nil, fmt.Errorf("checksum mismatch: expected %s, got %s", expectedSHA256, insp.SHA256)
	}

	manifest := insp.Manifest
	targetDir := filepath.Join(m.pluginsDir, manifest.ID)

	// Clean existing installation if updating
	_ = os.RemoveAll(targetDir)

	if err := ExtractArchive(data, insp.ArchiveType, targetDir); err != nil {
		return nil, fmt.Errorf("failed to extract plugin archive: %w", err)
	}

	// Auto-build WASM if runtime is wasm and binary is missing/stub (<1KB), but Go sources exist
	if manifest.Runtime.Type == "wasm" {
		wasmEntrypoint := filepath.Join(targetDir, filepath.FromSlash(manifest.Runtime.Entrypoint))
		fi, statErr := os.Stat(wasmEntrypoint)
		needsCompile := statErr != nil || fi.Size() < 1024

		if needsCompile && (hasGoSourceFiles(filepath.Join(targetDir, "backend")) || hasGoSourceFiles(targetDir)) {
			log.Printf("[MANAGER] Auto-building WASM plugin '%s' from Go sources...", manifest.ID)
			buildRes, buildErr := BuildWasmPlugin(ctx, targetDir, manifest)
			if buildErr != nil {
				return nil, fmt.Errorf("plugin requires wasm compilation but auto-build failed: %w", buildErr)
			}
			log.Printf("[MANAGER] Plugin '%s' compiled successfully via %s in %dms (size: %d bytes)",
				manifest.ID, buildRes.Compiler, buildRes.DurationMs, buildRes.SizeBytes)
			audit.Log(m.db, userID, installedBy, "PLUGIN_BUILD", "plugin", manifest.ID,
				fmt.Sprintf("Auto-compiled WASM binary via %s in %dms (%d bytes)", buildRes.Compiler, buildRes.DurationMs, buildRes.SizeBytes), "SUCCESS")
		}
	}

	// Filter granted permissions to only those declared in manifest
	allowedMap := make(map[string]bool)
	for _, p := range manifest.Permissions {
		allowedMap[p] = true
	}
	finalGranted := make([]string, 0)
	for _, p := range grantedPerms {
		if allowedMap[p] {
			finalGranted = append(finalGranted, p)
		}
	}
	// If caller did not provide explicit subset, grant all declared permissions
	if len(grantedPerms) == 0 {
		finalGranted = manifest.Permissions
	}

	grantedJSON, _ := json.Marshal(finalGranted)
	manifestJSON, _ := json.Marshal(manifest)

	status := "installed"
	if enableImmediately {
		status = "enabled"
	}

	var existing models.InstalledPlugin
	res := m.db.Where("plugin_id = ?", manifest.ID).First(&existing)

	now := time.Now()
	var plugin models.InstalledPlugin

	if res.Error == nil {
		// Update existing record
		existing.Name = manifest.Name
		existing.Version = manifest.Version
		existing.Description = manifest.Description
		existing.Author = manifest.Author
		existing.Homepage = manifest.Homepage
		existing.License = manifest.License
		existing.RuntimeType = manifest.Runtime.Type
		existing.Status = status
		existing.LastError = ""
		existing.ManifestJSON = string(manifestJSON)
		existing.GrantedPermissions = string(grantedJSON)
		existing.SHA256 = insp.SHA256
		existing.SignatureVerified = insp.SignatureVerified
		existing.SignerKeyID = insp.SignerKeyID
		existing.InstallPath = targetDir
		existing.InstalledBy = installedBy
		if sourceURL != "" {
			existing.SourceURL = sourceURL
		}
		existing.UpdatedAt = now
		m.db.Save(&existing)
		plugin = existing
	} else {
		// Create new record
		plugin = models.InstalledPlugin{
			PluginID:           manifest.ID,
			Name:               manifest.Name,
			Version:            manifest.Version,
			Description:        manifest.Description,
			Author:             manifest.Author,
			Homepage:           manifest.Homepage,
			License:            manifest.License,
			RuntimeType:        manifest.Runtime.Type,
			Status:             status,
			ManifestJSON:       string(manifestJSON),
			GrantedPermissions: string(grantedJSON),
			SHA256:             insp.SHA256,
			SignatureVerified:  insp.SignatureVerified,
			SignerKeyID:        insp.SignerKeyID,
			SourceURL:          sourceURL,
			InstallPath:        targetDir,
			InstalledBy:        installedBy,
			CreatedAt:          now,
			UpdatedAt:          now,
		}
		if err := m.db.Create(&plugin).Error; err != nil {
			return nil, fmt.Errorf("failed to save plugin to database: %w", err)
		}
	}

	audit.Log(m.db, userID, installedBy, "PLUGIN_INSTALL", "plugin", manifest.ID,
		fmt.Sprintf("Installed plugin '%s' v%s (Runtime: %s)", manifest.Name, manifest.Version, manifest.Runtime.Type), "SUCCESS")

	// If enabled immediately and runtime is container, start container
	if status == "enabled" && manifest.Runtime.Type == "container" {
		cID, cErr := m.container.StartContainer(ctx, &plugin, manifest)
		if cErr != nil {
			plugin.Status = "error"
			plugin.LastError = cErr.Error()
			m.db.Model(&plugin).Updates(map[string]interface{}{
				"status":     "error",
				"last_error": cErr.Error(),
			})
		} else {
			m.db.Model(&plugin).Update("container_id", cID)
			plugin.ContainerID = cID
		}
	}

	return &plugin, nil
}

// EnablePlugin activates an installed plugin
func (m *Manager) EnablePlugin(ctx context.Context, pluginID string, username string, userID uint) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var p models.InstalledPlugin
	if err := m.db.Where("plugin_id = ?", pluginID).First(&p).Error; err != nil {
		return errors.New("plugin not found")
	}

	var manifest models.PluginManifest
	if err := json.Unmarshal([]byte(p.ManifestJSON), &manifest); err != nil {
		return fmt.Errorf("invalid plugin manifest: %w", err)
	}

	if manifest.Runtime.Type == "container" {
		cID, cErr := m.container.StartContainer(ctx, &p, &manifest)
		if cErr != nil {
			m.db.Model(&p).Updates(map[string]interface{}{
				"status":     "error",
				"last_error": cErr.Error(),
			})
			return fmt.Errorf("failed to start container for plugin: %w", cErr)
		}
		p.ContainerID = cID
	}

	m.db.Model(&p).Updates(map[string]interface{}{
		"status":       "enabled",
		"last_error":   "",
		"container_id": p.ContainerID,
		"updated_at":   time.Now(),
	})

	audit.Log(m.db, userID, username, "PLUGIN_ENABLE", "plugin", pluginID,
		fmt.Sprintf("Enabled plugin '%s'", p.Name), "SUCCESS")

	return nil
}

// DisablePlugin deactivates a running plugin
func (m *Manager) DisablePlugin(ctx context.Context, pluginID string, username string, userID uint) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var p models.InstalledPlugin
	if err := m.db.Where("plugin_id = ?", pluginID).First(&p).Error; err != nil {
		return errors.New("plugin not found")
	}

	if p.RuntimeType == "container" {
		_ = m.container.StopContainer(ctx, &p)
	}

	m.db.Model(&p).Updates(map[string]interface{}{
		"status":     "disabled",
		"updated_at": time.Now(),
	})

	audit.Log(m.db, userID, username, "PLUGIN_DISABLE", "plugin", pluginID,
		fmt.Sprintf("Disabled plugin '%s'", p.Name), "SUCCESS")

	return nil
}

// DeletePlugin removes a plugin, its files and sidecar containers
func (m *Manager) DeletePlugin(ctx context.Context, pluginID string, username string, userID uint) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var p models.InstalledPlugin
	if err := m.db.Where("plugin_id = ?", pluginID).First(&p).Error; err != nil {
		return errors.New("plugin not found")
	}

	if p.RuntimeType == "container" {
		_ = m.container.StopContainer(ctx, &p)
	}

	// Remove files
	if p.InstallPath != "" {
		_ = os.RemoveAll(p.InstallPath)
	}

	// Clear KV storage for plugin
	m.db.Where("plugin_id = ?", pluginID).Delete(&models.PluginKVStore{})

	// Delete record
	m.db.Delete(&p)

	audit.Log(m.db, userID, username, "PLUGIN_DELETE", "plugin", pluginID,
		fmt.Sprintf("Uninstalled plugin '%s'", p.Name), "SUCCESS")

	return nil
}

// CallPlugin executes an action on the backend of a plugin
func (m *Manager) CallPlugin(
	ctx context.Context,
	pluginID string,
	action string,
	payload interface{},
	username string,
	userRole string,
) (interface{}, error) {
	m.mu.RLock()
	var p models.InstalledPlugin
	err := m.db.Where("plugin_id = ?", pluginID).First(&p).Error
	m.mu.RUnlock()

	if err != nil {
		return nil, errors.New("plugin not found")
	}
	if p.Status != "enabled" {
		return nil, fmt.Errorf("plugin '%s' is not enabled (current status: %s)", pluginID, p.Status)
	}

	var manifest models.PluginManifest
	if err := json.Unmarshal([]byte(p.ManifestJSON), &manifest); err != nil {
		return nil, errors.New("corrupted plugin manifest")
	}

	var grantedPerms []string
	_ = json.Unmarshal([]byte(p.GrantedPermissions), &grantedPerms)
	permMap := make(map[string]bool)
	for _, perm := range grantedPerms {
		permMap[perm] = true
	}

	// 1. WASM Runtime
	if manifest.Runtime.Type == "wasm" {
		entrypoint := filepath.Join(p.InstallPath, manifest.Runtime.Entrypoint)
		bridge := &WasmHostBridge{
			PluginID:    pluginID,
			Permissions: permMap,
			DB:          m.db,
		}
		rawRes, execErr := m.wasm.Execute(ctx, entrypoint, action, payload, bridge)
		if execErr != nil {
			return nil, execErr
		}

		// Intercept needs_host_adapter if returned by the WASM module
		if respMap, ok := rawRes.(map[string]interface{}); ok {
			if st, _ := respMap["status"].(string); st == "needs_host_adapter" {
				return m.ResolveHostAdapter(ctx, respMap, permMap, pluginID, username, 0)
			}
		}

		return rawRes, nil
	}

	// 2. Container Runtime
	if manifest.Runtime.Type == "container" {
		return m.container.ExecuteCall(ctx, &p, &manifest, action, payload, username, userRole)
	}

	// 3. Built-in action handling for generic host primitives
	if action == "hosts.list" && permMap["hosts:read"] {
		return m.executeHostsList(ctx)
	}
	if action == "hosts.exec" && permMap["hosts:exec"] {
		if payloadMap, ok := payload.(map[string]interface{}); ok {
			return m.executeHostExec(ctx, pluginID, payloadMap, username, 0)
		}
		return nil, errors.New("hosts.exec requires {host, cmd} object")
	}

	// 4. Built-in action handling for UI-only plugins with KV storage
	if action == "kv_get" && permMap["storage:kv"] {
		key, ok := payload.(string)
		if !ok {
			return nil, errors.New("kv_get requires string key")
		}
		var kv models.PluginKVStore
		if err := m.db.Where("plugin_id = ? AND key = ?", pluginID, key).First(&kv).Error; err != nil {
			return nil, nil
		}
		return kv.Value, nil
	}

	if action == "kv_set" && permMap["storage:kv"] {
		payloadMap, ok := payload.(map[string]interface{})
		if !ok {
			return nil, errors.New("kv_set requires {key, value} object")
		}
		k := fmt.Sprintf("%v", payloadMap["key"])
		v := fmt.Sprintf("%v", payloadMap["value"])
		var kv models.PluginKVStore
		if err := m.db.Where("plugin_id = ? AND key = ?", pluginID, k).First(&kv).Error; err != nil {
			kv = models.PluginKVStore{PluginID: pluginID, Key: k, Value: v, UpdatedAt: time.Now()}
			m.db.Create(&kv)
		} else {
			m.db.Model(&kv).Updates(map[string]interface{}{"value": v, "updated_at": time.Now()})
		}
		return map[string]string{"status": "ok"}, nil
	}

	return nil, fmt.Errorf("action '%s' not supported by plugin runtime '%s'", action, manifest.Runtime.Type)
}

// GetPluginWorkerJS returns the frontend worker.js file content
func (m *Manager) GetPluginWorkerJS(pluginID string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var p models.InstalledPlugin
	if err := m.db.Where("plugin_id = ?", pluginID).First(&p).Error; err != nil {
		return nil, errors.New("plugin not found")
	}

	var manifest models.PluginManifest
	if err := json.Unmarshal([]byte(p.ManifestJSON), &manifest); err != nil {
		return nil, errors.New("corrupted plugin manifest")
	}

	ep := manifest.Frontend.Entrypoint
	if ep == "" {
		ep = "frontend/worker.js"
	}

	filePath := filepath.Join(p.InstallPath, ep)
	return os.ReadFile(filePath)
}

// ListPlugins returns all installed plugins
func (m *Manager) ListPlugins() ([]models.InstalledPlugin, error) {
	var list []models.InstalledPlugin
	err := m.db.Order("name ASC").Find(&list).Error
	return list, err
}

// GetActiveUIExtensions gathers all enabled UI extensions across active plugins
func (m *Manager) GetActiveUIExtensions() (models.PluginUIExtensions, error) {
	var plugins []models.InstalledPlugin
	if err := m.db.Where("status = ?", "enabled").Find(&plugins).Error; err != nil {
		return models.PluginUIExtensions{}, err
	}

	var result models.PluginUIExtensions
	for _, p := range plugins {
		var manifest models.PluginManifest
		if err := json.Unmarshal([]byte(p.ManifestJSON), &manifest); err != nil {
			continue
		}

		for _, s := range manifest.UIExtensions.Sidebar {
			// Prefix with plugin ID to prevent collisions
			result.Sidebar = append(result.Sidebar, models.PluginSidebarExtension{
				ID:    fmt.Sprintf("%s:%s", p.PluginID, s.ID),
				Title: s.Title,
				Icon:  s.Icon,
				Badge: s.Badge,
			})
		}

		for _, w := range manifest.UIExtensions.DashboardWidgets {
			result.DashboardWidgets = append(result.DashboardWidgets, models.PluginDashboardExtension{
				ID:    fmt.Sprintf("%s:%s", p.PluginID, w.ID),
				Title: w.Title,
			})
		}

		for _, t := range manifest.UIExtensions.ServerTabs {
			result.ServerTabs = append(result.ServerTabs, models.PluginServerTabExtension{
				ID:    fmt.Sprintf("%s:%s", p.PluginID, t.ID),
				Title: t.Title,
			})
		}
	}

	return result, nil
}

// RebuildPlugin manually compiles the plugin Go sources into WASM
func (m *Manager) RebuildPlugin(ctx context.Context, pluginID string, username string, userID uint) (*BuildResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var plugin models.InstalledPlugin
	if err := m.db.Where("plugin_id = ?", pluginID).First(&plugin).Error; err != nil {
		return nil, fmt.Errorf("plugin '%s' not found: %w", pluginID, err)
	}

	var manifest models.PluginManifest
	if err := json.Unmarshal([]byte(plugin.ManifestJSON), &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse manifest: %w", err)
	}

	if manifest.Runtime.Type != "wasm" {
		return nil, fmt.Errorf("plugin '%s' does not use wasm runtime (type is '%s')", pluginID, manifest.Runtime.Type)
	}

	targetDir := filepath.Join(m.pluginsDir, manifest.ID)
	buildRes, err := BuildWasmPlugin(ctx, targetDir, &manifest)
	if err != nil {
		audit.Log(m.db, userID, username, "PLUGIN_BUILD", "plugin", pluginID,
			fmt.Sprintf("Failed manual rebuild: %v", err), "FAILED")
		return nil, err
	}

	audit.Log(m.db, userID, username, "PLUGIN_BUILD", "plugin", pluginID,
		fmt.Sprintf("Manual rebuild succeeded via %s in %dms (%d bytes)", buildRes.Compiler, buildRes.DurationMs, buildRes.SizeBytes), "SUCCESS")

	return buildRes, nil
}

