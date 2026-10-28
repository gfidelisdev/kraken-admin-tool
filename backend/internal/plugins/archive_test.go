package plugins

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/kraken/admin-tool/backend/internal/models"
)

func createTestZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("failed to create zip entry: %v", err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("failed to write zip content: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("failed to close zip: %v", err)
	}
	return buf.Bytes()
}

func createTestTarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)
	for name, content := range files {
		data := []byte(content)
		hdr := &tar.Header{
			Name: name,
			Mode: 0644,
			Size: int64(len(data)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("failed to write tar header: %v", err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatalf("failed to write tar content: %v", err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("failed to close tar: %v", err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatalf("failed to close gzip: %v", err)
	}
	return buf.Bytes()
}

func TestInspectAndExtract_Zip(t *testing.T) {
	manifestJSON := `{
		"id": "com.example.test-plugin",
		"name": "Test Plugin",
		"version": "1.0.0",
		"description": "A unit test plugin",
		"runtime": { "type": "none" },
		"permissions": ["ui:sidebar"]
	}`

	files := map[string]string{
		"plugin.json":       manifestJSON,
		"README.md":         "# Test Readme\nThis is a test.",
		"frontend/worker.js": "console.log('worker ready');",
	}

	zipBytes := createTestZip(t, files)

	insp, err := InspectArchive("test.zip", zipBytes, nil)
	if err != nil {
		t.Fatalf("failed to inspect zip: %v", err)
	}

	if insp.Manifest.ID != "com.example.test-plugin" {
		t.Errorf("expected ID com.example.test-plugin, got %s", insp.Manifest.ID)
	}
	if insp.ArchiveType != ArchiveTypeZip {
		t.Errorf("expected ArchiveTypeZip, got %s", insp.ArchiveType)
	}
	if len(insp.ReadmeMarkdown) == 0 {
		t.Errorf("expected README content to be extracted")
	}

	tmpDir := t.TempDir()
	extractDest := filepath.Join(tmpDir, "extracted")
	if err := ExtractArchive(zipBytes, ArchiveTypeZip, extractDest); err != nil {
		t.Fatalf("failed to extract zip: %v", err)
	}

	if _, err := os.Stat(filepath.Join(extractDest, "plugin.json")); err != nil {
		t.Errorf("plugin.json was not extracted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(extractDest, "frontend", "worker.js")); err != nil {
		t.Errorf("frontend/worker.js was not extracted: %v", err)
	}
}

func TestInspectAndExtract_TarGz(t *testing.T) {
	manifestJSON := `{
		"id": "com.example.targz-plugin",
		"name": "TarGz Plugin",
		"version": "2.0.0",
		"runtime": { "type": "none" },
		"permissions": ["docker:read"]
	}`

	files := map[string]string{
		"my-plugin-root/plugin.json":       manifestJSON,
		"my-plugin-root/frontend/worker.js": "self.onmessage = () => {};",
	}

	tarGzBytes := createTestTarGz(t, files)

	insp, err := InspectArchive("plugin.tar.gz", tarGzBytes, nil)
	if err != nil {
		t.Fatalf("failed to inspect tar.gz: %v", err)
	}

	if insp.Manifest.ID != "com.example.targz-plugin" {
		t.Errorf("expected ID com.example.targz-plugin, got %s", insp.Manifest.ID)
	}
	if insp.ArchiveType != ArchiveTypeTarGz {
		t.Errorf("expected ArchiveTypeTarGz, got %s", insp.ArchiveType)
	}

	tmpDir := t.TempDir()
	extractDest := filepath.Join(tmpDir, "extracted-targz")
	if err := ExtractArchive(tarGzBytes, ArchiveTypeTarGz, extractDest); err != nil {
		t.Fatalf("failed to extract tar.gz: %v", err)
	}

	if _, err := os.Stat(filepath.Join(extractDest, "plugin.json")); err != nil {
		t.Errorf("plugin.json was not extracted: %v", err)
	}
}

func TestExtractArchive_ZipSlipBlocked(t *testing.T) {
	files := map[string]string{
		"plugin.json":          `{"id": "com.bad", "name": "Bad", "version": "1.0.0"}`,
		"../../escape_attempt": "malicious content",
	}

	zipBytes := createTestZip(t, files)
	tmpDir := t.TempDir()
	extractDest := filepath.Join(tmpDir, "target")

	err := ExtractArchive(zipBytes, ArchiveTypeZip, extractDest)
	if err == nil {
		t.Fatalf("expected ZipSlip error, got nil!")
	}
}

func TestInspectArchive_Ed25519Signature(t *testing.T) {
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate ed25519 key: %v", err)
	}

	pubB64 := base64.StdEncoding.EncodeToString(pubKey)
	trustedKeys := []models.PluginTrustedKey{
		{
			ID:               1,
			Name:             "Official Kraken Team",
			PublicKeyEd25519: pubB64,
			Fingerprint:      "fp-official-1",
		},
	}

	// Create archive without signature first
	baseFiles := map[string]string{
		"plugin.json": `{"id": "com.signed.plugin", "name": "Signed", "version": "1.0.0"}`,
	}
	zipWithoutSig := createTestZip(t, baseFiles)
	hash := sha256.Sum256(zipWithoutSig)
	hashHex := hex.EncodeToString(hash[:])

	// Sign the hash
	sig := ed25519.Sign(privKey, []byte(hashHex))
	sigB64 := base64.StdEncoding.EncodeToString(sig)

	// Now create archive containing SIGNATURE.sig
	filesWithSig := map[string]string{
		"plugin.json":   `{"id": "com.signed.plugin", "name": "Signed", "version": "1.0.0"}`,
		"SIGNATURE.sig": sigB64,
	}
	zipWithSig := createTestZip(t, filesWithSig)

	// Verify using the helper
	verified, fp, name := verifySignature(hashHex, []byte(sigB64), trustedKeys)
	if !verified {
		t.Fatalf("expected signature to be verified")
	}
	if fp != "fp-official-1" || name != "Official Kraken Team" {
		t.Errorf("unexpected key match: %s, %s", fp, name)
	}

	_ = zipWithSig
}
