package plugins

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kraken/admin-tool/backend/internal/models"
)

// InspectDirectory inspects an uncompressed plugin directory on disk without installing it
func InspectDirectory(dirPath string, trustedKeys []models.PluginTrustedKey) (*ExtractedInspection, error) {
	cleanPath := filepath.Clean(strings.TrimSpace(dirPath))
	if cleanPath == "" {
		return nil, errors.New("directory path is required")
	}

	absPath, err := filepath.Abs(cleanPath)
	if err != nil {
		return nil, fmt.Errorf("invalid directory path: %w", err)
	}

	fi, err := os.Stat(absPath)
	if err != nil {
		return nil, fmt.Errorf("directory not found: %s", absPath)
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("path is not a directory: %s", absPath)
	}

	// 1. Locate root containing plugin.json
	root := absPath
	manifestPath := filepath.Join(root, "plugin.json")
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		// Check if wrapped in a single child directory
		entries, readErr := os.ReadDir(absPath)
		if readErr == nil && len(entries) == 1 && entries[0].IsDir() {
			subPath := filepath.Join(absPath, entries[0].Name())
			if _, subErr := os.Stat(filepath.Join(subPath, "plugin.json")); subErr == nil {
				root = subPath
				manifestPath = filepath.Join(root, "plugin.json")
			}
		}
	}

	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("directory '%s' does not contain 'plugin.json'", absPath)
	}

	manifest, err := ParseAndValidateManifest(manifestBytes)
	if err != nil {
		return nil, fmt.Errorf("invalid manifest in directory: %w", err)
	}

	// 2. Read optional metadata files
	var readmeData []byte
	for _, cand := range []string{"README.md", "readme.md", "Readme.md"} {
		if data, rErr := os.ReadFile(filepath.Join(root, cand)); rErr == nil {
			readmeData = data
			break
		}
	}

	iconDataURL, iconSVGStr := ResolvePluginDirIcon(root)

	var signatureData []byte
	if data, sErr := os.ReadFile(filepath.Join(root, "SIGNATURE.sig")); sErr == nil {
		signatureData = data
	}

	// 3. Scan files, calculate total size and deterministic checksum
	var sourceFiles []string
	var dockerfileFiles []string
	wasmFiles := make(map[string]int64)

	var fileCount int
	var totalSize int64

	hasher := sha256.New()
	type fileEntry struct {
		relPath string
		full    string
		size    int64
	}
	var fileList []fileEntry

	walkErr := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		// Skip VCS directories
		if strings.HasPrefix(rel, ".git") || strings.HasPrefix(rel, ".svn") || strings.HasPrefix(rel, ".hg") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if info.IsDir() {
			return nil
		}

		fileCount++
		if fileCount > MaxArchiveFileCount {
			return errors.New("directory contains too many files (exceeds limit)")
		}

		totalSize += info.Size()
		if totalSize > MaxExtractedSizeBytes {
			return errors.New("directory content exceeds maximum allowed size limit (150MB)")
		}

		normName := filepath.ToSlash(rel)
		if strings.HasSuffix(strings.ToLower(normName), ".go") {
			sourceFiles = append(sourceFiles, normName)
		} else if strings.HasSuffix(strings.ToLower(normName), ".wasm") {
			wasmFiles[normName] = info.Size()
		} else if strings.EqualFold(filepath.Base(normName), "dockerfile") || strings.HasSuffix(strings.ToLower(normName), ".dockerfile") {
			dockerfileFiles = append(dockerfileFiles, normName)
			sourceFiles = append(sourceFiles, normName)
		}

		fileList = append(fileList, fileEntry{
			relPath: normName,
			full:    path,
			size:    info.Size(),
		})
		return nil
	})

	if walkErr != nil {
		return nil, fmt.Errorf("failed to scan directory files: %w", walkErr)
	}

	// Deterministic hash over sorted file relative paths and contents
	sort.Slice(fileList, func(i, j int) bool {
		return fileList[i].relPath < fileList[j].relPath
	})

	for _, fe := range fileList {
		hasher.Write([]byte(fe.relPath))
		f, openErr := os.Open(fe.full)
		if openErr == nil {
			_, _ = io.Copy(hasher, f)
			_ = f.Close()
		}
	}
	sha256Hex := hex.EncodeToString(hasher.Sum(nil))

	// 4. Signature verification
	var hasSignature bool
	var signatureVerified bool
	var signerKeyID string
	var signerName string

	if len(signatureData) > 0 {
		hasSignature = true
		signatureVerified, signerKeyID, signerName = verifySignature(sha256Hex, signatureData, trustedKeys)
	}

	// 5. Detect compilation needs
	var needsCompilation bool
	var sourceDetected bool

	if manifest.Runtime.Type == "wasm" {
		wasmEntrypoint := filepath.ToSlash(manifest.Runtime.Entrypoint)
		wasmSize, hasWasm := wasmFiles[wasmEntrypoint]
		if !hasWasm {
			wasmSize, hasWasm = wasmFiles[filepath.FromSlash(wasmEntrypoint)]
		}

		if len(sourceFiles) > 0 {
			sourceDetected = true
		}

		// Needs compile if wasm is missing or a placeholder stub (< 1KB), and Go source files exist
		if (!hasWasm || wasmSize < 1024) && len(sourceFiles) > 0 {
			needsCompilation = true
		}
	} else if manifest.Runtime.Type == "container" {
		if len(dockerfileFiles) > 0 || len(sourceFiles) > 0 {
			sourceDetected = true
			needsCompilation = true
		}
	}

	return &ExtractedInspection{
		Manifest:          manifest,
		SHA256:            sha256Hex,
		HasSignature:      hasSignature,
		SignatureVerified: signatureVerified,
		SignerKeyID:       signerKeyID,
		SignerName:        signerName,
		ArchiveType:       ArchiveTypeDirectory,
		ArchiveFormat:     "directory",
		SizeBytes:         totalSize,
		ReadmeMarkdown:    string(readmeData),
		IconSVG:           iconSVGStr,
		IconURL:           iconDataURL,
		NeedsCompilation:  needsCompilation,
		SourceDetected:    sourceDetected,
		SourceFiles:       sourceFiles,
	}, nil
}

// CopyDirectory recursively copies all files from srcDir to dstDir, preserving structure and permissions
func CopyDirectory(srcDir, dstDir string) error {
	cleanSrc := filepath.Clean(srcDir)
	cleanDst := filepath.Clean(dstDir)

	if cleanSrc == cleanDst {
		return nil // In-place installation
	}

	if err := os.MkdirAll(cleanDst, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory '%s': %w", cleanDst, err)
	}

	return filepath.Walk(cleanSrc, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(cleanSrc, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		// Skip VCS directories
		if strings.HasPrefix(rel, ".git") || strings.HasPrefix(rel, ".svn") || strings.HasPrefix(rel, ".hg") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		targetPath := filepath.Join(cleanDst, rel)

		// Check for symlinks pointing outside srcDir
		if info.Mode()&os.ModeSymlink != 0 {
			realTarget, evalErr := filepath.EvalSymlinks(path)
			if evalErr != nil {
				return nil // Skip broken symlinks safely
			}
			relToSrc, relErr := filepath.Rel(cleanSrc, realTarget)
			if relErr != nil || strings.HasPrefix(relToSrc, "..") {
				return nil // Skip symlinks pointing outside source directory (directory traversal protection)
			}
		}

		if info.IsDir() {
			return os.MkdirAll(targetPath, 0755)
		}

		if info.Mode().IsRegular() {
			if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				return err
			}

			srcFile, err := os.Open(path)
			if err != nil {
				return err
			}
			defer srcFile.Close()

			perm := info.Mode().Perm()
			if perm == 0 {
				perm = 0644
			}

			dstFile, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
			if err != nil {
				return err
			}
			defer dstFile.Close()

			if _, err := io.Copy(dstFile, srcFile); err != nil {
				return err
			}
		}

		return nil
	})
}
