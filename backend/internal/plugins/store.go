package plugins

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/kraken/admin-tool/backend/internal/models"
)

type StoreService struct {
	db         *gorm.DB
	httpClient *http.Client
}

func NewStoreService(db *gorm.DB) *StoreService {
	return &StoreService{
		db: db,
		httpClient: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

// DownloadArchive fetches archive bytes from a direct URL and verifies size
func (s *StoreService) DownloadArchive(ctx context.Context, downloadURL string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", downloadURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("invalid download URL: %w", err)
	}
	req.Header.Set("User-Agent", "Kraken-Admin-Tool/1.0 Plugin-Installer")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("failed to download plugin archive: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("download failed with HTTP status: %d %s", resp.StatusCode, resp.Status)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxArchiveSizeBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("failed reading archive response: %w", err)
	}
	if int64(len(data)) > MaxArchiveSizeBytes {
		return nil, "", fmt.Errorf("downloaded archive exceeds maximum size limit of %d MB", MaxArchiveSizeBytes/(1024*1024))
	}

	hash := sha256.Sum256(data)
	sha256Hex := hex.EncodeToString(hash[:])

	return data, sha256Hex, nil
}

// SyncCatalog fetches and caches an online plugin catalog (index.json)
func (s *StoreService) SyncCatalog(ctx context.Context, sourceID uint) (*models.PluginCatalogIndex, error) {
	var src models.PluginCatalogSource
	if err := s.db.First(&src, sourceID).Error; err != nil {
		return nil, errors.New("catalog source not found")
	}

	req, err := http.NewRequestWithContext(ctx, "GET", src.URL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Kraken-Admin-Tool/1.0 Plugin-Catalog-Sync")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch catalog: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("catalog URL returned HTTP status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024))
	if err != nil {
		return nil, err
	}

	var index models.PluginCatalogIndex
	if err := json.Unmarshal(body, &index); err != nil {
		return nil, fmt.Errorf("invalid catalog index.json schema: %w", err)
	}

	now := time.Now()
	s.db.Model(&src).Updates(map[string]interface{}{
		"cached_index_json": string(body),
		"last_synced_at":    &now,
	})

	return &index, nil
}

// ListStorePlugins aggregates plugins from all enabled catalog sources
func (s *StoreService) ListStorePlugins(ctx context.Context) ([]models.PluginCatalogItem, error) {
	var sources []models.PluginCatalogSource
	if err := s.db.Where("enabled = ?", true).Find(&sources).Error; err != nil {
		return nil, err
	}

	// Fetch installed plugins to mark is_installed and versions
	var installed []models.InstalledPlugin
	_ = s.db.Find(&installed).Error
	installedMap := make(map[string]*models.InstalledPlugin)
	for i := range installed {
		installedMap[installed[i].PluginID] = &installed[i]
	}

	itemMap := make(map[string]models.PluginCatalogItem)

	// Built-in Demo Plugins so the user has immediate functional plugins to try
	demoItems := getBuiltinDemoPlugins()
	for _, item := range demoItems {
		itemMap[item.ID] = item
	}

	// Incorporate remote catalogs
	for _, src := range sources {
		if src.CachedIndexJSON == "" {
			// Trigger on-the-fly sync if not cached
			_, _ = s.SyncCatalog(ctx, src.ID)
			_ = s.db.First(&src, src.ID).Error
		}

		if src.CachedIndexJSON != "" {
			var idx models.PluginCatalogIndex
			if err := json.Unmarshal([]byte(src.CachedIndexJSON), &idx); err == nil {
				for _, it := range idx.Plugins {
					itemMap[it.ID] = it
				}
			}
		}
	}

	res := make([]models.PluginCatalogItem, 0, len(itemMap))
	for _, it := range itemMap {
		if inst, ok := installedMap[it.ID]; ok {
			it.IsInstalled = true
			it.InstalledVersion = inst.Version
			if it.Version != inst.Version {
				it.HasUpdate = true
			}
		}
		res = append(res, it)
	}

	return res, nil
}

func getBuiltinDemoPlugins() []models.PluginCatalogItem {
	return []models.PluginCatalogItem{
		{
			ID:          "com.kraken.port-auditor",
			Name:        "Host Port & Security Auditor",
			Version:     "1.0.0",
			Description: "Analisa portas de rede abertas e listeners ativos nos hosts Docker cadastrados.",
			Author:      "Kraken Security Team",
			License:     "MIT",
			Homepage:    "https://github.com/kraken-admin-tool/plugins",
			DownloadURL: "/builtin/plugins/host-port-auditor.zip",
			RuntimeType: "wasm",
			Permissions: []string{"ui:sidebar", "docker:read", "hosts:read", "storage:kv"},
			UIExtensions: models.PluginUIExtensions{
				Sidebar: []models.PluginSidebarExtension{
					{
						ID:    "port-auditor",
						Title: "Port Auditor",
						Icon:  "ShieldAlert",
						Badge: "WASM",
					},
				},
				DashboardWidgets: []models.PluginDashboardExtension{
					{
						ID:    "ports-widget",
						Title: "Portas Abertas nos Hosts",
					},
				},
			},
			ReadmeMarkdown: `# Host Port & Security Auditor
Audita portas expostas e serviços em execução nos nós gerenciados pelo Kraken Admin Tool.

- **Runtime**: WebAssembly (wazero Sandbox isolado)
- **UI**: Remote UI Nativo com suporte aos temas do Kraken
- **Permissões**: ui:sidebar, docker:read, hosts:read, storage:kv`,
		},
		{
			ID:          "com.kraken.system-benchmark",
			Name:        "Host & Container Benchmark",
			Version:     "1.0.0",
			Description: "Executa testes sintéticos de IOPS de disco e taxa de transferência de memória em hosts selecionados.",
			Author:      "Kraken Core",
			License:     "Apache-2.0",
			Homepage:    "https://github.com/kraken-admin-tool/plugins",
			DownloadURL: "/builtin/plugins/system-benchmark.zip",
			RuntimeType: "wasm",
			Permissions: []string{"ui:sidebar", "hosts:read", "hosts:exec", "storage:kv"},
			UIExtensions: models.PluginUIExtensions{
				Sidebar: []models.PluginSidebarExtension{
					{
						ID:    "benchmarks",
						Title: "Benchmarks",
						Icon:  "Gauge",
						Badge: "Plugin",
					},
				},
			},
			ReadmeMarkdown: `# Host Benchmark Suite
Mede desempenho de I/O e processamento dos hosts remotos via SSH isolado.`,
		},
	}
}

// ExtractFilenameFromURL parses the last path component of a URL to guess the file format
func ExtractFilenameFromURL(u string) string {
	parts := strings.Split(u, "?")
	path := parts[0]
	idx := strings.LastIndex(path, "/")
	if idx >= 0 && idx < len(path)-1 {
		return path[idx+1:]
	}
	return "plugin.zip"
}
