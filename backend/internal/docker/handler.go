package docker

import (
	"context"
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
	"github.com/kraken/admin-tool/backend/internal/models"
	"github.com/kraken/admin-tool/backend/internal/registry"
	"gorm.io/gorm"
)

type Handler struct {
	Service *DockerService
	DB      *gorm.DB
}

func NewHandler(service *DockerService, db *gorm.DB) *Handler {
	return &Handler{
		Service: service,
		DB:      db,
	}
}

// getAuthorizedServerID verifies user access to the requested server.
// If an explicit server ID is provided (via X-Server-ID header or serverId query param),
// it ensures that the server exists and belongs to the authenticated user (user_id = claims.UserID).
// If no server was explicitly requested (serverID == 0), it automatically picks the user's default/first server.
// If the user does not own the requested server, or has no servers at all, it returns an error.
func (h *Handler) getAuthorizedServerID(r *http.Request) (uint, error) {
	var requestedID uint
	if headerVal := r.Header.Get("X-Server-ID"); headerVal != "" {
		if id, err := strconv.ParseUint(headerVal, 10, 32); err == nil {
			requestedID = uint(id)
		}
	}
	if queryVal := r.URL.Query().Get("serverId"); queryVal != "" && requestedID == 0 {
		if id, err := strconv.ParseUint(queryVal, 10, 32); err == nil {
			requestedID = uint(id)
		}
	}

	claims := auth.GetUserFromContext(r.Context())
	if claims == nil || claims.UserID == 0 {
		if h.DB == nil {
			return requestedID, nil
		}
		return 0, errors.New("unauthorized: missing user context")
	}

	if h.DB == nil {
		return requestedID, nil
	}

	if requestedID > 0 {
		var srv models.DockerServer
		if err := h.DB.Where("id = ? AND user_id = ?", requestedID, claims.UserID).First(&srv).Error; err != nil {
			return 0, fmt.Errorf("server %d not found or not accessible", requestedID)
		}
		return srv.ID, nil
	}

	// requestedID is 0: find a server owned by this user
	var userSrv models.DockerServer
	if err := h.DB.Where("user_id = ?", claims.UserID).Order("is_default desc, id asc").First(&userSrv).Error; err != nil {
		return 0, errors.New("no server configured for user")
	}
	return userSrv.ID, nil
}

// getServerID extracts and authorizes the target docker server for the requesting user
func (h *Handler) getServerID(r *http.Request) uint {
	id, err := h.getAuthorizedServerID(r)
	if err != nil {
		return 0
	}
	return id
}

// --- Server Management Handlers ---

// ListServersHandler handles GET /api/docker/servers
func (h *Handler) ListServersHandler(w http.ResponseWriter, r *http.Request) {
	probe := r.URL.Query().Get("probe") == "true"
	claims := auth.GetUserFromContext(r.Context())
	var userID uint = 0
	if claims != nil {
		userID = claims.UserID
	}

	servers, err := h.Service.ListServersForUser(r.Context(), userID, probe)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": err.Error(),
			"data":  []interface{}{},
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"data": servers,
	})
}

// StreamServersStatusHandler streams real-time status probe updates for all registered servers via SSE.
// Each server is probed in parallel, and as each probe completes, a status update is immediately sent.
func (h *Handler) StreamServersStatusHandler(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	claims := auth.GetUserFromContext(r.Context())
	var userID uint = 0
	if claims != nil {
		userID = claims.UserID
	}

	servers, err := h.Service.ListServersForUser(r.Context(), userID, false)
	if err != nil {
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	// Channel to collect status updates as they finish in parallel
	type updateMsg struct {
		ServerID        uint   `json:"server_id"`
		Status          string `json:"status"`
		Version         string `json:"version,omitempty"`
		ContainersCount int    `json:"containers_count"`
		ImagesCount     int    `json:"images_count"`
		ErrMsg          string `json:"err_msg,omitempty"`
	}

	updateChan := make(chan updateMsg, len(servers))
	var wg sync.WaitGroup

	for _, srv := range servers {
		wg.Add(1)
		go func(s models.DockerServer) {
			defer wg.Done()
			res := h.Service.ProbeServer(r.Context(), s.ID)
			updateChan <- updateMsg{
				ServerID:        res.ID,
				Status:          res.Status,
				Version:         res.Version,
				ContainersCount: res.ContainersCount,
				ImagesCount:     res.ImagesCount,
				ErrMsg:          res.ErrMsg,
			}
		}(srv)
	}

	go func() {
		wg.Wait()
		close(updateChan)
	}()

	for msg := range updateChan {
		data, _ := json.Marshal(msg)
		_, _ = fmt.Fprintf(w, "event: server_status\ndata: %s\n\n", data)
		flusher.Flush()
	}

	_, _ = fmt.Fprintf(w, "event: complete\ndata: {\"total\":%d}\n\n", len(servers))
	flusher.Flush()
}

// CreateServerHandler handles POST /api/docker/servers
func (h *Handler) CreateServerHandler(w http.ResponseWriter, r *http.Request) {
	var req models.CreateServerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request payload"})
		return
	}

	user := auth.GetUserFromContext(r.Context())
	username := "system"
	var userID uint = 0
	if user != nil {
		username = user.Username
		userID = user.UserID
	}

	server, err := h.Service.CreateServer(r.Context(), req, userID)
	if err != nil {
		audit.Log(h.DB, userID, username, "CREATE_DOCKER_SERVER", "docker_server", req.Name, err.Error(), "FAILED")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	audit.Log(h.DB, userID, username, "CREATE_DOCKER_SERVER", "docker_server", server.Name, "Server registered successfully", "SUCCESS")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(server)
}

// UpdateServerHandler handles PUT /api/docker/servers/{id}
func (h *Handler) UpdateServerHandler(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid server ID"})
		return
	}

	var existing models.DockerServer
	if err := h.DB.First(&existing, id).Error; err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Server not found"})
		return
	}

	user := auth.GetUserFromContext(r.Context())
	username := "system"
	var userID uint = 0
	userRole := ""
	if user != nil {
		username = user.Username
		userID = user.UserID
		userRole = user.Role
	}

	if existing.IsDefault && userRole != "admin" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Only admins can modify the default server"})
		return
	}

	if !existing.IsDefault && userRole != "admin" && existing.UserID != userID {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Permission denied: you can only modify servers you own"})
		return
	}

	var req models.UpdateServerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request payload"})
		return
	}

	server, err := h.Service.UpdateServer(r.Context(), uint(id), req)
	if err != nil {
		audit.Log(h.DB, userID, username, "UPDATE_DOCKER_SERVER", "docker_server", idStr, err.Error(), "FAILED")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	audit.Log(h.DB, userID, username, "UPDATE_DOCKER_SERVER", "docker_server", server.Name, "Server updated successfully", "SUCCESS")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(server)
}

// DeleteServerHandler handles DELETE /api/docker/servers/{id}
func (h *Handler) DeleteServerHandler(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid server ID"})
		return
	}

	var existing models.DockerServer
	if err := h.DB.First(&existing, id).Error; err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Server not found"})
		return
	}

	if existing.IsDefault {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "The default server cannot be deleted"})
		return
	}

	user := auth.GetUserFromContext(r.Context())
	username := "system"
	var userID uint = 0
	userRole := ""
	if user != nil {
		username = user.Username
		userID = user.UserID
		userRole = user.Role
	}

	if userRole != "admin" && existing.UserID != userID {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Permission denied: you can only delete servers you own"})
		return
	}

	if err := h.Service.DeleteServer(r.Context(), uint(id)); err != nil {
		audit.Log(h.DB, userID, username, "DELETE_DOCKER_SERVER", "docker_server", idStr, err.Error(), "FAILED")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	audit.Log(h.DB, userID, username, "DELETE_DOCKER_SERVER", "docker_server", idStr, "Server removed successfully", "SUCCESS")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "Server deleted successfully"})
}

// SetDefaultServerHandler handles POST /api/docker/servers/{id}/default
func (h *Handler) SetDefaultServerHandler(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid server ID"})
		return
	}

	if err := h.Service.SetDefaultServer(r.Context(), uint(id)); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "Default server updated successfully"})
}

// TestServerHandler handles POST /api/docker/servers/test
func (h *Handler) TestServerHandler(w http.ResponseWriter, r *http.Request) {
	var req models.TestServerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request payload"})
		return
	}

	testCtx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()

	if err := h.Service.PrepareTestServerRequest(testCtx, &req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "UNREACHABLE",
			"error":   err.Error(),
			"message": err.Error(),
			"logs":    []interface{}{},
		})
		return
	}

	res, err := RunServerDiagnostics(testCtx, req, nil)
	w.Header().Set("Content-Type", "application/json")
	if err != nil || res.Status != "CONNECTED" {
		w.WriteHeader(http.StatusBadRequest)
		errMsg := "Connection failed"
		if err != nil {
			errMsg = err.Error()
		} else if res != nil && res.Message != "" {
			errMsg = res.Message
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "UNREACHABLE",
			"error":   errMsg,
			"message": errMsg,
			"logs":    res.Logs,
		})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":      "CONNECTED",
		"version":     res.Version,
		"api_version": res.APIVersion,
		"os_arch":     res.OSArch,
		"message":     res.Message,
		"logs":        res.Logs,
	})
}

// TestServerStreamHandler handles POST /api/docker/servers/test/stream
func (h *Handler) TestServerStreamHandler(w http.ResponseWriter, r *http.Request) {
	var req models.TestServerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request payload"})
		return
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

	testCtx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()

	if err := h.Service.PrepareTestServerRequest(testCtx, &req); err != nil {
		errLog := ServerDiagnosticLog{
			Level:     "error",
			Message:   fmt.Sprintf("Failed to resolve dynamic credentials: %v", err),
			Timestamp: time.Now().Format("15:04:05"),
		}
		line, _ := json.Marshal(errLog)
		_, _ = w.Write(line)
		_, _ = w.Write([]byte("\n"))
		resLine, _ := json.Marshal(map[string]interface{}{
			"type":    "result",
			"status":  "UNREACHABLE",
			"message": err.Error(),
			"error":   err.Error(),
		})
		_, _ = w.Write(resLine)
		_, _ = w.Write([]byte("\n"))
		flusher.Flush()
		return
	}

	onLog := func(log ServerDiagnosticLog) {
		line, _ := json.Marshal(log)
		_, _ = w.Write(line)
		_, _ = w.Write([]byte("\n"))
		flusher.Flush()
	}

	res, err := RunServerDiagnostics(testCtx, req, onLog)
	finalPayload := map[string]interface{}{
		"type":    "result",
		"status":  res.Status,
		"version": res.Version,
		"message": res.Message,
	}
	if err != nil {
		finalPayload["error"] = err.Error()
	}
	finalLine, _ := json.Marshal(finalPayload)
	_, _ = w.Write(finalLine)
	_, _ = w.Write([]byte("\n"))
	flusher.Flush()
}

// --- Container Handlers ---

// ListContainersHandler handles GET /api/docker/containers
func (h *Handler) ListContainersHandler(w http.ResponseWriter, r *http.Request) {
	all := r.URL.Query().Get("all") == "true"
	serverID := h.getServerID(r)
	if serverID == 0 {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": []models.ContainerSummary{},
		})
		return
	}

	containers, err := h.Service.ListContainersOnServer(r.Context(), serverID, all)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": err.Error(),
			"data":  []interface{}{},
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"data": containers,
	})
}

// StartContainerHandler handles POST /api/docker/containers/{id}/start
func (h *Handler) StartContainerHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	serverID := h.getServerID(r)
	user := auth.GetUserFromContext(r.Context())
	username := "system"
	var userID uint = 0
	if user != nil {
		username = user.Username
		userID = user.UserID
	}

	err := h.Service.StartContainerOnServer(r.Context(), serverID, id)
	if err != nil {
		audit.Log(h.DB, userID, username, "START_CONTAINER", "docker_container", id, err.Error(), "FAILED")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	audit.Log(h.DB, userID, username, "START_CONTAINER", "docker_container", id, "Started successfully", "SUCCESS")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "Container started successfully"})
}

// StopContainerHandler handles POST /api/docker/containers/{id}/stop
func (h *Handler) StopContainerHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	serverID := h.getServerID(r)
	user := auth.GetUserFromContext(r.Context())
	username := "system"
	var userID uint = 0
	if user != nil {
		username = user.Username
		userID = user.UserID
	}

	err := h.Service.StopContainerOnServer(r.Context(), serverID, id)
	if err != nil {
		audit.Log(h.DB, userID, username, "STOP_CONTAINER", "docker_container", id, err.Error(), "FAILED")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	audit.Log(h.DB, userID, username, "STOP_CONTAINER", "docker_container", id, "Stopped successfully", "SUCCESS")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "Container stopped successfully"})
}

// RestartContainerHandler handles POST /api/docker/containers/{id}/restart
func (h *Handler) RestartContainerHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	serverID := h.getServerID(r)
	user := auth.GetUserFromContext(r.Context())
	username := "system"
	var userID uint = 0
	if user != nil {
		username = user.Username
		userID = user.UserID
	}

	err := h.Service.RestartContainerOnServer(r.Context(), serverID, id)
	if err != nil {
		audit.Log(h.DB, userID, username, "RESTART_CONTAINER", "docker_container", id, err.Error(), "FAILED")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	audit.Log(h.DB, userID, username, "RESTART_CONTAINER", "docker_container", id, "Restarted successfully", "SUCCESS")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "Container restarted successfully"})
}

// RemoveContainerHandler handles DELETE /api/docker/containers/{id}
func (h *Handler) RemoveContainerHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	serverID := h.getServerID(r)
	force := r.URL.Query().Get("force") == "true"
	user := auth.GetUserFromContext(r.Context())
	username := "system"
	var userID uint = 0
	if user != nil {
		username = user.Username
		userID = user.UserID
	}

	err := h.Service.RemoveContainerOnServer(r.Context(), serverID, id, force)
	if err != nil {
		audit.Log(h.DB, userID, username, "REMOVE_CONTAINER", "docker_container", id, err.Error(), "FAILED")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	audit.Log(h.DB, userID, username, "REMOVE_CONTAINER", "docker_container", id, "Removed successfully", "SUCCESS")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "Container removed successfully"})
}

// --- Image Handlers ---

// ListImagesHandler handles GET /api/docker/images
func (h *Handler) ListImagesHandler(w http.ResponseWriter, r *http.Request) {
	serverID := h.getServerID(r)
	if serverID == 0 {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": []models.ImageSummary{},
		})
		return
	}
	images, err := h.Service.ListImagesOnServer(r.Context(), serverID)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": err.Error(),
			"data":  []interface{}{},
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"data": images,
	})
}

// SearchImagesHandler handles GET /api/docker/images/search?term=...
func (h *Handler) SearchImagesHandler(w http.ResponseWriter, r *http.Request) {
	serverID := h.getServerID(r)
	term := strings.TrimSpace(r.URL.Query().Get("term"))
	if len(term) < 2 {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": []models.ImageSearchResult{},
		})
		return
	}

	results, err := h.Service.SearchImagesOnServer(r.Context(), serverID, term)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": err.Error(),
			"data":  []models.ImageSearchResult{},
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"data": results,
	})
}

// PullImageStreamHandler handles POST /api/docker/images/pull/stream
func (h *Handler) PullImageStreamHandler(w http.ResponseWriter, r *http.Request) {
	serverID := h.getServerID(r)
	var req models.PullImageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Image) == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Valid image name required"})
		return
	}

	user := auth.GetUserFromContext(r.Context())
	username := "system"
	var userID uint = 0
	if user != nil {
		username = user.Username
		userID = user.UserID
	}

	out, err := h.Service.PullImageStreamOnServer(r.Context(), serverID, req.Image)
	if err != nil {
		audit.Log(h.DB, userID, username, "PULL_IMAGE", "docker_image", req.Image, err.Error(), "FAILED")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	defer out.Close()

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

	dec := json.NewDecoder(out)
	var lastErr error
	for dec.More() {
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			lastErr = err
			break
		}

		var errCheck struct {
			Error string `json:"error"`
		}
		if _ = json.Unmarshal(raw, &errCheck); errCheck.Error != "" {
			lastErr = errors.New(errCheck.Error)
		}

		_, _ = w.Write(raw)
		_, _ = w.Write([]byte("\n"))
		flusher.Flush()
	}

	if lastErr != nil {
		audit.Log(h.DB, userID, username, "PULL_IMAGE", "docker_image", req.Image, lastErr.Error(), "FAILED")
	} else {
		audit.Log(h.DB, userID, username, "PULL_IMAGE", "docker_image", req.Image, "Pulled image successfully", "SUCCESS")
	}
}

// PullImageHandler handles POST /api/docker/images/pull
func (h *Handler) PullImageHandler(w http.ResponseWriter, r *http.Request) {
	serverID := h.getServerID(r)
	var req models.PullImageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Image == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Valid image name required"})
		return
	}

	user := auth.GetUserFromContext(r.Context())
	username := "system"
	var userID uint = 0
	if user != nil {
		username = user.Username
		userID = user.UserID
	}

	err := h.Service.PullImageOnServer(r.Context(), serverID, req.Image)
	if err != nil {
		audit.Log(h.DB, userID, username, "PULL_IMAGE", "docker_image", req.Image, err.Error(), "FAILED")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	audit.Log(h.DB, userID, username, "PULL_IMAGE", "docker_image", req.Image, "Pulled image successfully", "SUCCESS")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "Image pulled successfully"})
}

// RemoveImageHandler handles DELETE /api/docker/images/{id}
func (h *Handler) RemoveImageHandler(w http.ResponseWriter, r *http.Request) {
	serverID := h.getServerID(r)
	id := chi.URLParam(r, "id")
	force := r.URL.Query().Get("force") == "true"
	user := auth.GetUserFromContext(r.Context())
	username := "system"
	var userID uint = 0
	if user != nil {
		username = user.Username
		userID = user.UserID
	}

	err := h.Service.RemoveImageOnServer(r.Context(), serverID, id, force)
	if err != nil {
		audit.Log(h.DB, userID, username, "REMOVE_IMAGE", "docker_image", id, err.Error(), "FAILED")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	audit.Log(h.DB, userID, username, "REMOVE_IMAGE", "docker_image", id, "Removed image successfully", "SUCCESS")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "Image removed successfully"})
}

// CheckImageUpdatesHandler handles GET /api/docker/images/updates
func (h *Handler) CheckImageUpdatesHandler(w http.ResponseWriter, r *http.Request) {
	serverID := h.getServerID(r)
	imageID := strings.TrimSpace(r.URL.Query().Get("id"))

	regClient := registry.NewClient()
	cli, err := h.Service.GetClient(r.Context(), serverID)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	if imageID != "" {
		inspect, _, err := cli.ImageInspectWithRaw(r.Context(), imageID)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}

		targetRef := imageID
		if len(inspect.RepoTags) > 0 && inspect.RepoTags[0] != "<none>:<none>" {
			targetRef = inspect.RepoTags[0]
		}

		var created time.Time
		if inspect.Created != "" {
			created, _ = time.Parse(time.RFC3339Nano, inspect.Created)
		}

		info, err := regClient.CheckUpdates(r.Context(), targetRef, inspect.ID, created)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		info.ImageID = inspect.ID
		info.RepoTags = inspect.RepoTags

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(info)
		return
	}

	// Bulk check for images on server
	images, err := h.Service.ListImagesOnServer(r.Context(), serverID)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	var results []models.ImageUpdateInfo
	for _, img := range images {
		if len(img.RepoTags) == 0 || img.RepoTags[0] == "<none>:<none>" {
			continue
		}
		created := time.Unix(img.Created, 0)
		info, err := regClient.CheckUpdates(r.Context(), img.RepoTags[0], img.ID, created)
		if err == nil {
			info.ImageID = img.ID
			info.RepoTags = img.RepoTags
			results = append(results, *info)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"data": results,
	})
}

// UpdateImageHandler handles POST /api/docker/images/update
func (h *Handler) UpdateImageHandler(w http.ResponseWriter, r *http.Request) {
	serverID := h.getServerID(r)
	var req models.UpdateImageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request payload"})
		return
	}
	if req.ServerID == 0 {
		req.ServerID = serverID
	}

	user := auth.GetUserFromContext(r.Context())
	username := "system"
	var userID uint = 0
	if user != nil {
		username = user.Username
		userID = user.UserID
	}

	res, err := h.Service.UpdateImageOnServer(r.Context(), req)
	if err != nil {
		audit.Log(h.DB, userID, username, "UPDATE_IMAGE", "docker_image", req.TargetTag, err.Error(), "FAILED")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	audit.Log(h.DB, userID, username, "UPDATE_IMAGE", "docker_image", req.TargetTag, fmt.Sprintf("Updated to %s (mode: %s, recreated: %d)", req.TargetTag, req.Mode, len(res.RecreatedContainers)), "SUCCESS")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}

// --- Docker Compose Stack Handlers ---

// ListComposeStacksHandler handles GET /api/docker/stacks
func (h *Handler) ListComposeStacksHandler(w http.ResponseWriter, r *http.Request) {
	serverID := h.getServerID(r)
	if serverID == 0 {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": []models.ComposeStack{},
		})
		return
	}
	stacks, err := h.Service.ListComposeStacks(r.Context(), serverID)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": err.Error(),
			"data":  []interface{}{},
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"data": stacks,
	})
}

// GetComposeStackHandler handles GET /api/docker/stacks/{name}
func (h *Handler) GetComposeStackHandler(w http.ResponseWriter, r *http.Request) {
	serverID := h.getServerID(r)
	name := chi.URLParam(r, "name")

	stack, err := h.Service.GetComposeStack(r.Context(), serverID, name)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"data": stack,
	})
}

// StartComposeStackHandler handles POST /api/docker/stacks/{name}/start
func (h *Handler) StartComposeStackHandler(w http.ResponseWriter, r *http.Request) {
	serverID := h.getServerID(r)
	name := chi.URLParam(r, "name")

	user := auth.GetUserFromContext(r.Context())
	username := "system"
	var userID uint = 0
	if user != nil {
		username = user.Username
		userID = user.UserID
	}

	if err := h.Service.StartComposeStack(r.Context(), serverID, name); err != nil {
		audit.Log(h.DB, userID, username, "START_STACK", "docker_compose", name, err.Error(), "FAILED")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	audit.Log(h.DB, userID, username, "START_STACK", "docker_compose", name, "Started compose stack successfully", "SUCCESS")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "Stack started successfully"})
}

// StopComposeStackHandler handles POST /api/docker/stacks/{name}/stop
func (h *Handler) StopComposeStackHandler(w http.ResponseWriter, r *http.Request) {
	serverID := h.getServerID(r)
	name := chi.URLParam(r, "name")

	user := auth.GetUserFromContext(r.Context())
	username := "system"
	var userID uint = 0
	if user != nil {
		username = user.Username
		userID = user.UserID
	}

	if err := h.Service.StopComposeStack(r.Context(), serverID, name); err != nil {
		audit.Log(h.DB, userID, username, "STOP_STACK", "docker_compose", name, err.Error(), "FAILED")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	audit.Log(h.DB, userID, username, "STOP_STACK", "docker_compose", name, "Stopped compose stack successfully", "SUCCESS")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "Stack stopped successfully"})
}

// RestartComposeStackHandler handles POST /api/docker/stacks/{name}/restart
func (h *Handler) RestartComposeStackHandler(w http.ResponseWriter, r *http.Request) {
	serverID := h.getServerID(r)
	name := chi.URLParam(r, "name")

	user := auth.GetUserFromContext(r.Context())
	username := "system"
	var userID uint = 0
	if user != nil {
		username = user.Username
		userID = user.UserID
	}

	if err := h.Service.RestartComposeStack(r.Context(), serverID, name); err != nil {
		audit.Log(h.DB, userID, username, "RESTART_STACK", "docker_compose", name, err.Error(), "FAILED")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	audit.Log(h.DB, userID, username, "RESTART_STACK", "docker_compose", name, "Restarted compose stack successfully", "SUCCESS")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "Stack restarted successfully"})
}

// RemoveComposeStackHandler handles DELETE /api/docker/stacks/{name}
func (h *Handler) RemoveComposeStackHandler(w http.ResponseWriter, r *http.Request) {
	serverID := h.getServerID(r)
	name := chi.URLParam(r, "name")
	removeVolumes := r.URL.Query().Get("volumes") == "true"

	user := auth.GetUserFromContext(r.Context())
	username := "system"
	var userID uint = 0
	if user != nil {
		username = user.Username
		userID = user.UserID
	}

	if err := h.Service.RemoveComposeStack(r.Context(), serverID, name, removeVolumes); err != nil {
		audit.Log(h.DB, userID, username, "REMOVE_STACK", "docker_compose", name, err.Error(), "FAILED")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	audit.Log(h.DB, userID, username, "REMOVE_STACK", "docker_compose", name, "Removed compose stack successfully", "SUCCESS")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "Stack removed successfully"})
}

// DeployComposeStackHandler handles POST /api/docker/stacks/deploy
func (h *Handler) DeployComposeStackHandler(w http.ResponseWriter, r *http.Request) {
	serverID := h.getServerID(r)

	var req models.DeployStackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request body"})
		return
	}

	user := auth.GetUserFromContext(r.Context())
	username := "system"
	var userID uint = 0
	if user != nil {
		username = user.Username
		userID = user.UserID
	}

	if err := h.Service.DeployComposeStack(r.Context(), serverID, req); err != nil {
		audit.Log(h.DB, userID, username, "DEPLOY_STACK", "docker_compose", req.Name, err.Error(), "FAILED")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	audit.Log(h.DB, userID, username, "DEPLOY_STACK", "docker_compose", req.Name, "Deployed compose stack successfully", "SUCCESS")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "Stack deployed successfully"})
}

// GetComposeStackYAMLHandler handles GET /api/docker/stacks/{name}/yaml
func (h *Handler) GetComposeStackYAMLHandler(w http.ResponseWriter, r *http.Request) {
	serverID := h.getServerID(r)
	name := chi.URLParam(r, "name")

	resp, err := h.Service.GetComposeStackYAML(r.Context(), serverID, name)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// UpdateComposeStackYAMLHandler handles PUT /api/docker/stacks/{name}/yaml
func (h *Handler) UpdateComposeStackYAMLHandler(w http.ResponseWriter, r *http.Request) {
	serverID := h.getServerID(r)
	name := chi.URLParam(r, "name")

	var req models.UpdateStackYAMLRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request payload"})
		return
	}

	user := auth.GetUserFromContext(r.Context())
	username := "system"
	var userID uint = 0
	if user != nil {
		username = user.Username
		userID = user.UserID
	}

	if err := h.Service.UpdateComposeStackYAML(r.Context(), serverID, name, req); err != nil {
		audit.Log(h.DB, userID, username, "UPDATE_STACK_YAML", "docker_compose", name, err.Error(), "FAILED")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	actionDesc := "Updated compose YAML successfully"
	if req.Redeploy {
		actionDesc = "Updated compose YAML and redeployed stack successfully"
	}
	audit.Log(h.DB, userID, username, "UPDATE_STACK_YAML", "docker_compose", name, actionDesc, "SUCCESS")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"message": actionDesc})
}

// GetImageDockerfileHandler handles GET /api/docker/images/dockerfile?id=... or GET /api/docker/images/{id}/dockerfile
func (h *Handler) GetImageDockerfileHandler(w http.ResponseWriter, r *http.Request) {
	serverID := h.getServerID(r)
	imageRef := chi.URLParam(r, "id")
	if imageRef == "" {
		imageRef = r.URL.Query().Get("id")
	}
	if imageRef == "" {
		imageRef = r.URL.Query().Get("tag")
	}

	if strings.TrimSpace(imageRef) == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Image ID or tag is required"})
		return
	}

	resp, err := h.Service.GetImageDockerfile(r.Context(), serverID, imageRef)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// SaveImageDockerfileHandler handles PUT /api/docker/images/dockerfile
func (h *Handler) SaveImageDockerfileHandler(w http.ResponseWriter, r *http.Request) {
	serverID := h.getServerID(r)

	var payload struct {
		Tag        string `json:"tag"`
		ImageID    string `json:"image_id"`
		Dockerfile string `json:"dockerfile"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request payload"})
		return
	}

	user := auth.GetUserFromContext(r.Context())
	username := "system"
	var userID uint = 0
	if user != nil {
		username = user.Username
		userID = user.UserID
	}

	if err := h.Service.SaveImageDockerfile(r.Context(), serverID, payload.Tag, payload.ImageID, payload.Dockerfile); err != nil {
		audit.Log(h.DB, userID, username, "SAVE_IMAGE_DOCKERFILE", "docker_image", payload.Tag, err.Error(), "FAILED")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	audit.Log(h.DB, userID, username, "SAVE_IMAGE_DOCKERFILE", "docker_image", payload.Tag, "Saved Dockerfile successfully", "SUCCESS")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "Dockerfile saved successfully"})
}

// BuildImageStreamHandler handles POST /api/docker/images/build/stream
func (h *Handler) BuildImageStreamHandler(w http.ResponseWriter, r *http.Request) {
	serverID := h.getServerID(r)

	var req models.BuildImageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Tag) == "" || strings.TrimSpace(req.Dockerfile) == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Tag and Dockerfile are required"})
		return
	}

	user := auth.GetUserFromContext(r.Context())
	username := "system"
	var userID uint = 0
	if user != nil {
		username = user.Username
		userID = user.UserID
	}

	out, err := h.Service.BuildImageStreamOnServer(r.Context(), serverID, req)
	if err != nil {
		audit.Log(h.DB, userID, username, "BUILD_IMAGE", "docker_image", req.Tag, err.Error(), "FAILED")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	defer out.Close()

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

	dec := json.NewDecoder(out)
	var lastErr error
	for dec.More() {
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			lastErr = err
			break
		}

		var errCheck struct {
			Error string `json:"error"`
		}
		if _ = json.Unmarshal(raw, &errCheck); errCheck.Error != "" {
			lastErr = errors.New(errCheck.Error)
		}

		_, _ = w.Write(raw)
		_, _ = w.Write([]byte("\n"))
		flusher.Flush()
	}

	if lastErr != nil {
		audit.Log(h.DB, userID, username, "BUILD_IMAGE", "docker_image", req.Tag, lastErr.Error(), "FAILED")
	} else {
		audit.Log(h.DB, userID, username, "BUILD_IMAGE", "docker_image", req.Tag, "Built Docker image successfully", "SUCCESS")
	}
}


