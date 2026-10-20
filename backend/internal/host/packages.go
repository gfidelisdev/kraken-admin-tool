package host

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/kraken/admin-tool/backend/internal/models"
)

// Allowed characters in Linux package names: must start with alphanumeric, followed by alphanumerics, underscores, hyphens, periods, plus signs
var safePackageRegex = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_\-\.\+]*$`)

// ValidatePackageName ensures no shell metacharacters or leading option flags exist in package names to prevent command and argument injection
func ValidatePackageName(pkg string) error {
	trimmed := strings.TrimSpace(pkg)
	if trimmed == "" {
		return errors.New("package name cannot be empty")
	}
	if !safePackageRegex.MatchString(trimmed) {
		return fmt.Errorf("invalid package name '%s': must start with alphanumeric character and contain only safe characters", pkg)
	}
	return nil
}

// IsValidPackageManager checks whether the given string is a supported package manager
func IsValidPackageManager(pm string) bool {
	switch pm {
	case "apt", "dnf", "yum", "apk", "pacman", "zypper", "unknown", "":
		return true
	default:
		return false
	}
}

// DetectPackageManager queries the host to identify which package manager is installed
func DetectPackageManager(ctx context.Context, exec Executor) (string, error) {
	// Probe for package manager binaries in priority order with explicit PATH and /etc/os-release inspection
	checkCmd := `sh -c '
		PATH="$PATH:/usr/bin:/bin:/usr/sbin:/sbin:/usr/local/bin:/usr/local/sbin"
		if [ -f /etc/os-release ]; then
			. /etc/os-release
			case "$ID $ID_LIKE" in
				*debian*|*ubuntu*|*mint*|*raspbian*)
					if command -v apt-get >/dev/null 2>&1 || [ -x /usr/bin/apt-get ]; then echo "apt"; exit 0; fi ;;
				*fedora*|*rhel*|*rocky*|*alma*|*centos*)
					if command -v dnf >/dev/null 2>&1 || [ -x /usr/bin/dnf ]; then echo "dnf"; exit 0; fi
					if command -v yum >/dev/null 2>&1 || [ -x /usr/bin/yum ]; then echo "yum"; exit 0; fi ;;
				*alpine*)
					if command -v apk >/dev/null 2>&1 || [ -x /sbin/apk ] || [ -x /usr/bin/apk ]; then echo "apk"; exit 0; fi ;;
				*arch*|*manjaro*|*endeavouros*)
					if command -v pacman >/dev/null 2>&1 || [ -x /usr/bin/pacman ]; then echo "pacman"; exit 0; fi ;;
				*suse*)
					if command -v zypper >/dev/null 2>&1 || [ -x /usr/bin/zypper ]; then echo "zypper"; exit 0; fi ;;
			esac
		fi
		if command -v apt-get >/dev/null 2>&1 || [ -x /usr/bin/apt-get ]; then echo "apt";
		elif command -v dnf >/dev/null 2>&1 || [ -x /usr/bin/dnf ]; then echo "dnf";
		elif command -v yum >/dev/null 2>&1 || [ -x /usr/bin/yum ]; then echo "yum";
		elif command -v apk >/dev/null 2>&1 || [ -x /sbin/apk ] || [ -x /usr/bin/apk ]; then echo "apk";
		elif command -v pacman >/dev/null 2>&1 || [ -x /usr/bin/pacman ]; then echo "pacman";
		elif command -v zypper >/dev/null 2>&1 || [ -x /usr/bin/zypper ]; then echo "zypper";
		else echo "unknown"; fi
	'`
	stdout, _, _, err := exec.Execute(ctx, checkCmd)
	if err != nil {
		return "unknown", err
	}
	res := strings.TrimSpace(stdout)
	if res == "" {
		return "unknown", nil
	}
	return res, nil
}

// RefreshPackageRepositories updates package lists (requires sudo if running as non-root)
func RefreshPackageRepositories(ctx context.Context, exec Executor, pkgManager string, sudoPassword string) error {
	var cmd string
	switch pkgManager {
	case "apt":
		cmd = "DEBIAN_FRONTEND=noninteractive apt-get update"
	case "dnf":
		cmd = "dnf check-update --refresh -q"
	case "yum":
		cmd = "yum check-update -q"
	case "apk":
		cmd = "apk update"
	case "pacman":
		cmd = "pacman -Sy"
	case "zypper":
		cmd = "zypper --non-interactive refresh"
	default:
		return fmt.Errorf("unsupported package manager: %s", pkgManager)
	}

	stdout, stderr, exitCode, err := exec.ExecuteSudo(ctx, cmd, sudoPassword)
	// For dnf/yum check-update, exit code 100 means updates available, which is not an error
	if (pkgManager == "dnf" || pkgManager == "yum") && exitCode == 100 {
		return nil
	}
	if err != nil {
		if errors.Is(err, ErrInvalidSudoPassword) {
			return ErrInvalidSudoPassword
		}
		return fmt.Errorf("refresh repository failed (exit %d): %s %w", exitCode, strings.TrimSpace(stderr+" "+stdout), err)
	}
	return nil
}

// CheckAvailableUpdates probes the host for available package updates without modifying system state
func CheckAvailableUpdates(ctx context.Context, exec Executor, pkgManager string) ([]models.PackageUpdate, error) {
	switch pkgManager {
	case "apt":
		return checkAptUpdates(ctx, exec)
	case "dnf", "yum":
		return checkDnfUpdates(ctx, exec, pkgManager)
	case "apk":
		return checkApkUpdates(ctx, exec)
	case "pacman":
		return checkPacmanUpdates(ctx, exec)
	case "zypper":
		return checkZypperUpdates(ctx, exec)
	default:
		return nil, fmt.Errorf("unsupported package manager: %s", pkgManager)
	}
}

// --- APT (Debian / Ubuntu) ---
// Uses simulated upgrade: `apt-get -s dist-upgrade` which does NOT require root
func checkAptUpdates(ctx context.Context, exec Executor) ([]models.PackageUpdate, error) {
	cmd := "LANG=C LC_ALL=C apt-get -s dist-upgrade"
	stdout, stderr, exitCode, err := exec.Execute(ctx, cmd)
	if err != nil && exitCode != 0 {
		return nil, fmt.Errorf("apt check failed: %s %w", stderr, err)
	}

	return ParseAptOutput(stdout), nil
}

// ParseAptOutput parses lines like:
// Inst curl [7.88.1-10+deb12u5] (7.88.1-10+deb12u6 Debian:12.6/stable, Debian-Security:12/stable [amd64])
func ParseAptOutput(output string) []models.PackageUpdate {
	updates := make([]models.PackageUpdate, 0)
	scanner := bufio.NewScanner(strings.NewReader(output))

	// Regex for Inst lines
	instRegex := regexp.MustCompile(`^Inst\s+([^\s]+)(?:\s+\[([^\]]+)\])?\s+\(([^\s]+)\s+([^)]*)\)`)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "Inst ") {
			continue
		}

		matches := instRegex.FindStringSubmatch(line)
		if len(matches) >= 4 {
			pkgName := matches[1]
			currentVer := matches[2]
			newVer := matches[3]
			repo := ""
			if len(matches) >= 5 {
				repo = matches[4]
			}

			isSecurity := strings.Contains(strings.ToLower(repo), "security") ||
				strings.Contains(strings.ToLower(repo), "-sec")

			updates = append(updates, models.PackageUpdate{
				Name:           pkgName,
				CurrentVersion: currentVer,
				NewVersion:     newVer,
				Repository:     repo,
				IsSecurity:     isSecurity,
			})
		}
	}
	return updates
}

// --- DNF / YUM (RHEL / CentOS / Fedora / Rocky) ---
func checkDnfUpdates(ctx context.Context, exec Executor, binary string) ([]models.PackageUpdate, error) {
	cmd := fmt.Sprintf("LANG=C LC_ALL=C %s check-update -q", binary)
	stdout, _, exitCode, _ := exec.Execute(ctx, cmd)
	// dnf check-update returns exit code 100 if updates are available, 0 if up to date

	updates := ParseDnfOutput(stdout)

	// Check security advisories if available
	secCmd := fmt.Sprintf("LANG=C LC_ALL=C %s updateinfo list security -q", binary)
	secStdout, _, _, err := exec.Execute(ctx, secCmd)
	if err == nil && len(secStdout) > 0 {
		secSet := make(map[string]bool)
		secScanner := bufio.NewScanner(strings.NewReader(secStdout))
		for secScanner.Scan() {
			fields := strings.Fields(secScanner.Text())
			if len(fields) >= 3 {
				// Typically: FEDORA-2024-xxxx Security package-version
				pkgPart := fields[len(fields)-1]
				parts := strings.Split(pkgPart, "-")
				if len(parts) > 1 {
					secSet[parts[0]] = true
				}
			}
		}
		for i := range updates {
			if secSet[updates[i].Name] {
				updates[i].IsSecurity = true
			}
		}
	}

	if exitCode != 0 && exitCode != 100 && len(updates) == 0 {
		return nil, fmt.Errorf("%s check-update failed with code %d", binary, exitCode)
	}

	return updates, nil
}

func ParseDnfOutput(output string) []models.PackageUpdate {
	updates := make([]models.PackageUpdate, 0)
	scanner := bufio.NewScanner(strings.NewReader(output))

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "Security:") || strings.HasPrefix(line, "Obsoleting") || strings.HasPrefix(line, "Last metadata") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 3 && safePackageRegex.MatchString(fields[0]) {
			pkgFullName := fields[0]
			newVer := fields[1]
			repo := fields[2]

			// Strip architecture (e.g. curl.x86_64 -> curl)
			pkgName := pkgFullName
			if idx := strings.LastIndex(pkgName, "."); idx != -1 {
				pkgName = pkgName[:idx]
			}

			updates = append(updates, models.PackageUpdate{
				Name:           pkgName,
				CurrentVersion: "installed",
				NewVersion:     newVer,
				Repository:     repo,
			})
		}
	}
	return updates
}

// --- APK (Alpine Linux) ---
func checkApkUpdates(ctx context.Context, exec Executor) ([]models.PackageUpdate, error) {
	cmd := "LANG=C LC_ALL=C apk version -v -l '<'"
	stdout, stderr, exitCode, err := exec.Execute(ctx, cmd)
	if err != nil && exitCode != 0 {
		return nil, fmt.Errorf("apk check failed: %s %w", stderr, err)
	}

	return ParseApkOutput(stdout), nil
}

// ParseApkOutput parses lines like:
// busybox-1.36.1-r29 < 1.36.1-r30
func ParseApkOutput(output string) []models.PackageUpdate {
	updates := make([]models.PackageUpdate, 0)
	scanner := bufio.NewScanner(strings.NewReader(output))

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.Contains(line, "<") {
			continue
		}

		parts := strings.Split(line, "<")
		if len(parts) == 2 {
			left := strings.TrimSpace(parts[0])
			newVer := strings.TrimSpace(parts[1])

			// Extract package name and current version from e.g. "curl-8.5.0-r0"
			lastHyphen := strings.LastIndex(left, "-")
			if lastHyphen != -1 {
				secondLast := strings.LastIndex(left[:lastHyphen], "-")
				if secondLast != -1 {
					pkgName := left[:secondLast]
					curVer := left[secondLast+1:]
					updates = append(updates, models.PackageUpdate{
						Name:           pkgName,
						CurrentVersion: curVer,
						NewVersion:     newVer,
					})
					continue
				}
			}

			updates = append(updates, models.PackageUpdate{
				Name:           left,
				CurrentVersion: "installed",
				NewVersion:     newVer,
			})
		}
	}
	return updates
}

// --- Pacman (Arch Linux) ---
func checkPacmanUpdates(ctx context.Context, exec Executor) ([]models.PackageUpdate, error) {
	// checkupdates is the Arch standard for safe non-root update checking
	cmd := "checkupdates 2>/dev/null || pacman -Qu"
	stdout, _, _, _ := exec.Execute(ctx, cmd)
	return ParsePacmanOutput(stdout), nil
}

// ParsePacmanOutput parses lines like:
// linux 6.8.1.arch1-1 -> 6.8.2.arch1-1
func ParsePacmanOutput(output string) []models.PackageUpdate {
	updates := make([]models.PackageUpdate, 0)
	scanner := bufio.NewScanner(strings.NewReader(output))

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		fields := strings.Fields(line)
		if len(fields) >= 4 && fields[2] == "->" {
			updates = append(updates, models.PackageUpdate{
				Name:           fields[0],
				CurrentVersion: fields[1],
				NewVersion:     fields[3],
			})
		}
	}
	return updates
}

// --- Zypper (openSUSE) ---
func checkZypperUpdates(ctx context.Context, exec Executor) ([]models.PackageUpdate, error) {
	cmd := "LANG=C LC_ALL=C zypper --non-interactive list-updates"
	stdout, _, _, _ := exec.Execute(ctx, cmd)
	return ParseZypperOutput(stdout), nil
}

func ParseZypperOutput(output string) []models.PackageUpdate {
	updates := make([]models.PackageUpdate, 0)
	scanner := bufio.NewScanner(strings.NewReader(output))

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "---") || strings.HasPrefix(line, "S |") || strings.HasPrefix(line, "Loading") {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) >= 5 {
			status := strings.TrimSpace(parts[0])
			if status == "v" {
				pkgName := strings.TrimSpace(parts[2])
				curVer := strings.TrimSpace(parts[3])
				newVer := strings.TrimSpace(parts[4])
				updates = append(updates, models.PackageUpdate{
					Name:           pkgName,
					CurrentVersion: curVer,
					NewVersion:     newVer,
				})
			}
		}
	}
	return updates
}

// BuildUpgradeCommand constructs the package manager command for upgrading packages
func BuildUpgradeCommand(pkgManager string, packages []string) (string, error) {
	pkgList := strings.Join(packages, " ")
	switch pkgManager {
	case "apt":
		if len(packages) > 0 {
			return fmt.Sprintf("DEBIAN_FRONTEND=noninteractive apt-get install -y --only-upgrade -- %s", pkgList), nil
		}
		return "DEBIAN_FRONTEND=noninteractive apt-get dist-upgrade -y", nil
	case "dnf":
		if len(packages) > 0 {
			return fmt.Sprintf("dnf upgrade -y -- %s", pkgList), nil
		}
		return "dnf upgrade -y", nil
	case "yum":
		if len(packages) > 0 {
			return fmt.Sprintf("yum update -y -- %s", pkgList), nil
		}
		return "yum update -y", nil
	case "apk":
		if len(packages) > 0 {
			return fmt.Sprintf("apk add -u -- %s", pkgList), nil
		}
		return "apk upgrade", nil
	case "pacman":
		if len(packages) > 0 {
			return fmt.Sprintf("pacman -S --noconfirm -- %s", pkgList), nil
		}
		return "pacman -Syu --noconfirm", nil
	case "zypper":
		if len(packages) > 0 {
			return fmt.Sprintf("zypper --non-interactive update -- %s", pkgList), nil
		}
		return "zypper --non-interactive update -y", nil
	default:
		return "", fmt.Errorf("unsupported package manager: %s", pkgManager)
	}
}

// UpgradePackages performs upgrade of specified packages (or all packages if empty)
func UpgradePackages(ctx context.Context, exec Executor, pkgManager string, packages []string, sudoPassword string) (string, error) {
	return UpgradePackagesStream(ctx, exec, pkgManager, packages, sudoPassword, nil)
}

// UpgradePackagesStream performs upgrade with real-time output callback
func UpgradePackagesStream(ctx context.Context, exec Executor, pkgManager string, packages []string, sudoPassword string, onChunk func(string)) (string, error) {
	// Sanitize every package name
	for _, pkg := range packages {
		if err := ValidatePackageName(pkg); err != nil {
			return "", err
		}
	}

	cmd, err := BuildUpgradeCommand(pkgManager, packages)
	if err != nil {
		return "", err
	}

	stdout, stderr, exitCode, err := exec.ExecuteSudoStream(ctx, cmd, sudoPassword, onChunk)
	if err != nil {
		if errors.Is(err, ErrInvalidSudoPassword) {
			return "", ErrInvalidSudoPassword
		}
		return "", fmt.Errorf("upgrade failed (exit code %d): %s %w", exitCode, strings.TrimSpace(stderr+" "+stdout), err)
	}

	return stdout, nil
}
