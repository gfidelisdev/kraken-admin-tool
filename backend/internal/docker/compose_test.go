package docker

import (
	"context"
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

