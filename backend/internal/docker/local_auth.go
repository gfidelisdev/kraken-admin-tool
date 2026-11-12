package docker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// runHostNamespaceCommand executes a command inside the host operating system namespace.
// If input is non-empty, it is piped via standard input to avoid exposing passwords in process tables.
func runHostNamespaceCommand(ctx context.Context, shellCmd string, input string) error {
	var execCmd *exec.Cmd
	if _, err := os.Stat("/.dockerenv"); err == nil {
		if err := exec.Command("nsenter", "-t", "1", "-m", "-u", "-i", "-n", "-p", "--", "true").Run(); err == nil {
			execCmd = exec.CommandContext(ctx, "nsenter", "-t", "1", "-m", "-u", "-i", "-n", "-p", "--", "sh", "-c", shellCmd)
		} else {
			execCmd = exec.CommandContext(ctx, "docker", "run", "--rm", "-i", "--privileged", "--pid=host", "alpine", "nsenter", "-t", "1", "-m", "-u", "-i", "-n", "-p", "--", "sh", "-c", shellCmd)
		}
	} else {
		execCmd = exec.CommandContext(ctx, "sh", "-c", shellCmd)
	}

	if input != "" {
		execCmd.Stdin = strings.NewReader(input)
	}

	out, err := execCmd.CombinedOutput()
	if err != nil {
		outStr := strings.TrimSpace(string(out))
		if outStr == "" {
			outStr = err.Error()
		}
		return fmt.Errorf("%s", outStr)
	}
	return nil
}

// runHostNamespaceOutput executes a shell command in the host namespace and returns stdout.
func runHostNamespaceOutput(ctx context.Context, shellCmd string) (string, error) {
	var execCmd *exec.Cmd
	if _, err := os.Stat("/.dockerenv"); err == nil {
		if err := exec.Command("nsenter", "-t", "1", "-m", "-u", "-i", "-n", "-p", "--", "true").Run(); err == nil {
			execCmd = exec.CommandContext(ctx, "nsenter", "-t", "1", "-m", "-u", "-i", "-n", "-p", "--", "sh", "-c", shellCmd)
		} else {
			execCmd = exec.CommandContext(ctx, "docker", "run", "--rm", "-i", "--privileged", "--pid=host", "alpine", "nsenter", "-t", "1", "-m", "-u", "-i", "-n", "-p", "--", "sh", "-c", shellCmd)
		}
	} else {
		execCmd = exec.CommandContext(ctx, "sh", "-c", shellCmd)
	}

	out, err := execCmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// verifyLocalHostCredentials verifies that the user and password correspond to a valid, authorized account
// on the host operating system before allowing the local Unix socket to be registered.
func verifyLocalHostCredentials(ctx context.Context, username, password string) error {
	username = strings.TrimSpace(username)
	password = strings.TrimSpace(password)
	if username == "" || password == "" {
		return errors.New("usuário e senha do sistema operacional hospedeiro são obrigatórios para autorizar o host local")
	}

	if os.Getenv("KRAKEN_SKIP_HOST_AUTH_VERIFY") == "true" {
		return nil
	}

	// 1. Sanitize username
	for _, c := range username {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-' || c == '.') {
			return fmt.Errorf("nome de usuário inválido: %s", username)
		}
	}

	// 2. Check if user exists on the host
	checkUserCmd := fmt.Sprintf("id -u '%s' 2>/dev/null", username)
	if err := runHostNamespaceCommand(ctx, checkUserCmd, ""); err != nil {
		return fmt.Errorf("o usuário '%s' não existe no sistema operacional hospedeiro", username)
	}

	// 3. Special handling for root user:
	// In modern Linux distributions (Fedora, Ubuntu, Debian), direct root login is locked by default.
	// Users must log in with their personal account and escalate via sudo.
	if username == "root" {
		checkRootPassCmd := "awk -F: '$1 == \"root\" {print $2}' /etc/shadow"
		var shadowHash string
		out, err := runHostNamespaceOutput(ctx, checkRootPassCmd)
		if err == nil {
			shadowHash = strings.TrimSpace(out)
		}
		if shadowHash == "" || shadowHash == "!" || shadowHash == "*" || strings.HasPrefix(shadowHash, "!") || strings.HasPrefix(shadowHash, "*") {
			return errors.New("a conta root do sistema hospedeiro está bloqueada sem senha direta. Por segurança, utilize o seu usuário pessoal do sistema com permissões de administrador (sudo)")
		}
	}

	// 4. Authenticate via sudo as the specified user.
	// When running inside nsenter as root, executing:
	//   su -s /bin/sh -c 'sudo -k -S -p "" true' <username>
	// switches to <username> and runs sudo with stdin password prompt.
	// If the user's password is correct AND user has sudo access, it returns exit code 0.
	// If the password is wrong or user lacks sudo access, it returns exit code 1.
	authCmd := fmt.Sprintf("su -s /bin/sh -c 'sudo -k -S -p \"\" true' '%s'", username)
	err := runHostNamespaceCommand(ctx, authCmd, password+"\n")
	if err != nil {
		return fmt.Errorf("falha na autenticação do host local: usuário ou senha incorretos, ou usuário sem permissão sudo no hospedeiro")
	}

	return nil
}
