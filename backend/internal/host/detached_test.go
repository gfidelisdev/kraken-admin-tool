package host

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kraken/admin-tool/backend/internal/models"
)

func TestLaunchDetachedUpgrade(t *testing.T) {
	mock := &mockExecutor{
		responses: map[string]string{
			"echo $! >": "12345\n",
		},
	}

	ctx := context.Background()
	pid, err := LaunchDetachedUpgrade(ctx, mock, 1, "apt", []string{"curl", "nginx"}, "test-pass")
	if err != nil {
		t.Fatalf("unexpected error launching detached upgrade: %v", err)
	}

	if pid != 12345 {
		t.Errorf("expected PID 12345, got %d", pid)
	}

	if len(mock.sudoCalls) == 0 {
		t.Fatalf("expected sudoCalls to have launch script, got 0")
	}

	call := mock.sudoCalls[0]
	if !strings.Contains(call, "/tmp/.kraken-upgrade-1") {
		t.Errorf("expected call to contain detached dir, got: %s", call)
	}
	if !strings.Contains(call, "nohup sh -c") {
		t.Errorf("expected call to contain nohup invocation, got: %s", call)
	}
	if !strings.Contains(call, "===KRAKEN_UPGRADE_FINISHED:$EC===") {
		t.Errorf("expected call to contain finished sentinel, got: %s", call)
	}
}

func TestLaunchDetachedUpgrade_UpdateAll(t *testing.T) {
	mock := &mockExecutor{
		responses: map[string]string{
			"echo $! >": "54321\n",
		},
	}

	ctx := context.Background()
	// Empty package list represents Update All
	pid, err := LaunchDetachedUpgrade(ctx, mock, 2, "apt", nil, "test-pass")
	if err != nil {
		t.Fatalf("unexpected error launching detached upgrade for update all: %v", err)
	}

	if pid != 54321 {
		t.Errorf("expected PID 54321, got %d", pid)
	}

	if len(mock.sudoCalls) == 0 {
		t.Fatalf("expected sudoCalls to have launch script, got 0")
	}

	call := mock.sudoCalls[0]
	if !strings.Contains(call, "dist-upgrade") {
		t.Errorf("expected call for apt update all to contain dist-upgrade, got: %s", call)
	}
	if !strings.Contains(call, "PYTHONUNBUFFERED=1") {
		t.Errorf("expected call to configure unbuffered output, got: %s", call)
	}
}

func TestCheckDetachedStatus(t *testing.T) {
	ctx := context.Background()

	t.Run("running process", func(t *testing.T) {
		mock := &mockExecutor{
			responses: map[string]string{
				"kill -0": "RUNNING:1:-1:54321\n",
			},
		}
		status, isAlive, exitCode, pid, err := CheckDetachedStatus(ctx, mock, 2)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if status != "RUNNING" || !isAlive || pid != 54321 || exitCode != -1 {
			t.Errorf("unexpected status results: status=%s isAlive=%v exitCode=%d pid=%d", status, isAlive, exitCode, pid)
		}
	})

	t.Run("finished process", func(t *testing.T) {
		mock := &mockExecutor{
			responses: map[string]string{
				"kill -0": "SUCCESS:0:0:54321\n",
			},
		}
		status, isAlive, exitCode, pid, err := CheckDetachedStatus(ctx, mock, 2)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if status != "SUCCESS" || isAlive || pid != 54321 || exitCode != 0 {
			t.Errorf("unexpected status results: status=%s isAlive=%v exitCode=%d pid=%d", status, isAlive, exitCode, pid)
		}
	})

	t.Run("no directory exists", func(t *testing.T) {
		mock := &mockExecutor{
			responses: map[string]string{
				"kill -0": "NONE\n",
			},
		}
		status, isAlive, _, _, err := CheckDetachedStatus(ctx, mock, 3)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if status != "NONE" || isAlive {
			t.Errorf("expected NONE and not alive, got %s, %v", status, isAlive)
		}
	})
}

func TestStreamDetachedLogs_SentinelDetection(t *testing.T) {
	mock := &mockExecutor{
		responses: map[string]string{
			"tail -n": "Line 1: updating apt repos\nLine 2: unpacking curl\nLine 3: setting up curl\n===KRAKEN_UPGRADE_FINISHED:0===\n",
		},
	}

	var received []string
	ctx := context.Background()
	count, exitCode, err := StreamDetachedLogs(ctx, mock, 1, 1, func(line string) {
		received = append(received, line)
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}

	if count != 3 {
		t.Errorf("expected 3 log lines, got %d", count)
	}

	if len(received) != 3 || received[0] != "Line 1: updating apt repos" {
		t.Errorf("unexpected received lines: %v", received)
	}
}

func TestJobManager_LifecycleAndReattach(t *testing.T) {
	mgr := &JobManager{
		jobs: make(map[uint]*UpgradeJob),
	}

	serverID := uint(99)
	job, isNew := mgr.CreateOrGetJob(serverID, "prod-host", "apt", []string{"curl"})
	if !isNew || job == nil {
		t.Fatalf("expected new job creation")
	}

	// Add some logs
	job.AddLog(PackageUpgradeLog{
		Type:      "log",
		Message:   "Starting initial step",
		Level:     "info",
		Timestamp: "2026-10-20T12:00:00Z",
	})
	job.AddLog(PackageUpgradeLog{
		Type:      "log",
		Message:   "Processing step 2",
		Level:     "stdout",
		Timestamp: "2026-10-20T12:00:01Z",
	})

	// Client 1 attaches: should get 2 past logs
	ch1, pastLogs1, _, status1, unsub1 := job.Subscribe()
	defer unsub1()

	if len(pastLogs1) != 2 {
		t.Errorf("expected 2 past logs, got %d", len(pastLogs1))
	}
	if status1 != "running" {
		t.Errorf("expected status 'running', got %s", status1)
	}

	// Client 2 also attaches (simulating second tab or reattach)
	ch2, pastLogs2, _, _, unsub2 := job.Subscribe()
	defer unsub2()

	if len(pastLogs2) != 2 {
		t.Errorf("expected client 2 to also get 2 past logs, got %d", len(pastLogs2))
	}

	// Broadcast live log
	liveMsg := "Live streaming log chunk"
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		select {
		case msg := <-ch1:
			if msg.Message != liveMsg {
				t.Errorf("client 1 received wrong message: %s", msg.Message)
			}
		case <-time.After(1 * time.Second):
			t.Errorf("client 1 timed out waiting for live message")
		}
	}()

	go func() {
		defer wg.Done()
		select {
		case msg := <-ch2:
			if msg.Message != liveMsg {
				t.Errorf("client 2 received wrong message: %s", msg.Message)
			}
		case <-time.After(1 * time.Second):
			t.Errorf("client 2 timed out waiting for live message")
		}
	}()

	job.AddLog(PackageUpgradeLog{
		Type:      "log",
		Message:   liveMsg,
		Level:     "stdout",
		Timestamp: "2026-10-20T12:00:02Z",
	})

	wg.Wait()

	// Finish job
	job.Finish(PackageUpgradeResult{
		Success:                 true,
		Message:                 "All packages upgraded successfully",
		RemainingUpdatesCount: 0,
		SecurityUpdatesCount:  0,
	})

	if job.Status != "success" {
		t.Errorf("expected job status 'success', got %s", job.Status)
	}

	// Third client attaches after completion: should get all 3 logs and final result
	_, pastLogs3, res3, status3, unsub3 := job.Subscribe()
	defer unsub3()

	if len(pastLogs3) != 3 {
		t.Errorf("expected client 3 to get all 3 logs, got %d", len(pastLogs3))
	}
	if status3 != "success" {
		t.Errorf("expected status 'success', got %s", status3)
	}
	if res3 == nil || !res3.Success {
		t.Errorf("expected non-nil success result for client 3")
	}
}

func TestResumeDetachedUpgrade(t *testing.T) {
	mock := &mockExecutor{
		responses: map[string]string{
			"tail -n": "Log after resume\n===KRAKEN_UPGRADE_FINISHED:0===\n",
			"kill -0": "SUCCESS:0:0:1234\n",
		},
	}

	mgr := &JobManager{
		jobs: make(map[uint]*UpgradeJob),
	}

	job := &UpgradeJob{
		ID:             "pkg-upg-1-recovered",
		ServerID:       1,
		ServerName:     "localhost",
		Status:         "running",
		PackageManager: "dnf",
		Logs:           []PackageUpgradeLog{{Type: "log", Message: "Initial log"}},
		subscribers:    make(map[chan PackageUpgradeLog]struct{}),
	}
	mgr.SetJob(1, job)

	execResolver := func(ctx context.Context) (Executor, error) {
		return mock, nil
	}

	err := mgr.ResumeDetachedUpgrade(job, nil, nil, execResolver, &models.DockerServer{ID: 1, Name: "localhost"}, 1, 1, "admin")
	if err != nil {
		t.Fatalf("unexpected error resuming detached upgrade: %v", err)
	}

	// Wait briefly for goroutine to process sentinel
	time.Sleep(100 * time.Millisecond)

	job.mu.RLock()
	logsLen := len(job.Logs)
	job.mu.RUnlock()

	if logsLen < 2 {
		t.Errorf("expected at least 2 logs after resume, got %d", logsLen)
	}
}
