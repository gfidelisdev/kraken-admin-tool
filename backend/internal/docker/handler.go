package docker

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
	"github.com/kraken/admin-tool/backend/internal/models"
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

// getServerID extracts the target docker server from the X-Server-ID header or serverId query param
func (h *Handler) getServerID(r *http.Request) uint {
	if headerVal := r.Header.Get("X-Server-ID"); headerVal != "" {
		if id, err := strconv.ParseUint(headerVal, 10, 32); err == nil {
			return uint(id)
		}
	}
	if queryVal := r.URL.Query().Get("serverId"); queryVal != "" {
		if id, err := strconv.ParseUint(queryVal, 10, 32); err == nil {
			return uint(id)
		}
	}
	return 0
}

// --- Server Management Handlers ---

// ListServersHandler handles GET /api/docker/servers
func (h *Handler) ListServersHandler(w http.ResponseWriter, r *http.Request) {
	servers, err := h.Service.ListServers(r.Context())
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

	server, err := h.Service.CreateServer(r.Context(), req)
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

	var req models.UpdateServerRequest
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

	user := auth.GetUserFromContext(r.Context())
	username := "system"
	var userID uint = 0
	if user != nil {
		username = user.Username
		userID = user.UserID
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

// --- Docker Compose Stack Handlers ---

// ListComposeStacksHandler handles GET /api/docker/stacks
func (h *Handler) ListComposeStacksHandler(w http.ResponseWriter, r *http.Request) {
	serverID := h.getServerID(r)
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

