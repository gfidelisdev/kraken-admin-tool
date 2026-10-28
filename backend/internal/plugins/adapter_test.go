package plugins

import (
	"context"
	"testing"
)

func TestParseListenersOutput(t *testing.T) {
	sampleSS := `Netid State  Recv-Q Send-Q Local Address:Port  Peer Address:PortProcess
tcp   LISTEN 0      128          0.0.0.0:22         0.0.0.0:*    users:(("sshd",pid=123,fd=3))
tcp   LISTEN 0      4096         0.0.0.0:80         0.0.0.0:*    users:(("nginx",pid=456,fd=6))
tcp   LISTEN 0      4096       127.0.0.1:5432       0.0.0.0:*    users:(("postgres",pid=789,fd=7))
tcp   LISTEN 0      128             [::]:22            [::]:*    users:(("sshd",pid=123,fd=4))
tcp   LISTEN 0      4096            [::]:8080          [::]:*    users:(("kraken",pid=1001,fd=5))
udp   UNCONN 0      0            0.0.0.0:5353       0.0.0.0:*    users:(("avahi-daemon",pid=321,fd=12))
`

	ports := parseListenersOutput("srv-prod-01", sampleSS)

	// Should extract 0.0.0.0:22 (SSH), 0.0.0.0:80 (HTTP), [::]:8080 (kraken), 0.0.0.0:5353 (udp)
	// Should IGNORE 127.0.0.1:5432 (loopback) and dedup [::]:22 with 0.0.0.0:22
	if len(ports) < 3 {
		t.Fatalf("expected at least 3 external ports, got %d", len(ports))
	}

	foundSSH := false
	foundNginx := false
	foundLocalhostDB := false

	for _, p := range ports {
		if p.Port == 22 && p.Protocol == "tcp" {
			foundSSH = true
			if p.Service != "SSH" {
				t.Errorf("expected SSH service, got %s", p.Service)
			}
		}
		if p.Port == 80 && p.Protocol == "tcp" {
			foundNginx = true
		}
		if p.Port == 5432 {
			foundLocalhostDB = true
		}
	}

	if !foundSSH {
		t.Errorf("expected external SSH port 22 to be detected")
	}
	if !foundNginx {
		t.Errorf("expected external HTTP port 80 to be detected")
	}
	if foundLocalhostDB {
		t.Errorf("loopback port 5432 should not be included in external listeners")
	}
}

func TestResolveHostAdapter_Permissions(t *testing.T) {
	m := &Manager{}
	ctx := context.Background()

	// 1. Missing hosts:read permission
	req := map[string]interface{}{
		"status": "needs_host_adapter",
		"data": map[string]interface{}{
			"operation": "network.scan_external_listeners",
		},
	}

	res, err := m.ResolveHostAdapter(ctx, req, map[string]bool{}, "test.plugin", "testuser", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resp, ok := res.(HostAdapterResponse)
	if !ok || resp.Status != "denied" {
		t.Fatalf("expected status 'denied' when hosts:read is missing, got %+v", res)
	}
}
