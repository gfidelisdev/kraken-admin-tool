package plugins

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/kraken/admin-tool/backend/internal/auth"
	"github.com/kraken/admin-tool/backend/internal/models"
)

//go:embed KRAKEN_PLUGIN_AGENT_GUIDE.md
var aiPluginGuideContent string

type Handler struct {
	mgr *Manager
}

func NewHandler(mgr *Manager) *Handler {
	return &Handler{mgr: mgr}
}

// ListPluginsHandler returns installed plugins and active UI extensions
func (h *Handler) ListPluginsHandler(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetUserFromContext(r.Context())
	var userID uint = 0
	if claims != nil {
		userID = claims.UserID
	}

	plugins, err := h.mgr.ListPluginsForUser(userID)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	uiExt, _ := h.mgr.GetActiveUIExtensions(userID)

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"plugins":       plugins,
		"ui_extensions": uiExt,
	})
}

// ListStorePluginsHandler returns plugins from online store catalogs
func (h *Handler) ListStorePluginsHandler(w http.ResponseWriter, r *http.Request) {
	items, err := h.mgr.GetStoreService().ListStorePlugins(r.Context())
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"plugins": items,
	})
}

// InspectUploadHandler inspects an uploaded archive without installing
func (h *Handler) InspectUploadHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(MaxArchiveSizeBytes); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Failed to parse multipart form"})
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Missing 'file' form field"})
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, MaxArchiveSizeBytes+1))
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to read file"})
		return
	}

	insp, err := h.mgr.InspectUpload(header.Filename, data)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	respondJSON(w, http.StatusOK, insp)
}

// InspectURLHandler downloads and inspects a plugin from a remote URL without installing
func (h *Handler) InspectURLHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.URL) == "" {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Valid 'url' is required"})
		return
	}

	insp, err := h.mgr.InspectURL(r.Context(), strings.TrimSpace(req.URL))
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	respondJSON(w, http.StatusOK, insp)
}

// InspectDirectoryHandler inspects an uncompressed plugin directory on disk
func (h *Handler) InspectDirectoryHandler(w http.ResponseWriter, r *http.Request) {
	var req models.InspectDirectoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Path) == "" {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Valid 'path' is required"})
		return
	}

	insp, err := h.mgr.InspectDirectory(strings.TrimSpace(req.Path))
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	respondJSON(w, http.StatusOK, insp)
}

// InstallUploadHandler extracts, verifies and installs a plugin from an uploaded file
func (h *Handler) InstallUploadHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(MaxArchiveSizeBytes); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Failed to parse multipart form"})
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Missing 'file' field"})
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, MaxArchiveSizeBytes+1))
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to read archive"})
		return
	}

	expectedSHA := strings.TrimSpace(r.FormValue("expected_sha256"))
	enableImmediately := r.FormValue("enable_immediately") == "true"

	var perms []string
	permsRaw := r.FormValue("permissions")
	if permsRaw != "" {
		_ = json.Unmarshal([]byte(permsRaw), &perms)
	}

	claims := auth.GetUserFromContext(r.Context())
	username := "system"
	userID := uint(0)
	if claims != nil {
		username = claims.Username
		userID = claims.UserID
	}

	p, err := h.mgr.InstallFromArchive(r.Context(), header.Filename, data, expectedSHA, perms, enableImmediately, username, "", userID)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	respondJSON(w, http.StatusCreated, p)
}

// InstallURLHandler downloads and installs a plugin from a direct URL
func (h *Handler) InstallURLHandler(w http.ResponseWriter, r *http.Request) {
	var req models.InstallPluginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request payload"})
		return
	}

	claims := auth.GetUserFromContext(r.Context())
	username := "system"
	userID := uint(0)
	if claims != nil {
		username = claims.Username
		userID = claims.UserID
	}

	if strings.TrimSpace(req.Path) != "" {
		p, err := h.mgr.InstallFromDirectory(r.Context(), strings.TrimSpace(req.Path), req.ExpectedSHA256, req.GrantedPermissions, req.EnableImmediately, username, userID)
		if err != nil {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		respondJSON(w, http.StatusCreated, p)
		return
	}

	if strings.TrimSpace(req.URL) == "" {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Valid 'url' or 'path' is required"})
		return
	}

	cleanURL := strings.TrimSpace(req.URL)
	data, _, err := h.mgr.GetStoreService().DownloadArchive(r.Context(), cleanURL)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	filename := ExtractFilenameFromURL(cleanURL)

	p, err := h.mgr.InstallFromArchive(r.Context(), filename, data, req.ExpectedSHA256, req.GrantedPermissions, req.EnableImmediately, username, cleanURL, userID)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	respondJSON(w, http.StatusCreated, p)
}

// InstallDirectoryHandler installs a plugin from an uncompressed directory on disk
func (h *Handler) InstallDirectoryHandler(w http.ResponseWriter, r *http.Request) {
	var req models.InstallPluginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Path) == "" {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Valid 'path' is required"})
		return
	}

	claims := auth.GetUserFromContext(r.Context())
	username := "system"
	userID := uint(0)
	if claims != nil {
		username = claims.Username
		userID = claims.UserID
	}

	p, err := h.mgr.InstallFromDirectory(r.Context(), strings.TrimSpace(req.Path), req.ExpectedSHA256, req.GrantedPermissions, req.EnableImmediately, username, userID)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	respondJSON(w, http.StatusCreated, p)
}

// EnablePluginHandler activates an installed plugin
func (h *Handler) EnablePluginHandler(w http.ResponseWriter, r *http.Request) {
	pluginID := chi.URLParam(r, "id")
	claims := auth.GetUserFromContext(r.Context())
	username := "system"
	userID := uint(0)
	if claims != nil {
		username = claims.Username
		userID = claims.UserID
	}

	if err := h.mgr.EnablePlugin(r.Context(), pluginID, username, userID); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"message": "Plugin enabled successfully"})
}

// DisablePluginHandler deactivates a running plugin
func (h *Handler) DisablePluginHandler(w http.ResponseWriter, r *http.Request) {
	pluginID := chi.URLParam(r, "id")
	claims := auth.GetUserFromContext(r.Context())
	username := "system"
	userID := uint(0)
	if claims != nil {
		username = claims.Username
		userID = claims.UserID
	}

	if err := h.mgr.DisablePlugin(r.Context(), pluginID, username, userID); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"message": "Plugin disabled successfully"})
}

// DeletePluginHandler uninstalls a plugin
func (h *Handler) DeletePluginHandler(w http.ResponseWriter, r *http.Request) {
	pluginID := chi.URLParam(r, "id")
	claims := auth.GetUserFromContext(r.Context())
	username := "system"
	userID := uint(0)
	if claims != nil {
		username = claims.Username
		userID = claims.UserID
	}

	if err := h.mgr.DeletePlugin(r.Context(), pluginID, username, userID); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"message": "Plugin deleted successfully"})
}

// RebuildPluginHandler compiles/re-compiles the plugin WASM from Go sources
func (h *Handler) RebuildPluginHandler(w http.ResponseWriter, r *http.Request) {
	pluginID := chi.URLParam(r, "id")
	claims := auth.GetUserFromContext(r.Context())
	username := "system"
	userID := uint(0)
	if claims != nil {
		username = claims.Username
		userID = claims.UserID
	}

	result, err := h.mgr.RebuildPlugin(r.Context(), pluginID, username, userID)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"message": "Plugin compiled successfully",
		"result":  result,
	})
}

// GetWorkerJSHandler serves the plugin frontend worker bundle
func (h *Handler) GetWorkerJSHandler(w http.ResponseWriter, r *http.Request) {
	pluginID := chi.URLParam(r, "id")
	jsBytes, err := h.mgr.GetPluginWorkerJS(pluginID)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Plugin worker script not found"})
		return
	}

	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(jsBytes)
}

// GetAIGuideHandler serves the markdown developer instruction guide for AI agents
func (h *Handler) GetAIGuideHandler(w http.ResponseWriter, r *http.Request) {
	format := r.URL.Query().Get("format")
	if format == "json" {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"filename": "KRAKEN_PLUGIN_AGENT_GUIDE.md",
			"content":  aiPluginGuideContent,
		})
		return
	}

	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=\"KRAKEN_PLUGIN_AGENT_GUIDE.md\"")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(aiPluginGuideContent))
}

// CallPluginHandler dispatches action invocation to plugin runtime
func (h *Handler) CallPluginHandler(w http.ResponseWriter, r *http.Request) {
	pluginID := chi.URLParam(r, "id")
	var req models.PluginCallRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Action) == "" {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Valid 'action' is required"})
		return
	}

	claims := auth.GetUserFromContext(r.Context())
	if claims == nil {
		respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
		return
	}
	if claims.Role != "admin" && claims.Role != "operator" {
		respondJSON(w, http.StatusForbidden, map[string]string{"error": "Forbidden: insufficient permissions"})
		return
	}

	actionClean := strings.TrimSpace(req.Action)
	// Internal secret resolution should NOT be directly callable via external CallPluginHandler HTTP endpoint
	if actionClean == models.PluginActionVaultResolveSecret || actionClean == "credentials.resolve" {
		if claims.Role != "admin" {
			respondJSON(w, http.StatusForbidden, map[string]string{"error": "Forbidden: direct secret resolution requires admin role"})
			return
		}
	}

	username := claims.Username
	userRole := claims.Role
	userID := claims.UserID

	res, err := h.mgr.CallPlugin(r.Context(), pluginID, actionClean, req.Payload, username, userRole, userID)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, models.PluginCallResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	respondJSON(w, http.StatusOK, models.PluginCallResponse{
		Success: true,
		Result:  res,
	})
}

// ListCatalogSourcesHandler returns configured catalog sources
func (h *Handler) ListCatalogSourcesHandler(w http.ResponseWriter, r *http.Request) {
	var sources []models.PluginCatalogSource
	if err := h.mgr.db.Order("id ASC").Find(&sources).Error; err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	respondJSON(w, http.StatusOK, sources)
}

// CreateCatalogSourceHandler adds a new catalog URL
func (h *Handler) CreateCatalogSourceHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.URL) == "" {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Valid URL is required"})
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = req.URL
	}

	src := models.PluginCatalogSource{
		Name:    name,
		URL:     strings.TrimSpace(req.URL),
		Enabled: true,
	}

	if err := h.mgr.db.Create(&src).Error; err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Failed to add catalog: " + err.Error()})
		return
	}

	// Trigger immediate initial sync
	go func() {
		_, _ = h.mgr.GetStoreService().SyncCatalog(context.Background(), src.ID)
	}()

	respondJSON(w, http.StatusCreated, src)
}

// DeleteCatalogSourceHandler removes a catalog source
func (h *Handler) DeleteCatalogSourceHandler(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid ID"})
		return
	}

	if err := h.mgr.db.Delete(&models.PluginCatalogSource{}, uint(id)).Error; err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"message": "Catalog source deleted"})
}

// SyncCatalogSourceHandler forces a catalog refresh
func (h *Handler) SyncCatalogSourceHandler(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid ID"})
		return
	}

	idx, err := h.mgr.GetStoreService().SyncCatalog(r.Context(), uint(id))
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	respondJSON(w, http.StatusOK, idx)
}

// ListTrustedKeysHandler returns configured trusted public keys
func (h *Handler) ListTrustedKeysHandler(w http.ResponseWriter, r *http.Request) {
	keys, err := h.mgr.GetTrustedKeys()
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	respondJSON(w, http.StatusOK, keys)
}

// CreateTrustedKeyHandler registers an Ed25519 public key
func (h *Handler) CreateTrustedKeyHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name             string `json:"name"`
		PublicKeyEd25519 string `json:"public_key_ed25519"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}

	pubBytes, err := base64.StdEncoding.DecodeString(strings.TrimSpace(req.PublicKeyEd25519))
	if err != nil || len(pubBytes) != ed25519.PublicKeySize {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid Ed25519 public key: must be base64-encoded 32-byte key"})
		return
	}

	fpHash := sha256.Sum256(pubBytes)
	fingerprint := hex.EncodeToString(fpHash[:16])

	claims := auth.GetUserFromContext(r.Context())
	username := "system"
	if claims != nil {
		username = claims.Username
	}

	key := models.PluginTrustedKey{
		Name:             strings.TrimSpace(req.Name),
		PublicKeyEd25519: strings.TrimSpace(req.PublicKeyEd25519),
		Fingerprint:      fingerprint,
		AddedBy:          username,
	}

	if err := h.mgr.db.Create(&key).Error; err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Key already exists or invalid: " + err.Error()})
		return
	}

	respondJSON(w, http.StatusCreated, key)
}

// DeleteTrustedKeyHandler removes a trusted key
func (h *Handler) DeleteTrustedKeyHandler(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid ID"})
		return
	}

	if err := h.mgr.db.Delete(&models.PluginTrustedKey{}, uint(id)).Error; err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"message": "Trusted key deleted"})
}

// ListVaultSecretsHandler queries available secrets from the specified Key Vault plugin
func (h *Handler) ListVaultSecretsHandler(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetUserFromContext(r.Context())
	if claims == nil {
		respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
		return
	}
	if claims.Role != "admin" && claims.Role != "operator" {
		respondJSON(w, http.StatusForbidden, map[string]string{"error": "Forbidden: insufficient permissions"})
		return
	}

	pluginID := chi.URLParam(r, "id")
	if strings.TrimSpace(pluginID) == "" {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Plugin ID is required"})
		return
	}

	var req models.ListVaultSecretsRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	req.CredentialPluginID = pluginID

	resp, err := h.mgr.ListSecrets(r.Context(), req)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	respondJSON(w, http.StatusOK, resp)
}

// PutVaultSecretHandler creates or updates a secret in the Key Vault via plugin
func (h *Handler) PutVaultSecretHandler(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetUserFromContext(r.Context())
	if claims == nil {
		respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
		return
	}
	if claims.Role != "admin" && claims.Role != "operator" {
		respondJSON(w, http.StatusForbidden, map[string]string{"error": "Forbidden: insufficient permissions"})
		return
	}

	pluginID := chi.URLParam(r, "id")
	if strings.TrimSpace(pluginID) == "" {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Plugin ID is required"})
		return
	}

	var req models.PutVaultSecretRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}
	req.CredentialPluginID = pluginID

	item, err := h.mgr.PutSecret(r.Context(), req)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	respondJSON(w, http.StatusCreated, item)
}

// TestVaultConnectionHandler tests authentication and connectivity to the Key Vault
func (h *Handler) TestVaultConnectionHandler(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetUserFromContext(r.Context())
	if claims == nil {
		respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
		return
	}
	if claims.Role != "admin" && claims.Role != "operator" {
		respondJSON(w, http.StatusForbidden, map[string]string{"error": "Forbidden: insufficient permissions"})
		return
	}

	pluginID := chi.URLParam(r, "id")
	if strings.TrimSpace(pluginID) == "" {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Plugin ID is required"})
		return
	}

	var req models.TestVaultConnectionRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	req.CredentialPluginID = pluginID

	resp, err := h.mgr.TestVaultConnection(r.Context(), req)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	respondJSON(w, http.StatusOK, resp)
}

func respondJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(data)
}
