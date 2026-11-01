package host

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
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
	JobMgr        *JobManager
}

func NewHandler(db *gorm.DB, dockerService *docker.DockerService) *Handler {
	return &Handler{
		DB:            db,
		DockerService: dockerService,
		JobMgr:        GetJobManager(),
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

	// Auto-detect or heal local server package manager if erroneously set to container's apk
	if !docker.IsSSHHost(server.HostURL) && (server.PackageManager == "" || server.PackageManager == "apk" || server.PackageManager == "unknown") {
		exec := NewLocalExecutor()
		if detected, err := DetectPackageManager(r.Context(), exec); err == nil && detected != "unknown" {
			if detected != server.PackageManager {
				server.PackageManager = detected
				h.DB.Model(&server).Update("package_manager", detected)
			}
		}
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

	isLocalHost := !docker.IsSSHHost(server.HostURL)
	hasSavedSudo := server.SudoPassword != ""
	requiresSudo := !isLocalHost && server.SSHUser != "root" && !strings.EqualFold(server.SSHUser, "root")
	sudoPolicy := server.SudoPolicy
	if isLocalHost {
		requiresSudo = false
		sudoPolicy = "not_required"
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(models.HostPackageStatus{
		ServerID:              server.ID,
		ServerName:            server.Name,
		PackageManager:        server.PackageManager,
		UpdatesCount:          len(pkgUpdates),
		SecurityCount:         securityCount,
		AvailableUpdatesCount: len(pkgUpdates),
		SecurityUpdatesCount:  securityCount,
		Updates:               pkgUpdates,
		LastChecked:           lastChecked,
		RequiresSudo:          requiresSudo,
		SudoPolicy:            sudoPolicy,
		HasSavedSudoPassword:  hasSavedSudo,
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

	isLocalHost := !docker.IsSSHHost(server.HostURL)

	// Resolve sudo password based on policy and request
	sudoPassword := req.SudoPassword
	if sudoPassword == "" {
		sudoPassword = r.Header.Get("X-Sudo-Password")
	}
	if sudoPassword == "" && server.SudoPassword != "" && server.SudoPolicy != "always_prompt" {
		decrypted, decErr := krakencrypto.Decrypt(server.SudoPassword)
		if decErr == nil {
			sudoPassword = decrypted
			if server.SudoPolicy == "" {
				server.SudoPolicy = "save_securely"
				h.DB.Model(server).Update("sudo_policy", "save_securely")
			}
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

	if pkgManager == "" || pkgManager == "unknown" || (isLocalHost && pkgManager == "apk") {
		detected, dErr := DetectPackageManager(r.Context(), exec)
		if dErr == nil && detected != "unknown" {
			pkgManager = detected
			h.DB.Model(server).Update("package_manager", pkgManager)
			server.PackageManager = pkgManager
		} else if pkgManager == "" || pkgManager == "unknown" {
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
		if !isLocalHost && sudoPassword == "" && server.SSHUser != "" && !strings.EqualFold(server.SSHUser, "root") {
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

	requiresSudo := !isLocalHost && server.SSHUser != "root" && !strings.EqualFold(server.SSHUser, "root")
	sudoPolicy := server.SudoPolicy
	if isLocalHost {
		requiresSudo = false
		sudoPolicy = "not_required"
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(models.HostPackageStatus{
		ServerID:              server.ID,
		ServerName:            server.Name,
		PackageManager:        pkgManager,
		UpdatesCount:          len(updates),
		SecurityCount:         secCount,
		AvailableUpdatesCount: len(updates),
		SecurityUpdatesCount:  secCount,
		Updates:               updates,
		LastChecked:           now,
		RequiresSudo:          requiresSudo,
		SudoPolicy:            sudoPolicy,
		HasSavedSudoPassword:  server.SudoPassword != "",
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

	isLocalHost := !docker.IsSSHHost(server.HostURL)

	// Resolve sudo password
	sudoPassword := req.SudoPassword
	if sudoPassword == "" {
		sudoPassword = r.Header.Get("X-Sudo-Password")
	}
	if sudoPassword == "" && server.SudoPassword != "" && server.SudoPolicy != "always_prompt" {
		decrypted, decErr := krakencrypto.Decrypt(server.SudoPassword)
		if decErr == nil {
			sudoPassword = decrypted
			if server.SudoPolicy == "" {
				server.SudoPolicy = "save_securely"
				h.DB.Model(server).Update("sudo_policy", "save_securely")
			}
		}
	}

	if !isLocalHost && sudoPassword == "" && server.SSHUser != "" && !strings.EqualFold(server.SSHUser, "root") {
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

	if pkgManager == "" || pkgManager == "unknown" || (isLocalHost && pkgManager == "apk") {
		detected, dErr := DetectPackageManager(r.Context(), exec)
		if dErr == nil && detected != "unknown" {
			pkgManager = detected
			h.DB.Model(server).Update("package_manager", pkgManager)
			server.PackageManager = pkgManager
		} else if pkgManager == "" || pkgManager == "unknown" {
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

// GetActiveUpgradeJobHandler returns the status of any active or recently finished background upgrade job
func (h *Handler) GetActiveUpgradeJobHandler(w http.ResponseWriter, r *http.Request) {
	idParam := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(idParam, 10, 32)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid server ID"})
		return
	}

	job := h.JobMgr.GetJob(uint(id))
	w.Header().Set("Content-Type", "application/json")
	if job == nil {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"has_active_job": false,
		})
		return
	}

	job.mu.RLock()
	defer job.mu.RUnlock()

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"has_active_job":   job.Status == "running",
		"job_id":           job.ID,
		"server_id":        job.ServerID,
		"server_name":      job.ServerName,
		"status":           job.Status,
		"package_manager":  job.PackageManager,
		"packages":         job.Packages,
		"started_at":       job.StartedAt,
		"finished_at":      job.FinishedAt,
		"duration_seconds": job.DurationSec,
		"logs_count":       len(job.Logs),
		"result":           job.Result,
	})
}

// ListActiveUpgradeJobsHandler returns all currently running upgrade jobs across all servers
func (h *Handler) ListActiveUpgradeJobsHandler(w http.ResponseWriter, r *http.Request) {
	jobs := h.JobMgr.GetActiveJobs()
	type activeItem struct {
		ServerID       uint      `json:"server_id"`
		ServerName     string    `json:"server_name"`
		Status         string    `json:"status"`
		PackageManager string    `json:"package_manager"`
		StartedAt      time.Time `json:"started_at"`
		LogsCount      int       `json:"logs_count"`
	}

	list := make([]activeItem, 0, len(jobs))
	for _, j := range jobs {
		j.mu.RLock()
		list = append(list, activeItem{
			ServerID:       j.ServerID,
			ServerName:     j.ServerName,
			Status:         j.Status,
			PackageManager: j.PackageManager,
			StartedAt:      j.StartedAt,
			LogsCount:      len(j.Logs),
		})
		j.mu.RUnlock()
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(list)
}

// UpgradePackagesStreamHandler streams live package upgrade output and delivers a final result.
// It supports detached execution, backgrounding, and reattaching even across network disconnects.
func (h *Handler) UpgradePackagesStreamHandler(w http.ResponseWriter, r *http.Request) {
	idParam := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(idParam, 10, 32)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid server ID"})
		return
	}

	serverID := uint(id)
	existingJob := h.JobMgr.GetJob(serverID)

	var job *UpgradeJob

	// If job is already running on this host, reattach directly to it!
	if existingJob != nil && existingJob.Status == "running" {
		job = existingJob
	} else if r.Method == "GET" {
		// If GET method was called, client expects to attach to an existing job
		if existingJob == nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "No upgrade job found for host"})
			return
		}
		job = existingJob
	} else {
		// POST request to initiate a new upgrade job
		var req models.UpgradePackagesRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			// If body was empty but existingJob was completed, we can still reattach to completed result
			if existingJob != nil {
				job = existingJob
			} else {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request body"})
				return
			}
		}

		if job == nil {
			server, exec, err := h.resolveServerAndExecutor(r, serverID)
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
			if sudoPassword == "" && server.SudoPassword != "" && server.SudoPolicy != "always_prompt" {
				decrypted, decErr := krakencrypto.Decrypt(server.SudoPassword)
				if decErr == nil {
					sudoPassword = decrypted
					if server.SudoPolicy == "" {
						server.SudoPolicy = "save_securely"
						h.DB.Model(server).Update("sudo_policy", "save_securely")
					}
				}
			}

			isLocalHost := !docker.IsSSHHost(server.HostURL)
	if !isLocalHost && sudoPassword == "" && server.SSHUser != "" && !strings.EqualFold(server.SSHUser, "root") {
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

			if pkgManager == "" || pkgManager == "unknown" || (isLocalHost && pkgManager == "apk") {
				detected, dErr := DetectPackageManager(r.Context(), exec)
				if dErr == nil && detected != "unknown" {
					pkgManager = detected
					h.DB.Model(server).Update("package_manager", pkgManager)
					server.PackageManager = pkgManager
				} else if pkgManager == "" || pkgManager == "unknown" {
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
			_ = flusher

			claims := auth.GetUserFromContext(r.Context())
			userID := uint(0)
			username := "system"
			if claims != nil {
				userID = claims.UserID
				username = claims.Username
			}

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

			execResolver := func(ctx context.Context) (Executor, error) {
				cleanHost := strings.TrimSpace(server.HostURL)
				if docker.IsSSHHost(cleanHost) {
					if h.DockerService == nil {
						return nil, errors.New("docker service is nil")
					}
					client, _, err := h.DockerService.GetSSHClient(ctx, server.ID, overrides...)
					if err != nil {
						return nil, err
					}
					return NewSSHExecutor(client), nil
				}
				return NewLocalExecutor(), nil
			}

			var created bool
			job, created = h.JobMgr.CreateOrGetJob(server.ID, server.Name, pkgManager, req.Packages)
			if created {
				startErr := h.JobMgr.StartBackgroundUpgrade(job, h.DB, h.DockerService, execResolver, server, sudoPassword, userID, username)
				if startErr != nil {
					if errors.Is(startErr, ErrInvalidSudoPassword) {
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
					_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to launch detached upgrade on host: " + startErr.Error()})
					return
				}
			}
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

	ch, existingLogs, result, status, unsubscribe := job.Subscribe()
	defer unsubscribe()

	// 1. Replay past buffered logs first
	for _, l := range existingLogs {
		sendEvent(map[string]interface{}{
			"type":      "log",
			"message":   l.Message,
			"level":     l.Level,
			"timestamp": l.Timestamp,
		})
	}

	// 2. If already completed, deliver final result payload
	if status != "running" && result != nil {
		sendEvent(map[string]interface{}{
			"type":                    "result",
			"success":                 result.Success,
			"message":                 result.Message,
			"remaining_updates_count": result.RemainingUpdatesCount,
			"security_updates_count":  result.SecurityUpdatesCount,
			"error":                   result.Error,
			"code":                    result.Code,
		})
		return
	}

	// 3. Stream live logs and exit when client disconnects (backgrounded) or job completes
	for {
		select {
		case <-r.Context().Done():
			// Client disconnected (sent to background or closed tab).
			// The detached background process on the host keeps running!
			return
		case item, ok := <-ch:
			if !ok {
				return
			}
			if item.Type == "result" {
				job.mu.RLock()
				res := job.Result
				job.mu.RUnlock()
				if res != nil {
					sendEvent(map[string]interface{}{
						"type":                    "result",
						"success":                 res.Success,
						"message":                 res.Message,
						"remaining_updates_count": res.RemainingUpdatesCount,
						"security_updates_count":  res.SecurityUpdatesCount,
						"error":                   res.Error,
						"code":                    res.Code,
					})
				}
				return
			}
			sendEvent(map[string]interface{}{
				"type":      "log",
				"message":   item.Message,
				"level":     item.Level,
				"timestamp": item.Timestamp,
			})
		}
	}
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
