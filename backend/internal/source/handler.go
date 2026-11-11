package source

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"github.com/kraken/admin-tool/backend/internal/models"
)

// Handler provides HTTP endpoints for source directory scanning and reverse engineering links
type Handler struct {
	Service *Service
	DB      *gorm.DB
}

// NewHandler constructs a new Handler
func NewHandler(service *Service, db *gorm.DB) *Handler {
	return &Handler{
		Service: service,
		DB:      db,
	}
}

// ListSearchDirsHandler returns all configured search directories for a host
func (h *Handler) ListSearchDirsHandler(w http.ResponseWriter, r *http.Request) {
	serverID, err := parseServerID(r)
	if err != nil {
		http.Error(w, `{"error":"invalid server id"}`, http.StatusBadRequest)
		return
	}

	dirs, err := h.Service.ListSearchDirs(r.Context(), serverID)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(dirs)
}

// CreateSearchDirHandler adds a new search directory
func (h *Handler) CreateSearchDirHandler(w http.ResponseWriter, r *http.Request) {
	serverID, err := parseServerID(r)
	if err != nil {
		http.Error(w, `{"error":"invalid server id"}`, http.StatusBadRequest)
		return
	}

	var req models.CreateSourceDirRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	dir, err := h.Service.CreateSearchDir(r.Context(), serverID, req)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(dir)
}

// UpdateSearchDirHandler updates search directory settings
func (h *Handler) UpdateSearchDirHandler(w http.ResponseWriter, r *http.Request) {
	serverID, err := parseServerID(r)
	if err != nil {
		http.Error(w, `{"error":"invalid server id"}`, http.StatusBadRequest)
		return
	}

	dirIDStr := chi.URLParam(r, "dirId")
	dirID, err := strconv.ParseUint(dirIDStr, 10, 32)
	if err != nil {
		http.Error(w, `{"error":"invalid directory id"}`, http.StatusBadRequest)
		return
	}

	var req models.UpdateSourceDirRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	dir, err := h.Service.UpdateSearchDir(r.Context(), serverID, uint(dirID), req)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(dir)
}

// DeleteSearchDirHandler deletes a search directory
func (h *Handler) DeleteSearchDirHandler(w http.ResponseWriter, r *http.Request) {
	serverID, err := parseServerID(r)
	if err != nil {
		http.Error(w, `{"error":"invalid server id"}`, http.StatusBadRequest)
		return
	}

	dirIDStr := chi.URLParam(r, "dirId")
	dirID, err := strconv.ParseUint(dirIDStr, 10, 32)
	if err != nil {
		http.Error(w, `{"error":"invalid directory id"}`, http.StatusBadRequest)
		return
	}

	if err := h.Service.DeleteSearchDir(r.Context(), serverID, uint(dirID)); err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "search directory removed successfully"})
}

// ScanSourcesHandler triggers an immediate host scan
func (h *Handler) ScanSourcesHandler(w http.ResponseWriter, r *http.Request) {
	serverID, err := parseServerID(r)
	if err != nil {
		http.Error(w, `{"error":"invalid server id"}`, http.StatusBadRequest)
		return
	}

	sudoPassword := r.Header.Get("X-Sudo-Password")

	isSSE := r.Header.Get("Accept") == "text/event-stream"
	if isSSE {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
			return
		}

		progressChan := make(chan models.SourceScanProgress, 50)
		doneChan := make(chan struct{})

		go func() {
			for progress := range progressChan {
				_, _ = w.Write([]byte(FormatProgressEvent(progress)))
				flusher.Flush()
			}
			close(doneChan)
		}()

		_, scanErr := h.Service.ScanServer(r.Context(), serverID, sudoPassword, progressChan)
		close(progressChan)
		<-doneChan

		if scanErr != nil {
			errEvent := models.SourceScanProgress{
				Stage: "error",
				Error: scanErr.Error(),
			}
			_, _ = w.Write([]byte(FormatProgressEvent(errEvent)))
			flusher.Flush()
		}
		return
	}

	// Standard JSON execution
	summary, scanErr := h.Service.ScanServer(r.Context(), serverID, sudoPassword, nil)
	if scanErr != nil {
		http.Error(w, `{"error":"`+scanErr.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(summary)
}

// ListSourceLinksHandler returns discovered links for images or stacks
func (h *Handler) ListSourceLinksHandler(w http.ResponseWriter, r *http.Request) {
	serverID, err := parseServerID(r)
	if err != nil {
		http.Error(w, `{"error":"invalid server id"}`, http.StatusBadRequest)
		return
	}

	kind := r.URL.Query().Get("kind")
	ref := r.URL.Query().Get("ref")

	links, err := h.Service.ListSourceLinks(r.Context(), serverID, kind, ref)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(links)
}

// ConfirmSourceLinkHandler confirms a candidate link
func (h *Handler) ConfirmSourceLinkHandler(w http.ResponseWriter, r *http.Request) {
	serverID, err := parseServerID(r)
	if err != nil {
		http.Error(w, `{"error":"invalid server id"}`, http.StatusBadRequest)
		return
	}

	linkIDStr := chi.URLParam(r, "linkId")
	linkID, err := strconv.ParseUint(linkIDStr, 10, 32)
	if err != nil {
		http.Error(w, `{"error":"invalid link id"}`, http.StatusBadRequest)
		return
	}

	if err := h.Service.ConfirmSourceLink(r.Context(), serverID, uint(linkID)); err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "source link confirmed"})
}

// RejectSourceLinkHandler rejects a candidate link
func (h *Handler) RejectSourceLinkHandler(w http.ResponseWriter, r *http.Request) {
	serverID, err := parseServerID(r)
	if err != nil {
		http.Error(w, `{"error":"invalid server id"}`, http.StatusBadRequest)
		return
	}

	linkIDStr := chi.URLParam(r, "linkId")
	linkID, err := strconv.ParseUint(linkIDStr, 10, 32)
	if err != nil {
		http.Error(w, `{"error":"invalid link id"}`, http.StatusBadRequest)
		return
	}

	if err := h.Service.RejectSourceLink(r.Context(), serverID, uint(linkID)); err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "source link rejected"})
}

// ListDiscoveredFilesHandler returns all discovered files on a server (unbound and bound)
func (h *Handler) ListDiscoveredFilesHandler(w http.ResponseWriter, r *http.Request) {
	serverID, err := parseServerID(r)
	if err != nil {
		http.Error(w, `{"error":"invalid server id"}`, http.StatusBadRequest)
		return
	}

	kind := r.URL.Query().Get("kind")
	files, err := h.Service.ListDiscoveredFiles(r.Context(), serverID, kind)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(files)
}

// GetSourceContentHandler reads the file content for a discovered source file in read-only mode
func (h *Handler) GetSourceContentHandler(w http.ResponseWriter, r *http.Request) {
	serverID, err := parseServerID(r)
	if err != nil {
		http.Error(w, `{"error":"invalid server id"}`, http.StatusBadRequest)
		return
	}

	filePath := r.URL.Query().Get("path")
	if filePath == "" {
		http.Error(w, `{"error":"path query parameter is required"}`, http.StatusBadRequest)
		return
	}

	content, err := h.Service.GetSourceFileContent(r.Context(), serverID, filePath)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "access denied") || strings.Contains(err.Error(), "disallowed") {
			status = http.StatusForbidden
		} else if strings.Contains(err.Error(), "must be a valid") {
			status = http.StatusBadRequest
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"file_path":    filePath,
		"content":      content,
		"is_read_only": true,
	})
}

// BindSourceHandler manually binds a source file to an image or stack
func (h *Handler) BindSourceHandler(w http.ResponseWriter, r *http.Request) {
	serverID, err := parseServerID(r)
	if err != nil {
		http.Error(w, `{"error":"invalid server id"}`, http.StatusBadRequest)
		return
	}

	var req models.BindSourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	link, err := h.Service.BindSource(r.Context(), serverID, req)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(link)
}

// UnbindSourceHandler unbinds a source file from its workload
func (h *Handler) UnbindSourceHandler(w http.ResponseWriter, r *http.Request) {
	serverID, err := parseServerID(r)
	if err != nil {
		http.Error(w, `{"error":"invalid server id"}`, http.StatusBadRequest)
		return
	}

	var req models.UnbindSourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	if err := h.Service.UnbindSource(r.Context(), serverID, req); err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "source unbind successful"})
}

func parseServerID(r *http.Request) (uint, error) {
	idParam := chi.URLParam(r, "id")
	if idParam == "" {
		idParam = r.URL.Query().Get("server_id")
	}
	if idParam == "" {
		idParam = r.Header.Get("X-Server-ID")
	}
	id, err := strconv.ParseUint(idParam, 10, 32)
	if err != nil {
		return 0, err
	}
	return uint(id), nil
}
