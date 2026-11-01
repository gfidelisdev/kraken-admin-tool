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

func TestParseAndValidateManifest_ContainerRuntimeDockerfile(t *testing.T) {
	// Case 1: only dockerfile specified
	raw := []byte(`{
		"id": "com.kraken.docker-service",
		"name": "Docker Service",
		"version": "2.1.0",
		"runtime": {
			"type": "container",
			"dockerfile": "Dockerfile",
			"port": 9000
		}
	}`)

	m, err := ParseAndValidateManifest(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if m.Runtime.Dockerfile != "Dockerfile" {
		t.Errorf("expected Dockerfile, got %s", m.Runtime.Dockerfile)
	}
	expectedTag := "kraken-plugin-com-kraken-docker-service:2.1.0"
	if m.Runtime.Entrypoint != expectedTag {
		t.Errorf("expected entrypoint tag %s, got %s", expectedTag, m.Runtime.Entrypoint)
	}
	if m.Runtime.Port != 9000 {
		t.Errorf("expected port 9000, got %d", m.Runtime.Port)
	}

	// Case 2: entrypoint set to Dockerfile
	raw2 := []byte(`{
		"id": "com.kraken.df-entrypoint",
		"name": "DF Entrypoint",
		"version": "1.0.0",
		"runtime": {
			"type": "container",
			"entrypoint": "backend/Dockerfile"
		}
	}`)
	m2, err2 := ParseAndValidateManifest(raw2)
	if err2 != nil {
		t.Fatalf("unexpected error: %v", err2)
	}
	if m2.Runtime.Dockerfile != "backend/Dockerfile" {
		t.Errorf("expected Dockerfile backend/Dockerfile, got %s", m2.Runtime.Dockerfile)
	}
	if m2.Runtime.Entrypoint != "kraken-plugin-com-kraken-df-entrypoint:1.0.0" {
		t.Errorf("expected auto tag, got %s", m2.Runtime.Entrypoint)
	}

	// Case 3: path traversal in dockerfile
	raw3 := []byte(`{
		"id": "com.kraken.df-traversal",
		"name": "DF Traversal",
		"version": "1.0.0",
		"runtime": {
			"type": "container",
			"dockerfile": "../../Dockerfile"
		}
	}`)
	_, err3 := ParseAndValidateManifest(raw3)
	if err3 == nil {
		t.Errorf("expected error for directory traversal in dockerfile, got nil")
	}
}
