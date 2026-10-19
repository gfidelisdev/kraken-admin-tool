package k8s

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/kraken/admin-tool/backend/internal/audit"
	"github.com/kraken/admin-tool/backend/internal/auth"
	"github.com/kraken/admin-tool/backend/internal/models"
	"gorm.io/gorm"
)

type Handler struct {
	Service *K8sService
	DB      *gorm.DB
}

func NewHandler(service *K8sService, db *gorm.DB) *Handler {
	return &Handler{
		Service: service,
		DB:      db,
	}
}

// getServerID extracts server ID from X-Server-ID header or serverId query param
func getServerID(r *http.Request) uint {
	if header := r.Header.Get("X-Server-ID"); header != "" {
		if id, err := strconv.ParseUint(header, 10, 32); err == nil {
			return uint(id)
		}
	}
	if query := r.URL.Query().Get("serverId"); query != "" {
		if id, err := strconv.ParseUint(query, 10, 32); err == nil {
			return uint(id)
		}
	}
	return 0
}

// ListPodsHandler handles GET /api/k8s/pods
func (h *Handler) ListPodsHandler(w http.ResponseWriter, r *http.Request) {
	namespace := r.URL.Query().Get("namespace")
	if err := ValidateK8sNamespace(namespace); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	serverID := getServerID(r)

	pods, err := h.Service.ListPodsOnServer(r.Context(), serverID, namespace)
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
		"data": pods,
	})
}

// DeletePodHandler handles DELETE /api/k8s/pods/{namespace}/{name}
func (h *Handler) DeletePodHandler(w http.ResponseWriter, r *http.Request) {
	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")
	if err := ValidateK8sNamespace(namespace); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	if err := ValidateK8sName(name); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	serverID := getServerID(r)

	user := auth.GetUserFromContext(r.Context())
	username := "system"
	var userID uint = 0
	if user != nil {
		username = user.Username
		userID = user.UserID
	}

	resourceID := fmt.Sprintf("%s/%s", namespace, name)
	err := h.Service.DeletePodOnServer(r.Context(), serverID, namespace, name)
	if err != nil {
		audit.Log(h.DB, userID, username, "DELETE_POD", "k8s_pod", resourceID, err.Error(), "FAILED")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	audit.Log(h.DB, userID, username, "DELETE_POD", "k8s_pod", resourceID, "Pod deleted successfully", "SUCCESS")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "Pod deleted successfully"})
}

// ListDeploymentsHandler handles GET /api/k8s/deployments
func (h *Handler) ListDeploymentsHandler(w http.ResponseWriter, r *http.Request) {
	namespace := r.URL.Query().Get("namespace")
	if err := ValidateK8sNamespace(namespace); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	serverID := getServerID(r)

	deployments, err := h.Service.ListDeploymentsOnServer(r.Context(), serverID, namespace)
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
		"data": deployments,
	})
}

// ScaleDeploymentHandler handles POST /api/k8s/deployments/{namespace}/{name}/scale
func (h *Handler) ScaleDeploymentHandler(w http.ResponseWriter, r *http.Request) {
	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")
	if err := ValidateK8sNamespace(namespace); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	if err := ValidateK8sName(name); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	serverID := getServerID(r)

	var req models.ScaleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Replicas < 0 || req.Replicas > 10000 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Valid replicas count (between 0 and 10000) required"})
		return
	}

	user := auth.GetUserFromContext(r.Context())
	username := "system"
	var userID uint = 0
	if user != nil {
		username = user.Username
		userID = user.UserID
	}

	resourceID := fmt.Sprintf("%s/%s", namespace, name)
	err := h.Service.ScaleDeploymentOnServer(r.Context(), serverID, namespace, name, req.Replicas)
	if err != nil {
		audit.Log(h.DB, userID, username, "SCALE_DEPLOYMENT", "k8s_deployment", resourceID, err.Error(), "FAILED")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	audit.Log(h.DB, userID, username, "SCALE_DEPLOYMENT", "k8s_deployment", resourceID, fmt.Sprintf("Scaled to %d replicas", req.Replicas), "SUCCESS")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "Deployment scaled successfully"})
}

// RestartDeploymentHandler handles POST /api/k8s/deployments/{namespace}/{name}/restart
func (h *Handler) RestartDeploymentHandler(w http.ResponseWriter, r *http.Request) {
	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")
	if err := ValidateK8sNamespace(namespace); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	if err := ValidateK8sName(name); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	serverID := getServerID(r)

	user := auth.GetUserFromContext(r.Context())
	username := "system"
	var userID uint = 0
	if user != nil {
		username = user.Username
		userID = user.UserID
	}

	resourceID := fmt.Sprintf("%s/%s", namespace, name)
	err := h.Service.RestartDeploymentOnServer(r.Context(), serverID, namespace, name)
	if err != nil {
		audit.Log(h.DB, userID, username, "RESTART_DEPLOYMENT", "k8s_deployment", resourceID, err.Error(), "FAILED")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	audit.Log(h.DB, userID, username, "RESTART_DEPLOYMENT", "k8s_deployment", resourceID, "Rollout restart triggered", "SUCCESS")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "Rollout restart triggered successfully"})
}
