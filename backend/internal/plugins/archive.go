package plugins

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/kraken/admin-tool/backend/internal/models"
)

const (
	MaxArchiveSizeBytes   = 50 * 1024 * 1024  // 50 MB max upload / download
	MaxExtractedSizeBytes = 150 * 1024 * 1024 // 150 MB max unpacked size (anti-bomb)
	MaxArchiveFileCount   = 5000              // 5,000 max entries
)

// ArchiveType indicates zip vs tar.gz
type ArchiveType string

const (
	ArchiveTypeZip       ArchiveType = "zip"
	ArchiveTypeTarGz     ArchiveType = "tar.gz"
	ArchiveTypeDirectory ArchiveType = "directory"
)

// ExtractedInspection holds the preview details extracted from an archive
type ExtractedInspection struct {
	Manifest          *models.PluginManifest `json:"manifest"`
	SHA256            string                 `json:"sha256"`
	HasSignature      bool                   `json:"has_signature"`
	SignatureVerified bool                   `json:"signature_verified"`
	SignerKeyID       string                 `json:"signer_key_id,omitempty"`
	SignerName        string                 `json:"signer_name,omitempty"`
	ArchiveType       ArchiveType            `json:"archive_type"`
	ArchiveFormat     string                 `json:"archive_format"`
	SizeBytes         int64                  `json:"size_bytes"`
	ReadmeMarkdown    string                 `json:"readme_markdown,omitempty"`
	IconSVG           string                 `json:"icon_svg,omitempty"`
	IconURL           string                 `json:"icon_url,omitempty"`
	NeedsCompilation  bool                   `json:"needs_compilation"`
	SourceDetected    bool                   `json:"source_detected"`
	SourceFiles       []string               `json:"source_files,omitempty"`
}

const maxIconFileSizeBytes = 2 * 1024 * 1024 // 2 MB max icon size

// matchPluginIconPath checks whether a normalized relative archive/directory path is a supported plugin icon
// (ico.svg, ico.png, ico.jpg, ico.jpeg, or fallback icon.svg, icon.png, icon.jpg, icon.jpeg).
// Returns mimeType, isSVG, priority (lower is higher priority), and matched boolean.
func matchPluginIconPath(normName string) (string, bool, int, bool) {
	clean := strings.ToLower(strings.TrimPrefix(filepath.ToSlash(normName), "/"))
	dir := filepath.Dir(clean)
	if dir != "." && dir != "frontend" && dir != "assets" {
		return "", false, 99, false
	}
	base := filepath.Base(clean)

	var mime string
	var isSVG bool
	var basePriority int

	switch base {
	case "ico.svg":
		mime, isSVG, basePriority = "image/svg+xml", true, 1
	case "ico.png":
		mime, isSVG, basePriority = "image/png", false, 2
	case "ico.jpg", "ico.jpeg":
		mime, isSVG, basePriority = "image/jpeg", false, 3
	case "icon.svg":
		mime, isSVG, basePriority = "image/svg+xml", true, 4
	case "icon.png":
		mime, isSVG, basePriority = "image/png", false, 5
	case "icon.jpg", "icon.jpeg":
		mime, isSVG, basePriority = "image/jpeg", false, 6
	default:
		return "", false, 99, false
	}

	if dir != "." {
		basePriority += 10
	}
	return mime, isSVG, basePriority, true
}

func encodeIconDataURL(mimeType string, data []byte) string {
	if len(data) == 0 || mimeType == "" {
		return ""
	}
	return "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data)
}

// ResolvePluginDirIcon scans an extracted plugin directory on disk (case-insensitively)
// for ico.(svg|png|jpg|jpeg) or icon.(svg|png|jpg|jpeg) and returns its base64 data URL and raw SVG string (if SVG).
func ResolvePluginDirIcon(installPath string) (string, string) {
	if strings.TrimSpace(installPath) == "" {
		return "", ""
	}

	bestPrio := 99
	var bestFull string
	var bestMime string
	var bestIsSVG bool

	subDirs := []string{".", "frontend", "assets"}
	for _, sub := range subDirs {
		dirPath := installPath
		if sub != "." {
			dirPath = filepath.Join(installPath, sub)
		}
		entries, err := os.ReadDir(dirPath)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			rel := entry.Name()
			if sub != "." {
				rel = sub + "/" + entry.Name()
			}
			mime, isSVG, prio, ok := matchPluginIconPath(rel)
			if !ok || prio >= bestPrio {
				continue
			}
			fi, err := entry.Info()
			if err != nil || fi.Size() <= 0 || fi.Size() > maxIconFileSizeBytes {
				continue
			}
			bestPrio = prio
			bestFull = filepath.Join(dirPath, entry.Name())
			bestMime = mime
			bestIsSVG = isSVG
		}
	}

	if bestFull == "" {
		return "", ""
	}
	data, err := os.ReadFile(bestFull)
	if err != nil || len(data) == 0 {
		return "", ""
	}
	dataURL := encodeIconDataURL(bestMime, data)
	if bestIsSVG {
		return dataURL, string(data)
	}
	return dataURL, ""
}

// DetectArchiveType identifies whether the given data is a zip or tar.gz archive
func DetectArchiveType(filename string, data []byte) (ArchiveType, error) {
	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		return ArchiveTypeTarGz, nil
	}
	if len(data) >= 4 && data[0] == 'P' && data[1] == 'K' && data[2] == 0x03 && data[3] == 0x04 {
		return ArchiveTypeZip, nil
	}

	lower := strings.ToLower(filename)
	if strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz") {
		return ArchiveTypeTarGz, nil
	}
	if strings.HasSuffix(lower, ".zip") {
		return ArchiveTypeZip, nil
	}

	return "", errors.New("unsupported archive format: only .zip and .tar.gz / .tgz are supported")
}

// InspectArchive reads and inspects an archive buffer without writing to disk
func InspectArchive(filename string, data []byte, trustedKeys []models.PluginTrustedKey) (*ExtractedInspection, error) {
	if int64(len(data)) > MaxArchiveSizeBytes {
		return nil, fmt.Errorf("archive size (%d bytes) exceeds maximum allowed limit (%d bytes)", len(data), MaxArchiveSizeBytes)
	}

	archiveType, err := DetectArchiveType(filename, data)
	if err != nil {
		return nil, err
	}

	hash := sha256.Sum256(data)
	sha256Hex := hex.EncodeToString(hash[:])

	var manifestData []byte
	var readmeData []byte
	var iconData []byte
	var iconMime string
	var iconIsSVG bool
	iconPriority := 99
	var signatureData []byte
	var sourceFiles []string
	var dockerfileFiles []string
	wasmFiles := make(map[string]int64)

	var fileCount int
	var totalUncompressedSize int64

	switch archiveType {
	case ArchiveTypeZip:
		zr, zErr := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if zErr != nil {
			return nil, fmt.Errorf("failed to read zip archive: %w", zErr)
		}
		for _, f := range zr.File {
			fileCount++
			if fileCount > MaxArchiveFileCount {
				return nil, errors.New("archive contains too many files (possible decompression bomb)")
			}
			totalUncompressedSize += int64(f.UncompressedSize64)
			if totalUncompressedSize > MaxExtractedSizeBytes {
				return nil, errors.New("extracted archive content exceeds maximum allowed size limit")
			}

			normName := strings.TrimPrefix(filepath.ToSlash(f.Name), "/")
			// Strip top-level directory if entire archive is wrapped in a single root directory
			normName = stripTopLevelDir(normName)

			if strings.HasSuffix(strings.ToLower(normName), ".go") {
				sourceFiles = append(sourceFiles, normName)
			} else if strings.HasSuffix(strings.ToLower(normName), ".wasm") {
				wasmFiles[normName] = int64(f.UncompressedSize64)
			} else if strings.EqualFold(filepath.Base(normName), "dockerfile") || strings.HasSuffix(strings.ToLower(normName), ".dockerfile") {
				dockerfileFiles = append(dockerfileFiles, normName)
				sourceFiles = append(sourceFiles, normName)
			}

			if normName == "plugin.json" {
				rc, rcErr := f.Open()
				if rcErr != nil {
					return nil, rcErr
				}
				manifestData, _ = io.ReadAll(io.LimitReader(rc, 1*1024*1024))
				rc.Close()
			} else if strings.EqualFold(normName, "readme.md") {
				rc, rcErr := f.Open()
				if rcErr == nil {
					readmeData, _ = io.ReadAll(io.LimitReader(rc, 1*1024*1024))
					rc.Close()
				}
			} else if mime, isSVG, prio, ok := matchPluginIconPath(normName); ok && prio < iconPriority {
				rc, rcErr := f.Open()
				if rcErr == nil {
					readBytes, _ := io.ReadAll(io.LimitReader(rc, maxIconFileSizeBytes))
					rc.Close()
					if len(readBytes) > 0 {
						iconData = readBytes
						iconMime = mime
						iconIsSVG = isSVG
						iconPriority = prio
					}
				}
			} else if normName == "SIGNATURE.sig" {
				rc, rcErr := f.Open()
				if rcErr == nil {
					signatureData, _ = io.ReadAll(io.LimitReader(rc, 64*1024))
					rc.Close()
				}
			}
		}

	case ArchiveTypeTarGz:
		gzr, gzErr := gzip.NewReader(bytes.NewReader(data))
		if gzErr != nil {
			return nil, fmt.Errorf("failed to open gzip stream: %w", gzErr)
		}
		defer gzr.Close()
		tr := tar.NewReader(gzr)

		for {
			header, tErr := tr.Next()
			if tErr == io.EOF {
				break
			}
			if tErr != nil {
				return nil, fmt.Errorf("tar read error: %w", tErr)
			}
			fileCount++
			if fileCount > MaxArchiveFileCount {
				return nil, errors.New("archive contains too many files (possible decompression bomb)")
			}
			totalUncompressedSize += header.Size
			if totalUncompressedSize > MaxExtractedSizeBytes {
				return nil, errors.New("extracted archive content exceeds maximum allowed size limit")
			}

			normName := strings.TrimPrefix(filepath.ToSlash(header.Name), "/")
			normName = stripTopLevelDir(normName)

			if strings.HasSuffix(strings.ToLower(normName), ".go") {
				sourceFiles = append(sourceFiles, normName)
			} else if strings.HasSuffix(strings.ToLower(normName), ".wasm") {
				wasmFiles[normName] = header.Size
			} else if strings.EqualFold(filepath.Base(normName), "dockerfile") || strings.HasSuffix(strings.ToLower(normName), ".dockerfile") {
				dockerfileFiles = append(dockerfileFiles, normName)
				sourceFiles = append(sourceFiles, normName)
			}

			if normName == "plugin.json" {
				manifestData, _ = io.ReadAll(io.LimitReader(tr, 1*1024*1024))
			} else if strings.EqualFold(normName, "readme.md") {
				readmeData, _ = io.ReadAll(io.LimitReader(tr, 1*1024*1024))
			} else if mime, isSVG, prio, ok := matchPluginIconPath(normName); ok && prio < iconPriority {
				readBytes, _ := io.ReadAll(io.LimitReader(tr, maxIconFileSizeBytes))
				if len(readBytes) > 0 {
					iconData = readBytes
					iconMime = mime
					iconIsSVG = isSVG
					iconPriority = prio
				}
			} else if normName == "SIGNATURE.sig" {
				signatureData, _ = io.ReadAll(io.LimitReader(tr, 64*1024))
			}
		}
	}

	if len(manifestData) == 0 {
		return nil, errors.New("archive does not contain a 'plugin.json' manifest at root")
	}

	manifest, err := ParseAndValidateManifest(manifestData)
	if err != nil {
		return nil, err
	}

	var hasSig bool
	var sigVerified bool
	var signerKeyID, signerName string

	if len(signatureData) > 0 {
		hasSig = true
		sigVerified, signerKeyID, signerName = verifySignature(sha256Hex, signatureData, trustedKeys)
	}

	var needsCompilation bool
	var sourceDetected bool
	if manifest.Runtime.Type == "wasm" {
		ep := manifest.Runtime.Entrypoint
		if ep == "" {
			ep = "backend/plugin.wasm"
		}
		epNorm := filepath.ToSlash(ep)
		size, found := wasmFiles[epNorm]
		if !found {
			for name, sz := range wasmFiles {
				if strings.EqualFold(name, epNorm) || strings.EqualFold(filepath.Base(name), filepath.Base(epNorm)) {
					found = true
					size = sz
					break
				}
			}
		}
		// If entrypoint wasm is absent or smaller than 1024 bytes (stub header), and Go sources are present
		if (!found || size < 1024) && len(sourceFiles) > 0 {
			needsCompilation = true
			sourceDetected = true
		} else if len(sourceFiles) > 0 {
			sourceDetected = true
		}
	} else if manifest.Runtime.Type == "container" {
		var foundDockerfile string
		if manifest.Runtime.Dockerfile != "" {
			dfNorm := filepath.ToSlash(manifest.Runtime.Dockerfile)
			for _, df := range dockerfileFiles {
				if strings.EqualFold(df, dfNorm) || strings.EqualFold(filepath.Base(df), filepath.Base(dfNorm)) {
					foundDockerfile = df
					break
				}
			}
			if foundDockerfile == "" {
				return nil, fmt.Errorf("declared dockerfile '%s' not found in plugin archive", manifest.Runtime.Dockerfile)
			}
		} else {
			// Find Dockerfile automatically in archive (prefer root "Dockerfile", then "backend/Dockerfile")
			for _, df := range dockerfileFiles {
				if strings.EqualFold(df, "Dockerfile") {
					foundDockerfile = df
					break
				}
			}
			if foundDockerfile == "" {
				for _, df := range dockerfileFiles {
					if strings.EqualFold(df, "backend/Dockerfile") {
						foundDockerfile = df
						break
					}
				}
			}
			if foundDockerfile == "" && len(dockerfileFiles) > 0 {
				foundDockerfile = dockerfileFiles[0]
			}
		}

		if foundDockerfile != "" {
			manifest.Runtime.Dockerfile = foundDockerfile
			needsCompilation = true
			sourceDetected = true
		} else {
			// No Dockerfile found in archive. Check if manifest explicitly specified an image entrypoint.
			var rawCheck struct {
				Runtime struct {
					Entrypoint string `json:"entrypoint"`
					Dockerfile string `json:"dockerfile"`
				} `json:"runtime"`
			}
			_ = json.Unmarshal(manifestData, &rawCheck)
			if strings.TrimSpace(rawCheck.Runtime.Entrypoint) == "" && strings.TrimSpace(rawCheck.Runtime.Dockerfile) == "" {
				return nil, errors.New("container runtime requires a 'Dockerfile' in the plugin archive or an 'entrypoint' specifying a container image")
			}
		}
	}

	var iconSVGStr string
	if iconIsSVG && len(iconData) > 0 {
		iconSVGStr = string(iconData)
	}
	iconDataURL := encodeIconDataURL(iconMime, iconData)

	return &ExtractedInspection{
		Manifest:          manifest,
		SHA256:            sha256Hex,
		HasSignature:      hasSig,
		SignatureVerified: sigVerified,
		SignerKeyID:       signerKeyID,
		SignerName:        signerName,
		ArchiveType:       archiveType,
		ArchiveFormat:     string(archiveType),
		SizeBytes:         int64(len(data)),
		ReadmeMarkdown:    string(readmeData),
		IconSVG:           iconSVGStr,
		IconURL:           iconDataURL,
		NeedsCompilation:  needsCompilation,
		SourceDetected:    sourceDetected,
		SourceFiles:       sourceFiles,
	}, nil
}

// ExtractArchive safely unpacks the archive into targetDir
func ExtractArchive(data []byte, archiveType ArchiveType, targetDir string) error {
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("failed to create plugin target dir: %w", err)
	}

	cleanTargetDir := filepath.Clean(targetDir)
	var fileCount int
	var totalSize int64

	switch archiveType {
	case ArchiveTypeZip:
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return err
		}
		for _, f := range zr.File {
			fileCount++
			if fileCount > MaxArchiveFileCount {
				return errors.New("exceeded max archive file count")
			}
			totalSize += int64(f.UncompressedSize64)
			if totalSize > MaxExtractedSizeBytes {
				return errors.New("exceeded max uncompressed size")
			}

			normName := stripTopLevelDir(filepath.ToSlash(f.Name))
			if normName == "" || strings.HasSuffix(normName, "/") {
				continue
			}

			destPath := filepath.Join(cleanTargetDir, filepath.FromSlash(normName))
			cleanDest := filepath.Clean(destPath)
			if !strings.HasPrefix(cleanDest, cleanTargetDir+string(filepath.Separator)) && cleanDest != cleanTargetDir {
				return fmt.Errorf("zip slip detected: path %s traverses outside destination directory", f.Name)
			}

			if f.FileInfo().IsDir() {
				if err := os.MkdirAll(cleanDest, 0755); err != nil {
					return err
				}
				continue
			}

			if err := os.MkdirAll(filepath.Dir(cleanDest), 0755); err != nil {
				return err
			}

			rc, err := f.Open()
			if err != nil {
				return err
			}

			out, err := os.OpenFile(cleanDest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
			if err != nil {
				rc.Close()
				return err
			}

			_, copyErr := io.Copy(out, io.LimitReader(rc, MaxExtractedSizeBytes))
			out.Close()
			rc.Close()
			if copyErr != nil {
				return copyErr
			}
		}

	case ArchiveTypeTarGz:
		gzr, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return err
		}
		defer gzr.Close()
		tr := tar.NewReader(gzr)

		for {
			header, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}

			fileCount++
			if fileCount > MaxArchiveFileCount {
				return errors.New("exceeded max archive file count")
			}
			totalSize += header.Size
			if totalSize > MaxExtractedSizeBytes {
				return errors.New("exceeded max uncompressed size")
			}

			normName := stripTopLevelDir(filepath.ToSlash(header.Name))
			if normName == "" {
				continue
			}

			destPath := filepath.Join(cleanTargetDir, filepath.FromSlash(normName))
			cleanDest := filepath.Clean(destPath)
			if !strings.HasPrefix(cleanDest, cleanTargetDir+string(filepath.Separator)) && cleanDest != cleanTargetDir {
				return fmt.Errorf("zip slip detected: path %s traverses outside destination directory", header.Name)
			}

			switch header.Typeflag {
			case tar.TypeDir:
				if err := os.MkdirAll(cleanDest, 0755); err != nil {
					return err
				}
			case tar.TypeReg, tar.TypeRegA:
				if err := os.MkdirAll(filepath.Dir(cleanDest), 0755); err != nil {
					return err
				}
				out, err := os.OpenFile(cleanDest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
				if err != nil {
					return err
				}
				_, copyErr := io.Copy(out, io.LimitReader(tr, MaxExtractedSizeBytes))
				out.Close()
				if copyErr != nil {
					return copyErr
				}
			}
		}
	}

	return nil
}

// stripTopLevelDir cleans initial components if all files are inside a root folder like "my-plugin-1.0.0/plugin.json"
func stripTopLevelDir(name string) string {
	parts := strings.Split(filepath.ToSlash(name), "/")
	if len(parts) > 1 {
		// If first part is a directory wrapper and second part is plugin.json or frontend or backend, strip wrapper
		if parts[1] == "plugin.json" || parts[1] == "frontend" || parts[1] == "backend" || parts[1] == "README.md" || parts[1] == "SIGNATURE.sig" {
			return strings.Join(parts[1:], "/")
		}
	}
	return name
}

// verifySignature attempts to verify signature against trusted Ed25519 public keys
func verifySignature(sha256Hex string, signatureBytes []byte, trustedKeys []models.PluginTrustedKey) (bool, string, string) {
	sigStr := strings.TrimSpace(string(signatureBytes))
	decodedSig, err := base64.StdEncoding.DecodeString(sigStr)
	if err != nil || len(decodedSig) != ed25519.SignatureSize {
		return false, "", ""
	}

	msg := []byte(sha256Hex)

	for _, k := range trustedKeys {
		pubBytes, err := base64.StdEncoding.DecodeString(strings.TrimSpace(k.PublicKeyEd25519))
		if err != nil || len(pubBytes) != ed25519.PublicKeySize {
			continue
		}
		if ed25519.Verify(pubBytes, msg, decodedSig) {
			return true, k.Fingerprint, k.Name
		}
	}

	return false, "", ""
}
