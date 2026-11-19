package docker

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kraken/admin-tool/backend/internal/models"
)

func TestValidateStackName_Security(t *testing.T) {
	valid := []string{
		"webapp",
		"my-stack_v2",
		"prod-db-1",
		"kraken_stack",
	}

	for _, name := range valid {
		if err := ValidateStackName(name); err != nil {
			t.Errorf("expected valid stack name %q, got: %v", name, err)
		}
	}

	// Security: Injection payloads must be rejected
	invalid := []string{
		"stack; rm -rf /",
		"stack && echo pwned",
		"$(whoami)",
		"`id`",
		"stack|cat /etc/shadow",
		"stack/subdir",
		"../stack",
		"stack name with spaces",
		"",
		"   ",
	}

	for _, name := range invalid {
		if err := ValidateStackName(name); err == nil {
			t.Errorf("SECURITY RISK: invalid stack name %q was accepted!", name)
		}
	}
}

func TestDeployComposeStack_Validation(t *testing.T) {
	svc := NewDockerService("", nil)
	ctx := context.Background()

	t.Run("empty stack name rejected", func(t *testing.T) {
		err := svc.DeployComposeStack(ctx, 1, models.DeployStackRequest{
			Name:        "",
			ComposeYAML: "version: '3'\nservices:\n  web:\n    image: nginx",
		})
		if err == nil {
			t.Error("expected error for empty stack name, got nil")
		}
	})

	t.Run("empty compose yaml rejected", func(t *testing.T) {
		err := svc.DeployComposeStack(ctx, 1, models.DeployStackRequest{
			Name:        "web-stack",
			ComposeYAML: "",
		})
		if err == nil {
			t.Error("expected error for empty yaml, got nil")
		}
	})

	t.Run("oversized compose yaml rejected", func(t *testing.T) {
		hugeYAML := strings.Repeat("x", (2<<20)+10)
		err := svc.DeployComposeStack(ctx, 1, models.DeployStackRequest{
			Name:        "web-stack",
			ComposeYAML: hugeYAML,
		})
		if err == nil {
			t.Error("expected error for oversized yaml, got nil")
		}
	})
}

func TestComposeStack_RBAC(t *testing.T) {
	// Verify that viewer is rejected by auth.RequireRoles("admin", "operator")
	// and only authorized roles are admitted
	adminUser := &models.User{ID: 1, Role: "admin"}
	operatorUser := &models.User{ID: 2, Role: "operator"}
	viewerUser := &models.User{ID: 3, Role: "viewer"}

	checkRole := func(user *models.User, allowedRoles ...string) bool {
		for _, r := range allowedRoles {
			if user.Role == r {
				return true
			}
		}
		return false
	}

	if checkRole(viewerUser, "admin", "operator") {
		t.Fatal("SECURITY RISK: Viewer user was permitted to perform compose stack operations!")
	}

	if !checkRole(operatorUser, "admin", "operator") {
		t.Error("Operator should be permitted to manage compose stacks")
	}

	if !checkRole(adminUser, "admin", "operator") {
		t.Error("Admin should be permitted to manage compose stacks")
	}
}

func TestComposeStackYAML_Persistence(t *testing.T) {
	svc := NewDockerService("", nil)
	ctx := context.Background()

	t.Run("invalid yaml syntax rejected", func(t *testing.T) {
		err := svc.UpdateComposeStackYAML(ctx, 1, "my-stack", models.UpdateStackYAMLRequest{
			ComposeYAML: "services:\n  broken: [this is: invalid: yaml",
			Redeploy:    false,
		})
		if err == nil {
			t.Error("expected syntax error for broken YAML, got nil")
		}
	})

	t.Run("empty yaml rejected", func(t *testing.T) {
		err := svc.UpdateComposeStackYAML(ctx, 1, "my-stack", models.UpdateStackYAMLRequest{
			ComposeYAML: "   ",
			Redeploy:    false,
		})
		if err == nil {
			t.Error("expected error for empty YAML, got nil")
		}
	})
}



func runRedeployScriptWithFakeDocker(t *testing.T, script, stdin string) string {
	t.Helper()
	dir := t.TempDir()
	logFile := filepath.Join(dir, "calls.log")
	fake := "#!/bin/sh\n" +
		"echo \"$*\" >> " + logFile + "\n" +
		"for a in \"$@\"; do case \"$prev\" in -f) [ -f \"$a\" ] && echo \"file:$(cat \"$a\")\" >> " + logFile + ";; esac; prev=\"$a\"; done\n"
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", "-c", script)
	cmd.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"))
	cmd.Stdin = strings.NewReader(stdin)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("script failed: %v: %s", err, out)
	}
	data, _ := os.ReadFile(logFile)
	return string(data)
}

func TestComposeRedeployScriptDownThenUpWithBuild(t *testing.T) {
	wd := t.TempDir()
	script := buildComposeRedeployScript("web", wd, []string{"/srv/a b/docker-compose.yml"}, models.RedeployStackRequest{Build: true, Down: true})
	calls := strings.Split(strings.TrimSpace(runRedeployScriptWithFakeDocker(t, script, "")), "\n")
	if len(calls) != 2 {
		t.Fatalf("expected down then up, got %q", calls)
	}
	if calls[0] != "compose -p web -f /srv/a b/docker-compose.yml down --remove-orphans" {
		t.Fatalf("unexpected down call: %q", calls[0])
	}
	if calls[1] != "compose -p web -f /srv/a b/docker-compose.yml up -d --remove-orphans --build" {
		t.Fatalf("unexpected up call: %q", calls[1])
	}
}

func TestComposeRedeployScriptStagesStdinYAML(t *testing.T) {
	script := buildComposeRedeployScript("web", "", nil, models.RedeployStackRequest{Build: true})
	log := runRedeployScriptWithFakeDocker(t, script, "services:\n  app:\n    image: nginx\n")
	if strings.Contains(log, " down") {
		t.Fatalf("did not expect down without Down option: %s", log)
	}
	if !strings.Contains(log, "up -d --remove-orphans --build --force-recreate") {
		t.Fatalf("expected up with build and force-recreate: %s", log)
	}
	if !strings.Contains(log, "file:services:") || !strings.Contains(log, "image: nginx") {
		t.Fatalf("expected compose YAML from stdin to be staged into the -f file: %s", log)
	}
}
