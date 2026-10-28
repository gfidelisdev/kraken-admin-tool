package plugins

import (
	"testing"
)

func TestParseAndValidateManifest_Success(t *testing.T) {
	raw := []byte(`{
		"id": "com.kraken.port-checker",
		"name": "Port Checker Plugin",
		"version": "1.0.0",
		"description": "Scans ports on hosts",
		"author": "Security Team",
		"runtime": {
			"type": "wasm",
			"entrypoint": "backend/plugin.wasm"
		},
		"frontend": {
			"entrypoint": "frontend/worker.js"
		},
		"permissions": ["ui:sidebar", "docker:read", "storage:kv"],
		"ui_extensions": {
			"sidebar": [
				{
					"id": "ports",
					"title": "Port Checker"
				}
			]
		}
	}`)

	m, err := ParseAndValidateManifest(raw)
	if err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}

	if m.ID != "com.kraken.port-checker" {
		t.Errorf("expected ID com.kraken.port-checker, got %s", m.ID)
	}
	if len(m.Permissions) != 3 {
		t.Errorf("expected 3 permissions, got %d", len(m.Permissions))
	}
	if m.Runtime.Type != "wasm" {
		t.Errorf("expected runtime wasm, got %s", m.Runtime.Type)
	}
}

func TestParseAndValidateManifest_InvalidID(t *testing.T) {
	invalidCases := []string{
		`{"id": "../bad", "name": "Test", "version": "1.0.0"}`,
		`{"id": "com/kraken/bad", "name": "Test", "version": "1.0.0"}`,
		`{"id": "", "name": "Test", "version": "1.0.0"}`,
	}

	for _, c := range invalidCases {
		_, err := ParseAndValidateManifest([]byte(c))
		if err == nil {
			t.Errorf("expected error for manifest: %s", c)
		}
	}
}

func TestParseAndValidateManifest_InvalidPermission(t *testing.T) {
	raw := []byte(`{
		"id": "com.kraken.bad-perm",
		"name": "Test",
		"version": "1.0.0",
		"permissions": ["system:root_takeover"]
	}`)

	_, err := ParseAndValidateManifest(raw)
	if err == nil {
		t.Fatalf("expected error for invalid permission, got nil")
	}
}

func TestParseAndValidateManifest_PathTraversalEntrypoint(t *testing.T) {
	raw := []byte(`{
		"id": "com.kraken.traversal",
		"name": "Test",
		"version": "1.0.0",
		"runtime": {
			"type": "wasm",
			"entrypoint": "../../etc/passwd"
		}
	}`)

	_, err := ParseAndValidateManifest(raw)
	if err == nil {
		t.Fatalf("expected error for directory traversal entrypoint, got nil")
	}
}
