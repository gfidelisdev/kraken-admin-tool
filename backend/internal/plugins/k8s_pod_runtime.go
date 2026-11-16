package plugins

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"

	"github.com/kraken/admin-tool/backend/internal/docker"
	"github.com/kraken/admin-tool/backend/internal/models"
	"github.com/kraken/admin-tool/backend/internal/plugins/sidecarauth"
)

const (
	k8sPluginManagedByLabel = "kraken-plugin-runtime"
	maxK8sSecretTarBytes    = 950 * 1024 // Stay safely below Kubernetes' 1 MiB Secret ceiling
)

// DockerfileCopyOp represents a COPY or ADD instruction extracted from a plugin Dockerfile.
type DockerfileCopyOp struct {
	FromStage string
	Src       string
	Dst       string
}

// DockerfileStage represents a single build or runtime stage inside a plugin Dockerfile.
type DockerfileStage struct {
	BaseImage  string
	Alias      string
	WorkDir    string
	Env        map[string]string
	EnvOrder   []string
	RunCmds    []string
	CopyOps    []DockerfileCopyOp
	ExposePort int
	Entrypoint []string
	EntryShell bool
	Cmd        []string
	CmdShell   bool
}

// DockerfilePodSpec captures the parsed Dockerfile structure needed to launch a plugin as a Kubernetes Pod.
type DockerfilePodSpec struct {
	DockerfileRelPath string
	ContextDir        string
	Stages            []DockerfileStage
}

func sanitizeK8sPluginName(pluginID string) string {
	sanitized := sanitizeContainerName(pluginID)
	if len(sanitized) > 45 {
		sanitized = strings.Trim(sanitized[:45], "-")
	}
	if sanitized == "" {
		return "plugin"
	}
	return sanitized
}

func k8sPluginResourceName(pluginID string) string {
	return "kraken-plugin-" + sanitizeK8sPluginName(pluginID)
}

func k8sPluginContextSecretName(pluginID string) string {
	return "kraken-plugin-ctx-" + sanitizeK8sPluginName(pluginID)
}

func sanitizeK8sLabelValue(v string) string {
	var sb strings.Builder
	for _, r := range v {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			sb.WriteRune(r)
		} else {
			sb.WriteRune('-')
		}
	}
	res := sb.String()
	if len(res) > 63 {
		res = res[:63]
	}
	res = strings.TrimFunc(res, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'))
	})
	if res == "" {
		return "unknown"
	}
	return res
}

func shellQuoteArg(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func shellQuoteArgs(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, a := range args {
		quoted = append(quoted, shellQuoteArg(a))
	}
	return strings.Join(quoted, " ")
}

// resolvePluginDockerfile locates the Dockerfile and its effective build context directory.
func resolvePluginDockerfile(targetDir string, manifest *models.PluginManifest) (string, string, string, error) {
	if strings.TrimSpace(targetDir) == "" {
		return "", "", "", errors.New("plugin install path is empty")
	}

	var dockerfileRelPath string
	if manifest != nil && manifest.Runtime.Dockerfile != "" {
		dfCandidate := filepath.Join(targetDir, filepath.FromSlash(manifest.Runtime.Dockerfile))
		if _, err := os.Stat(dfCandidate); err == nil {
			dockerfileRelPath = manifest.Runtime.Dockerfile
		}
	}

	if dockerfileRelPath == "" {
		candidates := []string{
			"Dockerfile",
			"dockerfile",
			"backend/Dockerfile",
			"backend/dockerfile",
		}
		for _, cand := range candidates {
			p := filepath.Join(targetDir, filepath.FromSlash(cand))
			if _, err := os.Stat(p); err == nil {
				dockerfileRelPath = cand
				break
			}
		}
	}

	if dockerfileRelPath == "" {
		_ = filepath.Walk(targetDir, func(p string, fi os.FileInfo, err error) error {
			if err == nil && !fi.IsDir() && strings.EqualFold(fi.Name(), "dockerfile") {
				rel, relErr := filepath.Rel(targetDir, p)
				if relErr == nil {
					dockerfileRelPath = filepath.ToSlash(rel)
					return filepath.SkipAll
				}
			}
			return nil
		})
	}

	if dockerfileRelPath == "" {
		return "", "", "", fmt.Errorf("no Dockerfile found in plugin directory '%s'", targetDir)
	}

	fullDockerfilePath := filepath.Join(targetDir, filepath.FromSlash(dockerfileRelPath))
	contextDir := targetDir

	if dfRaw, readErr := os.ReadFile(fullDockerfilePath); readErr == nil {
		patched := sanitizeDockerfileContent(dfRaw)
		if !bytes.Equal(patched, dfRaw) {
			_ = os.WriteFile(fullDockerfilePath, patched, 0644)
			dfRaw = patched
		}
		if strings.Contains(dockerfileRelPath, "/") {
			dfText := string(dfRaw)
			subDir := filepath.Dir(dockerfileRelPath)
			if !strings.Contains(dfText, "COPY "+subDir) && !strings.Contains(dfText, "ADD "+subDir) {
				contextDir = filepath.Join(targetDir, filepath.FromSlash(subDir))
			}
		}
	}

	return dockerfileRelPath, fullDockerfilePath, contextDir, nil
}

// ParsePluginDockerfile parses a plugin's Dockerfile into stages suitable for Kubernetes Pod execution.
func ParsePluginDockerfile(targetDir string, manifest *models.PluginManifest) (*DockerfilePodSpec, error) {
	relDF, fullDF, contextDir, err := resolvePluginDockerfile(targetDir, manifest)
	if err != nil {
		return nil, err
	}

	raw, err := os.ReadFile(fullDF)
	if err != nil {
		return nil, fmt.Errorf("failed to read Dockerfile '%s': %w", fullDF, err)
	}

	lines := collapseDockerfileContinuations(string(raw))
	var stages []DockerfileStage
	var current *DockerfileStage

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		keyword, rest := splitDockerfileInstruction(trimmed)
		switch strings.ToUpper(keyword) {
		case "FROM":
			fields := strings.Fields(rest)
			if len(fields) == 0 {
				continue
			}
			baseImg := fields[0]
			alias := ""
			for i := 1; i+1 < len(fields); i++ {
				if strings.EqualFold(fields[i], "AS") {
					alias = fields[i+1]
					break
				}
			}
			stages = append(stages, DockerfileStage{
				BaseImage: baseImg,
				Alias:     alias,
				Env:       make(map[string]string),
			})
			current = &stages[len(stages)-1]

		case "WORKDIR":
			if current != nil && rest != "" {
				current.WorkDir = strings.TrimSpace(rest)
			}

		case "ENV":
			if current != nil && rest != "" {
				parseDockerfileEnv(rest, current)
			}

		case "RUN":
			if current != nil && rest != "" {
				current.RunCmds = append(current.RunCmds, rest)
			}

		case "COPY", "ADD":
			if current != nil && rest != "" {
				if op, ok := parseDockerfileCopy(rest); ok {
					current.CopyOps = append(current.CopyOps, op)
				}
			}

		case "EXPOSE":
			if current != nil && rest != "" {
				for _, tok := range strings.Fields(rest) {
					portPart := strings.Split(tok, "/")[0]
					if p, convErr := strconv.Atoi(portPart); convErr == nil && p > 0 && p <= 65535 {
						current.ExposePort = p
						break
					}
				}
			}

		case "ENTRYPOINT":
			if current != nil && rest != "" {
				args, isExec := parseDockerfileExecOrShell(rest)
				current.Entrypoint = args
				current.EntryShell = !isExec
			}

		case "CMD":
			if current != nil && rest != "" {
				args, isExec := parseDockerfileExecOrShell(rest)
				current.Cmd = args
				current.CmdShell = !isExec
			}
		}
	}

	if len(stages) == 0 {
		return nil, fmt.Errorf("dockerfile '%s' contains no FROM instruction", fullDF)
	}

	return &DockerfilePodSpec{
		DockerfileRelPath: relDF,
		ContextDir:        contextDir,
		Stages:            stages,
	}, nil
}

func collapseDockerfileContinuations(raw string) []string {
	var out []string
	var buf strings.Builder
	scanner := bufio.NewScanner(strings.NewReader(raw))
	for scanner.Scan() {
		line := scanner.Text()
		trimmedRight := strings.TrimRight(line, " \t\r")
		trimmed := strings.TrimSpace(trimmedRight)
		if buf.Len() == 0 && (trimmed == "" || strings.HasPrefix(trimmed, "#")) {
			continue
		}
		if strings.HasSuffix(trimmedRight, "\\") {
			part := strings.TrimSuffix(trimmedRight, "\\")
			buf.WriteString(strings.TrimSpace(part))
			buf.WriteByte(' ')
			continue
		}
		if buf.Len() > 0 {
			buf.WriteString(strings.TrimSpace(trimmedRight))
			out = append(out, buf.String())
			buf.Reset()
		} else {
			out = append(out, trimmed)
		}
	}
	if buf.Len() > 0 {
		out = append(out, strings.TrimSpace(buf.String()))
	}
	return out
}

func splitDockerfileInstruction(line string) (string, string) {
	for i, r := range line {
		if r == ' ' || r == '\t' {
			return line[:i], strings.TrimSpace(line[i+1:])
		}
	}
	return line, ""
}

func parseDockerfileEnv(rest string, stage *DockerfileStage) {
	if !strings.Contains(rest, "=") {
		fields := strings.Fields(rest)
		if len(fields) >= 2 {
			k := fields[0]
			v := strings.TrimSpace(strings.TrimPrefix(rest, k))
			v = trimMatchingQuotes(v)
			if _, exists := stage.Env[k]; !exists {
				stage.EnvOrder = append(stage.EnvOrder, k)
			}
			stage.Env[k] = v
		}
		return
	}

	tokens := splitRespectingQuotes(rest)
	for _, tok := range tokens {
		idx := strings.IndexByte(tok, '=')
		if idx <= 0 {
			continue
		}
		k := strings.TrimSpace(tok[:idx])
		v := trimMatchingQuotes(strings.TrimSpace(tok[idx+1:]))
		if _, exists := stage.Env[k]; !exists {
			stage.EnvOrder = append(stage.EnvOrder, k)
		}
		stage.Env[k] = v
	}
}

func parseDockerfileCopy(rest string) (DockerfileCopyOp, bool) {
	tokens := splitRespectingQuotes(rest)
	var fromStage string
	var paths []string
	for _, tok := range tokens {
		if strings.HasPrefix(tok, "--from=") {
			fromStage = strings.TrimPrefix(tok, "--from=")
			continue
		}
		if strings.HasPrefix(tok, "--") {
			continue
		}
		paths = append(paths, trimMatchingQuotes(tok))
	}
	if len(paths) < 2 {
		return DockerfileCopyOp{}, false
	}
	return DockerfileCopyOp{
		FromStage: fromStage,
		Src:       paths[0],
		Dst:       paths[len(paths)-1],
	}, true
}

func parseDockerfileExecOrShell(rest string) ([]string, bool) {
	trimmed := strings.TrimSpace(rest)
	if strings.HasPrefix(trimmed, "[") {
		var arr []string
		if err := json.Unmarshal([]byte(trimmed), &arr); err == nil && len(arr) > 0 {
			return arr, true
		}
	}
	return []string{trimmed}, false
}

func splitRespectingQuotes(s string) []string {
	var tokens []string
	var cur strings.Builder
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			} else {
				cur.WriteByte(c)
			}
			continue
		}
		if c == '"' || c == '\'' {
			quote = c
			continue
		}
		if c == ' ' || c == '\t' {
			if cur.Len() > 0 {
				tokens = append(tokens, cur.String())
				cur.Reset()
			}
			continue
		}
		cur.WriteByte(c)
	}
	if cur.Len() > 0 {
		tokens = append(tokens, cur.String())
	}
	return tokens
}

func trimMatchingQuotes(s string) string {
	if len(s) >= 2 && ((s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'')) {
		return s[1 : len(s)-1]
	}
	return s
}

func targzDirectoryToBuffer(srcDir string) (*bytes.Buffer, error) {
	buf := new(bytes.Buffer)
	gzw := gzip.NewWriter(buf)
	tw := tar.NewWriter(gzw)

	err := filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() && !info.Mode().IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		if relPath == "." {
			return nil
		}

		if strings.HasPrefix(relPath, ".git") || strings.HasPrefix(relPath, ".svn") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		header, err := tar.FileInfoHeader(info, info.Name())
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(relPath)

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		if info.Mode().IsRegular() {
			f, openErr := os.Open(path)
			if openErr != nil {
				return openErr
			}
			_, copyErr := io.Copy(tw, f)
			_ = f.Close()
			if copyErr != nil {
				return copyErr
			}
		}
		return nil
	})

	if err != nil {
		_ = tw.Close()
		_ = gzw.Close()
		return nil, fmt.Errorf("failed to pack tar.gz context from '%s': %w", srcDir, err)
	}
	if err := tw.Close(); err != nil {
		_ = gzw.Close()
		return nil, err
	}
	if err := gzw.Close(); err != nil {
		return nil, err
	}
	return buf, nil
}

// PrepareKubernetesPluginBuild builds a plugin container via Docker/containerd when available, or validates
// and prepares the plugin Dockerfile for transparent in-cluster Kubernetes Pod execution without requiring dockerd.
func PrepareKubernetesPluginBuild(ctx context.Context, dockerService *docker.DockerService, targetDir string, manifest *models.PluginManifest) (*BuildResult, error) {
	start := time.Now()

	// If local Docker daemon is reachable, attempt normal image build + containerd import first
	if dockerService != nil {
		if cli, err := dockerService.GetSystemClient(); err == nil && cli != nil {
			buildRes, buildErr := BuildContainerPluginImage(ctx, dockerService, targetDir, manifest)
			if buildErr == nil {
				if ctrBin, ctrErr := exec.LookPath("ctr"); ctrErr == nil {
					if rc, saveErr := cli.ImageSave(ctx, []string{buildRes.Entrypoint}); saveErr == nil {
						importCmd := exec.CommandContext(ctx, ctrBin, "-n", "k8s.io", "images", "import", "-")
						importCmd.Stdin = rc
						var out bytes.Buffer
						importCmd.Stdout = &out
						importCmd.Stderr = &out
						if impErr := importCmd.Run(); impErr != nil {
							log.Printf("[K8S-PLUGIN] Notice: containerd import skipped (%v); Pod runtime will use Dockerfile conversion if needed.", impErr)
						} else {
							log.Printf("[K8S-PLUGIN] Imported built image '%s' into containerd k8s.io namespace.", buildRes.Entrypoint)
						}
						_ = rc.Close()
					}
				}
				return buildRes, nil
			}
			log.Printf("[K8S-PLUGIN] Local Docker build skipped/failed (%v); validating Dockerfile for Pod conversion.", buildErr)
		}
	}

	spec, err := ParsePluginDockerfile(targetDir, manifest)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare plugin for Kubernetes Pod runtime: %w", err)
	}

	if manifest != nil {
		if strings.TrimSpace(manifest.Runtime.Dockerfile) == "" {
			manifest.Runtime.Dockerfile = spec.DockerfileRelPath
		}
		if strings.TrimSpace(manifest.Runtime.Entrypoint) == "" {
			manifest.Runtime.Entrypoint = fmt.Sprintf("kraken-plugin-%s:%s", sanitizeContainerName(manifest.ID), manifest.Version)
		}
		if manifest.Runtime.Port <= 0 {
			finalStage := spec.Stages[len(spec.Stages)-1]
			if finalStage.ExposePort > 0 {
				manifest.Runtime.Port = finalStage.ExposePort
			} else {
				manifest.Runtime.Port = 8080
			}
		}
	}

	var totalSize int64
	_ = filepath.Walk(spec.ContextDir, func(_ string, info os.FileInfo, walkErr error) error {
		if walkErr == nil && info != nil && info.Mode().IsRegular() {
			totalSize += info.Size()
		}
		return nil
	})

	duration := time.Since(start)
	entry := ""
	if manifest != nil {
		entry = manifest.Runtime.Entrypoint
	}
	log.Printf("[K8S-PLUGIN] Prepared plugin '%s' for Kubernetes Pod conversion from Dockerfile '%s' in %v (context: %d bytes)",
		manifest.ID, spec.DockerfileRelPath, duration, totalSize)

	return &BuildResult{
		Compiler:   "k8s-pod-converter",
		Entrypoint: entry,
		SizeBytes:  totalSize,
		DurationMs: duration.Milliseconds(),
	}, nil
}

func (m *ContainerRuntimeManager) startKubernetesPod(ctx context.Context, plugin *models.InstalledPlugin, manifest *models.PluginManifest) (string, error) {
	cli, ns := m.getK8sClient()
	if cli == nil {
		return "", errors.New("kubernetes client unavailable")
	}

	imageName := strings.TrimSpace(manifest.Runtime.Entrypoint)
	if imageName == "" {
		return "", errors.New("container entrypoint (image) cannot be empty")
	}

	podName := k8sPluginResourceName(plugin.PluginID)
	internalPort := manifest.Runtime.Port
	if internalPort <= 0 {
		internalPort = 8080
	}

	// Ensure cryptographically secure installation ID and instance secret exist
	if plugin.InstallationID == "" {
		plugin.InstallationID = sidecarauth.GenerateInstallationID()
	}
	if plugin.InstanceSecret == "" {
		sec, secErr := sidecarauth.GenerateInstanceSecret()
		if secErr == nil {
			plugin.InstanceSecret = sec
		}
	}

	// 1. Check if Pod already exists and is running
	existing, err := cli.CoreV1().Pods(ns).Get(ctx, podName, metav1.GetOptions{})
	if err == nil {
		if existing.DeletionTimestamp == nil && existing.Status.Phase == corev1.PodRunning {
			if svcErr := m.ensureKubernetesPluginService(ctx, cli, ns, plugin, podName, internalPort); svcErr != nil {
				return "", svcErr
			}
			return podName, nil
		}
		// Delete stale/failed/terminating pod and wait briefly for removal
		grace := int64(0)
		_ = cli.CoreV1().Pods(ns).Delete(ctx, podName, metav1.DeleteOptions{GracePeriodSeconds: &grace})
		waitDelDeadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(waitDelDeadline) {
			if _, getErr := cli.CoreV1().Pods(ns).Get(ctx, podName, metav1.GetOptions{}); apierrors.IsNotFound(getErr) {
				break
			}
			time.Sleep(250 * time.Millisecond)
		}
	}

	labels := map[string]string{
		"app.kubernetes.io/managed-by":      k8sPluginManagedByLabel,
		"com.kraken.plugin.name":            podName,
		"com.kraken.plugin.id":              sanitizeK8sLabelValue(plugin.PluginID),
		"com.kraken.plugin.installation_id": sanitizeK8sLabelValue(plugin.InstallationID),
		"com.kraken.plugin.version":         sanitizeK8sLabelValue(plugin.Version),
	}

	// 2. Determine whether to run a pre-built/registry image or convert a plugin Dockerfile into a Pod
	var pod *corev1.Pod
	useDockerfileConversion := false
	if plugin.InstallPath != "" && strings.HasPrefix(imageName, "kraken-plugin-") {
		if hasDockerfile(plugin.InstallPath) || hasDockerfile(filepath.Join(plugin.InstallPath, "backend")) || manifest.Runtime.Dockerfile != "" {
			useDockerfileConversion = true
		}
	}

	if useDockerfileConversion {
		spec, parseErr := ParsePluginDockerfile(plugin.InstallPath, manifest)
		if parseErr != nil {
			return "", fmt.Errorf("failed to parse plugin Dockerfile for Kubernetes Pod conversion: %w", parseErr)
		}
		pod, err = m.buildConvertedDockerfilePod(ctx, cli, ns, plugin, manifest, spec, podName, internalPort, labels)
		if err != nil {
			return "", err
		}
	} else {
		pod = m.buildImagePod(plugin, podName, imageName, internalPort, labels)
	}

	createdPod, createErr := cli.CoreV1().Pods(ns).Create(ctx, pod, metav1.CreateOptions{})
	if createErr != nil {
		return "", fmt.Errorf("failed to create plugin pod '%s/%s': %w", ns, podName, createErr)
	}

	if svcErr := m.ensureKubernetesPluginService(ctx, cli, ns, plugin, podName, internalPort); svcErr != nil {
		return "", svcErr
	}

	// 3. Optionally wait briefly for Pod to become Running on real clusters
	if m.podReadyWaitTimeout > 0 && createdPod.Status.Phase != corev1.PodRunning {
		waitCtx, cancel := context.WithTimeout(ctx, m.podReadyWaitTimeout)
		defer cancel()
		for {
			cur, getErr := cli.CoreV1().Pods(ns).Get(waitCtx, podName, metav1.GetOptions{})
			if getErr == nil {
				if cur.Status.Phase == corev1.PodRunning {
					break
				}
				if cur.Status.Phase == corev1.PodFailed {
					return podName, fmt.Errorf("plugin pod '%s/%s' entered Failed phase: %s", ns, podName, cur.Status.Reason)
				}
			}
			select {
			case <-waitCtx.Done():
				return podName, nil
			case <-time.After(500 * time.Millisecond):
			}
		}
	}

	return podName, nil
}

func (m *ContainerRuntimeManager) buildImagePod(
	plugin *models.InstalledPlugin,
	podName string,
	imageName string,
	internalPort int,
	labels map[string]string,
) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:   podName,
			Labels: labels,
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyAlways,
			Containers: []corev1.Container{
				{
					Name:            "plugin",
					Image:           imageName,
					ImagePullPolicy: corev1.PullIfNotPresent,
					Ports: []corev1.ContainerPort{
						{
							Name:          "http",
							ContainerPort: int32(internalPort),
							Protocol:      corev1.ProtocolTCP,
						},
					},
					Env: []corev1.EnvVar{
						{Name: "KRAKEN_PLUGIN_ID", Value: plugin.PluginID},
						{Name: "KRAKEN_PLUGIN_INSTALLATION_ID", Value: plugin.InstallationID},
						{Name: "KRAKEN_PLUGIN_INSTANCE_SECRET", Value: plugin.InstanceSecret},
						{Name: "PORT", Value: strconv.Itoa(internalPort)},
					},
				},
			},
		},
	}
}

func (m *ContainerRuntimeManager) buildConvertedDockerfilePod(
	ctx context.Context,
	cli kubernetes.Interface,
	ns string,
	plugin *models.InstalledPlugin,
	manifest *models.PluginManifest,
	spec *DockerfilePodSpec,
	podName string,
	internalPort int,
	labels map[string]string,
) (*corev1.Pod, error) {
	tarBuf, err := targzDirectoryToBuffer(spec.ContextDir)
	if err != nil {
		return nil, err
	}
	if tarBuf.Len() > maxK8sSecretTarBytes {
		return nil, fmt.Errorf("plugin Dockerfile context (%d bytes) exceeds Kubernetes Secret transport limit (%d bytes); publish a container image in manifest.runtime.entrypoint instead", tarBuf.Len(), maxK8sSecretTarBytes)
	}

	secretName := k8sPluginContextSecretName(plugin.PluginID)
	secretObj := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:   secretName,
			Labels: labels,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"context.tar.gz": tarBuf.Bytes(),
		},
	}
	if _, getErr := cli.CoreV1().Secrets(ns).Get(ctx, secretName, metav1.GetOptions{}); getErr == nil {
		if _, updErr := cli.CoreV1().Secrets(ns).Update(ctx, secretObj, metav1.UpdateOptions{}); updErr != nil {
			return nil, fmt.Errorf("failed to update plugin context secret '%s/%s': %w", ns, secretName, updErr)
		}
	} else {
		if _, crtErr := cli.CoreV1().Secrets(ns).Create(ctx, secretObj, metav1.CreateOptions{}); crtErr != nil {
			return nil, fmt.Errorf("failed to create plugin context secret '%s/%s': %w", ns, secretName, crtErr)
		}
	}

	volumes := []corev1.Volume{
		{
			Name: "kraken-ctx",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: secretName,
				},
			},
		},
	}

	var initContainers []corev1.Container
	finalStage := spec.Stages[len(spec.Stages)-1]

	// Collect any cross-stage COPY --from artifacts needed by the final stage
	type stageArtifact struct {
		artifactID string
		fromStage  string
		srcPath    string
		dstPath    string
	}
	var artifacts []stageArtifact
	for idx, op := range finalStage.CopyOps {
		if op.FromStage != "" {
			artifacts = append(artifacts, stageArtifact{
				artifactID: fmt.Sprintf("artifact-%d", idx),
				fromStage:  op.FromStage,
				srcPath:    op.Src,
				dstPath:    op.Dst,
			})
		}
	}

	if len(spec.Stages) > 1 {
		volumes = append(volumes, corev1.Volume{
			Name: "kraken-stage",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})

		for sIdx := 0; sIdx < len(spec.Stages)-1; sIdx++ {
			st := spec.Stages[sIdx]
			workDir := st.WorkDir
			if workDir == "" {
				workDir = "/kraken-build"
			}

			var sb strings.Builder
			sb.WriteString("set -e\n")
			sb.WriteString("mkdir -p /kraken-src ")
			sb.WriteString(shellQuoteArg(workDir))
			sb.WriteString(" /kraken-stage\n")
			sb.WriteString("tar -xzf /kraken-ctx/context.tar.gz -C /kraken-src\n")
			sb.WriteString("cd ")
			sb.WriteString(shellQuoteArg(workDir))
			sb.WriteString("\n")
			appendCopyCommands(&sb, st.CopyOps, workDir, nil)
			for _, runCmd := range st.RunCmds {
				sb.WriteString(runCmd)
				sb.WriteString("\n")
			}

			// Export artifacts requested by the final stage from this builder stage
			for _, art := range artifacts {
				if art.fromStage == st.Alias || art.fromStage == strconv.Itoa(sIdx) {
					src := art.srcPath
					if !strings.HasPrefix(src, "/") {
						src = filepath.ToSlash(filepath.Join(workDir, src))
					}
					sb.WriteString(fmt.Sprintf("cp -a %s /kraken-stage/%s\n", shellQuoteArg(src), art.artifactID))
				}
			}

			initContainers = append(initContainers, corev1.Container{
				Name:            fmt.Sprintf("stage-%d", sIdx),
				Image:           st.BaseImage,
				ImagePullPolicy: corev1.PullIfNotPresent,
				Command:         []string{"sh", "-c", sb.String()},
				Env:             buildStageEnvVars(st.EnvOrder, st.Env),
				VolumeMounts: []corev1.VolumeMount{
					{Name: "kraken-ctx", MountPath: "/kraken-ctx", ReadOnly: true},
					{Name: "kraken-stage", MountPath: "/kraken-stage"},
				},
			})
		}
	}

	runtimeImage := finalStage.BaseImage
	if strings.EqualFold(runtimeImage, "scratch") || runtimeImage == "" {
		runtimeImage = "alpine:3.21"
	}

	finalWorkDir := finalStage.WorkDir
	if finalWorkDir == "" {
		finalWorkDir = "/app"
	}

	var mainScript strings.Builder
	mainScript.WriteString("set -e\n")
	mainScript.WriteString("mkdir -p /kraken-src ")
	mainScript.WriteString(shellQuoteArg(finalWorkDir))
	mainScript.WriteString("\n")
	mainScript.WriteString("tar -xzf /kraken-ctx/context.tar.gz -C /kraken-src\n")
	mainScript.WriteString("cd ")
	mainScript.WriteString(shellQuoteArg(finalWorkDir))
	mainScript.WriteString("\n")

	artifactMap := make(map[int]string)
	for idx, op := range finalStage.CopyOps {
		if op.FromStage != "" {
			artifactMap[idx] = fmt.Sprintf("/kraken-stage/artifact-%d", idx)
		}
	}
	appendCopyCommands(&mainScript, finalStage.CopyOps, finalWorkDir, artifactMap)

	for _, runCmd := range finalStage.RunCmds {
		mainScript.WriteString(runCmd)
		mainScript.WriteString("\n")
	}

	execLine := buildFinalExecCommand(finalStage)
	if execLine == "" {
		return nil, fmt.Errorf("dockerfile '%s' must define ENTRYPOINT or CMD for Kubernetes Pod execution", spec.DockerfileRelPath)
	}
	mainScript.WriteString(execLine)
	mainScript.WriteString("\n")

	mainMounts := []corev1.VolumeMount{
		{Name: "kraken-ctx", MountPath: "/kraken-ctx", ReadOnly: true},
	}
	if len(spec.Stages) > 1 {
		mainMounts = append(mainMounts, corev1.VolumeMount{Name: "kraken-stage", MountPath: "/kraken-stage"})
	}

	envVars := buildStageEnvVars(finalStage.EnvOrder, finalStage.Env)
	envVars = upsertEnvVar(envVars, "KRAKEN_PLUGIN_ID", plugin.PluginID)
	envVars = upsertEnvVar(envVars, "KRAKEN_PLUGIN_INSTALLATION_ID", plugin.InstallationID)
	envVars = upsertEnvVar(envVars, "KRAKEN_PLUGIN_INSTANCE_SECRET", plugin.InstanceSecret)
	envVars = upsertEnvVar(envVars, "PORT", strconv.Itoa(internalPort))

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:   podName,
			Labels: labels,
		},
		Spec: corev1.PodSpec{
			RestartPolicy:  corev1.RestartPolicyAlways,
			InitContainers: initContainers,
			Containers: []corev1.Container{
				{
					Name:            "plugin",
					Image:           runtimeImage,
					ImagePullPolicy: corev1.PullIfNotPresent,
					Command:         []string{"sh", "-c", mainScript.String()},
					Ports: []corev1.ContainerPort{
						{
							Name:          "http",
							ContainerPort: int32(internalPort),
							Protocol:      corev1.ProtocolTCP,
						},
					},
					Env:          envVars,
					VolumeMounts: mainMounts,
				},
			},
			Volumes: volumes,
		},
	}, nil
}

func appendCopyCommands(sb *strings.Builder, ops []DockerfileCopyOp, workDir string, artifactMap map[int]string) {
	for idx, op := range ops {
		dst := op.Dst
		if dst == "." || dst == "./" {
			dst = workDir
		} else if !strings.HasPrefix(dst, "/") {
			dst = filepath.ToSlash(filepath.Join(workDir, dst))
		}

		if artPath, isFrom := artifactMap[idx]; isFrom {
			if strings.HasSuffix(op.Dst, "/") {
				sb.WriteString(fmt.Sprintf("mkdir -p %s && cp -a %s %s\n", shellQuoteArg(dst), shellQuoteArg(artPath), shellQuoteArg(dst)))
			} else {
				sb.WriteString(fmt.Sprintf("mkdir -p $(dirname %s) && cp -a %s %s\n", shellQuoteArg(dst), shellQuoteArg(artPath), shellQuoteArg(dst)))
			}
			continue
		}

		src := op.Src
		if src == "." || src == "./" {
			sb.WriteString(fmt.Sprintf("mkdir -p %s && cp -a /kraken-src/. %s/\n", shellQuoteArg(dst), shellQuoteArg(strings.TrimSuffix(dst, "/"))))
		} else {
			cleanSrc := strings.TrimPrefix(src, "./")
			sb.WriteString(fmt.Sprintf("mkdir -p $(dirname %s) && cp -a /kraken-src/%s %s\n", shellQuoteArg(dst), shellQuoteArg(cleanSrc), shellQuoteArg(dst)))
		}
	}
}

func buildFinalExecCommand(stage DockerfileStage) string {
	if len(stage.Entrypoint) == 0 && len(stage.Cmd) == 0 {
		return ""
	}
	if len(stage.Entrypoint) > 0 && stage.EntryShell {
		cmdLine := stage.Entrypoint[0]
		if len(stage.Cmd) > 0 {
			cmdLine += " " + strings.Join(stage.Cmd, " ")
		}
		return "exec " + cmdLine
	}
	if len(stage.Entrypoint) == 0 && len(stage.Cmd) > 0 && stage.CmdShell {
		return "exec " + stage.Cmd[0]
	}
	combined := append([]string{}, stage.Entrypoint...)
	combined = append(combined, stage.Cmd...)
	return "exec " + shellQuoteArgs(combined)
}

func buildStageEnvVars(order []string, envMap map[string]string) []corev1.EnvVar {
	out := make([]corev1.EnvVar, 0, len(order))
	for _, k := range order {
		out = append(out, corev1.EnvVar{Name: k, Value: envMap[k]})
	}
	return out
}

func upsertEnvVar(vars []corev1.EnvVar, name, val string) []corev1.EnvVar {
	for i := range vars {
		if vars[i].Name == name {
			vars[i].Value = val
			return vars
		}
	}
	return append(vars, corev1.EnvVar{Name: name, Value: val})
}

func (m *ContainerRuntimeManager) ensureKubernetesPluginService(
	ctx context.Context,
	cli kubernetes.Interface,
	ns string,
	plugin *models.InstalledPlugin,
	podName string,
	internalPort int,
) error {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: podName,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": k8sPluginManagedByLabel,
				"com.kraken.plugin.name":       podName,
				"com.kraken.plugin.id":         sanitizeK8sLabelValue(plugin.PluginID),
			},
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Selector: map[string]string{
				"com.kraken.plugin.name": podName,
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       int32(internalPort),
					TargetPort: intstr.FromInt(internalPort),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}

	existing, err := cli.CoreV1().Services(ns).Get(ctx, podName, metav1.GetOptions{})
	if err == nil {
		svc.ResourceVersion = existing.ResourceVersion
		svc.Spec.ClusterIP = existing.Spec.ClusterIP
		if _, updErr := cli.CoreV1().Services(ns).Update(ctx, svc, metav1.UpdateOptions{}); updErr != nil {
			return fmt.Errorf("failed to update plugin service '%s/%s': %w", ns, podName, updErr)
		}
		return nil
	}

	if _, crtErr := cli.CoreV1().Services(ns).Create(ctx, svc, metav1.CreateOptions{}); crtErr != nil {
		return fmt.Errorf("failed to create plugin service '%s/%s': %w", ns, podName, crtErr)
	}
	return nil
}

func (m *ContainerRuntimeManager) stopKubernetesPod(ctx context.Context, plugin *models.InstalledPlugin) error {
	cli, ns := m.getK8sClient()
	if cli == nil {
		return nil
	}

	podName := k8sPluginResourceName(plugin.PluginID)
	secretName := k8sPluginContextSecretName(plugin.PluginID)
	grace := int64(5)
	delOpts := metav1.DeleteOptions{GracePeriodSeconds: &grace}

	if err := cli.CoreV1().Pods(ns).Delete(ctx, podName, delOpts); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to delete plugin pod '%s/%s': %w", ns, podName, err)
	}
	if err := cli.CoreV1().Services(ns).Delete(ctx, podName, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to delete plugin service '%s/%s': %w", ns, podName, err)
	}
	if err := cli.CoreV1().Secrets(ns).Delete(ctx, secretName, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to delete plugin context secret '%s/%s': %w", ns, secretName, err)
	}

	return nil
}

func (m *ContainerRuntimeManager) resolveKubernetesPluginURLs(
	ctx context.Context,
	plugin *models.InstalledPlugin,
	internalPort int,
	path string,
) ([]string, error) {
	cli, ns := m.getK8sClient()
	if cli == nil {
		return nil, errors.New("kubernetes client unavailable")
	}

	podName := k8sPluginResourceName(plugin.PluginID)
	pod, err := cli.CoreV1().Pods(ns).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("plugin pod '%s/%s' is not running: %w", ns, podName, err)
	}
	if pod.Status.Phase == corev1.PodFailed {
		return nil, fmt.Errorf("plugin pod '%s/%s' failed: %s", ns, podName, pod.Status.Reason)
	}

	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	var urls []string
	if pod.Status.PodIP != "" {
		urls = append(urls, fmt.Sprintf("http://%s:%d%s", pod.Status.PodIP, internalPort, path))
	}
	urls = append(urls,
		fmt.Sprintf("http://%s.%s.svc.cluster.local:%d%s", podName, ns, internalPort, path),
		fmt.Sprintf("http://%s:%d%s", podName, internalPort, path),
	)
	return urls, nil
}
