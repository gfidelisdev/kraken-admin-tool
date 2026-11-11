package plugins_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kraken/admin-tool/backend/internal/plugins"
)

func TestInspectDirectory_Valid(t *testing.T) {
	tmpDir := t.TempDir()

	manifestContent := `{
		"id": "com.test.local-dir-plugin",
		"name": "Local Directory Plugin",
		"version": "1.0.0",
		"description": "Testing installation from uncompressed directory",
		"author": "Tester",
		"license": "MIT",
		"runtime": {
			"type": "wasm",
			"entrypoint": "backend/plugin.wasm"
		},
		"permissions": ["ui:sidebar", "docker:read"]
	}`

	if err := os.WriteFile(filepath.Join(tmpDir, "plugin.json"), []byte(manifestContent), 0644); err != nil {
		t.Fatalf("failed to write plugin.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "README.md"), []byte("# Local Plugin Readme"), 0644); err != nil {
		t.Fatalf("failed to write README.md: %v", err)
	}

	// Add Go source files to trigger source detection
	backendDir := filepath.Join(tmpDir, "backend")
	_ = os.MkdirAll(backendDir, 0755)
	_ = os.WriteFile(filepath.Join(backendDir, "main.go"), []byte("package main\nfunc main() {}\n"), 0644)

	insp, err := plugins.InspectDirectory(tmpDir, nil)
	if err != nil {
		t.Fatalf("unexpected error inspecting directory: %v", err)
	}

	if insp.Manifest.ID != "com.test.local-dir-plugin" {
		t.Errorf("expected plugin ID 'com.test.local-dir-plugin', got '%s'", insp.Manifest.ID)
	}
	if insp.ArchiveFormat != "directory" {
		t.Errorf("expected archive_format 'directory', got '%s'", insp.ArchiveFormat)
	}
	if !insp.NeedsCompilation {
		t.Errorf("expected NeedsCompilation=true since wasm binary is missing and Go sources exist")
	}
	if !insp.SourceDetected {
		t.Errorf("expected SourceDetected=true")
	}
	if insp.ReadmeMarkdown != "# Local Plugin Readme" {
		t.Errorf("expected readme markdown to match, got '%s'", insp.ReadmeMarkdown)
	}
	if insp.SHA256 == "" {
		t.Errorf("expected non-empty deterministic SHA256 checksum for directory")
	}
}

func TestInspectDirectory_WrappedInSubfolder(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "plugin-root")
	_ = os.MkdirAll(subDir, 0755)

	manifestContent := `{
		"id": "com.test.wrapped-plugin",
		"name": "Wrapped Directory Plugin",
		"version": "1.0.0",
		"author": "Tester",
		"license": "MIT",
		"permissions": ["ui:sidebar"]
	}`
	_ = os.WriteFile(filepath.Join(subDir, "plugin.json"), []byte(manifestContent), 0644)

	// Inspecting the parent directory should resolve the wrapped root
	insp, err := plugins.InspectDirectory(tmpDir, nil)
	if err != nil {
		t.Fatalf("failed to inspect wrapped directory: %v", err)
	}

	if insp.Manifest.ID != "com.test.wrapped-plugin" {
		t.Errorf("expected plugin ID 'com.test.wrapped-plugin', got '%s'", insp.Manifest.ID)
	}
}

func TestInspectDirectory_MissingManifest(t *testing.T) {
	emptyDir := t.TempDir()
	_, err := plugins.InspectDirectory(emptyDir, nil)
	if err == nil {
		t.Fatalf("expected error when directory lacks plugin.json, got nil")
	}
}

func TestCopyDirectory(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := filepath.Join(t.TempDir(), "target")

	_ = os.WriteFile(filepath.Join(srcDir, "file1.txt"), []byte("hello"), 0644)
	sub := filepath.Join(srcDir, "subdir")
	_ = os.MkdirAll(sub, 0755)
	_ = os.WriteFile(filepath.Join(sub, "file2.txt"), []byte("world"), 0644)

	// .git folder should be ignored
	gitDir := filepath.Join(srcDir, ".git")
	_ = os.MkdirAll(gitDir, 0755)
	_ = os.WriteFile(filepath.Join(gitDir, "config"), []byte("git config"), 0644)

	if err := plugins.CopyDirectory(srcDir, dstDir); err != nil {
		t.Fatalf("CopyDirectory failed: %v", err)
	}

	// Verify copied files
	if data, err := os.ReadFile(filepath.Join(dstDir, "file1.txt")); err != nil || string(data) != "hello" {
		t.Errorf("file1.txt not copied correctly")
	}
	if data, err := os.ReadFile(filepath.Join(dstDir, "subdir", "file2.txt")); err != nil || string(data) != "world" {
		t.Errorf("file2.txt not copied correctly")
	}
	// Verify .git was skipped
	if _, err := os.Stat(filepath.Join(dstDir, ".git")); !os.IsNotExist(err) {
		t.Errorf(".git directory should have been skipped")
	}
}

func TestInstallFromDirectory(t *testing.T) {
	db := setupPluginTestDB(t)
	pluginsDir := t.TempDir()

	mgr, err := plugins.NewManager(context.Background(), db, nil, pluginsDir)
	if err != nil {
		t.Fatalf("failed to create plugin manager: %v", err)
	}

	srcDir := t.TempDir()
	manifestContent := `{
		"id": "com.test.installed-from-dir",
		"name": "Installed From Directory Plugin",
		"version": "1.2.3",
		"author": "Tester",
		"license": "Apache-2.0",
		"runtime": { "type": "none" },
		"permissions": ["ui:sidebar", "storage:kv"]
	}`
	_ = os.WriteFile(filepath.Join(srcDir, "plugin.json"), []byte(manifestContent), 0644)
	_ = os.WriteFile(filepath.Join(srcDir, "README.md"), []byte("# Direct Directory Plugin"), 0644)

	ctx := context.Background()
	p, err := mgr.InstallFromDirectory(
		ctx,
		srcDir,
		"",
		[]string{"ui:sidebar", "storage:kv"},
		true,
		"admin",
		1,
	)
	if err != nil {
		t.Fatalf("InstallFromDirectory failed: %v", err)
	}

	if p.PluginID != "com.test.installed-from-dir" {
		t.Errorf("expected plugin ID 'com.test.installed-from-dir', got '%s'", p.PluginID)
	}
	if p.Status != "enabled" {
		t.Errorf("expected status 'enabled', got '%s'", p.Status)
	}
	if p.Version != "1.2.3" {
		t.Errorf("expected version '1.2.3', got '%s'", p.Version)
	}

	// Verify plugin files exist in the plugins target directory
	targetPath := filepath.Join(pluginsDir, "com.test.installed-from-dir")
	if _, err := os.Stat(filepath.Join(targetPath, "plugin.json")); err != nil {
		t.Errorf("expected plugin.json in target directory: %v", err)
	}
}
