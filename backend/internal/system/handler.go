package system

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/kraken/admin-tool/backend/internal/database"
	"github.com/kraken/admin-tool/backend/internal/docker"
	"github.com/kraken/admin-tool/backend/internal/k8s"
	"github.com/kraken/admin-tool/backend/internal/models"
)

type Handler struct {
	DBHandle  *database.Handle
	Docker    *docker.DockerService
	K8s       *k8s.K8sService
	StartTime time.Time
	Version   string
}

func NewHandler(dbHandle *database.Handle, docker *docker.DockerService, k8s *k8s.K8sService, startTime time.Time) *Handler {
	return &Handler{
		DBHandle:  dbHandle,
		Docker:    docker,
		K8s:       k8s,
		StartTime: startTime,
		Version:   "1.0.0",
	}
}

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

// StatusHandler handles GET /api/system/status
func (h *Handler) StatusHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	serverID := getServerID(r)

	dockerConnected := false
	dockerVersion := ""
	dockerContainers := 0
	dockerImages := 0

	if h.Docker != nil {
		dockerVersion = h.Docker.GetVersionOnServer(ctx, serverID)
		dockerConnected = dockerVersion != "unavailable" && dockerVersion != ""
		if dockerConnected {
			dockerContainers, dockerImages = h.Docker.GetCountsOnServer(ctx, serverID)
		}
	}

	k8sConnected := false
	k8sVersion := ""
	k8sPods := 0
	k8sDeployments := 0

	if h.K8s != nil {
		k8sVersion = h.K8s.GetVersionOnServer(ctx, serverID)
		k8sConnected = k8sVersion != "unavailable" && k8sVersion != ""
		if k8sConnected {
			k8sPods, k8sDeployments = h.K8s.GetCountsOnServer(ctx, serverID)
		}
	}

	dbType := "Unknown"
	dbStatus := "disconnected"
	if h.DBHandle != nil {
		dbType = h.DBHandle.DBType
		if err := h.DBHandle.CheckHealth(); err == nil {
			dbStatus = "connected"
		} else {
			dbStatus = "degraded"
		}
	}

	resp := models.SystemStatusResponse{
		Version:          h.Version,
		Uptime:           time.Since(h.StartTime).Truncate(time.Second).String(),
		DockerConnected:  dockerConnected,
		DockerVersion:    dockerVersion,
		DockerContainers: dockerContainers,
		DockerImages:     dockerImages,
		K8sConnected:     k8sConnected,
		K8sVersion:       k8sVersion,
		K8sPods:          k8sPods,
		K8sDeployments:   k8sDeployments,
		DatabaseType:     dbType,
		DatabaseStatus:   dbStatus,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
