package host

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/kraken/admin-tool/backend/internal/audit"
	"github.com/kraken/admin-tool/backend/internal/docker"
	"github.com/kraken/admin-tool/backend/internal/models"
	"gorm.io/gorm"
)

type PackageUpgradeLog struct {
	Type      string `json:"type"` // "log"
	Message   string `json:"message"`
	Level     string `json:"level"` // "info", "stdout", "stderr", "error"
	Timestamp string `json:"timestamp"`
}

type PackageUpgradeResult struct {
	Type                  string `json:"type"` // "result"
	Success               bool   `json:"success"`
	Message               string `json:"message"`
	Output                string `json:"output,omitempty"`
	RemainingUpdatesCount int    `json:"remaining_updates_count"`
	SecurityUpdatesCount  int    `json:"security_updates_count"`
	Error                 string `json:"error,omitempty"`
	Code                  string `json:"code,omitempty"`
}

type UpgradeJob struct {
	ID             string                `json:"id"`
	ServerID       uint                  `json:"server_id"`
	ServerName     string                `json:"server_name"`
	Status         string                `json:"status"` // "running", "success", "failed"
	PackageManager string                `json:"package_manager"`
	Packages       []string              `json:"packages"`
	StartedAt      time.Time             `json:"started_at"`
	FinishedAt     *time.Time            `json:"finished_at,omitempty"`
	DurationSec    int                   `json:"duration_seconds"`
	Logs           []PackageUpgradeLog   `json:"logs"`
	Result         *PackageUpgradeResult `json:"result,omitempty"`

	subscribers map[chan PackageUpgradeLog]struct{}
	cancelFunc  context.CancelFunc
	mu          sync.RWMutex
}

func (j *UpgradeJob) AddLog(log PackageUpgradeLog) {
	j.mu.Lock()
	j.Logs = append(j.Logs, log)
	subs := make([]chan PackageUpgradeLog, 0, len(j.subscribers))
	for ch := range j.subscribers {
		subs = append(subs, ch)
	}
	j.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- log:
		default:
			// Non-blocking write to avoid slow clients blocking the job
		}
	}
}

func (j *UpgradeJob) Finish(result PackageUpgradeResult) {
	j.mu.Lock()
	now := time.Now()
	j.FinishedAt = &now
	j.DurationSec = int(now.Sub(j.StartedAt).Seconds())
	if result.Success {
		j.Status = "success"
	} else {
		j.Status = "failed"
	}
	j.Result = &result

	resultLog := PackageUpgradeLog{
		Type:      "result",
		Message:   result.Message,
		Level:     "info",
		Timestamp: now.UTC().Format(time.RFC3339),
	}
	subs := make([]chan PackageUpgradeLog, 0, len(j.subscribers))
	for ch := range j.subscribers {
		subs = append(subs, ch)
	}
	j.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- resultLog:
		default:
		}
	}
}

// Subscribe registers a subscriber channel to receive live logs and returns existing buffered logs.
func (j *UpgradeJob) Subscribe() (chan PackageUpgradeLog, []PackageUpgradeLog, *PackageUpgradeResult, string, func()) {
	j.mu.Lock()
	defer j.mu.Unlock()

	ch := make(chan PackageUpgradeLog, 200)
	if j.subscribers == nil {
		j.subscribers = make(map[chan PackageUpgradeLog]struct{})
	}
	j.subscribers[ch] = struct{}{}

	logsCopy := make([]PackageUpgradeLog, len(j.Logs))
	copy(logsCopy, j.Logs)

	unsubscribe := func() {
		j.mu.Lock()
		delete(j.subscribers, ch)
		close(ch)
		j.mu.Unlock()
	}

	return ch, logsCopy, j.Result, j.Status, unsubscribe
}

type JobManager struct {
	jobs map[uint]*UpgradeJob
	mu   sync.RWMutex
}

var defaultJobManager = &JobManager{
	jobs: make(map[uint]*UpgradeJob),
}

func GetJobManager() *JobManager {
	return defaultJobManager
}

func (m *JobManager) GetJob(serverID uint) *UpgradeJob {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.jobs[serverID]
}

func (m *JobManager) GetActiveJobs() []*UpgradeJob {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var list []*UpgradeJob
	for _, j := range m.jobs {
		j.mu.RLock()
		if j.Status == "running" {
			list = append(list, j)
		}
		j.mu.RUnlock()
	}
	return list
}

func (m *JobManager) CreateOrGetJob(serverID uint, serverName string, pkgManager string, packages []string) (*UpgradeJob, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	existing, exists := m.jobs[serverID]
	if exists {
		existing.mu.RLock()
		isRunning := existing.Status == "running"
		existing.mu.RUnlock()
		if isRunning {
			return existing, false
		}
	}

	jobID := fmt.Sprintf("pkg-upg-%d-%d", serverID, time.Now().Unix())
	job := &UpgradeJob{
		ID:             jobID,
		ServerID:       serverID,
		ServerName:     serverName,
		Status:         "running",
		PackageManager: pkgManager,
		Packages:       packages,
		StartedAt:      time.Now(),
		Logs:           make([]PackageUpgradeLog, 0, 100),
		subscribers:    make(map[chan PackageUpgradeLog]struct{}),
	}
	m.jobs[serverID] = job
	return job, true
}

func (m *JobManager) StartBackgroundUpgrade(
	job *UpgradeJob,
	db *gorm.DB,
	dockerService *docker.DockerService,
	execResolver func(ctx context.Context) (Executor, error),
	server *models.DockerServer,
	sudoPassword string,
	userID uint,
	username string,
) error {
	ctx, cancel := context.WithCancel(context.Background())
	job.cancelFunc = cancel

	initExec, err := execResolver(ctx)
	if err != nil {
		cancel()
		job.Finish(PackageUpgradeResult{
			Success: false,
			Message: "Falha ao conectar ao host para iniciar atualização: " + err.Error(),
			Error:   err.Error(),
		})
		return err
	}

	// 1. Launch detached nohup process on host
	pid, err := LaunchDetachedUpgrade(ctx, initExec, server.ID, job.PackageManager, job.Packages, sudoPassword)
	if err != nil {
		cancel()
		job.Finish(PackageUpgradeResult{
			Success: false,
			Message: "Falha ao iniciar processo desanexado no host: " + err.Error(),
			Error:   err.Error(),
		})
		return err
	}

	initMsg := fmt.Sprintf("Processo desanexado iniciado no host com PID %d usando gerenciador '%s'.", pid, job.PackageManager)
	if len(job.Packages) > 0 {
		initMsg = fmt.Sprintf("Processo desanexado iniciado (PID %d) para %d pacote(s) [%s] usando '%s'.", pid, len(job.Packages), strings.Join(job.Packages, ", "), job.PackageManager)
	}
	job.AddLog(PackageUpgradeLog{
		Type:      "log",
		Message:   initMsg,
		Level:     "info",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})

	// 2. Spawn persistent background worker that handles tailing and SSH reconnections
	go func() {
		defer cancel()

		var currentExec Executor = initExec
		lastLineProcessed := 0
		retryCount := 0
		maxRetries := 30 // Allow reconnections for up to ~1.5 minutes of network downtime

		for {
			if currentExec == nil {
				reconnCtx, reconnCancel := context.WithTimeout(context.Background(), 15*time.Second)
				var rErr error
				currentExec, rErr = execResolver(reconnCtx)
				reconnCancel()
				if rErr != nil {
					retryCount++
					if retryCount > maxRetries {
						job.AddLog(PackageUpgradeLog{
							Type:      "log",
							Message:   "Não foi possível restabelecer conexão SSH com o host para acompanhar logs. O processo continua no host.",
							Level:     "error",
							Timestamp: time.Now().UTC().Format(time.RFC3339),
						})
						break
					}
					time.Sleep(3 * time.Second)
					continue
				}
				retryCount = 0
				job.AddLog(PackageUpgradeLog{
					Type:      "log",
					Message:   "Conexão SSH restabelecida com o host. Retomando transmissão dos logs...",
					Level:     "info",
					Timestamp: time.Now().UTC().Format(time.RFC3339),
				})
			}

			// Stream detached logs from lastLineProcessed + 1
			linesCount, exitCode, sErr := StreamDetachedLogs(ctx, currentExec, server.ID, lastLineProcessed+1, func(line string) {
				job.AddLog(PackageUpgradeLog{
					Type:      "log",
					Message:   line,
					Level:     "stdout",
					Timestamp: time.Now().UTC().Format(time.RFC3339),
				})
			})

			if linesCount > lastLineProcessed {
				lastLineProcessed = linesCount
			}

			if sErr == nil && exitCode >= 0 {
				// Completed successfully or with command exit code!
				finishJob(job, db, currentExec, server, exitCode, userID, username)
				return
			}

			// If error occurred (e.g. SSH connection dropped), check if host process is still alive
			checkCtx, checkCancel := context.WithTimeout(context.Background(), 10*time.Second)
			status, isAlive, finalExit, _, checkErr := CheckDetachedStatus(checkCtx, currentExec, server.ID)
			checkCancel()

			if checkErr == nil {
				if !isAlive && status != "RUNNING" {
					finishJob(job, db, currentExec, server, finalExit, userID, username)
					return
				}
			}

			// Lost connection to host -> reset currentExec to trigger reconnection on next loop
			currentExec = nil
			time.Sleep(2 * time.Second)
		}

		// Fallback finish if retries exhausted
		job.Finish(PackageUpgradeResult{
			Success: false,
			Message: "Perda prolongada de conexão com o host durante a atualização.",
			Error:   "SSH connection lost",
		})
	}()

	return nil
}

func finishJob(job *UpgradeJob, db *gorm.DB, exec Executor, server *models.DockerServer, exitCode int, userID uint, username string) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	success := exitCode == 0

	// Refresh remaining updates count from host
	remaining, _ := CheckAvailableUpdates(ctx, exec, job.PackageManager)
	now := time.Now()
	db.Where("server_id = ?", server.ID).Delete(&models.HostPackageUpdate{})
	secCount := 0
	for _, u := range remaining {
		if u.IsSecurity {
			secCount++
		}
		db.Create(&models.HostPackageUpdate{
			ServerID:       server.ID,
			Name:           u.Name,
			CurrentVersion: u.CurrentVersion,
			NewVersion:     u.NewVersion,
			Repository:     u.Repository,
			IsSecurity:     u.IsSecurity,
			CreatedAt:      now,
		})
	}

	db.Model(server).Updates(map[string]interface{}{
		"packages_last_checked":   &now,
		"available_updates_count": len(remaining),
		"security_updates_count":  secCount,
	})

	if success {
		audit.Log(db, userID, username, "UPGRADE_PACKAGES", "host", server.Name,
			"Upgraded packages on host successfully (detached)", "SUCCESS")

		job.Finish(PackageUpgradeResult{
			Success:                 true,
			Message:                 "Pacotes atualizados com sucesso",
			RemainingUpdatesCount: len(remaining),
			SecurityUpdatesCount:  secCount,
		})
	} else {
		errMsg := fmt.Sprintf("Atualização de pacotes finalizada com código de erro %d", exitCode)
		audit.Log(db, userID, username, "UPGRADE_PACKAGES", "host", server.Name,
			errMsg, "FAILED")

		job.Finish(PackageUpgradeResult{
			Success:                 false,
			Message:                 errMsg,
			RemainingUpdatesCount: len(remaining),
			SecurityUpdatesCount:  secCount,
			Error:                   errMsg,
		})
	}
}
