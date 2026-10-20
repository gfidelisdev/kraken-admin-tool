package host

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/kraken/admin-tool/backend/internal/audit"
	"github.com/kraken/admin-tool/backend/internal/auth"
	krakencrypto "github.com/kraken/admin-tool/backend/internal/crypto"
	"github.com/kraken/admin-tool/backend/internal/docker"
	"github.com/kraken/admin-tool/backend/internal/models"
	"gorm.io/gorm"
)

type Handler struct {
	DB            *gorm.DB
	DockerService *docker.DockerService
}

func NewHandler(db *gorm.DB, dockerService *docker.DockerService) *Handler {
	return &Handler{
		DB:            db,
		DockerService: dockerService,
	}
}

// resolveServerAndExecutor resolves the server and constructs an appropriate Executor (SSH or Local)
func (h *Handler) resolveServerAndExecutor(r *http.Request, serverID uint) (*models.DockerServer, Executor, error) {
	var server models.DockerServer
	if err := h.DB.First(&server, serverID).Error; err != nil {
		return nil, nil, errors.New("server not found")
	}

	cleanHost := strings.TrimSpace(server.HostURL)
	if docker.IsSSHHost(cleanHost) {
		// Extract transient SSH credentials if server is in always_prompt mode
		transientPassword := r.Header.Get("X-Host-Password")
		transientKey := r.Header.Get("X-Host-Key")
		transientPassphrase := r.Header.Get("X-Host-Key-Passphrase")

		var overrides []docker.SSHConfig
		if transientPassword != "" || transientKey != "" {
			overrides = append(overrides, docker.SSHConfig{
				Password:      transientPassword,
				PrivateKey:    transientKey,
				KeyPassphrase: transientPassphrase,
			})
		}

		client, _, err := h.DockerService.GetSSHClient(r.Context(), serverID, overrides...)
		if err != nil {
			return &server, nil, err
		}
		return &server, NewSSHExecutor(client), nil
	}

	// Local host or non-SSH (socket)
	return &server, NewLocalExecutor(), nil
}

// GetPackageStatusHandler retrieves the current package updates status from the database
func (h *Handler) GetPackageStatusHandler(w http.ResponseWriter, r *http.Request) {
	idParam := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(idParam, 10, 32)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid server ID"})
		return
	}

	var server models.DockerServer
	if err := h.DB.First(&server, id).Error; err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Server not found"})
		return
	}

	var updates []models.HostPackageUpdate
	h.DB.Where("server_id = ?", id).Order("is_security desc, name asc").Find(&updates)

	pkgUpdates := make([]models.PackageUpdate, 0)
	securityCount := 0
	for _, u := range updates {
		if u.IsSecurity {
			securityCount++
		}
		pkgUpdates = append(pkgUpdates, models.PackageUpdate{
			Name:           u.Name,
			CurrentVersion: u.CurrentVersion,
			NewVersion:     u.NewVersion,
			Repository:     u.Repository,
			IsSecurity:     u.IsSecurity,
			Description:    u.Description,
		})
	}

	lastChecked := time.Time{}
	if server.PackagesLastChecked != nil {
		lastChecked = *server.PackagesLastChecked
	}

	hasSavedSudo := server.SudoPassword != ""
	requiresSudo := server.SSHUser != "root" && !strings.EqualFold(server.SSHUser, "root")

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(models.HostPackageStatus{
		ServerID:             server.ID,
		ServerName:           server.Name,
		PackageManager:       server.PackageManager,
		UpdatesCount:         len(pkgUpdates),
		SecurityCount:        securityCount,
		Updates:              pkgUpdates,
		LastChecked:          lastChecked,
		RequiresSudo:         requiresSudo,
		SudoPolicy:           server.SudoPolicy,
		HasSavedSudoPassword: hasSavedSudo,
	})
}

// CheckPackagesHandler probes the host for updates and refreshes the database
func (h *Handler) CheckPackagesHandler(w http.ResponseWriter, r *http.Request) {
	idParam := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(idParam, 10, 32)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid server ID"})
		return
	}

	var req models.CheckPackagesRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	server, exec, err := h.resolveServerAndExecutor(r, uint(id))
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to connect to host: " + err.Error()})
		return
	}

	// Resolve sudo password based on policy and request
	sudoPassword := req.SudoPassword
	if sudoPassword == "" {
		sudoPassword = r.Header.Get("X-Sudo-Password")
	}
	if sudoPassword == "" && server.SudoPolicy == "save_securely" && server.SudoPassword != "" {
		decrypted, decErr := krakencrypto.Decrypt(server.SudoPassword)
		if decErr == nil {
			sudoPassword = decrypted
		}
	}

	// Detect or use package manager
	pkgManager := strings.ToLower(strings.TrimSpace(req.PackageManager))
	if pkgManager != "" && pkgManager != "unknown" {
		if !IsValidPackageManager(pkgManager) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Unsupported package manager: " + pkgManager})
			return
		}
		h.DB.Model(server).Update("package_manager", pkgManager)
		server.PackageManager = pkgManager
	} else {
		pkgManager = server.PackageManager
	}

	if pkgManager == "" || pkgManager == "unknown" {
		detected, dErr := DetectPackageManager(r.Context(), exec)
		if dErr == nil && detected != "unknown" {
			pkgManager = detected
			h.DB.Model(server).Update("package_manager", pkgManager)
			server.PackageManager = pkgManager
		} else {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"error": "Could not identify package manager on host. Please select one manually from the dropdown.",
				"code":  "PACKAGE_MANAGER_UNKNOWN",
				"available_package_managers": []string{"apt", "dnf", "yum", "apk", "pacman", "zypper"},
			})
			return
		}
	}

	// If refreshing repositories requested, sudo might be needed
	if req.RefreshRepos {
		if sudoPassword == "" && server.SSHUser != "root" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"error":          "Sudo password required to refresh package repositories",
				"code":           "SUDO_PASSWORD_REQUIRED",
				"sudo_policy":    server.SudoPolicy,
				"requires_sudo":  true,
			})
			return
		}

		if err := RefreshPackageRepositories(r.Context(), exec, pkgManager, sudoPassword); err != nil {
			if errors.Is(err, ErrInvalidSudoPassword) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"error": "Invalid sudo password",
					"code":  "INVALID_SUDO_PASSWORD",
				})
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Repository refresh failed: " + err.Error()})
			return
		}
	}

	// Handle sudo policy saving if user decided on this request
	if req.SaveSudoPolicy == "save_securely" && req.SudoPassword != "" {
		enc, _ := krakencrypto.Encrypt(req.SudoPassword)
		h.DB.Model(server).Updates(map[string]interface{}{
			"sudo_policy":   "save_securely",
			"sudo_password": enc,
		})
		server.SudoPolicy = "save_securely"
		server.SudoPassword = enc
	} else if req.SaveSudoPolicy == "always_prompt" {
		h.DB.Model(server).Updates(map[string]interface{}{
			"sudo_policy":   "always_prompt",
			"sudo_password": "",
		})
		server.SudoPolicy = "always_prompt"
		server.SudoPassword = ""
	}

	// Check available updates
	updates, err := CheckAvailableUpdates(r.Context(), exec, pkgManager)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to check package updates: " + err.Error()})
		return
	}

	// Save updates to DB
	now := time.Now()
	h.DB.Where("server_id = ?", server.ID).Delete(&models.HostPackageUpdate{})

	secCount := 0
	for _, u := range updates {
		if u.IsSecurity {
			secCount++
		}
		h.DB.Create(&models.HostPackageUpdate{
			ServerID:       server.ID,
			Name:           u.Name,
			CurrentVersion: u.CurrentVersion,
			NewVersion:     u.NewVersion,
			Repository:     u.Repository,
			IsSecurity:     u.IsSecurity,
			Description:    u.Description,
			CreatedAt:      now,
		})
	}

	h.DB.Model(server).Updates(map[string]interface{}{
		"packages_last_checked":   &now,
		"available_updates_count": len(updates),
		"security_updates_count":  secCount,
		"package_manager":         pkgManager,
	})

	claims := auth.GetUserFromContext(r.Context())
	userID := uint(0)
	username := "system"
	if claims != nil {
		userID = claims.UserID
		username = claims.Username
	}
	audit.Log(h.DB, userID, username, "CHECK_PACKAGES", "host", server.Name,
		"Detected available package updates: "+strconv.Itoa(len(updates)), "SUCCESS")

	if updates == nil {
		updates = make([]models.PackageUpdate, 0)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(models.HostPackageStatus{
		ServerID:             server.ID,
		ServerName:           server.Name,
		PackageManager:       pkgManager,
		UpdatesCount:         len(updates),
		SecurityCount:        secCount,
		Updates:              updates,
		LastChecked:          now,
		RequiresSudo:         server.SSHUser != "root",
		SudoPolicy:           server.SudoPolicy,
		HasSavedSudoPassword: server.SudoPassword != "",
	})
}

// UpgradePackagesHandler performs installation of package updates
func (h *Handler) UpgradePackagesHandler(w http.ResponseWriter, r *http.Request) {
	idParam := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(idParam, 10, 32)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid server ID"})
		return
	}

	var req models.UpgradePackagesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request body"})
		return
	}

	server, exec, err := h.resolveServerAndExecutor(r, uint(id))
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to connect to host: " + err.Error()})
		return
	}

	// Resolve sudo password
	sudoPassword := req.SudoPassword
	if sudoPassword == "" {
		sudoPassword = r.Header.Get("X-Sudo-Password")
	}
	if sudoPassword == "" && server.SudoPolicy == "save_securely" && server.SudoPassword != "" {
		decrypted, decErr := krakencrypto.Decrypt(server.SudoPassword)
		if decErr == nil {
			sudoPassword = decrypted
		}
	}

	if sudoPassword == "" && server.SSHUser != "root" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error":          "Sudo password required to upgrade packages",
			"code":           "SUDO_PASSWORD_REQUIRED",
			"sudo_policy":    server.SudoPolicy,
			"requires_sudo":  true,
		})
		return
	}

	// Update policy if requested
	if req.SaveSudoPolicy == "save_securely" && req.SudoPassword != "" {
		enc, _ := krakencrypto.Encrypt(req.SudoPassword)
		h.DB.Model(server).Updates(map[string]interface{}{
			"sudo_policy":   "save_securely",
			"sudo_password": enc,
		})
		server.SudoPolicy = "save_securely"
		server.SudoPassword = enc
	} else if req.SaveSudoPolicy == "always_prompt" {
		h.DB.Model(server).Updates(map[string]interface{}{
			"sudo_policy":   "always_prompt",
			"sudo_password": "",
		})
		server.SudoPolicy = "always_prompt"
		server.SudoPassword = ""
	}

	pkgManager := strings.ToLower(strings.TrimSpace(req.PackageManager))
	if pkgManager != "" && pkgManager != "unknown" {
		if !IsValidPackageManager(pkgManager) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Unsupported package manager: " + pkgManager})
			return
		}
		h.DB.Model(server).Update("package_manager", pkgManager)
		server.PackageManager = pkgManager
	} else {
		pkgManager = server.PackageManager
	}

	if pkgManager == "" || pkgManager == "unknown" {
		detected, dErr := DetectPackageManager(r.Context(), exec)
		if dErr == nil && detected != "unknown" {
			pkgManager = detected
			h.DB.Model(server).Update("package_manager", pkgManager)
			server.PackageManager = pkgManager
		} else {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"error": "Could not identify package manager on host. Please select one manually from the dropdown.",
				"code":  "PACKAGE_MANAGER_UNKNOWN",
				"available_package_managers": []string{"apt", "dnf", "yum", "apk", "pacman", "zypper"},
			})
			return
		}
	}

	output, err := UpgradePackages(r.Context(), exec, pkgManager, req.Packages, sudoPassword)
	if err != nil {
		if errors.Is(err, ErrInvalidSudoPassword) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"error": "Invalid sudo password",
				"code":  "INVALID_SUDO_PASSWORD",
			})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Package upgrade failed: " + err.Error()})
		return
	}

	// Refresh remaining updates count
	remaining, _ := CheckAvailableUpdates(r.Context(), exec, pkgManager)
	now := time.Now()
	h.DB.Where("server_id = ?", server.ID).Delete(&models.HostPackageUpdate{})
	secCount := 0
	for _, u := range remaining {
		if u.IsSecurity {
			secCount++
		}
		h.DB.Create(&models.HostPackageUpdate{
			ServerID:       server.ID,
			Name:           u.Name,
			CurrentVersion: u.CurrentVersion,
			NewVersion:     u.NewVersion,
			Repository:     u.Repository,
			IsSecurity:     u.IsSecurity,
			CreatedAt:      now,
		})
	}

	h.DB.Model(server).Updates(map[string]interface{}{
		"packages_last_checked":   &now,
		"available_updates_count": len(remaining),
		"security_updates_count":  secCount,
	})

	claims := auth.GetUserFromContext(r.Context())
	userID := uint(0)
	username := "system"
	if claims != nil {
		userID = claims.UserID
		username = claims.Username
	}
	audit.Log(h.DB, userID, username, "UPGRADE_PACKAGES", "host", server.Name,
		"Upgraded packages on host successfully", "SUCCESS")

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"message":                 "Packages upgraded successfully",
		"output":                  output,
		"remaining_updates_count": len(remaining),
		"security_updates_count":  secCount,
	})
}

// UpgradePackagesStreamHandler streams live package upgrade output and delivers a final result
func (h *Handler) UpgradePackagesStreamHandler(w http.ResponseWriter, r *http.Request) {
	idParam := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(idParam, 10, 32)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid server ID"})
		return
	}

	var req models.UpgradePackagesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request body"})
		return
	}

	server, exec, err := h.resolveServerAndExecutor(r, uint(id))
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to connect to host: " + err.Error()})
		return
	}

	// Resolve sudo password
	sudoPassword := req.SudoPassword
	if sudoPassword == "" {
		sudoPassword = r.Header.Get("X-Sudo-Password")
	}
	if sudoPassword == "" && server.SudoPolicy == "save_securely" && server.SudoPassword != "" {
		decrypted, decErr := krakencrypto.Decrypt(server.SudoPassword)
		if decErr == nil {
			sudoPassword = decrypted
		}
	}

	if sudoPassword == "" && server.SSHUser != "root" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error":         "Sudo password required to upgrade packages",
			"code":          "SUDO_PASSWORD_REQUIRED",
			"sudo_policy":   server.SudoPolicy,
			"requires_sudo": true,
		})
		return
	}

	// Update policy if requested
	if req.SaveSudoPolicy == "save_securely" && req.SudoPassword != "" {
		enc, _ := krakencrypto.Encrypt(req.SudoPassword)
		h.DB.Model(server).Updates(map[string]interface{}{
			"sudo_policy":   "save_securely",
			"sudo_password": enc,
		})
		server.SudoPolicy = "save_securely"
		server.SudoPassword = enc
	} else if req.SaveSudoPolicy == "always_prompt" {
		h.DB.Model(server).Updates(map[string]interface{}{
			"sudo_policy":   "always_prompt",
			"sudo_password": "",
		})
		server.SudoPolicy = "always_prompt"
		server.SudoPassword = ""
	}

	pkgManager := strings.ToLower(strings.TrimSpace(req.PackageManager))
	if pkgManager != "" && pkgManager != "unknown" {
		if !IsValidPackageManager(pkgManager) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Unsupported package manager: " + pkgManager})
			return
		}
		h.DB.Model(server).Update("package_manager", pkgManager)
		server.PackageManager = pkgManager
	} else {
		pkgManager = server.PackageManager
	}

	if pkgManager == "" || pkgManager == "unknown" {
		detected, dErr := DetectPackageManager(r.Context(), exec)
		if dErr == nil && detected != "unknown" {
			pkgManager = detected
			h.DB.Model(server).Update("package_manager", pkgManager)
			server.PackageManager = pkgManager
		} else {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"error":                      "Could not identify package manager on host. Please select one manually from the dropdown.",
				"code":                       "PACKAGE_MANAGER_UNKNOWN",
				"available_package_managers": []string{"apt", "dnf", "yum", "apk", "pacman", "zypper"},
			})
			return
		}
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Streaming unsupported"})
		return
	}

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	sendEvent := func(evt map[string]interface{}) {
		data, err := json.Marshal(evt)
		if err == nil {
			_, _ = w.Write(data)
			_, _ = w.Write([]byte("\n"))
			flusher.Flush()
		}
	}

	// Initial informational event
	initMsg := fmt.Sprintf("Iniciando atualização de pacotes usando gerenciador '%s'...", pkgManager)
	if len(req.Packages) > 0 {
		initMsg = fmt.Sprintf("Iniciando atualização de %d pacote(s) [%s] usando '%s'...", len(req.Packages), strings.Join(req.Packages, ", "), pkgManager)
	}
	sendEvent(map[string]interface{}{
		"type":      "log",
		"message":   initMsg,
		"level":     "info",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})

	var lineBuf strings.Builder
	var mu sync.Mutex

	onChunk := func(chunk string) {
		mu.Lock()
		defer mu.Unlock()
		lineBuf.WriteString(chunk)
		str := lineBuf.String()
		for {
			idx := strings.Index(str, "\n")
			if idx == -1 {
				lineBuf.Reset()
				lineBuf.WriteString(str)
				break
			}
			line := str[:idx]
			str = str[idx+1:]
			lineBuf.Reset()
			lineBuf.WriteString(str)

			sendEvent(map[string]interface{}{
				"type":      "log",
				"message":   line,
				"level":     "stdout",
				"timestamp": time.Now().UTC().Format(time.RFC3339),
			})
		}
	}

	output, upgradeErr := UpgradePackagesStream(r.Context(), exec, pkgManager, req.Packages, sudoPassword, onChunk)

	// Flush any trailing output in lineBuf
	mu.Lock()
	if lineBuf.Len() > 0 {
		sendEvent(map[string]interface{}{
			"type":      "log",
			"message":   lineBuf.String(),
			"level":     "stdout",
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		})
		lineBuf.Reset()
	}
	mu.Unlock()

	claims := auth.GetUserFromContext(r.Context())
	userID := uint(0)
	username := "system"
	if claims != nil {
		userID = claims.UserID
		username = claims.Username
	}

	if upgradeErr != nil {
		audit.Log(h.DB, userID, username, "UPGRADE_PACKAGES", "host", server.Name,
			"Package upgrade failed: "+upgradeErr.Error(), "FAILED")

		resPayload := map[string]interface{}{
			"type":    "result",
			"success": false,
			"message": "Falha na atualização dos pacotes: " + upgradeErr.Error(),
			"output":  output,
		}
		if errors.Is(upgradeErr, ErrInvalidSudoPassword) {
			resPayload["code"] = "INVALID_SUDO_PASSWORD"
			resPayload["message"] = "Senha sudo inválida"
		}
		sendEvent(resPayload)
		return
	}

	// Refresh remaining updates count
	remaining, _ := CheckAvailableUpdates(r.Context(), exec, pkgManager)
	now := time.Now()
	h.DB.Where("server_id = ?", server.ID).Delete(&models.HostPackageUpdate{})
	secCount := 0
	for _, u := range remaining {
		if u.IsSecurity {
			secCount++
		}
		h.DB.Create(&models.HostPackageUpdate{
			ServerID:       server.ID,
			Name:           u.Name,
			CurrentVersion: u.CurrentVersion,
			NewVersion:     u.NewVersion,
			Repository:     u.Repository,
			IsSecurity:     u.IsSecurity,
			CreatedAt:      now,
		})
	}

	h.DB.Model(server).Updates(map[string]interface{}{
		"packages_last_checked":   &now,
		"available_updates_count": len(remaining),
		"security_updates_count":  secCount,
	})

	audit.Log(h.DB, userID, username, "UPGRADE_PACKAGES", "host", server.Name,
		"Upgraded packages on host successfully", "SUCCESS")

	sendEvent(map[string]interface{}{
		"type":                    "result",
		"success":                 true,
		"message":                 "Pacotes atualizados com sucesso",
		"output":                  output,
		"remaining_updates_count": len(remaining),
		"security_updates_count":  secCount,
	})
}

// UpdateSudoPolicyHandler updates the sudo credential retention policy
func (h *Handler) UpdateSudoPolicyHandler(w http.ResponseWriter, r *http.Request) {
	idParam := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(idParam, 10, 32)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid server ID"})
		return
	}

	var req models.UpdateSudoPolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request body"})
		return
	}

	var server models.DockerServer
	if err := h.DB.First(&server, id).Error; err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Server not found"})
		return
	}

	updates := map[string]interface{}{}
	if req.SudoPolicy == "save_securely" {
		if req.SudoPassword == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Sudo password required when choosing 'save_securely'"})
			return
		}
		enc, _ := krakencrypto.Encrypt(req.SudoPassword)
		updates["sudo_policy"] = "save_securely"
		updates["sudo_password"] = enc
	} else if req.SudoPolicy == "always_prompt" {
		updates["sudo_policy"] = "always_prompt"
		updates["sudo_password"] = ""
	} else {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid sudo policy: must be 'always_prompt' or 'save_securely'"})
		return
	}

	h.DB.Model(&server).Updates(updates)

	claims := auth.GetUserFromContext(r.Context())
	userID := uint(0)
	username := "system"
	if claims != nil {
		userID = claims.UserID
		username = claims.Username
	}
	audit.Log(h.DB, userID, username, "UPDATE_SUDO_POLICY", "host", server.Name,
		"Updated sudo policy to: "+req.SudoPolicy, "SUCCESS")

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"message":                 "Sudo policy updated successfully",
		"sudo_policy":            req.SudoPolicy,
		"has_saved_sudo_password": req.SudoPolicy == "save_securely",
	})
}

// ClearSudoPasswordHandler deletes stored sudo password and resets policy to always_prompt
func (h *Handler) ClearSudoPasswordHandler(w http.ResponseWriter, r *http.Request) {
	idParam := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(idParam, 10, 32)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid server ID"})
		return
	}

	var server models.DockerServer
	if err := h.DB.First(&server, id).Error; err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Server not found"})
		return
	}

	h.DB.Model(&server).Updates(map[string]interface{}{
		"sudo_policy":   "always_prompt",
		"sudo_password": "",
	})

	claims := auth.GetUserFromContext(r.Context())
	userID := uint(0)
	username := "system"
	if claims != nil {
		userID = claims.UserID
		username = claims.Username
	}
	audit.Log(h.DB, userID, username, "CLEAR_SUDO_PASSWORD", "host", server.Name,
		"Cleared saved sudo password and reset policy to always_prompt", "SUCCESS")

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "Sudo password cleared successfully"})
}

// UpdatePackageManagerHandler sets or overrides the package manager for a host
func (h *Handler) UpdatePackageManagerHandler(w http.ResponseWriter, r *http.Request) {
	idParam := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(idParam, 10, 32)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid server ID"})
		return
	}

	var req models.UpdatePackageManagerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request body"})
		return
	}

	pm := strings.ToLower(strings.TrimSpace(req.PackageManager))
	if !IsValidPackageManager(pm) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": "Unsupported package manager. Must be one of: apt, dnf, yum, apk, pacman, zypper, unknown, or empty",
		})
		return
	}

	var server models.DockerServer
	if err := h.DB.First(&server, id).Error; err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Server not found"})
		return
	}

	h.DB.Model(&server).Update("package_manager", pm)

	claims := auth.GetUserFromContext(r.Context())
	userID := uint(0)
	username := "system"
	if claims != nil {
		userID = claims.UserID
		username = claims.Username
	}
	audit.Log(h.DB, userID, username, "UPDATE_PACKAGE_MANAGER", "host", server.Name,
		"Set package manager to: "+pm, "SUCCESS")

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"message":         "Package manager updated successfully",
		"package_manager": pm,
	})
}
