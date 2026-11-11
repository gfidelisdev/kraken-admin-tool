package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/kraken/admin-tool/backend/internal/audit"
	"github.com/kraken/admin-tool/backend/internal/auth"
	"github.com/kraken/admin-tool/backend/internal/docker"
	"github.com/kraken/admin-tool/backend/internal/models"
)

type Manager struct {
	mu              sync.RWMutex
	db              *gorm.DB
	dockerService   *docker.DockerService
	pluginsDir      string
	wasm            *WasmRuntimeManager
	container       *ContainerRuntimeManager
	store            *StoreService
	msTokenProvider  func(ctx context.Context, scope string) (string, *models.MicrosoftSessionAccount, error)
	msSessionRevoker func(userID uint, sessionID string)
}

func (m *Manager) SetMicrosoftTokenProvider(fn func(ctx context.Context, scope string) (string, *models.MicrosoftSessionAccount, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.msTokenProvider = fn
}

func (m *Manager) SetMicrosoftSessionRevoker(fn func(userID uint, sessionID string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.msSessionRevoker = fn
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

	if db != nil {
		if db.Migrator().HasIndex(&models.PluginKVStore{}, "idx_plugin_kv") {
			_ = db.Migrator().DropIndex(&models.PluginKVStore{}, "idx_plugin_kv")
		}
		_ = db.Exec("DROP INDEX IF EXISTS idx_plugin_kv").Error
		_ = db.AutoMigrate(&models.PluginKVStore{})
		db.Where("user_id = 0 AND plugin_id = ?", "com.kraken.azure-keyvault").Delete(&models.PluginKVStore{})
	}

	m.syncBundledPlugins()

	// Auto-start active container plugins if any
	go m.initActivePlugins(ctx)

	return m, nil
}

// syncBundledPlugins updates already-installed plugins in m.pluginsDir when newer bundled plugin files are present in the container image
func (m *Manager) syncBundledPlugins() {
	candidates := []struct {
		pluginID  string
		sourceDir string
	}{
		{"com.kraken.azure-keyvault", "/app/bundled_plugins/com.kraken.azure-keyvault"},
		{"com.kraken.azure-keyvault", "./bundled_plugins/com.kraken.azure-keyvault"},
		{"com.kraken.azure-keyvault", "../examples/plugins/azure-keyvault"},
		{"com.kraken.azure-keyvault", "./examples/plugins/azure-keyvault"},
	}

	for _, c := range candidates {
		manifestPath := filepath.Join(c.sourceDir, "plugin.json")
		if _, err := os.Stat(manifestPath); err != nil {
			continue
		}
		targetDir := filepath.Join(m.pluginsDir, c.pluginID)
		if st, err := os.Stat(targetDir); err != nil || !st.IsDir() {
			continue
		}

		// Copy updated files from sourceDir to targetDir
		relFiles := []string{
			"plugin.json",
			"frontend/worker.js",
			"backend/plugin.wasm",
			"backend/main.go",
			"backend/go.mod",
		}
		for _, rel := range relFiles {
			srcFile := filepath.Join(c.sourceDir, rel)
			data, err := os.ReadFile(srcFile)
			if err != nil {
				continue
			}
			dstFile := filepath.Join(targetDir, rel)
			_ = os.MkdirAll(filepath.Dir(dstFile), 0755)
			_ = os.WriteFile(dstFile, data, 0644)
		}

		if m.db != nil {
			if manifestBytes, err := os.ReadFile(manifestPath); err == nil {
				var manifest models.PluginManifest
				if json.Unmarshal(manifestBytes, &manifest) == nil && manifest.ID == c.pluginID {
					m.db.Model(&models.InstalledPlugin{}).Where("plugin_id = ?", c.pluginID).Updates(map[string]interface{}{
						"version":       manifest.Version,
						"manifest_json": string(manifestBytes),
						"updated_at":    time.Now(),
					})
				}
			}
		}
		log.Printf("[PLUGINS] Synchronized bundled plugin '%s' into '%s'", c.pluginID, targetDir)
		break
	}
}

func (m *Manager) initActivePlugins(ctx context.Context) {
	m.syncAllPluginServerBindings()

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

// InspectDirectory inspects an uncompressed plugin directory without installing
func (m *Manager) InspectDirectory(dirPath string) (*ExtractedInspection, error) {
	keys, _ := m.GetTrustedKeys()
	return InspectDirectory(dirPath, keys)
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

	var existing models.InstalledPlugin
	hasExisting := m.db.Where("plugin_id = ?", manifest.ID).First(&existing).Error == nil

	// If updating an existing container plugin that is currently running, stop it first so it recreates
	if hasExisting && existing.RuntimeType == "container" {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 15*time.Second)
		_ = m.container.StopContainer(stopCtx, &existing)
		stopCancel()
	}

	// Clean existing installation if updating
	_ = os.RemoveAll(targetDir)

	if err := ExtractArchive(data, insp.ArchiveType, targetDir); err != nil {
		return nil, fmt.Errorf("failed to extract plugin archive: %w", err)
	}

	// Invalidate WASM cache for updated files
	if m.wasm != nil {
		m.wasm.InvalidateCache("")
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

	// Auto-build container image if runtime is container and Dockerfile exists
	if manifest.Runtime.Type == "container" {
		hasDF := manifest.Runtime.Dockerfile != "" ||
			hasDockerfile(targetDir) ||
			hasDockerfile(filepath.Join(targetDir, "backend"))

		if hasDF {
			log.Printf("[MANAGER] Auto-building container image for plugin '%s'...", manifest.ID)
			buildRes, buildErr := BuildContainerPluginImage(ctx, m.dockerService, targetDir, manifest)
			if buildErr != nil {
				return nil, fmt.Errorf("plugin requires container build but image build failed: %w", buildErr)
			}
			log.Printf("[MANAGER] Plugin '%s' container image '%s' built successfully in %dms (size: %d bytes)",
				manifest.ID, buildRes.Entrypoint, buildRes.DurationMs, buildRes.SizeBytes)
			audit.Log(m.db, userID, installedBy, "PLUGIN_BUILD", "plugin", manifest.ID,
				fmt.Sprintf("Auto-built container image '%s' via %s in %dms (%d bytes)", buildRes.Entrypoint, buildRes.Compiler, buildRes.DurationMs, buildRes.SizeBytes), "SUCCESS")
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
	if enableImmediately || (hasExisting && existing.Status == "enabled") {
		status = "enabled"
	}

	now := time.Now()
	var plugin models.InstalledPlugin

	if hasExisting {
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

		audit.Log(m.db, userID, installedBy, "PLUGIN_UPDATE", "plugin", manifest.ID,
			fmt.Sprintf("Updated plugin '%s' v%s (Runtime: %s)", manifest.Name, manifest.Version, manifest.Runtime.Type), "SUCCESS")
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

		audit.Log(m.db, userID, installedBy, "PLUGIN_INSTALL", "plugin", manifest.ID,
			fmt.Sprintf("Installed plugin '%s' v%s (Runtime: %s)", manifest.Name, manifest.Version, manifest.Runtime.Type), "SUCCESS")
	}

	// If enabled immediately and runtime is container, start container
	if status == "enabled" && manifest.Runtime.Type == "container" {
		startCtx, startCancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer startCancel()
		cID, cErr := m.container.StartContainer(startCtx, &plugin, manifest)
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

	// Record per-user state for the installer
	if userID > 0 {
		userStatus := "enabled"
		if !enableImmediately && !hasExisting {
			userStatus = "disabled"
		}
		var userState models.UserPluginState
		if err := m.db.Where("user_id = ? AND plugin_id = ?", userID, manifest.ID).First(&userState).Error; err != nil {
			m.db.Create(&models.UserPluginState{
				UserID:    userID,
				PluginID:  manifest.ID,
				Status:    userStatus,
				UpdatedAt: now,
			})
		} else {
			m.db.Model(&userState).Updates(map[string]interface{}{
				"status":     userStatus,
				"updated_at": now,
			})
		}
	}

	plugin.IconURL, _ = ResolvePluginDirIcon(plugin.InstallPath)
	return &plugin, nil
}

// InstallFromDirectory copies, verifies and registers a plugin from an uncompressed directory on disk
func (m *Manager) InstallFromDirectory(
	ctx context.Context,
	sourceDir string,
	expectedSHA256 string,
	grantedPerms []string,
	enableImmediately bool,
	installedBy string,
	userID uint,
) (*models.InstalledPlugin, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	keys, _ := m.GetTrustedKeys()
	insp, err := InspectDirectory(sourceDir, keys)
	if err != nil {
		return nil, err
	}

	if expectedSHA256 != "" && !strings.EqualFold(expectedSHA256, insp.SHA256) {
		return nil, fmt.Errorf("checksum mismatch: expected %s, got %s", expectedSHA256, insp.SHA256)
	}

	manifest := insp.Manifest
	targetDir := filepath.Join(m.pluginsDir, manifest.ID)

	var existing models.InstalledPlugin
	hasExisting := m.db.Where("plugin_id = ?", manifest.ID).First(&existing).Error == nil

	// If updating an existing container plugin that is currently running, stop it first so it recreates
	if hasExisting && existing.RuntimeType == "container" {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 15*time.Second)
		_ = m.container.StopContainer(stopCtx, &existing)
		stopCancel()
	}

	cleanSrc, _ := filepath.Abs(filepath.Clean(sourceDir))
	cleanTarget, _ := filepath.Abs(filepath.Clean(targetDir))

	if cleanSrc != cleanTarget {
		_ = os.RemoveAll(targetDir)
		if err := CopyDirectory(cleanSrc, cleanTarget); err != nil {
			return nil, fmt.Errorf("failed to copy plugin directory: %w", err)
		}
	}

	// Invalidate WASM cache for updated files
	if m.wasm != nil {
		m.wasm.InvalidateCache("")
	}

	// Auto-build WASM if runtime is wasm and binary is missing/stub (<1KB), but Go sources exist
	if manifest.Runtime.Type == "wasm" {
		wasmEntrypoint := filepath.Join(targetDir, filepath.FromSlash(manifest.Runtime.Entrypoint))
		fi, statErr := os.Stat(wasmEntrypoint)
		needsCompile := statErr != nil || fi.Size() < 1024

		if needsCompile && (hasGoSourceFiles(filepath.Join(targetDir, "backend")) || hasGoSourceFiles(targetDir)) {
			log.Printf("[MANAGER] Auto-building WASM plugin '%s' from Go sources in directory...", manifest.ID)
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

	// Auto-build container image if runtime is container and Dockerfile exists
	if manifest.Runtime.Type == "container" {
		hasDF := manifest.Runtime.Dockerfile != "" ||
			hasDockerfile(targetDir) ||
			hasDockerfile(filepath.Join(targetDir, "backend"))

		if hasDF {
			log.Printf("[MANAGER] Auto-building container image for plugin '%s' from directory...", manifest.ID)
			buildRes, buildErr := BuildContainerPluginImage(ctx, m.dockerService, targetDir, manifest)
			if buildErr != nil {
				return nil, fmt.Errorf("plugin requires container build but image build failed: %w", buildErr)
			}
			log.Printf("[MANAGER] Plugin '%s' container image '%s' built successfully in %dms (size: %d bytes)",
				manifest.ID, buildRes.Entrypoint, buildRes.DurationMs, buildRes.SizeBytes)
			audit.Log(m.db, userID, installedBy, "PLUGIN_BUILD", "plugin", manifest.ID,
				fmt.Sprintf("Auto-built container image '%s' via %s in %dms (%d bytes)", buildRes.Entrypoint, buildRes.Compiler, buildRes.DurationMs, buildRes.SizeBytes), "SUCCESS")
		}
	}

	// Filter granted permissions to only those declared in manifest
	declaredMap := make(map[string]bool)
	for _, p := range manifest.Permissions {
		declaredMap[p] = true
	}
	finalPerms := make([]string, 0, len(grantedPerms))
	for _, p := range grantedPerms {
		if declaredMap[p] {
			finalPerms = append(finalPerms, p)
		}
	}

	manifestJSON, _ := json.Marshal(manifest)
	grantedJSON, _ := json.Marshal(finalPerms)

	status := "disabled"
	if enableImmediately {
		status = "enabled"
	}

	now := time.Now()
	var plugin models.InstalledPlugin

	if hasExisting {
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
		existing.SourceURL = "file://" + cleanSrc
		existing.UpdatedAt = now
		m.db.Save(&existing)
		plugin = existing

		audit.Log(m.db, userID, installedBy, "PLUGIN_UPDATE", "plugin", manifest.ID,
			fmt.Sprintf("Updated plugin '%s' v%s from directory '%s'", manifest.Name, manifest.Version, cleanSrc), "SUCCESS")
	} else {
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
			SourceURL:          "file://" + cleanSrc,
			InstallPath:        targetDir,
			InstalledBy:        installedBy,
			CreatedAt:          now,
			UpdatedAt:          now,
		}
		if err := m.db.Create(&plugin).Error; err != nil {
			return nil, fmt.Errorf("failed to save plugin to database: %w", err)
		}

		audit.Log(m.db, userID, installedBy, "PLUGIN_INSTALL", "plugin", manifest.ID,
			fmt.Sprintf("Installed plugin '%s' v%s from directory '%s'", manifest.Name, manifest.Version, cleanSrc), "SUCCESS")
	}

	// If enabled immediately and runtime is container, start container
	if status == "enabled" && manifest.Runtime.Type == "container" {
		startCtx, startCancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer startCancel()
		cID, cErr := m.container.StartContainer(startCtx, &plugin, manifest)
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

	// Record per-user state for the installer
	if userID > 0 {
		userStatus := "enabled"
		if !enableImmediately && !hasExisting {
			userStatus = "disabled"
		}
		var userState models.UserPluginState
		if err := m.db.Where("user_id = ? AND plugin_id = ?", userID, manifest.ID).First(&userState).Error; err != nil {
			m.db.Create(&models.UserPluginState{
				UserID:    userID,
				PluginID:  manifest.ID,
				Status:    userStatus,
				UpdatedAt: now,
			})
		} else {
			m.db.Model(&userState).Updates(map[string]interface{}{
				"status":     userStatus,
				"updated_at": now,
			})
		}
	}

	plugin.IconURL, _ = ResolvePluginDirIcon(plugin.InstallPath)
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

	if manifest.Runtime.Type == "container" && p.ContainerID == "" {
		startCtx, startCancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer startCancel()
		cID, cErr := m.container.StartContainer(startCtx, &p, &manifest)
		if cErr != nil {
			if userID > 0 {
				m.db.Model(&models.UserPluginState{}).Where("user_id = ? AND plugin_id = ?", userID, pluginID).Updates(map[string]interface{}{
					"status":     "error",
					"last_error": cErr.Error(),
				})
			}
			m.db.Model(&p).Updates(map[string]interface{}{
				"status":     "error",
				"last_error": cErr.Error(),
			})
			return fmt.Errorf("failed to start container for plugin: %w", cErr)
		}
		p.ContainerID = cID
		m.db.Model(&p).Update("container_id", p.ContainerID)
	}

	if userID > 0 {
		var userState models.UserPluginState
		if err := m.db.Where("user_id = ? AND plugin_id = ?", userID, pluginID).First(&userState).Error; err != nil {
			m.db.Create(&models.UserPluginState{
				UserID:    userID,
				PluginID:  pluginID,
				Status:    "enabled",
				LastError: "",
				UpdatedAt: time.Now(),
			})
		} else {
			m.db.Model(&userState).Updates(map[string]interface{}{
				"status":     "enabled",
				"last_error": "",
				"updated_at": time.Now(),
			})
		}
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

	if userID > 0 {
		var userState models.UserPluginState
		if err := m.db.Where("user_id = ? AND plugin_id = ?", userID, pluginID).First(&userState).Error; err != nil {
			m.db.Create(&models.UserPluginState{
				UserID:    userID,
				PluginID:  pluginID,
				Status:    "disabled",
				UpdatedAt: time.Now(),
			})
		} else {
			m.db.Model(&userState).Updates(map[string]interface{}{
				"status":     "disabled",
				"updated_at": time.Now(),
			})
		}
	}

	if p.RuntimeType == "container" {
		var activeCount int64
		if err := m.db.Model(&models.UserPluginState{}).Where("plugin_id = ? AND status = ?", pluginID, "enabled").Count(&activeCount).Error; err == nil && activeCount == 0 {
			_ = m.container.StopContainer(ctx, &p)
			m.db.Model(&p).Update("container_id", "")
		}
	}

	// Update installed plugin record if no users have it enabled or if userID == 0
	var activeCount int64
	m.db.Model(&models.UserPluginState{}).Where("plugin_id = ? AND status = ?", pluginID, "enabled").Count(&activeCount)
	if activeCount == 0 || userID == 0 {
		m.db.Model(&p).Updates(map[string]interface{}{
			"status":     "disabled",
			"updated_at": time.Now(),
		})
	}

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

	// Clear per-user plugin states
	m.db.Where("plugin_id = ?", pluginID).Delete(&models.UserPluginState{})

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
	userIDs ...uint,
) (interface{}, error) {
	var userID uint
	if len(userIDs) > 0 {
		userID = userIDs[0]
	}
	if userID == 0 {
		if claims := auth.GetUserFromContext(ctx); claims != nil {
			userID = claims.UserID
		}
	}

	m.mu.RLock()
	var p models.InstalledPlugin
	err := m.db.Where("plugin_id = ?", pluginID).First(&p).Error
	m.mu.RUnlock()

	if err != nil {
		return nil, errors.New("plugin not found")
	}

	if userID > 0 {
		var userState models.UserPluginState
		if err := m.db.Where("user_id = ? AND plugin_id = ?", userID, pluginID).First(&userState).Error; err == nil {
			if userState.Status != "enabled" {
				return nil, fmt.Errorf("plugin '%s' is not enabled for this user", pluginID)
			}
		} else if p.Status != "enabled" {
			return nil, fmt.Errorf("plugin '%s' is not enabled (current status: %s)", pluginID, p.Status)
		}
	} else if p.Status != "enabled" {
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

	// Security Check: Key Vault & Credential Resolution permissions
	if action == models.PluginActionVaultResolveSecret && !permMap["credentials:resolve"] {
		return nil, errors.New("plugin lacks mandatory permission 'credentials:resolve'")
	}
	if (action == models.PluginActionVaultListSecrets || action == models.PluginActionVaultPutSecret || action == models.PluginActionVaultTestConnection) && !permMap["credentials:manage"] {
		return nil, errors.New("plugin lacks mandatory permission 'credentials:manage'")
	}

	// 1. Built-in action handling for plugins with KV storage (must precede WASM/container delegation)
	if (action == "kv_get" || action == "storage.get" || action == "kv.get") && permMap["storage:kv"] {
		var key string
		if kStr, ok := payload.(string); ok {
			key = kStr
		} else if pMap, ok := payload.(map[string]interface{}); ok {
			if k, exists := pMap["key"]; exists {
				key = fmt.Sprintf("%v", k)
			}
		}
		if key == "" {
			return nil, errors.New("kv_get requires string key")
		}
		var kv models.PluginKVStore
		query := m.db.Where("plugin_id = ? AND key = ? AND user_id = ?", pluginID, key, userID)
		if err := query.First(&kv).Error; err != nil {
			return nil, nil
		}
		return kv.Value, nil
	}

	if (action == "kv_set" || action == "storage.set" || action == "kv.set") && permMap["storage:kv"] {
		payloadMap, ok := payload.(map[string]interface{})
		if !ok {
			return nil, errors.New("kv_set requires {key, value} object")
		}
		k := fmt.Sprintf("%v", payloadMap["key"])
		v := fmt.Sprintf("%v", payloadMap["value"])
		var kv models.PluginKVStore
		query := m.db.Where("plugin_id = ? AND key = ? AND user_id = ?", pluginID, k, userID)
		if err := query.First(&kv).Error; err != nil {
			kv = models.PluginKVStore{PluginID: pluginID, Key: k, Value: v, UserID: userID, UpdatedAt: time.Now()}
			m.db.Create(&kv)
		} else {
			m.db.Model(&kv).Updates(map[string]interface{}{"value": v, "updated_at": time.Now()})
		}
		// Keep prefixed key in sync so WASM and frontend KV always read the exact same per-user state
		if !strings.HasPrefix(k, pluginID+":") {
			scopedKey := pluginID + ":" + k
			var scopedKV models.PluginKVStore
			if err := m.db.Where("plugin_id = ? AND key = ? AND user_id = ?", pluginID, scopedKey, userID).First(&scopedKV).Error; err != nil {
				scopedKV = models.PluginKVStore{PluginID: pluginID, Key: scopedKey, Value: v, UserID: userID, UpdatedAt: time.Now()}
				m.db.Create(&scopedKV)
			} else {
				m.db.Model(&scopedKV).Updates(map[string]interface{}{"value": v, "updated_at": time.Now()})
			}
		}
		if k == "azure_kv_config" || k == pluginID+":azure_kv_config" {
			m.syncPluginServerBindings(pluginID, v, userID)
		}
		return map[string]string{"status": "ok"}, nil
	}

	// 2. WASM Runtime
	if manifest.Runtime.Type == "wasm" {
		entrypoint := filepath.Join(p.InstallPath, manifest.Runtime.Entrypoint)
		bridge := &WasmHostBridge{
			PluginID:          pluginID,
			UserID:            userID,
			Permissions:       permMap,
			DB:                m.db,
			GetMicrosoftToken: m.msTokenProvider,
			OnKVSet: func(key, val string) {
				if key == "azure_kv_config" || key == pluginID+":azure_kv_config" {
					m.syncPluginServerBindings(pluginID, val, userID)
				}
			},
		}
		rawRes, execErr := m.wasm.Execute(ctx, entrypoint, action, payload, bridge)
		if execErr != nil {
			// Fallback to KV-backed vault actions if WASM execution fails and plugin has storage:kv
			if permMap["storage:kv"] {
				switch action {
				case models.PluginActionVaultResolveSecret:
					return m.executeKVVaultResolve(pluginID, payload, userID)
				case models.PluginActionVaultListSecrets:
					return m.executeKVVaultList(pluginID, userID)
				case models.PluginActionVaultPutSecret:
					return m.executeKVVaultPut(pluginID, payload, userID)
				case models.PluginActionVaultTestConnection:
					return map[string]interface{}{
						"success": true,
						"message": "Connected to KV-backed vault storage",
						"vault":   "plugin-kv",
					}, nil
				}
			}
			return nil, execErr
		}

		// Intercept needs_host_adapter if returned by the WASM module
		if respMap, ok := rawRes.(map[string]interface{}); ok {
			if st, _ := respMap["status"].(string); st == "needs_host_adapter" {
				return m.ResolveHostAdapter(ctx, respMap, permMap, pluginID, username, userID)
			}
		}

		return rawRes, nil
	}

	// 3. Container Runtime
	if manifest.Runtime.Type == "container" {
		return m.container.ExecuteCall(ctx, &p, &manifest, action, payload, username, userRole)
	}

	// 4. Built-in action handling for generic host primitives
	if action == "hosts.list" && permMap["hosts:read"] {
		return m.executeHostsList(ctx, userID)
	}
	if action == "hosts.exec" && permMap["hosts:exec"] {
		if payloadMap, ok := payload.(map[string]interface{}); ok {
			return m.executeHostExec(ctx, pluginID, payloadMap, username, userID)
		}
		return nil, errors.New("hosts.exec requires {host, cmd} object")
	}

	// 5. Built-in action handling for KV-backed Key Vaults (UI plugins or dev/testing)
	if action == models.PluginActionVaultResolveSecret && permMap["storage:kv"] {
		return m.executeKVVaultResolve(pluginID, payload, userID)
	}
	if action == models.PluginActionVaultListSecrets && permMap["storage:kv"] {
		return m.executeKVVaultList(pluginID, userID)
	}
	if action == models.PluginActionVaultPutSecret && permMap["storage:kv"] {
		return m.executeKVVaultPut(pluginID, payload, userID)
	}
	if action == models.PluginActionVaultTestConnection && permMap["storage:kv"] {
		return map[string]interface{}{
			"success": true,
			"message": "Connected to KV-backed vault storage",
			"vault":   "plugin-kv",
		}, nil
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
	return m.ListPluginsForUser(0)
}

// ListPluginsForUser returns all installed plugins mapped to the specific user's activation state
func (m *Manager) ListPluginsForUser(userID uint) ([]models.InstalledPlugin, error) {
	var list []models.InstalledPlugin
	err := m.db.Order("name ASC").Find(&list).Error
	if err != nil {
		return nil, err
	}

	var userStates []models.UserPluginState
	if userID > 0 {
		_ = m.db.Where("user_id = ?", userID).Find(&userStates).Error
	}
	stateMap := make(map[string]models.UserPluginState)
	for _, s := range userStates {
		stateMap[s.PluginID] = s
	}

	for i := range list {
		list[i].IconURL, _ = ResolvePluginDirIcon(list[i].InstallPath)
		if userID > 0 {
			if us, ok := stateMap[list[i].PluginID]; ok {
				list[i].Status = us.Status
				if us.LastError != "" {
					list[i].LastError = us.LastError
				}
			} else {
				// Other users default to disabled until they activate it
				list[i].Status = "disabled"
			}
		}
	}
	return list, nil
}

// GetActiveUIExtensions gathers all enabled UI extensions across active plugins for a user
func (m *Manager) GetActiveUIExtensions(userIDs ...uint) (models.PluginUIExtensions, error) {
	var userID uint
	if len(userIDs) > 0 {
		userID = userIDs[0]
	}

	var plugins []models.InstalledPlugin
	if userID > 0 {
		var activeStates []models.UserPluginState
		if err := m.db.Where("user_id = ? AND status = ?", userID, "enabled").Find(&activeStates).Error; err != nil {
			return models.PluginUIExtensions{}, err
		}
		if len(activeStates) == 0 {
			return models.PluginUIExtensions{}, nil
		}
		var activeIDs []string
		for _, s := range activeStates {
			activeIDs = append(activeIDs, s.PluginID)
		}
		if err := m.db.Where("plugin_id IN ?", activeIDs).Find(&plugins).Error; err != nil {
			return models.PluginUIExtensions{}, err
		}
	} else {
		if err := m.db.Where("status = ?", "enabled").Find(&plugins).Error; err != nil {
			return models.PluginUIExtensions{}, err
		}
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

	if manifest.Runtime.Type != "wasm" && manifest.Runtime.Type != "container" {
		return nil, fmt.Errorf("plugin '%s' does not use wasm or container runtime (type is '%s')", pluginID, manifest.Runtime.Type)
	}

	targetDir := filepath.Join(m.pluginsDir, manifest.ID)
	var buildRes *BuildResult
	var buildErr error

	switch manifest.Runtime.Type {
	case "wasm":
		buildRes, buildErr = BuildWasmPlugin(ctx, targetDir, &manifest)
		if buildErr == nil {
			if m.wasm != nil {
				m.wasm.InvalidateCache("")
			}
			if plugin.Status == "error" {
				m.db.Model(&plugin).Updates(map[string]interface{}{
					"status":     "enabled",
					"last_error": "",
					"updated_at": time.Now(),
				})
			}
		}
	case "container":
		buildRes, buildErr = BuildContainerPluginImage(ctx, m.dockerService, targetDir, &manifest)
		if buildErr == nil {
			manifestBytes, _ := json.Marshal(&manifest)
			updates := map[string]interface{}{
				"manifest_json": string(manifestBytes),
				"last_error":    "",
				"updated_at":    time.Now(),
			}
			if plugin.Status == "enabled" || plugin.Status == "error" {
				startCtx, startCancel := context.WithTimeout(context.Background(), 2*time.Minute)
				defer startCancel()
				// Restart sidecar container with newly built image
				_ = m.container.StopContainer(startCtx, &plugin)
				cID, cErr := m.container.StartContainer(startCtx, &plugin, &manifest)
				if cErr == nil {
					updates["container_id"] = cID
					updates["status"] = "enabled"
					plugin.ContainerID = cID
				} else {
					updates["status"] = "error"
					updates["last_error"] = cErr.Error()
				}
			}
			m.db.Model(&plugin).Updates(updates)
		}
	}

	if buildErr != nil {
		audit.Log(m.db, userID, username, "PLUGIN_BUILD", "plugin", pluginID,
			fmt.Sprintf("Failed manual rebuild: %v", buildErr), "FAILED")
		return nil, buildErr
	}

	audit.Log(m.db, userID, username, "PLUGIN_BUILD", "plugin", pluginID,
		fmt.Sprintf("Manual rebuild succeeded via %s in %dms (%d bytes)", buildRes.Compiler, buildRes.DurationMs, buildRes.SizeBytes), "SUCCESS")

	return buildRes, nil
}

// Compile-time check that Manager implements docker.CredentialResolver
var _ docker.CredentialResolver = (*Manager)(nil)

// unwrapPluginVaultPayload extracts the inner JSON payload when a plugin uses a host adapter (e.g. hosts.exec curl)
// or returns a standard plugin envelope, and detects upstream vault/OAuth errors.
func unwrapPluginVaultPayload(res interface{}) ([]byte, error) {
	rawBytes, err := json.Marshal(res)
	if err != nil {
		return nil, fmt.Errorf("failed to encode plugin response: %w", err)
	}

	var env struct {
		Status string                 `json:"status"`
		Error  string                 `json:"error"`
		Data   map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(rawBytes, &env); err == nil {
		if env.Status == "error" {
			if env.Error != "" {
				return nil, errors.New(env.Error)
			}
			return nil, errors.New("plugin execution returned status error")
		}
		if env.Data != nil {
			if stdoutRaw, ok := env.Data["stdout"].(string); ok {
				trimmed := strings.TrimSpace(stdoutRaw)
				if trimmed == "" {
					return nil, errors.New("plugin command returned empty output")
				}
				candidate := []byte(trimmed)
				if !json.Valid(candidate) {
					// If stdout has multiple lines, pick the last valid JSON line/object
					lines := strings.Split(trimmed, "\n")
					for i := len(lines) - 1; i >= 0; i-- {
						line := strings.TrimSpace(lines[i])
						if (strings.HasPrefix(line, "{") || strings.HasPrefix(line, "[")) && json.Valid([]byte(line)) {
							candidate = []byte(line)
							break
						}
					}
				}
				if err := checkUpstreamVaultError(candidate); err != nil {
					return nil, err
				}
				return candidate, nil
			}
			if dataBytes, err := json.Marshal(env.Data); err == nil && len(env.Data) > 0 {
				if err := checkUpstreamVaultError(dataBytes); err != nil {
					return nil, err
				}
				return dataBytes, nil
			}
		}
	}

	if err := checkUpstreamVaultError(rawBytes); err != nil {
		return nil, err
	}
	return rawBytes, nil
}

func checkUpstreamVaultError(data []byte) error {
	var errObj struct {
		Error            interface{} `json:"error"`
		ErrorDescription string      `json:"error_description"`
	}
	if err := json.Unmarshal(data, &errObj); err != nil || errObj.Error == nil {
		return nil
	}
	switch v := errObj.Error.(type) {
	case string:
		if v == "" {
			return nil
		}
		if errObj.ErrorDescription != "" {
			return fmt.Errorf("vault error (%s): %s", v, errObj.ErrorDescription)
		}
		return fmt.Errorf("vault error: %s", v)
	case map[string]interface{}:
		code, _ := v["code"].(string)
		msg, _ := v["message"].(string)
		if msg != "" || code != "" {
			return fmt.Errorf("vault error [%s]: %s", code, msg)
		}
	}
	return nil
}

func extractAzureKeyVaultSecretName(idOrName string) string {
	s := strings.TrimSpace(idOrName)
	if idx := strings.Index(s, "/secrets/"); idx != -1 {
		rest := s[idx+len("/secrets/"):]
		parts := strings.Split(rest, "/")
		if len(parts) > 0 && parts[0] != "" {
			return parts[0]
		}
	}
	return s
}

func applySecretValueToCredentials(resolved *models.ResolvedCredentials, rawValue string, contentType string, tags map[string]string) {
	trimmed := strings.TrimSpace(rawValue)
	if strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}") {
		var inner map[string]interface{}
		if err := json.Unmarshal([]byte(trimmed), &inner); err == nil {
			getStr := func(keys ...string) string {
				for _, k := range keys {
					if v, ok := inner[k].(string); ok && strings.TrimSpace(v) != "" {
						return v
					}
				}
				return ""
			}
			if pw := getStr("ssh_password", "password", "secret", "value"); pw != "" {
				resolved.SSHPassword = pw
			}
			if pk := getStr("ssh_private_key", "private_key", "privateKey"); pk != "" {
				resolved.SSHPrivateKey = pk
			}
			if pp := getStr("ssh_key_passphrase", "passphrase"); pp != "" {
				resolved.SSHKeyPassphrase = pp
			}
			if sp := getStr("sudo_password", "sudoPassword"); sp != "" {
				resolved.SudoPassword = sp
			}
			if usr := getStr("ssh_user", "username", "user"); usr != "" && resolved.SSHUser == "" {
				resolved.SSHUser = usr
			}
			if am := getStr("ssh_auth_method", "auth_method"); am != "" {
				resolved.SSHAuthMethod = am
			}
		}
	}

	if resolved.SSHPassword == "" && resolved.SSHPrivateKey == "" && rawValue != "" {
		ctLower := strings.ToLower(contentType)
		isPrivateKey := strings.Contains(rawValue, "PRIVATE KEY") ||
			strings.Contains(ctLower, "private-key") ||
			strings.Contains(ctLower, "ssh-key") ||
			(tags != nil && (tags["type"] == "ssh-key" || tags["auth_method"] == "key"))
		if isPrivateKey {
			resolved.SSHPrivateKey = rawValue
			resolved.SSHAuthMethod = "key"
		} else {
			resolved.SSHPassword = rawValue
			resolved.SSHAuthMethod = "password"
		}
	}

	if resolved.SSHUser == "" && tags != nil {
		if u := strings.TrimSpace(tags["ssh_user"]); u != "" {
			resolved.SSHUser = u
		} else if u := strings.TrimSpace(tags["username"]); u != "" {
			resolved.SSHUser = u
		}
	}
}

// ResolveCredentials implements docker.CredentialResolver by delegating to the target plugin
func (m *Manager) ResolveCredentials(ctx context.Context, req models.ResolveSecretRequest) (*models.ResolvedCredentials, error) {
	if strings.TrimSpace(req.CredentialPluginID) == "" {
		return nil, errors.New("missing credential_plugin_id in request")
	}

	var userID uint
	if claims := auth.GetUserFromContext(ctx); claims != nil {
		userID = claims.UserID
	}
	if userID == 0 && req.ServerID > 0 && m.db != nil {
		var srv models.DockerServer
		if err := m.db.First(&srv, req.ServerID).Error; err == nil {
			userID = srv.UserID
		}
	}

	res, err := m.CallPlugin(ctx, req.CredentialPluginID, models.PluginActionVaultResolveSecret, req, "system", "admin", userID)
	if err != nil {
		return nil, fmt.Errorf("plugin '%s' failed to resolve secret: %w", req.CredentialPluginID, err)
	}

	data, err := unwrapPluginVaultPayload(res)
	if err != nil {
		return nil, fmt.Errorf("plugin '%s' failed to resolve secret '%s': %w", req.CredentialPluginID, req.SecretReference, err)
	}

	var resolved models.ResolvedCredentials
	_ = json.Unmarshal(data, &resolved)

	if resolved.SSHPassword == "" && resolved.SSHPrivateKey == "" && resolved.TLSCert == "" {
		var akvSecret struct {
			Value       string            `json:"value"`
			ContentType string            `json:"contentType"`
			ID          string            `json:"id"`
			Tags        map[string]string `json:"tags"`
		}
		if err := json.Unmarshal(data, &akvSecret); err == nil && akvSecret.Value != "" {
			applySecretValueToCredentials(&resolved, akvSecret.Value, akvSecret.ContentType, akvSecret.Tags)
		}
	}

	if resolved.SSHPassword == "" && resolved.SSHPrivateKey == "" && resolved.TLSCert == "" {
		return nil, fmt.Errorf("plugin '%s' returned empty credentials for secret '%s'", req.CredentialPluginID, req.SecretReference)
	}

	if resolved.SSHAuthMethod == "" {
		if resolved.SSHPrivateKey != "" {
			resolved.SSHAuthMethod = "key"
		} else if resolved.SSHPassword != "" {
			resolved.SSHAuthMethod = "password"
		}
	}
	if resolved.SudoPassword == "" && resolved.SSHPassword != "" {
		resolved.SudoPassword = resolved.SSHPassword
	}
	if resolved.SSHUser == "" && req.SSHUser != "" {
		resolved.SSHUser = req.SSHUser
	}

	return &resolved, nil
}

// ListSecrets implements docker.CredentialResolver by asking the plugin to list available secrets in the vault
func (m *Manager) ListSecrets(ctx context.Context, req models.ListVaultSecretsRequest) (*models.ListVaultSecretsResponse, error) {
	if strings.TrimSpace(req.CredentialPluginID) == "" {
		return nil, errors.New("missing credential_plugin_id in request")
	}

	res, err := m.CallPlugin(ctx, req.CredentialPluginID, models.PluginActionVaultListSecrets, req, "system", "admin")
	if err != nil {
		return nil, fmt.Errorf("plugin '%s' failed to list secrets: %w", req.CredentialPluginID, err)
	}

	data, err := unwrapPluginVaultPayload(res)
	if err != nil {
		return nil, fmt.Errorf("plugin '%s' failed to list secrets: %w", req.CredentialPluginID, err)
	}

	var listResp models.ListVaultSecretsResponse
	if err := json.Unmarshal(data, &listResp); err == nil && listResp.Secrets != nil {
		return &listResp, nil
	}

	// Support Azure Key Vault REST response: {"value": [{"id": "https://.../secrets/<name>", "contentType": "...", "attributes": {"enabled": true, "updated": 1700000000}, "tags": {...}}]}
	var akvList struct {
		Value []struct {
			ID          string            `json:"id"`
			Name        string            `json:"name"`
			ContentType string            `json:"contentType"`
			Tags        map[string]string `json:"tags"`
			Attributes  struct {
				Enabled bool  `json:"enabled"`
				Updated int64 `json:"updated"`
			} `json:"attributes"`
		} `json:"value"`
	}
	if err := json.Unmarshal(data, &akvList); err == nil && akvList.Value != nil {
		primary := make([]models.VaultSecretItem, 0, len(akvList.Value))
		metadata := make([]models.VaultSecretItem, 0)
		for _, raw := range akvList.Value {
			name := raw.Name
			if name == "" {
				name = extractAzureKeyVaultSecretName(raw.ID)
			}
			updatedStr := ""
			if raw.Attributes.Updated > 0 {
				updatedStr = time.Unix(raw.Attributes.Updated, 0).UTC().Format(time.RFC3339)
			}
			item := models.VaultSecretItem{
				ID:          raw.ID,
				Name:        name,
				ContentType: raw.ContentType,
				Enabled:     raw.Attributes.Enabled,
				UpdatedAt:   updatedStr,
				Tags:        raw.Tags,
			}
			if strings.HasPrefix(name, "zzz-json-info-") {
				metadata = append(metadata, item)
			} else {
				primary = append(primary, item)
			}
		}
		return &models.ListVaultSecretsResponse{Secrets: append(primary, metadata...)}, nil
	}

	// Also support direct slice return from plugin: []models.VaultSecretItem
	var items []models.VaultSecretItem
	if errSlice := json.Unmarshal(data, &items); errSlice == nil {
		return &models.ListVaultSecretsResponse{Secrets: items}, nil
	}

	return nil, fmt.Errorf("invalid secret list format from plugin")
}

// PutSecret implements docker.CredentialResolver by storing or generating a secret in the external vault
func (m *Manager) PutSecret(ctx context.Context, req models.PutVaultSecretRequest) (*models.VaultSecretItem, error) {
	if strings.TrimSpace(req.CredentialPluginID) == "" {
		return nil, errors.New("missing credential_plugin_id in request")
	}

	res, err := m.CallPlugin(ctx, req.CredentialPluginID, models.PluginActionVaultPutSecret, req, "system", "admin")
	if err != nil {
		return nil, fmt.Errorf("plugin '%s' failed to put secret: %w", req.CredentialPluginID, err)
	}

	data, err := unwrapPluginVaultPayload(res)
	if err != nil {
		return nil, fmt.Errorf("plugin '%s' failed to put secret: %w", req.CredentialPluginID, err)
	}

	var item models.VaultSecretItem
	if err := json.Unmarshal(data, &item); err != nil {
		return nil, fmt.Errorf("invalid secret item format from plugin: %w", err)
	}

	var akvItem struct {
		ID         string `json:"id"`
		Attributes struct {
			Enabled bool  `json:"enabled"`
			Updated int64 `json:"updated"`
		} `json:"attributes"`
	}
	if err := json.Unmarshal(data, &akvItem); err == nil {
		if item.Name == "" {
			item.Name = extractAzureKeyVaultSecretName(akvItem.ID)
		}
		if item.Name == "" {
			item.Name = req.Name
		}
		if akvItem.Attributes.Updated > 0 {
			item.Enabled = akvItem.Attributes.Enabled
			item.UpdatedAt = time.Unix(akvItem.Attributes.Updated, 0).UTC().Format(time.RFC3339)
		}
	}

	return &item, nil
}

// TestVaultConnection implements docker.CredentialResolver by checking connectivity with the vault
func (m *Manager) TestVaultConnection(ctx context.Context, req models.TestVaultConnectionRequest) (*models.TestVaultConnectionResponse, error) {
	if strings.TrimSpace(req.CredentialPluginID) == "" {
		return nil, errors.New("missing credential_plugin_id in request")
	}

	res, err := m.CallPlugin(ctx, req.CredentialPluginID, models.PluginActionVaultTestConnection, req, "system", "admin")
	if err != nil {
		return nil, fmt.Errorf("plugin '%s' failed to test vault connection: %w", req.CredentialPluginID, err)
	}

	data, err := unwrapPluginVaultPayload(res)
	if err != nil {
		return &models.TestVaultConnectionResponse{
			Success: false,
			Message: err.Error(),
		}, nil
	}

	var testResp models.TestVaultConnectionResponse
	if err := json.Unmarshal(data, &testResp); err != nil {
		return nil, fmt.Errorf("invalid test response format from plugin: %w", err)
	}

	if !testResp.Success && testResp.Message == "" {
		var akvList struct {
			Value []interface{} `json:"value"`
		}
		if err := json.Unmarshal(data, &akvList); err == nil && akvList.Value != nil {
			testResp.Success = true
			testResp.Message = "Connected to Azure Key Vault"
		}
	}

	return &testResp, nil
}

// syncAllPluginServerBindings synchronizes any existing server_bindings from plugin KV configs into docker_servers
func (m *Manager) syncAllPluginServerBindings() {
	if m.db == nil {
		return
	}
	var kvs []models.PluginKVStore
	if err := m.db.Where("key = ? AND user_id > 0", "azure_kv_config").Find(&kvs).Error; err != nil {
		return
	}
	for _, kv := range kvs {
		m.syncPluginServerBindings(kv.PluginID, kv.Value, kv.UserID)
	}
}

func extractHostFromURL(rawURL string) string {
	h := strings.TrimSpace(rawURL)
	for _, prefix := range []string{"ssh://", "tcp://", "http://", "https://"} {
		if strings.HasPrefix(strings.ToLower(h), prefix) {
			h = h[len(prefix):]
			break
		}
	}
	if atIdx := strings.LastIndex(h, "@"); atIdx >= 0 {
		h = h[atIdx+1:]
	}
	if colIdx := strings.Index(h, ":"); colIdx >= 0 {
		h = h[:colIdx]
	}
	if slashIdx := strings.Index(h, "/"); slashIdx >= 0 {
		h = h[:slashIdx]
	}
	return strings.TrimSpace(h)
}

// syncPluginServerBindings updates docker_servers with credential_source='plugin', credential_plugin_id, and secret_reference
// whenever a server is bound in the Azure Key Vault plugin configuration.
func (m *Manager) syncPluginServerBindings(pluginID string, configJSON string, userIDs ...uint) {
	if m.db == nil || strings.TrimSpace(configJSON) == "" {
		return
	}
	var userID uint
	if len(userIDs) > 0 {
		userID = userIDs[0]
	}
	var cfg struct {
		ServerBindings map[string]string `json:"server_bindings"`
	}
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil || len(cfg.ServerBindings) == 0 {
		return
	}

	var servers []models.DockerServer
	q := m.db
	if userID > 0 {
		q = q.Where("user_id = ?", userID)
	}
	if err := q.Find(&servers).Error; err != nil {
		return
	}

	for srvKey, secretName := range cfg.ServerBindings {
		srvKey = strings.TrimSpace(srvKey)
		secretName = strings.TrimSpace(secretName)
		if srvKey == "" || secretName == "" {
			continue
		}

		var parsedID uint64
		if idVal, err := strconv.ParseUint(srvKey, 10, 64); err == nil {
			parsedID = idVal
		}

		for _, srv := range servers {
			matched := false
			if parsedID > 0 {
				matched = uint64(srv.ID) == parsedID
			} else {
				srvHost := extractHostFromURL(srv.HostURL)
				if strings.EqualFold(srv.Name, srvKey) || strings.EqualFold(srv.Name, "srv-"+srvKey) {
					matched = true
				} else if srvHost != "" && strings.EqualFold(srvHost, srvKey) {
					matched = true
				}
			}

			if matched {
				if srv.CredentialSource != "plugin" || srv.CredentialPluginID != pluginID || srv.SecretReference != secretName {
					m.db.Model(&models.DockerServer{}).Where("id = ?", srv.ID).Updates(map[string]interface{}{
						"credential_source":    "plugin",
						"credential_plugin_id": pluginID,
						"secret_reference":     secretName,
					})
					if m.dockerService != nil {
						m.dockerService.InvalidateClientCache(srv.ID)
					}
				}
			}
		}
	}
}

// executeKVVaultResolve retrieves a secret stored in KV storage for testing/mocking
func (m *Manager) executeKVVaultResolve(pluginID string, payload interface{}, userIDs ...uint) (interface{}, error) {
	var userID uint
	if len(userIDs) > 0 {
		userID = userIDs[0]
	}

	payloadBytes, _ := json.Marshal(payload)
	var req models.ResolveSecretRequest
	_ = json.Unmarshal(payloadBytes, &req)

	ref := req.SecretReference
	if strings.Contains(ref, "/secrets/") {
		parts := strings.Split(ref, "/secrets/")
		if len(parts) > 1 {
			ref = parts[1]
		}
	}

	secretKey := fmt.Sprintf("vault:secret:%s", ref)
	var kv models.PluginKVStore
	query := m.db.Where("plugin_id = ? AND (key = ? OR key = ? OR key = ?) AND user_id = ?", pluginID, secretKey, ref, req.SecretReference, userID)
	if err := query.First(&kv).Error; err != nil {
		return nil, fmt.Errorf("secret '%s' not found in vault storage", req.SecretReference)
	}

	var resolved models.ResolvedCredentials
	if err := json.Unmarshal([]byte(kv.Value), &resolved); err != nil {
		// If stored as raw string, assume it's private key or password based on content
		val := kv.Value
		if strings.Contains(val, "PRIVATE KEY") {
			resolved.SSHPrivateKey = val
			resolved.SSHAuthMethod = "key"
		} else {
			resolved.SSHPassword = val
			resolved.SSHAuthMethod = "password"
		}
	}

	if resolved.SSHUser == "" {
		resolved.SSHUser = req.SSHUser
	}

	return resolved, nil
}

// executeKVVaultList lists secrets stored in KV storage for testing/mocking
func (m *Manager) executeKVVaultList(pluginID string, userIDs ...uint) (interface{}, error) {
	var userID uint
	if len(userIDs) > 0 {
		userID = userIDs[0]
	}

	var kvs []models.PluginKVStore
	query := m.db.Where("plugin_id = ? AND key LIKE ? AND user_id = ?", pluginID, "vault:secret:%", userID)
	if err := query.Find(&kvs).Error; err != nil {
		return models.ListVaultSecretsResponse{Secrets: []models.VaultSecretItem{}}, nil
	}

	items := make([]models.VaultSecretItem, 0, len(kvs))
	for _, kv := range kvs {
		name := strings.TrimPrefix(kv.Key, "vault:secret:")
		items = append(items, models.VaultSecretItem{
			ID:        name,
			Name:      name,
			Enabled:   true,
			UpdatedAt: kv.UpdatedAt.Format(time.RFC3339),
		})
	}

	return models.ListVaultSecretsResponse{Secrets: items}, nil
}

// executeKVVaultPut writes a secret to KV storage for testing/mocking
func (m *Manager) executeKVVaultPut(pluginID string, payload interface{}, userIDs ...uint) (interface{}, error) {
	var userID uint
	if len(userIDs) > 0 {
		userID = userIDs[0]
	}

	payloadBytes, _ := json.Marshal(payload)
	var req models.PutVaultSecretRequest
	_ = json.Unmarshal(payloadBytes, &req)

	if strings.TrimSpace(req.Name) == "" {
		return nil, errors.New("secret name is required")
	}

	secretKey := fmt.Sprintf("vault:secret:%s", req.Name)
	var kv models.PluginKVStore
	query := m.db.Where("plugin_id = ? AND key = ?", pluginID, secretKey)
	if userID > 0 {
		query = query.Where("user_id = ?", userID)
	} else {
		query = query.Where("user_id = 0")
	}
	if err := query.First(&kv).Error; err != nil {
		kv = models.PluginKVStore{PluginID: pluginID, Key: secretKey, Value: req.Value, UserID: userID, UpdatedAt: time.Now()}
		m.db.Create(&kv)
	} else {
		m.db.Model(&kv).Updates(map[string]interface{}{"value": req.Value, "updated_at": time.Now()})
	}

	return models.VaultSecretItem{
		ID:          req.Name,
		Name:        req.Name,
		ContentType: req.ContentType,
		Enabled:     true,
		UpdatedAt:   time.Now().Format(time.RFC3339),
	}, nil
}

