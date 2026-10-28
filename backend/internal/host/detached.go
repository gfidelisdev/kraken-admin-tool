package host

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	DetachedUpgradeDirPrefix = "/tmp/.kraken-upgrade-"
	SentinelFinishedPrefix   = "===KRAKEN_UPGRADE_FINISHED:"
)

// GetDetachedDir returns the isolated directory on the host for a given server
func GetDetachedDir(serverID uint) string {
	return fmt.Sprintf("%s%d", DetachedUpgradeDirPrefix, serverID)
}

// LaunchDetachedUpgrade starts the package manager upgrade as an independent, detached nohup process
// on the remote or local host. It returns the PID of the spawned process.
func LaunchDetachedUpgrade(ctx context.Context, exec Executor, serverID uint, pkgManager string, packages []string, sudoPassword string) (int, error) {
	for _, pkg := range packages {
		if err := ValidatePackageName(pkg); err != nil {
			return 0, err
		}
	}

	innerCmd, err := BuildUpgradeCommand(pkgManager, packages)
	if err != nil {
		return 0, err
	}

	dir := GetDetachedDir(serverID)

	// Launch script template:
	// 1. Sets up directory and log file with read permissions for the SSH user.
	// 2. Marks status as RUNNING.
	// 3. Spawns nohup subshell with </dev/null >/dev/null 2>&1 & so it survives SSH termination.
	// 4. Writes PID to pid file and prints it to stdout.
	launchScript := fmt.Sprintf(
		`umask 022 && DIR="%s" && rm -rf "$DIR" && mkdir -p "$DIR" && touch "$DIR/output.log" && chmod 777 "$DIR" 2>/dev/null && chmod 666 "$DIR/output.log" 2>/dev/null && echo "RUNNING" > "$DIR/status" && nohup sh -c '( %s ) >> "'"$DIR"'/output.log" 2>&1; EC=$?; echo "$EC" > "'"$DIR"'/exit_code"; if [ "$EC" -eq 0 ]; then echo "SUCCESS" > "'"$DIR"'/status"; else echo "FAILED" > "'"$DIR"'/status"; fi; chmod 666 "'"$DIR"'/exit_code" "'"$DIR"'/status" 2>/dev/null; echo "===KRAKEN_UPGRADE_FINISHED:$EC===" >> "'"$DIR"'/output.log"' </dev/null >/dev/null 2>&1 & echo $! > "$DIR/pid" && chmod 666 "$DIR/pid" 2>/dev/null && cat "$DIR/pid"`,
		dir,
		innerCmd,
	)

	stdout, stderr, exitCode, err := exec.ExecuteSudo(ctx, launchScript, sudoPassword)
	if err != nil {
		if errors.Is(err, ErrInvalidSudoPassword) {
			return 0, ErrInvalidSudoPassword
		}
		return 0, fmt.Errorf("failed to launch detached upgrade (exit %d): %s %w", exitCode, strings.TrimSpace(stderr+" "+stdout), err)
	}

	pidStr := strings.TrimSpace(stdout)
	pid, parseErr := strconv.Atoi(pidStr)
	if parseErr != nil || pid <= 0 {
		return 0, fmt.Errorf("invalid PID received from host: %q", pidStr)
	}

	return pid, nil
}

// CheckDetachedStatus queries the host to inspect if the background upgrade process is still alive and its status
func CheckDetachedStatus(ctx context.Context, exec Executor, serverID uint) (status string, isAlive bool, exitCode int, pid int, err error) {
	dir := GetDetachedDir(serverID)
	checkCmd := fmt.Sprintf(
		`DIR="%s"; if [ ! -d "$DIR" ]; then echo "NONE"; exit 0; fi; PID=$(cat "$DIR/pid" 2>/dev/null); STATUS=$(cat "$DIR/status" 2>/dev/null || echo "UNKNOWN"); EXIT=$(cat "$DIR/exit_code" 2>/dev/null || echo "-1"); ALIVE=0; if [ -n "$PID" ] && (kill -0 "$PID" 2>/dev/null || [ -d "/proc/$PID" ]); then ALIVE=1; fi; echo "$STATUS:$ALIVE:$EXIT:$PID"`,
		dir,
	)

	stdout, _, _, err := exec.Execute(ctx, checkCmd)
	if err != nil {
		return "ERROR", false, -1, 0, err
	}

	trimmed := strings.TrimSpace(stdout)
	if trimmed == "NONE" || trimmed == "" {
		return "NONE", false, -1, 0, nil
	}

	parts := strings.Split(trimmed, ":")
	if len(parts) < 4 {
		return "UNKNOWN", false, -1, 0, nil
	}

	status = parts[0]
	isAlive = parts[1] == "1"
	exitCode, _ = strconv.Atoi(parts[2])
	pid, _ = strconv.Atoi(parts[3])

	return status, isAlive, exitCode, pid, nil
}

// StreamDetachedLogs attaches to the persistent output.log on the host using tail,
// streaming each line to onLine. It stops when the sentinel finished line is detected
// or when the process dies. It returns the total lines processed and the exit code.
func StreamDetachedLogs(ctx context.Context, exec Executor, serverID uint, startLine int, onLine func(line string)) (int, int, error) {
	if startLine < 1 {
		startLine = 1
	}

	dir := GetDetachedDir(serverID)
	tailCmd := fmt.Sprintf(`tail -n +%d -f "%s/output.log" 2>/dev/null`, startLine, dir)

	tailCtx, cancelTail := context.WithCancel(ctx)
	defer cancelTail()

	// Watchdog goroutine: periodically checks if the remote process is still alive.
	// If process is dead and status is no longer RUNNING, wait 1 second for tail to catch up then cancel tail.
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-tailCtx.Done():
				return
			case <-ticker.C:
				status, isAlive, _, _, err := CheckDetachedStatus(tailCtx, exec, serverID)
				if err == nil && !isAlive && status != "RUNNING" && status != "UNKNOWN" {
					time.Sleep(1200 * time.Millisecond)
					cancelTail()
					return
				}
			}
		}
	}()

	lineCount := startLine - 1
	exitCode := -1

	onChunk := func(chunk string) {
		scanner := bufio.NewScanner(strings.NewReader(chunk))
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, SentinelFinishedPrefix) {
				ecStr := strings.TrimSuffix(strings.TrimPrefix(line, SentinelFinishedPrefix), "===")
				if ec, err := strconv.Atoi(ecStr); err == nil {
					exitCode = ec
				}
				cancelTail()
				return
			}
			lineCount++
			if onLine != nil {
				onLine(line)
			}
		}
	}

	_, _, _, _ = exec.ExecuteStream(tailCtx, tailCmd, onChunk)

	// If exitCode was not detected from stream, check exit_code file directly
	if exitCode == -1 {
		_, _, ec, _, _ := CheckDetachedStatus(ctx, exec, serverID)
		if ec >= 0 {
			exitCode = ec
		}
	}

	return lineCount, exitCode, nil
}

// CleanDetachedJob cleans up the temporary directory on the host once a job is acknowledged or completed
func CleanDetachedJob(ctx context.Context, exec Executor, serverID uint) error {
	dir := GetDetachedDir(serverID)
	cleanCmd := fmt.Sprintf(`rm -rf "%s"`, dir)
	_, _, _, err := exec.Execute(ctx, cleanCmd)
	return err
}
