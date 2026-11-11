package source

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/kraken/admin-tool/backend/internal/docker"
	"github.com/kraken/admin-tool/backend/internal/host"
	"github.com/kraken/admin-tool/backend/internal/models"
)

// Service provides source file discovery, matching, and lifecycle management
type Service struct {
	db            *gorm.DB
	dockerService *docker.DockerService
	schedulerStop chan struct{}
	mu            sync.Mutex
}

// NewSourceService creates a new source Service
func NewSourceService(db *gorm.DB, dockerService *docker.DockerService) *Service {
	return &Service{
		db:            db,
		dockerService: dockerService,
		schedulerStop: make(chan struct{}),
	}
}

// ListSearchDirs returns all configured search directories for a given server
func (s *Service) ListSearchDirs(ctx context.Context, serverID uint) ([]models.SourceSearchDir, error) {
	if s.db == nil {
		return nil, errors.New("database not available")
	}
	var dirs []models.SourceSearchDir
	if err := s.db.WithContext(ctx).Where("server_id = ?", serverID).Order("created_at asc").Find(&dirs).Error; err != nil {
		return nil, err
	}
	return dirs, nil
}

// CreateSearchDir adds a new search directory for a server
func (s *Service) CreateSearchDir(ctx context.Context, serverID uint, req models.CreateSourceDirRequest) (*models.SourceSearchDir, error) {
	if err := ValidateSearchPath(req.Path); err != nil {
		return nil, err
	}

	cleanPath := filepath.Clean(strings.TrimSpace(req.Path))

	var existing models.SourceSearchDir
	if err := s.db.WithContext(ctx).Where("server_id = ? AND path = ?", serverID, cleanPath).First(&existing).Error; err == nil {
		return nil, fmt.Errorf("search directory '%s' already configured for this server", cleanPath)
	}

	recursive := true
	if req.Recursive != nil {
		recursive = *req.Recursive
	}
	maxDepth := req.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 5
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	scheduleEnabled := false
	if req.ScheduleEnabled != nil {
		scheduleEnabled = *req.ScheduleEnabled
	}
	scheduleCron := req.ScheduleCron
	if strings.TrimSpace(scheduleCron) == "" {
		scheduleCron = "@daily"
	}

	dir := models.SourceSearchDir{
		ServerID:        serverID,
		Path:            cleanPath,
		Recursive:       recursive,
		MaxDepth:        maxDepth,
		Enabled:         enabled,
		ScheduleEnabled: scheduleEnabled,
		ScheduleCron:    scheduleCron,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}

	if err := s.db.WithContext(ctx).Create(&dir).Error; err != nil {
		return nil, err
	}

	return &dir, nil
}

// UpdateSearchDir modifies an existing search directory configuration
func (s *Service) UpdateSearchDir(ctx context.Context, serverID uint, dirID uint, req models.UpdateSourceDirRequest) (*models.SourceSearchDir, error) {
	var dir models.SourceSearchDir
	if err := s.db.WithContext(ctx).Where("id = ? AND server_id = ?", dirID, serverID).First(&dir).Error; err != nil {
		return nil, errors.New("search directory not found")
	}

	if req.Path != "" {
		if err := ValidateSearchPath(req.Path); err != nil {
			return nil, err
		}
		dir.Path = filepath.Clean(strings.TrimSpace(req.Path))
	}
	if req.Recursive != nil {
		dir.Recursive = *req.Recursive
	}
	if req.MaxDepth > 0 {
		dir.MaxDepth = req.MaxDepth
	}
	if req.Enabled != nil {
		dir.Enabled = *req.Enabled
	}
	if req.ScheduleEnabled != nil {
		dir.ScheduleEnabled = *req.ScheduleEnabled
	}
	if req.ScheduleCron != "" {
		dir.ScheduleCron = strings.TrimSpace(req.ScheduleCron)
	}
	dir.UpdatedAt = time.Now()

	if err := s.db.WithContext(ctx).Save(&dir).Error; err != nil {
		return nil, err
	}

	return &dir, nil
}

// DeleteSearchDir removes a search directory
func (s *Service) DeleteSearchDir(ctx context.Context, serverID uint, dirID uint) error {
	return s.db.WithContext(ctx).Where("id = ? AND server_id = ?", dirID, serverID).Delete(&models.SourceSearchDir{}).Error
}

// WorkloadCandidate represents a matched candidate workload with its confidence score
type WorkloadCandidate struct {
	TargetRef string  `json:"target_ref"`
	Kind      string  `json:"kind"`
	Name      string  `json:"name"`
	Score     float64 `json:"score"`
}

// ScanSummary reports metrics of a completed scan
type ScanSummary struct {
	TotalFilesFound int `json:"total_files_found"`
	ImageMatches    int `json:"image_matches"`
	StackMatches    int `json:"stack_matches"`
}

// ScanServer runs candidate discovery and reverse-engineering comparison across all enabled search directories
func (s *Service) ScanServer(
	ctx context.Context,
	serverID uint,
	sudoPassword string,
	progressChan chan<- models.SourceScanProgress,
) (*ScanSummary, error) {
	summary := &ScanSummary{}

	sendProgress := func(stage, msg string, files, matches int, currentPath, errStr string) {
		if progressChan != nil {
			select {
			case progressChan <- models.SourceScanProgress{
				Stage:       stage,
				Message:     msg,
				FilesFound:  files,
				MatchesMade: matches,
				CurrentPath: currentPath,
				Error:       errStr,
			}:
			default:
			}
		}
	}

	sendProgress("discovering", "Resolving server connection and search directories...", 0, 0, "", "")

	dirs, err := s.ListSearchDirs(ctx, serverID)
	if err != nil {
		sendProgress("error", "Failed to retrieve search directories", 0, 0, "", err.Error())
		return nil, err
	}

	var enabledDirs []models.SourceSearchDir
	for _, d := range dirs {
		if d.Enabled {
			enabledDirs = append(enabledDirs, d)
		}
	}

	if len(enabledDirs) == 0 {
		sendProgress("completed", "No enabled search directories configured for this host.", 0, 0, "", "")
		return summary, nil
	}

	exec, err := s.resolveExecutor(ctx, serverID)
	if err != nil {
		sendProgress("error", "Failed to establish host executor", 0, 0, "", err.Error())
		return nil, err
	}

	scanner := NewScanner(exec)

	// Collect candidates from all enabled directories
	var allCandidates []CandidateFile
	for _, dir := range enabledDirs {
		sendProgress("discovering", fmt.Sprintf("Scanning directory %s ...", dir.Path), len(allCandidates), 0, dir.Path, "")
		candidates, scanErr := scanner.ScanDirectory(ctx, dir, sudoPassword)
		now := time.Now()
		if scanErr != nil {
			s.db.Model(&models.SourceSearchDir{}).Where("id = ?", dir.ID).Updates(map[string]interface{}{
				"last_scanned_at": now,
				"last_scan_error": scanErr.Error(),
			})
			continue
		}
		s.db.Model(&models.SourceSearchDir{}).Where("id = ?", dir.ID).Updates(map[string]interface{}{
			"last_scanned_at": now,
			"last_scan_error": "",
		})
		allCandidates = append(allCandidates, candidates...)
	}

	summary.TotalFilesFound = len(allCandidates)
	sendProgress("matching", fmt.Sprintf("Found %d candidate source files. Evaluating Docker workloads...", len(allCandidates)), len(allCandidates), 0, "", "")

	// 1. Evaluate Dockerfile candidates against Images on this server
	dockerfileCandidates := filterCandidates(allCandidates, CandidateDockerfile)
	cli, cliErr := s.dockerService.GetClient(ctx, serverID)
	images, _ := s.dockerService.ListImagesOnServer(ctx, serverID)

	for _, cand := range dockerfileCandidates {
		var candidateMatches []WorkloadCandidate

		if cliErr == nil && len(images) > 0 {
			for _, img := range images {
				cleanID := strings.TrimPrefix(img.ID, "sha256:")
				primaryTag := cleanID
				if len(img.RepoTags) > 0 && img.RepoTags[0] != "<none>:<none>" {
					primaryTag = img.RepoTags[0]
				}

				inspect, _, inspectErr := cli.ImageInspectWithRaw(ctx, img.ID)
				if inspectErr != nil {
					continue
				}
				history, _ := cli.ImageHistory(ctx, img.ID)
				score := MatchImageWithCandidate(inspect, history, cand)

				if score >= 0.20 {
					candidateMatches = append(candidateMatches, WorkloadCandidate{
						TargetRef: primaryTag,
						Kind:      "image",
						Name:      primaryTag,
						Score:     score,
					})
				}
			}
		}

		// Sort candidate suggestions descending by score
		sortCandidateMatches(candidateMatches)

		metadataJSON := ""
		if len(candidateMatches) > 0 {
			if b, err := json.Marshal(candidateMatches); err == nil {
				metadataJSON = string(b)
			}
		}

		targetRef := ""
		matchScore := 0.0
		status := "unbound"

		if len(candidateMatches) > 0 {
			top := candidateMatches[0]
			if top.Score >= 0.65 {
				targetRef = top.TargetRef
				matchScore = top.Score
				status = "auto_matched"
				summary.ImageMatches++
				sendProgress("matching", fmt.Sprintf("Auto-matched %s with image %s (score: %.2f)", cand.Path, targetRef, matchScore), summary.TotalFilesFound, summary.ImageMatches+summary.StackMatches, cand.Path, "")
			} else if top.Score >= 0.40 {
				targetRef = top.TargetRef
				matchScore = top.Score
				status = "candidate"
				summary.ImageMatches++
				sendProgress("matching", fmt.Sprintf("Candidate match %s with image %s (score: %.2f)", cand.Path, targetRef, matchScore), summary.TotalFilesFound, summary.ImageMatches+summary.StackMatches, cand.Path, "")
			}
		}

		// Save/persist discovered file in DB regardless of whether it was bound
		s.saveDiscoveredFile(ctx, serverID, "image", cand, targetRef, matchScore, status, metadataJSON)
	}

	// 2. Evaluate Compose candidates against Compose Stacks
	composeCandidates := filterCandidates(allCandidates, CandidateCompose)
	stacks, _ := s.dockerService.ListComposeStacks(ctx, serverID)

	for _, cand := range composeCandidates {
		var candidateMatches []WorkloadCandidate

		for _, st := range stacks {
			score := MatchStackWithCandidate(st, cand)
			if score >= 0.20 {
				candidateMatches = append(candidateMatches, WorkloadCandidate{
					TargetRef: st.Name,
					Kind:      "stack",
					Name:      st.Name,
					Score:     score,
				})
			}
		}

		sortCandidateMatches(candidateMatches)

		metadataJSON := ""
		if len(candidateMatches) > 0 {
			if b, err := json.Marshal(candidateMatches); err == nil {
				metadataJSON = string(b)
			}
		}

		targetRef := ""
		matchScore := 0.0
		status := "unbound"

		if len(candidateMatches) > 0 {
			top := candidateMatches[0]
			if top.Score >= 0.65 {
				targetRef = top.TargetRef
				matchScore = top.Score
				status = "auto_matched"
				summary.StackMatches++
				sendProgress("matching", fmt.Sprintf("Auto-matched %s with stack %s (score: %.2f)", cand.Path, targetRef, matchScore), summary.TotalFilesFound, summary.ImageMatches+summary.StackMatches, cand.Path, "")
			} else if top.Score >= 0.40 {
				targetRef = top.TargetRef
				matchScore = top.Score
				status = "candidate"
				summary.StackMatches++
				sendProgress("matching", fmt.Sprintf("Candidate match %s with stack %s (score: %.2f)", cand.Path, targetRef, matchScore), summary.TotalFilesFound, summary.ImageMatches+summary.StackMatches, cand.Path, "")
			}
		}

		s.saveDiscoveredFile(ctx, serverID, "stack", cand, targetRef, matchScore, status, metadataJSON)
	}

	sendProgress("completed", fmt.Sprintf("Scan completed! %d files discovered, %d workloads linked.", summary.TotalFilesFound, summary.ImageMatches+summary.StackMatches), summary.TotalFilesFound, summary.ImageMatches+summary.StackMatches, "", "")
	return summary, nil
}

func sortCandidateMatches(matches []WorkloadCandidate) {
	for i := 0; i < len(matches)-1; i++ {
		for j := i + 1; j < len(matches); j++ {
			if matches[j].Score > matches[i].Score {
				matches[i], matches[j] = matches[j], matches[i]
			}
		}
	}
}

// saveDiscoveredFile upserts a discovered host source file, preserving any manual administrator decisions
func (s *Service) saveDiscoveredFile(
	ctx context.Context,
	serverID uint,
	kind string,
	cand CandidateFile,
	targetRef string,
	score float64,
	status string,
	metadata string,
) {
	if s.db == nil {
		return
	}

	var existing models.SourceLink
	err := s.db.WithContext(ctx).Where("server_id = ? AND file_path = ?", serverID, cand.Path).First(&existing).Error
	if err == nil {
		// If administrator previously confirmed or rejected, keep their decision
		if existing.Status == "user_confirmed" || existing.Status == "user_rejected" {
			s.db.WithContext(ctx).Model(&existing).Updates(map[string]interface{}{
				"file_mtime":   cand.MTime,
				"content_hash": cand.ContentHash,
				"metadata":     metadata,
				"updated_at":   time.Now(),
			})
			return
		}

		s.db.WithContext(ctx).Model(&existing).Updates(map[string]interface{}{
			"kind":         kind,
			"target_ref":   targetRef,
			"file_mtime":   cand.MTime,
			"content_hash": cand.ContentHash,
			"match_score":  score,
			"status":       status,
			"metadata":     metadata,
			"updated_at":   time.Now(),
		})
	} else {
		newLink := models.SourceLink{
			ServerID:    serverID,
			Kind:        kind,
			TargetRef:   targetRef,
			FilePath:    cand.Path,
			FileMTime:   cand.MTime,
			ContentHash: cand.ContentHash,
			MatchScore:  score,
			Status:      status,
			Metadata:    metadata,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}
		s.db.WithContext(ctx).Create(&newLink)
	}
}

// ListDiscoveredFiles returns all discovered files on a server (with candidate recommendations and binding status)
func (s *Service) ListDiscoveredFiles(ctx context.Context, serverID uint, kind string) ([]models.SourceLink, error) {
	if s.db == nil {
		return nil, errors.New("database not available")
	}
	query := s.db.WithContext(ctx).Where("server_id = ?", serverID)
	if kind != "" {
		query = query.Where("kind = ?", kind)
	}
	var links []models.SourceLink
	if err := query.Order("updated_at desc, match_score desc").Find(&links).Error; err != nil {
		return nil, err
	}
	return links, nil
}

// BindSource manually binds a discovered host source file to an image or stack
func (s *Service) BindSource(ctx context.Context, serverID uint, req models.BindSourceRequest) (*models.SourceLink, error) {
	if s.db == nil {
		return nil, errors.New("database not available")
	}

	targetRef := strings.TrimSpace(req.TargetRef)
	if targetRef == "" {
		return nil, errors.New("target workload reference cannot be empty")
	}

	var link models.SourceLink
	if req.LinkID > 0 {
		if err := s.db.WithContext(ctx).Where("id = ? AND server_id = ?", req.LinkID, serverID).First(&link).Error; err != nil {
			return nil, errors.New("source link not found")
		}
	} else if req.FilePath != "" {
		cleanPath := filepath.Clean(strings.TrimSpace(req.FilePath))
		if err := s.db.WithContext(ctx).Where("server_id = ? AND file_path = ?", serverID, cleanPath).First(&link).Error; err != nil {
			link = models.SourceLink{
				ServerID:  serverID,
				FilePath:  cleanPath,
				Kind:      req.Kind,
				CreatedAt: time.Now(),
			}
		}
	} else {
		return nil, errors.New("either link_id or file_path is required")
	}

	kind := req.Kind
	if kind == "" {
		kind = link.Kind
	}
	if kind == "" {
		kind = "image"
	}

	link.Kind = kind
	link.TargetRef = targetRef
	link.Status = "user_confirmed"
	link.MatchScore = 1.0
	link.UpdatedAt = time.Now()

	if err := s.db.WithContext(ctx).Save(&link).Error; err != nil {
		return nil, err
	}

	return &link, nil
}

// UnbindSource unbinds a source file from its target workload
func (s *Service) UnbindSource(ctx context.Context, serverID uint, req models.UnbindSourceRequest) error {
	if s.db == nil {
		return errors.New("database not available")
	}

	query := s.db.WithContext(ctx).Model(&models.SourceLink{}).Where("server_id = ?", serverID)
	if req.LinkID > 0 {
		query = query.Where("id = ?", req.LinkID)
	} else if req.FilePath != "" {
		cleanPath := filepath.Clean(strings.TrimSpace(req.FilePath))
		query = query.Where("file_path = ?", cleanPath)
	} else {
		return errors.New("either link_id or file_path is required")
	}

	return query.Updates(map[string]interface{}{
		"target_ref":  "",
		"status":      "unbound",
		"match_score": 0.0,
		"updated_at":  time.Now(),
	}).Error
}

func isAllowedSourceFilename(p string) bool {
	base := strings.ToLower(filepath.Base(p))
	if base == "dockerfile" || strings.HasPrefix(base, "dockerfile.") || strings.HasSuffix(base, ".dockerfile") {
		return true
	}
	if strings.HasPrefix(base, "docker-compose") || strings.HasPrefix(base, "compose.") {
		if strings.HasSuffix(base, ".yml") || strings.HasSuffix(base, ".yaml") {
			return true
		}
	}
	return false
}

// GetSourceFileContent reads a discovered or search-directory-scoped source file from the host in read-only mode
func (s *Service) GetSourceFileContent(ctx context.Context, serverID uint, filePath string) (string, error) {
	if s.db == nil {
		return "", errors.New("database not available")
	}

	cleanPath := filepath.Clean(strings.TrimSpace(filePath))
	if cleanPath == "" || !filepath.IsAbs(cleanPath) {
		return "", errors.New("file path must be a valid absolute path")
	}

	if err := ValidateSearchPath(cleanPath); err != nil {
		return "", err
	}

	// Allow if explicitly registered as a discovered/bound SourceLink for this server
	var linkCount int64
	if err := s.db.WithContext(ctx).Model(&models.SourceLink{}).
		Where("server_id = ? AND file_path = ?", serverID, cleanPath).
		Count(&linkCount).Error; err == nil && linkCount > 0 {
		return s.dockerService.ReadFileFromHost(ctx, serverID, cleanPath)
	}

	// Otherwise, require the file to be a recognized Dockerfile/Compose file inside an enabled SourceSearchDir
	if !isAllowedSourceFilename(cleanPath) {
		return "", errors.New("access denied: only discovered source files (Dockerfile / docker-compose) inside configured search directories may be read")
	}

	var dirs []models.SourceSearchDir
	if err := s.db.WithContext(ctx).Where("server_id = ? AND enabled = ?", serverID, true).Find(&dirs).Error; err != nil {
		return "", err
	}

	inAllowedDir := false
	for _, d := range dirs {
		dirClean := filepath.Clean(d.Path)
		if dirClean == "/" {
			continue
		}
		if strings.HasPrefix(cleanPath, dirClean+"/") {
			inAllowedDir = true
			break
		}
	}
	if !inAllowedDir {
		return "", errors.New("access denied: file path is outside configured source search directories")
	}

	return s.dockerService.ReadFileFromHost(ctx, serverID, cleanPath)
}

// ListSourceLinks returns discovered links for images or stacks
func (s *Service) ListSourceLinks(ctx context.Context, serverID uint, kind string, targetRef string) ([]models.SourceLink, error) {
	if s.db == nil {
		return nil, errors.New("database not available")
	}
	query := s.db.WithContext(ctx).Where("server_id = ?", serverID)
	if kind != "" {
		query = query.Where("kind = ?", kind)
	}
	if targetRef != "" {
		query = query.Where("target_ref = ?", targetRef)
	}
	var links []models.SourceLink
	if err := query.Order("match_score desc, updated_at desc").Find(&links).Error; err != nil {
		return nil, err
	}
	return links, nil
}

// ConfirmSourceLink marks a link as explicitly approved by an administrator
func (s *Service) ConfirmSourceLink(ctx context.Context, serverID uint, linkID uint) error {
	return s.db.WithContext(ctx).Model(&models.SourceLink{}).
		Where("id = ? AND server_id = ?", linkID, serverID).
		Updates(map[string]interface{}{
			"status":     "user_confirmed",
			"updated_at": time.Now(),
		}).Error
}

// RejectSourceLink marks a link as rejected
func (s *Service) RejectSourceLink(ctx context.Context, serverID uint, linkID uint) error {
	return s.db.WithContext(ctx).Model(&models.SourceLink{}).
		Where("id = ? AND server_id = ?", linkID, serverID).
		Updates(map[string]interface{}{
			"status":     "user_rejected",
			"updated_at": time.Now(),
		}).Error
}

// GetLinkedContent safely reads the host source file in read-only mode and verifies hash
func (s *Service) GetLinkedContent(ctx context.Context, serverID uint, kind string, targetRef string) (content string, path string, score float64, err error) {
	if s.db == nil {
		return "", "", 0, errors.New("database not available")
	}

	var link models.SourceLink
	query := s.db.WithContext(ctx).Where("server_id = ? AND kind = ? AND status != 'user_rejected'", serverID, kind)
	if strings.Contains(targetRef, ":") {
		cleanRef := strings.TrimPrefix(targetRef, "sha256:")
		query = query.Where("target_ref = ? OR target_ref = ?", targetRef, cleanRef)
	} else {
		query = query.Where("target_ref = ?", targetRef)
	}

	if err := query.First(&link).Error; err != nil {
		return "", "", 0, err
	}

	exec, err := s.resolveExecutor(ctx, serverID)
	if err != nil {
		return "", link.FilePath, link.MatchScore, err
	}

	scanner := NewScanner(exec)
	fileContent, mtime, readErr := scanner.readFile(ctx, link.FilePath, "")
	if readErr != nil {
		// Mark link as stale if file is missing
		s.db.WithContext(ctx).Model(&link).Update("status", "stale")
		return "", link.FilePath, link.MatchScore, readErr
	}

	hash := sha256.Sum256([]byte(fileContent))
	hexHash := hex.EncodeToString(hash[:])
	if hexHash != link.ContentHash {
		// Content changed on host
		s.db.WithContext(ctx).Model(&link).Updates(map[string]interface{}{
			"content_hash": hexHash,
			"file_mtime":   mtime,
			"updated_at":   time.Now(),
		})
	}

	return fileContent, link.FilePath, link.MatchScore, nil
}

func (s *Service) resolveExecutor(ctx context.Context, serverID uint) (host.Executor, error) {
	server, err := s.dockerService.GetServer(serverID)
	if err != nil {
		return nil, err
	}

	if docker.IsSSHHost(server.HostURL) {
		client, _, err := s.dockerService.GetSSHClient(ctx, serverID)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to host over SSH: %w", err)
		}
		return host.NewSSHExecutor(client), nil
	}

	return host.NewLocalExecutor(), nil
}

func filterCandidates(all []CandidateFile, cType CandidateType) []CandidateFile {
	var filtered []CandidateFile
	for _, c := range all {
		if c.Type == cType {
			filtered = append(filtered, c)
		}
	}
	return filtered
}

// StartScheduler begins periodic evaluations of search directories with schedule_enabled
func (s *Service) StartScheduler(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	go func() {
		for {
			select {
			case <-s.schedulerStop:
				ticker.Stop()
				return
			case <-ticker.C:
				s.evaluateScheduledDirs(ctx)
			}
		}
	}()
}

// StopScheduler gracefully stops the scheduler
func (s *Service) StopScheduler() {
	close(s.schedulerStop)
}

func (s *Service) evaluateScheduledDirs(ctx context.Context) {
	if s.db == nil {
		return
	}
	var scheduledDirs []models.SourceSearchDir
	if err := s.db.Where("schedule_enabled = ? AND enabled = ?", true, true).Find(&scheduledDirs).Error; err != nil {
		return
	}

	now := time.Now()
	for _, dir := range scheduledDirs {
		// Run daily if last scanned > 24 hours ago, or never scanned
		shouldRun := false
		if dir.LastScannedAt == nil {
			shouldRun = true
		} else if now.Sub(*dir.LastScannedAt) >= 24*time.Hour {
			shouldRun = true
		}

		if shouldRun {
			log.Printf("[SOURCE-SCHEDULER] Triggering scheduled scan for host %d, path %s", dir.ServerID, dir.Path)
			go func(d models.SourceSearchDir) {
				_, _ = s.ScanServer(context.Background(), d.ServerID, "", nil)
			}(dir)
		}
	}
}

// FormatProgressEvent formats a progress event as JSON for Server-Sent Events (SSE)
func FormatProgressEvent(p models.SourceScanProgress) string {
	b, _ := json.Marshal(p)
	return fmt.Sprintf("data: %s\n\n", string(b))
}
