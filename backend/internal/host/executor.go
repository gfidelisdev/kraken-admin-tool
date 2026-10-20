package host

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"golang.org/x/crypto/ssh"
)

var (
	ErrSudoPasswordRequired = errors.New("sudo password required")
	ErrInvalidSudoPassword  = errors.New("invalid sudo password")
	ErrCommandFailed        = errors.New("command execution failed")
)

// Executor defines the interface for executing system commands either locally or over SSH
type Executor interface {
	Execute(ctx context.Context, cmd string) (stdout string, stderr string, exitCode int, err error)
	ExecuteSudo(ctx context.Context, cmd string, sudoPassword string) (stdout string, stderr string, exitCode int, err error)
	ExecuteSudoStream(ctx context.Context, cmd string, sudoPassword string, onChunk func(chunk string)) (stdout string, stderr string, exitCode int, err error)
}

type streamWriter struct {
	buf     *bytes.Buffer
	onChunk func(string)
}

func (w *streamWriter) Write(p []byte) (int, error) {
	if w.onChunk != nil && len(p) > 0 {
		w.onChunk(string(p))
	}
	return w.buf.Write(p)
}

// SSHExecutor executes commands via an established SSH client
type SSHExecutor struct {
	client *ssh.Client
}

// NewSSHExecutor creates an executor wrapping an SSH client
func NewSSHExecutor(client *ssh.Client) *SSHExecutor {
	return &SSHExecutor{client: client}
}

// Execute runs a command as the SSH user
func (e *SSHExecutor) Execute(ctx context.Context, cmd string) (string, string, int, error) {
	if e.client == nil {
		return "", "", -1, errors.New("ssh client is nil")
	}

	session, err := e.client.NewSession()
	if err != nil {
		return "", "", -1, fmt.Errorf("failed to create ssh session: %w", err)
	}
	defer session.Close()

	var stdoutBuf, stderrBuf bytes.Buffer
	session.Stdout = &stdoutBuf
	session.Stderr = &stderrBuf

	done := make(chan error, 1)
	go func() {
		done <- session.Run(cmd)
	}()

	select {
	case <-ctx.Done():
		_ = session.Signal(ssh.SIGKILL)
		return stdoutBuf.String(), stderrBuf.String(), -1, ctx.Err()
	case err := <-done:
		exitCode := 0
		if err != nil {
			var exitErr *ssh.ExitError
			if errors.As(err, &exitErr) {
				exitCode = exitErr.ExitStatus()
			} else {
				exitCode = 1
			}
		}
		return stdoutBuf.String(), stderrBuf.String(), exitCode, err
	}
}

// ExecuteSudo runs a command with sudo, securely piping the password via stdin
func (e *SSHExecutor) ExecuteSudo(ctx context.Context, cmd string, sudoPassword string) (string, string, int, error) {
	return e.ExecuteSudoStream(ctx, cmd, sudoPassword, nil)
}

// ExecuteSudoStream runs a command with sudo and invokes onChunk for real-time output
func (e *SSHExecutor) ExecuteSudoStream(ctx context.Context, cmd string, sudoPassword string, onChunk func(string)) (string, string, int, error) {
	if e.client == nil {
		return "", "", -1, errors.New("ssh client is nil")
	}

	session, err := e.client.NewSession()
	if err != nil {
		return "", "", -1, fmt.Errorf("failed to create ssh session: %w", err)
	}
	defer session.Close()

	var stdoutBuf, stderrBuf bytes.Buffer
	if onChunk != nil {
		session.Stdout = &streamWriter{buf: &stdoutBuf, onChunk: onChunk}
		session.Stderr = &streamWriter{buf: &stderrBuf, onChunk: onChunk}
	} else {
		session.Stdout = &stdoutBuf
		session.Stderr = &stderrBuf
	}

	stdinPipe, err := session.StdinPipe()
	if err != nil {
		return "", "", -1, fmt.Errorf("failed to open stdin pipe for sudo: %w", err)
	}

	// Sudo invocation flags:
	// -S : Read password from stdin
	// -p '' : Suppress standard prompt text to avoid polluting stream
	// -- : End of sudo options, followed by env wrapper and the command
	sudoCmd := fmt.Sprintf("LANG=C LC_ALL=C sudo -S -p '' -- env %s", cmd)

	if err := session.Start(sudoCmd); err != nil {
		return "", "", -1, fmt.Errorf("failed to start sudo session: %w", err)
	}

	// Write password to stdin and close pipe
	if sudoPassword != "" {
		_, _ = io.WriteString(stdinPipe, sudoPassword+"\n")
	}
	_ = stdinPipe.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.Wait()
	}()

	select {
	case <-ctx.Done():
		_ = session.Signal(ssh.SIGKILL)
		return stdoutBuf.String(), stderrBuf.String(), -1, ctx.Err()
	case err := <-done:
		stderrStr := stderrBuf.String()
		stdoutStr := stdoutBuf.String()
		exitCode := 0

		if err != nil {
			var exitErr *ssh.ExitError
			if errors.As(err, &exitErr) {
				exitCode = exitErr.ExitStatus()
			} else {
				exitCode = 1
			}

			// Check for sudo password errors in stderr
			lowerErr := strings.ToLower(stderrStr)
			if strings.Contains(lowerErr, "incorrect password") ||
				strings.Contains(lowerErr, "try again") ||
				strings.Contains(lowerErr, "authentication failure") ||
				strings.Contains(lowerErr, "password is required") {
				return stdoutStr, stderrStr, exitCode, ErrInvalidSudoPassword
			}
		}

		return stdoutStr, stderrStr, exitCode, err
	}
}

// LocalExecutor executes commands locally (for local host or containerized daemons)
type LocalExecutor struct{}

func NewLocalExecutor() *LocalExecutor {
	return &LocalExecutor{}
}

func (e *LocalExecutor) Execute(ctx context.Context, cmd string) (string, string, int, error) {
	execCmd := exec.CommandContext(ctx, "sh", "-c", cmd)
	var stdoutBuf, stderrBuf bytes.Buffer
	execCmd.Stdout = &stdoutBuf
	execCmd.Stderr = &stderrBuf

	err := execCmd.Run()
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}
	return stdoutBuf.String(), stderrBuf.String(), exitCode, err
}

func (e *LocalExecutor) ExecuteSudo(ctx context.Context, cmd string, sudoPassword string) (string, string, int, error) {
	return e.ExecuteSudoStream(ctx, cmd, sudoPassword, nil)
}

func (e *LocalExecutor) ExecuteSudoStream(ctx context.Context, cmd string, sudoPassword string, onChunk func(string)) (string, string, int, error) {
	execCmd := exec.CommandContext(ctx, "sh", "-c", fmt.Sprintf("LANG=C LC_ALL=C sudo -S -p '' -- env %s", cmd))
	var stdoutBuf, stderrBuf bytes.Buffer
	if onChunk != nil {
		execCmd.Stdout = &streamWriter{buf: &stdoutBuf, onChunk: onChunk}
		execCmd.Stderr = &streamWriter{buf: &stderrBuf, onChunk: onChunk}
	} else {
		execCmd.Stdout = &stdoutBuf
		execCmd.Stderr = &stderrBuf
	}

	if sudoPassword != "" {
		execCmd.Stdin = strings.NewReader(sudoPassword + "\n")
	}

	err := execCmd.Run()
	stderrStr := stderrBuf.String()
	stdoutStr := stdoutBuf.String()
	exitCode := 0

	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}

		lowerErr := strings.ToLower(stderrStr)
		if strings.Contains(lowerErr, "incorrect password") ||
			strings.Contains(lowerErr, "try again") ||
			strings.Contains(lowerErr, "password is required") {
			return stdoutStr, stderrStr, exitCode, ErrInvalidSudoPassword
		}
	}

	return stdoutStr, stderrStr, exitCode, err
}

// Ensure interface implementation
var _ Executor = (*SSHExecutor)(nil)
var _ Executor = (*LocalExecutor)(nil)
