package source

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kraken/admin-tool/backend/internal/host"
	"github.com/kraken/admin-tool/backend/internal/models"
)

// CandidateType represents the category of detected container source file
type CandidateType string

const (
	CandidateDockerfile CandidateType = "dockerfile"
	CandidateCompose    CandidateType = "compose"
)

var (
	disallowedSystemPaths = []string{
		"/",
		"/proc",
		"/sys",
		"/dev",
		"/run",
		"/boot",
	}
)

// ValidateSearchPath validates that an administrator-provided search directory path is safe
func ValidateSearchPath(path string) error {
	p := strings.TrimSpace(path)
	if p == "" {
		return errors.New("search path cannot be empty")
	}
	clean := filepath.Clean(p)
	if !filepath.IsAbs(clean) && !strings.HasPrefix(clean, "/") {
		return fmt.Errorf("search path must be an absolute path: %s", clean)
	}

	for _, disallowed := range disallowedSystemPaths {
		if clean == disallowed {
			return fmt.Errorf("scanning system root or pseudo-filesystem path '%s' is disallowed", clean)
		}
	}
	return nil
}

// CandidateFile holds metadata and content of a discovered candidate source file
type CandidateFile struct {
	Path        string
	Type        CandidateType
	Heuristic   bool
	Content     string
	ContentHash string
	MTime       int64
}

// Scanner orchestrates discovery and reading of container source files on target hosts
type Scanner struct {
	executor host.Executor
	isLocal  bool
}

// NewScanner creates a new Scanner using a given host executor (SSH or Local)
func NewScanner(exec host.Executor) *Scanner {
	_, isLocal := exec.(*host.LocalExecutor)
	return &Scanner{
		executor: exec,
		isLocal:  isLocal,
	}
}

// getHostMountPrefix detects if running inside a container with the host root filesystem mounted at /host
func getHostMountPrefix() string {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		if fi, err := os.Stat("/host"); err == nil && fi.IsDir() {
			return "/host"
		}
	}
	return ""
}

// ScanDirectory discovers and loads candidates from a single SourceSearchDir
func (s *Scanner) ScanDirectory(ctx context.Context, dir models.SourceSearchDir, sudoPassword string) ([]CandidateFile, error) {
	if err := ValidateSearchPath(dir.Path); err != nil {
		return nil, err
	}

	maxDepth := dir.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 5
	}
	if maxDepth > 15 {
		maxDepth = 15
	}

	depthArg := fmt.Sprintf("-maxdepth %d", maxDepth)
	if !dir.Recursive {
		depthArg = "-maxdepth 1"
	}

	cleanPath := filepath.Clean(dir.Path)

	// If scanning locally inside a container, resolve against /host mount if available
	hostPrefix := ""
	execSearchPath := cleanPath
	if s.isLocal {
		hostPrefix = getHostMountPrefix()
		if hostPrefix != "" && !strings.HasPrefix(cleanPath, hostPrefix) {
			execSearchPath = filepath.Clean(hostPrefix + "/" + strings.TrimPrefix(cleanPath, "/"))
		}
	}

	qPath := quoteForShell(execSearchPath)

	// 1. Discover by filename pattern: dockerfile* | *.dockerfile | docker-compose*.y*ml | compose*.y*ml
	// Explicitly ignore *.dockerignore, *.bak, *.swp
	nameCmd := fmt.Sprintf(
		"find %s -xdev %s -type f -size -512k "+
			"! -path '*/.*' ! -path '*/node_modules/*' ! -path '*/vendor/*' "+
			"! -iname '*.dockerignore' ! -iname '*.bak' ! -iname '*.swp' "+
			"! -name '*.go' ! -name '*.ts' ! -name '*.tsx' ! -name '*.js' ! -name '*.jsx' "+
			"! -name '*.json' ! -name '*.md' ! -name '*.py' ! -name '*.html' ! -name '*.css' "+
			"\\( -iname 'dockerfile*' -o -iname '*.dockerfile' -o -iname 'docker-compose*.yml' -o -iname 'docker-compose*.yaml' -o -iname 'compose*.yml' -o -iname 'compose*.yaml' \\) "+
			"-print 2>/dev/null | head -n 300",
		qPath, depthArg,
	)

	stdout, _, _, err := s.runCmd(ctx, nameCmd, sudoPassword)
	if err != nil {
		return nil, fmt.Errorf("failed to scan for named source files in %s: %w", cleanPath, err)
	}

	candidateMap := make(map[string]CandidateType)
	for _, line := range strings.Split(stdout, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		// Strip /host prefix to retain the canonical host path
		canonicalPath := trimmed
		if hostPrefix != "" && strings.HasPrefix(canonicalPath, hostPrefix) {
			canonicalPath = filepath.Clean(strings.TrimPrefix(canonicalPath, hostPrefix))
		}

		base := strings.ToLower(filepath.Base(canonicalPath))
		ext := strings.ToLower(filepath.Ext(canonicalPath))
		if strings.HasSuffix(base, ".dockerignore") || ext == ".go" || ext == ".ts" || ext == ".tsx" || ext == ".js" || ext == ".jsx" || ext == ".md" || ext == ".json" || ext == ".py" || ext == ".html" || ext == ".css" {
			continue
		}

		if strings.HasPrefix(base, "dockerfile") || strings.HasSuffix(base, ".dockerfile") {
			candidateMap[canonicalPath] = CandidateDockerfile
		} else if strings.Contains(base, "compose") {
			candidateMap[canonicalPath] = CandidateCompose
		}
	}

	// 2. Discover heuristic candidates: files without obvious names but containing Dockerfile/Compose characteristics
	heuristicCmd := fmt.Sprintf(
		"find %s -xdev %s -type f -size -256k "+
			"! -path '*/.*' ! -path '*/node_modules/*' ! -path '*/vendor/*' "+
			"! -name '*.go' ! -name '*.js' ! -name '*.ts' ! -name '*.tsx' ! -name '*.jsx' "+
			"! -name '*.json' ! -name '*.html' ! -name '*.css' ! -name '*.md' ! -name '*.log' "+
			"! -name '*.dockerignore' ! -name '*.bak' "+
			"-exec grep -l -m 1 -E '^[[:space:]]*(FROM|ARG|services:)[[:space:]]' {} + 2>/dev/null | head -n 150",
		qPath, depthArg,
	)

	hStdout, _, _, hErr := s.runCmd(ctx, heuristicCmd, sudoPassword)
	if hErr == nil {
		for _, line := range strings.Split(hStdout, "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue
			}
			canonicalPath := trimmed
			if hostPrefix != "" && strings.HasPrefix(canonicalPath, hostPrefix) {
				canonicalPath = filepath.Clean(strings.TrimPrefix(canonicalPath, hostPrefix))
			}

			base := strings.ToLower(filepath.Base(canonicalPath))
			if strings.HasSuffix(base, ".dockerignore") {
				continue
			}

			if _, exists := candidateMap[canonicalPath]; !exists {
				candidateMap[canonicalPath] = ""
			}
		}
	}

	// 3. Read and validate candidate contents
	var results []CandidateFile
	for path, knownType := range candidateMap {
		content, mtime, readErr := s.readFile(ctx, path, sudoPassword)
		if readErr != nil || strings.TrimSpace(content) == "" {
			continue
		}

		cType := knownType
		isHeuristic := false
		if cType == "" {
			isHeuristic = true
			cType = classifyContentHeuristically(path, content)
			if cType == "" {
				continue
			}
		}

		hash := sha256.Sum256([]byte(content))
		results = append(results, CandidateFile{
			Path:        path,
			Type:        cType,
			Heuristic:   isHeuristic,
			Content:     content,
			ContentHash: hex.EncodeToString(hash[:]),
			MTime:       mtime,
		})
	}

	return results, nil
}

// readFile reads up to 512KB of file content encoded in base64 along with mtime
func (s *Scanner) readFile(ctx context.Context, path string, sudoPassword string) (string, int64, error) {
	targetPath := path
	if s.isLocal {
		if hostPrefix := getHostMountPrefix(); hostPrefix != "" && !strings.HasPrefix(targetPath, hostPrefix) {
			targetPath = filepath.Clean(hostPrefix + "/" + strings.TrimPrefix(targetPath, "/"))
		}
	}

	qPath := quoteForShell(targetPath)
	cmd := fmt.Sprintf("head -c 524288 %s 2>/dev/null | base64; echo '---MTIME---'; stat -c %%Y %s 2>/dev/null || echo 0", qPath, qPath)
	out, _, exitCode, err := s.runCmd(ctx, cmd, sudoPassword)
	if err != nil || exitCode != 0 {
		return "", 0, fmt.Errorf("failed to read file: %s", path)
	}

	parts := strings.Split(out, "---MTIME---")
	if len(parts) == 0 {
		return "", 0, errors.New("empty output")
	}

	b64Content := strings.TrimSpace(parts[0])
	var mtime int64
	if len(parts) > 1 {
		mtimeStr := strings.TrimSpace(parts[1])
		if parsed, err := strconv.ParseInt(mtimeStr, 10, 64); err == nil {
			mtime = parsed
		}
	}

	decoded, err := base64.StdEncoding.DecodeString(b64Content)
	if err != nil {
		// Fallback to raw string if base64 decoding failed
		return b64Content, mtime, nil
	}

	return string(decoded), mtime, nil
}

func (s *Scanner) runCmd(ctx context.Context, cmd string, sudoPassword string) (string, string, int, error) {
	if s.executor == nil {
		return "", "", -1, errors.New("no executor provided")
	}
	if strings.TrimSpace(sudoPassword) != "" {
		return s.executor.ExecuteSudo(ctx, cmd, sudoPassword)
	}
	return s.executor.Execute(ctx, cmd)
}

func quoteForShell(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// classifyContentHeuristically checks if content represents a Dockerfile or a Compose file
func classifyContentHeuristically(path string, content string) CandidateType {
	base := strings.ToLower(filepath.Base(path))
	if strings.HasSuffix(base, ".dockerignore") || strings.HasSuffix(base, ".md") || strings.HasSuffix(base, ".txt") {
		return ""
	}

	lines := strings.Split(content, "\n")
	hasFrom := false
	hasDirectives := false
	hasServices := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		upper := strings.ToUpper(trimmed)
		if strings.HasPrefix(upper, "FROM ") || strings.HasPrefix(upper, "ARG ") {
			hasFrom = true
		}
		if strings.HasPrefix(upper, "RUN ") || strings.HasPrefix(upper, "COPY ") ||
			strings.HasPrefix(upper, "ADD ") || strings.HasPrefix(upper, "ENTRYPOINT ") ||
			strings.HasPrefix(upper, "CMD ") || strings.HasPrefix(upper, "EXPOSE ") {
			hasDirectives = true
		}

		if strings.HasPrefix(trimmed, "services:") || strings.HasPrefix(trimmed, "version:") {
			hasServices = true
		}
	}

	if hasFrom && hasDirectives {
		return CandidateDockerfile
	}
	if hasServices {
		return CandidateCompose
	}
	return ""
}
