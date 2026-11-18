package docker

import (
	"testing"

	"github.com/kraken/admin-tool/backend/internal/models"
)

func TestRedactServerConnectionDetails(t *testing.T) {
	s := models.DockerServer{Name: "n", HostURL: "ssh://h", SSHUser: "root", VaultConfig: "x"}
	redactServerConnectionDetails(&s)
	if s.HostURL != "" || s.SSHUser != "" || s.VaultConfig != "" || s.Name != "n" {
		t.Fatalf("unexpected redaction result: %+v", s)
	}
}
