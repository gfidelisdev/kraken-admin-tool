package plugins

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kraken/admin-tool/backend/internal/models"
)

var (
	pluginIDRegex = regexp.MustCompile(`^[a-zA-Z0-9][-a-zA-Z0-9_.]*[a-zA-Z0-9]$`)
	versionRegex  = regexp.MustCompile(`^v?[0-9]+(\.[0-9]+)*(-[a-zA-Z0-9_.-]+)?$`)
)

// AllowedPermissions defines the valid permission scopes that a plugin can declare
var AllowedPermissions = map[string]string{
	"ui:sidebar":          "Adicionar entrada na barra lateral de navegação",
	"ui:dashboard_widget": "Renderizar widget no painel de visão geral (Overview)",
	"ui:server_tab":       "Adicionar aba ou ação de host na lista de Docker Hosts",
	"docker:read":         "Consultar containers, imagens, volumes e stacks Docker",
	"docker:write":        "Operar containers, iniciar/parar stacks e gerenciar imagens",
	"hosts:read":          "Consultar pacotes e inventário de hosts remotos",
	"hosts:exec":          "Executar comandos administrativos no host via SSH",
	"k8s:read":            "Consultar Pods e Deployments de clusters Kubernetes",
	"k8s:write":           "Reiniciar ou escalar cargas de trabalho Kubernetes",
	"storage:kv":          "Armazenamento de dados chave-valor isolado para o plugin",
	"http:outbound":       "Realizar chamadas HTTP externas para hosts autorizados",
}

// ParseAndValidateManifest parses raw plugin.json bytes and validates security & schema constraints
func ParseAndValidateManifest(data []byte) (*models.PluginManifest, error) {
	if len(data) == 0 {
		return nil, errors.New("manifest content is empty")
	}

	var m models.PluginManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("invalid plugin.json: %w", err)
	}

	m.ID = strings.TrimSpace(m.ID)
	if m.ID == "" {
		return nil, errors.New("manifest missing required 'id' field")
	}
	if len(m.ID) > 128 || !pluginIDRegex.MatchString(m.ID) || strings.Contains(m.ID, "..") {
		return nil, fmt.Errorf("invalid plugin ID '%s': must be alphanumeric with dots, dashes or underscores (e.g. 'com.example.plugin')", m.ID)
	}

	m.Name = strings.TrimSpace(m.Name)
	if m.Name == "" {
		return nil, errors.New("manifest missing required 'name' field")
	}
	if len(m.Name) > 128 {
		return nil, errors.New("plugin name exceeds 128 characters")
	}

	m.Version = strings.TrimSpace(m.Version)
	if m.Version == "" {
		return nil, errors.New("manifest missing required 'version' field")
	}
	if !versionRegex.MatchString(m.Version) {
		return nil, fmt.Errorf("invalid version '%s': must follow SemVer format (e.g. '1.0.0')", m.Version)
	}

	// Validate runtime
	rtType := strings.ToLower(strings.TrimSpace(m.Runtime.Type))
	if rtType == "" {
		rtType = "none"
	}
	switch rtType {
	case "none", "wasm", "container":
		m.Runtime.Type = rtType
	default:
		return nil, fmt.Errorf("unsupported runtime type '%s': allowed values are 'none', 'wasm', 'container'", rtType)
	}

	if rtType == "wasm" {
		ep := strings.TrimSpace(m.Runtime.Entrypoint)
		if ep == "" {
			ep = "backend/plugin.wasm"
		}
		if err := validateRelativeSafePath(ep); err != nil {
			return nil, fmt.Errorf("invalid wasm entrypoint: %w", err)
		}
		m.Runtime.Entrypoint = ep
	} else if rtType == "container" {
		ep := strings.TrimSpace(m.Runtime.Entrypoint)
		df := strings.TrimSpace(m.Runtime.Dockerfile)

		// If entrypoint looks like a Dockerfile, assign it to Dockerfile
		if df == "" && (strings.EqualFold(filepath.Base(ep), "dockerfile") || strings.HasSuffix(strings.ToLower(ep), ".dockerfile")) {
			df = ep
			ep = ""
		}

		if df != "" {
			if err := validateRelativeSafePath(df); err != nil {
				return nil, fmt.Errorf("invalid dockerfile path: %w", err)
			}
			m.Runtime.Dockerfile = filepath.ToSlash(df)
		}

		if ep == "" {
			ep = fmt.Sprintf("kraken-plugin-%s:%s", sanitizeContainerName(m.ID), m.Version)
		}
		m.Runtime.Entrypoint = ep

		if m.Runtime.Port <= 0 || m.Runtime.Port > 65535 {
			m.Runtime.Port = 8080 // default container internal port
		}
	}

	// Validate frontend entrypoint if declared
	if strings.TrimSpace(m.Frontend.Entrypoint) != "" {
		if err := validateRelativeSafePath(m.Frontend.Entrypoint); err != nil {
			return nil, fmt.Errorf("invalid frontend entrypoint: %w", err)
		}
	}

	// Validate permissions
	cleanPerms := make([]string, 0, len(m.Permissions))
	seenPerms := make(map[string]bool)
	for _, p := range m.Permissions {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, ok := AllowedPermissions[p]; !ok {
			return nil, fmt.Errorf("unknown permission requested: '%s'", p)
		}
		if !seenPerms[p] {
			seenPerms[p] = true
			cleanPerms = append(cleanPerms, p)
		}
	}
	m.Permissions = cleanPerms

	// Validate UI extension IDs
	for i := range m.UIExtensions.Sidebar {
		ext := &m.UIExtensions.Sidebar[i]
		ext.ID = strings.TrimSpace(ext.ID)
		ext.Title = strings.TrimSpace(ext.Title)
		if ext.ID == "" {
			ext.ID = fmt.Sprintf("sidebar-%d", i)
		}
		if ext.Title == "" {
			ext.Title = m.Name
		}
	}

	for i := range m.UIExtensions.DashboardWidgets {
		ext := &m.UIExtensions.DashboardWidgets[i]
		ext.ID = strings.TrimSpace(ext.ID)
		ext.Title = strings.TrimSpace(ext.Title)
		if ext.ID == "" {
			ext.ID = fmt.Sprintf("widget-%d", i)
		}
		if ext.Title == "" {
			ext.Title = m.Name
		}
	}

	return &m, nil
}

// validateRelativeSafePath ensures a file path within a plugin archive is strictly relative and does not traverse upwards
func validateRelativeSafePath(p string) error {
	cleaned := filepath.Clean(filepath.ToSlash(p))
	if strings.HasPrefix(cleaned, "/") || strings.HasPrefix(cleaned, "\\") {
		return fmt.Errorf("path '%s' must not be absolute", p)
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.Contains(cleaned, "/../") {
		return fmt.Errorf("path '%s' attempts directory traversal", p)
	}
	return nil
}
