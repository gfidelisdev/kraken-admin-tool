package k8s

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	dockercontainer "github.com/docker/docker/api/types/container"
	"golang.org/x/crypto/ssh"
	"gorm.io/gorm"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/kraken/admin-tool/backend/internal/docker"
	"github.com/kraken/admin-tool/backend/internal/models"
)

// validK8sIdentifier matches RFC 1123 DNS subdomains (lowercase alphanumeric, dashes, dots, max 253 chars)
var validK8sIdentifier = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`)

// ValidateK8sName verifies that a resource name adheres to RFC 1123 DNS subdomain format
func ValidateK8sName(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" || len(trimmed) > 253 || !validK8sIdentifier.MatchString(trimmed) {
		return fmt.Errorf("invalid kubernetes resource name: %q (must be lowercase alphanumeric, dashes, dots, max 253 chars)", name)
	}
	return nil
}

// ValidateK8sNamespace verifies that a namespace is valid or empty/all
func ValidateK8sNamespace(ns string) error {
	trimmed := strings.TrimSpace(ns)
	if trimmed == "" || trimmed == metav1.NamespaceAll || trimmed == "all" {
		return nil
	}
	if len(trimmed) > 63 || !validK8sIdentifier.MatchString(trimmed) {
		return fmt.Errorf("invalid kubernetes namespace: %q (must be lowercase alphanumeric, dashes, dots, max 63 chars)", ns)
	}
	return nil
}

// ValidateContainerName verifies that a container name adheres to Kubernetes conventions
func ValidateContainerName(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return nil
	}
	if len(trimmed) > 63 || !validK8sIdentifier.MatchString(trimmed) {
		return fmt.Errorf("invalid kubernetes container name: %q", name)
	}
	return nil
}

// shellEscape wraps string in single quotes and safely escapes internal single quotes
func shellEscape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// K8sService encapsulates interactions with Kubernetes clusters (both local and remote via SSH / Docker)
type K8sService struct {
	client    kubernetes.Interface
	connected bool
	errMsg    string
	db        *gorm.DB
	docker    *docker.DockerService
	cliCache  sync.Map // serverID (uint) -> remote CLI command (string)
}

// NewK8sService initializes the Kubernetes clientset with InCluster or Kubeconfig fallback
func NewK8sService(kubeconfigPath string) *K8sService {
	var cfg *rest.Config
	var err error

	// 1. Try InCluster config
	cfg, err = rest.InClusterConfig()
	if err == nil {
		log.Printf("[K8S] Loaded InCluster Kubernetes configuration.")
	} else {
		// 2. Try explicit or discovered kubeconfig path
		if kubeconfigPath == "" {
			kubeconfigPath = os.Getenv("KUBECONFIG")
			if kubeconfigPath == "" {
				home, _ := os.UserHomeDir()
				if home != "" {
					kubeconfigPath = filepath.Join(home, ".kube", "config")
				}
			}
		}

		if _, statErr := os.Stat(kubeconfigPath); statErr == nil {
			cfg, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
			if err == nil {
				log.Printf("[K8S] Loaded Kubernetes configuration from %s", kubeconfigPath)
			}
		}
	}

	if err != nil || cfg == nil {
		log.Printf("[K8S] Warning: Kubernetes cluster config not found (%v). Running in degraded mode.", err)
		return &K8sService{
			client:    nil,
			connected: false,
			errMsg:    "Kubernetes cluster config not accessible",
		}
	}

	// Set sane timeout for requests
	cfg.Timeout = 5 * time.Second

	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		log.Printf("[K8S] Failed to instantiate Kubernetes client: %v", err)
		return &K8sService{
			client:    nil,
			connected: false,
			errMsg:    err.Error(),
		}
	}

	// Verify connectivity
	_, err = clientset.Discovery().ServerVersion()
	if err != nil {
		log.Printf("[K8S] Warning: Kubernetes cluster unreachable (%v). Running in degraded mode.", err)
		return &K8sService{
			client:    clientset,
			connected: false,
			errMsg:    err.Error(),
		}
	}

	log.Printf("[K8S] Successfully connected to Kubernetes cluster.")
	return &K8sService{
		client:    clientset,
		connected: true,
	}
}

// SetDB associates the database handle after initialization
func (s *K8sService) SetDB(db *gorm.DB) {
	s.db = db
}

// SetDockerService associates the DockerService for remote SSH tunneling & container inspection
func (s *K8sService) SetDockerService(ds *docker.DockerService) {
	s.docker = ds
}

// InvalidateServerCache clears cached CLI detection for a server
func (s *K8sService) InvalidateServerCache(serverID uint) {
	s.cliCache.Delete(serverID)
}

// IsConnected returns whether the default cluster is connected
func (s *K8sService) IsConnected() bool {
	return s.connected && s.client != nil
}

// GetVersion returns server version string for default cluster
func (s *K8sService) GetVersion(ctx context.Context) string {
	return s.GetVersionOnServer(ctx, 0)
}

// GetCounts returns total pods and deployments for default cluster
func (s *K8sService) GetCounts(ctx context.Context) (int, int) {
	return s.GetCountsOnServer(ctx, 0)
}

// ListPods retrieves pods for default cluster
func (s *K8sService) ListPods(ctx context.Context, namespace string) ([]models.PodSummary, error) {
	return s.ListPodsOnServer(ctx, 0, namespace)
}

// DeletePod deletes a pod by name in default cluster
func (s *K8sService) DeletePod(ctx context.Context, namespace, name string) error {
	return s.DeletePodOnServer(ctx, 0, namespace, name)
}

// ListDeployments retrieves deployments for default cluster
func (s *K8sService) ListDeployments(ctx context.Context, namespace string) ([]models.DeploymentSummary, error) {
	return s.ListDeploymentsOnServer(ctx, 0, namespace)
}

// ScaleDeployment modifies replica count of a deployment in default cluster
func (s *K8sService) ScaleDeployment(ctx context.Context, namespace, name string, replicas int32) error {
	return s.ScaleDeploymentOnServer(ctx, 0, namespace, name, replicas)
}

// RestartDeployment triggers a rollout restart in default cluster
func (s *K8sService) RestartDeployment(ctx context.Context, namespace, name string) error {
	return s.RestartDeploymentOnServer(ctx, 0, namespace, name)
}

// StreamPodLogs opens a log stream for a pod container in default cluster
func (s *K8sService) StreamPodLogs(ctx context.Context, namespace, name, containerName string, follow bool, tailLines int64) (io.ReadCloser, error) {
	return s.StreamPodLogsOnServer(ctx, 0, namespace, name, containerName, follow, tailLines)
}

// --- Multi-Host Methods (*OnServer) Supporting SSH and Docker-Kubernetes ---

// GetVersionOnServer returns the Kubernetes version string for the given serverID
func (s *K8sService) GetVersionOnServer(ctx context.Context, serverID uint) string {
	if serverID == 0 {
		if !s.IsConnected() {
			return "unavailable"
		}
		ver, err := s.client.Discovery().ServerVersion()
		if err != nil {
			return "unknown"
		}
		return ver.GitVersion
	}

	cli, err := s.detectRemoteK8sCLI(ctx, serverID)
	if err == nil && cli != "" {
		out, execErr := s.execRemoteSSH(ctx, serverID, cli+" version -o json 2>/dev/null || "+cli+" version --short 2>/dev/null || "+cli+" --version")
		if execErr == nil && strings.TrimSpace(out) != "" {
			var vData struct {
				ServerVersion struct {
					GitVersion string `json:"gitVersion"`
				} `json:"serverVersion"`
				ClientVersion struct {
					GitVersion string `json:"gitVersion"`
				} `json:"clientVersion"`
				GitVersion string `json:"gitVersion"`
			}
			if json.Unmarshal([]byte(out), &vData) == nil {
				if vData.ServerVersion.GitVersion != "" {
					return vData.ServerVersion.GitVersion
				}
				if vData.ClientVersion.GitVersion != "" {
					return vData.ClientVersion.GitVersion
				}
				if vData.GitVersion != "" {
					return vData.GitVersion
				}
			}
			for _, line := range strings.Split(out, "\n") {
				trimmed := strings.TrimSpace(line)
				if trimmed != "" {
					return trimmed
				}
			}
		}
	}

	// Fallback: check if Docker contains Kubernetes containers on that server
	pods, _ := s.listPodsViaDocker(ctx, serverID, "")
	if len(pods) > 0 {
		return "k8s-in-docker"
	}

	return "unavailable"
}

// GetCountsOnServer returns pod and deployment counts for the given serverID
func (s *K8sService) GetCountsOnServer(ctx context.Context, serverID uint) (int, int) {
	if serverID == 0 {
		if !s.IsConnected() {
			return 0, 0
		}
		pods, err := s.client.CoreV1().Pods(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
		podCount := 0
		if err == nil {
			podCount = len(pods.Items)
		}
		deps, err := s.client.AppsV1().Deployments(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
		depCount := 0
		if err == nil {
			depCount = len(deps.Items)
		}
		return podCount, depCount
	}

	pods, _ := s.ListPodsOnServer(ctx, serverID, metav1.NamespaceAll)
	deps, _ := s.ListDeploymentsOnServer(ctx, serverID, metav1.NamespaceAll)
	return len(pods), len(deps)
}

// ListPodsOnServer retrieves pods for a server and namespace (or all namespaces)
func (s *K8sService) ListPodsOnServer(ctx context.Context, serverID uint, namespace string) ([]models.PodSummary, error) {
	if namespace == "" || namespace == "all" {
		namespace = metav1.NamespaceAll
	} else if err := ValidateK8sNamespace(namespace); err != nil {
		return []models.PodSummary{}, err
	}

	if serverID == 0 {
		if !s.IsConnected() {
			return []models.PodSummary{}, errors.New("kubernetes cluster is currently unavailable")
		}

		podList, err := s.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		return mapPodListToSummary(podList), nil
	}

	// 1. Try remote Kubernetes CLI via SSH
	cli, err := s.detectRemoteK8sCLI(ctx, serverID)
	if err == nil && cli != "" {
		pods, cliErr := s.listPodsViaCLI(ctx, serverID, cli, namespace)
		if cliErr == nil {
			return pods, nil
		}
		log.Printf("[K8S] CLI list pods failed on server %d: %v. Falling back to Docker inspection.", serverID, cliErr)
	}

	// 2. Fallback: Inspect Docker containers with Kubernetes labels
	pods, dockerErr := s.listPodsViaDocker(ctx, serverID, namespace)
	if dockerErr == nil && len(pods) > 0 {
		return pods, nil
	}

	if err != nil {
		return []models.PodSummary{}, fmt.Errorf("kubernetes inaccessible on server %d: %w", serverID, err)
	}
	if dockerErr != nil {
		return []models.PodSummary{}, fmt.Errorf("no kubernetes workloads found: %w", dockerErr)
	}

	return []models.PodSummary{}, nil
}

// DeletePodOnServer deletes a pod on the specified server
func (s *K8sService) DeletePodOnServer(ctx context.Context, serverID uint, namespace, name string) error {
	if namespace == "" {
		namespace = "default"
	}
	if err := ValidateK8sNamespace(namespace); err != nil {
		return err
	}
	if err := ValidateK8sName(name); err != nil {
		return err
	}

	if serverID == 0 {
		if !s.IsConnected() {
			return errors.New("kubernetes cluster is currently unavailable")
		}
		return s.client.CoreV1().Pods(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	}

	// 1. Try remote CLI
	cli, err := s.detectRemoteK8sCLI(ctx, serverID)
	if err == nil && cli != "" {
		cmd := fmt.Sprintf("%s delete pod %s -n %s", cli, shellEscape(name), shellEscape(namespace))
		_, execErr := s.execRemoteSSH(ctx, serverID, cmd)
		if execErr == nil {
			return nil
		}
	}

	// 2. Fallback: Remove matching Docker containers
	return s.deletePodViaDocker(ctx, serverID, namespace, name)
}

// ListDeploymentsOnServer retrieves deployments for a server and namespace
func (s *K8sService) ListDeploymentsOnServer(ctx context.Context, serverID uint, namespace string) ([]models.DeploymentSummary, error) {
	if namespace == "" || namespace == "all" {
		namespace = metav1.NamespaceAll
	} else if err := ValidateK8sNamespace(namespace); err != nil {
		return []models.DeploymentSummary{}, err
	}

	if serverID == 0 {
		if !s.IsConnected() {
			return []models.DeploymentSummary{}, errors.New("kubernetes cluster is currently unavailable")
		}

		depList, err := s.client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		return mapDeploymentListToSummary(depList), nil
	}

	// 1. Try remote CLI
	cli, err := s.detectRemoteK8sCLI(ctx, serverID)
	if err == nil && cli != "" {
		deps, cliErr := s.listDeploymentsViaCLI(ctx, serverID, cli, namespace)
		if cliErr == nil {
			return deps, nil
		}
	}

	return []models.DeploymentSummary{}, nil
}

// ScaleDeploymentOnServer modifies replica count for a deployment on the specified server
func (s *K8sService) ScaleDeploymentOnServer(ctx context.Context, serverID uint, namespace, name string, replicas int32) error {
	if namespace == "" {
		namespace = "default"
	}
	if err := ValidateK8sNamespace(namespace); err != nil {
		return err
	}
	if err := ValidateK8sName(name); err != nil {
		return err
	}
	if replicas < 0 || replicas > 10000 {
		return fmt.Errorf("invalid replicas count: %d (must be between 0 and 10000)", replicas)
	}

	if serverID == 0 {
		if !s.IsConnected() {
			return errors.New("kubernetes cluster is currently unavailable")
		}
		dep, err := s.client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		dep.Spec.Replicas = &replicas
		_, err = s.client.AppsV1().Deployments(namespace).Update(ctx, dep, metav1.UpdateOptions{})
		return err
	}

	cli, err := s.detectRemoteK8sCLI(ctx, serverID)
	if err != nil || cli == "" {
		return fmt.Errorf("kubernetes CLI not available on remote server: %w", err)
	}

	cmd := fmt.Sprintf("%s scale deployment %s --replicas=%d -n %s", cli, shellEscape(name), replicas, shellEscape(namespace))
	_, err = s.execRemoteSSH(ctx, serverID, cmd)
	return err
}

// RestartDeploymentOnServer triggers a rollout restart for a deployment on the specified server
func (s *K8sService) RestartDeploymentOnServer(ctx context.Context, serverID uint, namespace, name string) error {
	if namespace == "" {
		namespace = "default"
	}
	if err := ValidateK8sNamespace(namespace); err != nil {
		return err
	}
	if err := ValidateK8sName(name); err != nil {
		return err
	}

	if serverID == 0 {
		if !s.IsConnected() {
			return errors.New("kubernetes cluster is currently unavailable")
		}
		dep, err := s.client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if dep.Spec.Template.Annotations == nil {
			dep.Spec.Template.Annotations = make(map[string]string)
		}
		dep.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = time.Now().Format(time.RFC3339)
		_, err = s.client.AppsV1().Deployments(namespace).Update(ctx, dep, metav1.UpdateOptions{})
		return err
	}

	cli, err := s.detectRemoteK8sCLI(ctx, serverID)
	if err != nil || cli == "" {
		return fmt.Errorf("kubernetes CLI not available on remote server: %w", err)
	}

	cmd := fmt.Sprintf("%s rollout restart deployment %s -n %s", cli, shellEscape(name), shellEscape(namespace))
	_, err = s.execRemoteSSH(ctx, serverID, cmd)
	return err
}

// StreamPodLogsOnServer opens a log stream for a pod container on the specified server
func (s *K8sService) StreamPodLogsOnServer(ctx context.Context, serverID uint, namespace, name, containerName string, follow bool, tailLines int64) (io.ReadCloser, error) {
	if namespace == "" {
		namespace = "default"
	}
	if err := ValidateK8sNamespace(namespace); err != nil {
		return nil, err
	}
	if err := ValidateK8sName(name); err != nil {
		return nil, err
	}
	if err := ValidateContainerName(containerName); err != nil {
		return nil, err
	}
	if tailLines < 0 || tailLines > 100000 {
		tailLines = 100
	}

	if serverID == 0 {
		if !s.IsConnected() {
			return nil, errors.New("kubernetes cluster is currently unavailable")
		}
		opts := &corev1.PodLogOptions{
			Follow:     follow,
			Timestamps: true,
		}
		if containerName != "" {
			opts.Container = containerName
		}
		if tailLines > 0 {
			opts.TailLines = &tailLines
		}
		req := s.client.CoreV1().Pods(namespace).GetLogs(name, opts)
		return req.Stream(ctx)
	}

	// 1. Try remote CLI over SSH
	cli, err := s.detectRemoteK8sCLI(ctx, serverID)
	if err == nil && cli != "" {
		stream, streamErr := s.streamPodLogsViaSSH(ctx, serverID, cli, namespace, name, containerName, follow, tailLines)
		if streamErr == nil {
			return stream, nil
		}
		log.Printf("[K8S] CLI stream logs failed on server %d: %v. Falling back to Docker inspection.", serverID, streamErr)
	}

	// 2. Fallback: Stream directly from Docker container
	return s.streamPodLogsViaDocker(ctx, serverID, namespace, name, containerName, follow, tailLines)
}

// --- Remote SSH and CLI Helpers ---

func (s *K8sService) execRemoteSSH(ctx context.Context, serverID uint, command string) (string, error) {
	if s.docker == nil {
		return "", errors.New("docker service not available for SSH dialing")
	}

	sshClient, server, err := s.docker.GetSSHClient(ctx, serverID)
	if err != nil {
		return "", err
	}
	defer sshClient.Close()

	session, err := sshClient.NewSession()
	if err != nil {
		return "", fmt.Errorf("failed to create SSH session on %s: %w", server.Name, err)
	}
	defer session.Close()

	stdin, err := session.StdinPipe()
	if err != nil {
		return "", fmt.Errorf("failed to open stdin pipe on %s: %w", server.Name, err)
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	session.Stdout = &stdoutBuf
	session.Stderr = &stderrBuf

	cmdCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	// Launch POSIX sh via "sh -s" to execute all commands in a standard shell,
	// completely avoiding syntax incompatibilities with fish, zsh, csh, etc.
	if err := session.Start("sh -s"); err != nil {
		return "", fmt.Errorf("failed to start remote sh on %s: %w", server.Name, err)
	}

	go func() {
		_, _ = io.WriteString(stdin, command+"\n")
		_ = stdin.Close()
	}()

	done := make(chan error, 1)
	go func() {
		done <- session.Wait()
	}()

	select {
	case <-cmdCtx.Done():
		_ = session.Signal(ssh.SIGKILL)
		return "", fmt.Errorf("remote command timed out after 15 seconds")
	case waitErr := <-done:
		if waitErr != nil {
			errMsg := strings.TrimSpace(stderrBuf.String())
			if errMsg == "" {
				errMsg = strings.TrimSpace(stdoutBuf.String())
			}
			if errMsg == "" {
				errMsg = waitErr.Error()
			}
			return stdoutBuf.String(), fmt.Errorf("remote command failed: %s", errMsg)
		}
		return stdoutBuf.String(), nil
	}
}

func (s *K8sService) detectRemoteK8sCLI(ctx context.Context, serverID uint) (string, error) {
	if val, ok := s.cliCache.Load(serverID); ok {
		if cli, ok := val.(string); ok && cli != "" {
			return cli, nil
		}
	}

	// Universal kubectl detector: discovers the standard kubectl binary across PATH,
	// common installation directories (/usr/local/bin, /usr/bin, /snap/bin, $HOME/bin),
	// and checks accessible kubeconfig files. It relies strictly on universal kubectl commands.
	detectScript := `
KUBECTL_BIN=""
if command -v kubectl >/dev/null 2>&1; then
    KUBECTL_BIN="kubectl"
elif [ -x /usr/local/bin/kubectl ]; then
    KUBECTL_BIN="/usr/local/bin/kubectl"
elif [ -x /usr/bin/kubectl ]; then
    KUBECTL_BIN="/usr/bin/kubectl"
elif [ -x /snap/bin/kubectl ]; then
    KUBECTL_BIN="/snap/bin/kubectl"
elif [ -x "$HOME/bin/kubectl" ]; then
    KUBECTL_BIN="$HOME/bin/kubectl"
fi

if [ -z "$KUBECTL_BIN" ]; then
    if sudo -n command -v kubectl >/dev/null 2>&1; then
        KUBECTL_BIN="sudo -n kubectl"
    elif sudo -n test -x /usr/local/bin/kubectl; then
        KUBECTL_BIN="sudo -n /usr/local/bin/kubectl"
    elif sudo -n test -x /usr/bin/kubectl; then
        KUBECTL_BIN="sudo -n /usr/bin/kubectl"
    elif sudo -n test -x /snap/bin/kubectl; then
        KUBECTL_BIN="sudo -n /snap/bin/kubectl"
    fi
fi

if [ -z "$KUBECTL_BIN" ]; then
    echo "none"
    exit 0
fi

# 1. Test if default config works
if $KUBECTL_BIN cluster-info >/dev/null 2>&1 || $KUBECTL_BIN get nodes >/dev/null 2>&1; then
    echo "$KUBECTL_BIN"
    exit 0
fi

# 2. Probe standard kubeconfig locations using kubectl --kubeconfig=...
CONFIG_CANDIDATES="$HOME/.kube/config /etc/kubernetes/admin.conf /etc/kubernetes/kubelet.conf /etc/rancher/k3s/k3s.yaml /var/snap/microk8s/current/credentials/client.config"
for cfg in $CONFIG_CANDIDATES; do
    if [ -f "$cfg" ] && [ -r "$cfg" ]; then
        if $KUBECTL_BIN --kubeconfig="$cfg" cluster-info >/dev/null 2>&1 || $KUBECTL_BIN --kubeconfig="$cfg" get nodes >/dev/null 2>&1; then
            echo "$KUBECTL_BIN --kubeconfig=$cfg"
            exit 0
        fi
    fi
done

# 3. Probe sudo access for root-protected kubeconfig files
for cfg in "/etc/kubernetes/admin.conf" "/etc/rancher/k3s/k3s.yaml" "/root/.kube/config"; do
    if sudo -n test -r "$cfg" >/dev/null 2>&1; then
        if sudo -n $KUBECTL_BIN --kubeconfig="$cfg" cluster-info >/dev/null 2>&1 || sudo -n $KUBECTL_BIN --kubeconfig="$cfg" get nodes >/dev/null 2>&1; then
            echo "sudo -n $KUBECTL_BIN --kubeconfig=$cfg"
            exit 0
        fi
    fi
done

# Fallback: return the resolved kubectl binary directly
echo "$KUBECTL_BIN"
`
	out, err := s.execRemoteSSH(ctx, serverID, detectScript)
	if err != nil {
		return "", err
	}

	trimmed := strings.TrimSpace(out)
	if trimmed == "none" || trimmed == "" {
		return "", errors.New("kubectl command not found on remote host. Please install kubectl and ensure it is in PATH")
	}

	lines := strings.Split(trimmed, "\n")
	cli := strings.TrimSpace(lines[0])
	s.cliCache.Store(serverID, cli)
	return cli, nil
}

func (s *K8sService) listPodsViaCLI(ctx context.Context, serverID uint, cli, namespace string) ([]models.PodSummary, error) {
	nsFlag := fmt.Sprintf("-n %s", shellEscape(namespace))
	if namespace == metav1.NamespaceAll {
		nsFlag = "--all-namespaces"
	}
	cmd := fmt.Sprintf("%s get pods %s -o json", cli, nsFlag)

	out, err := s.execRemoteSSH(ctx, serverID, cmd)
	if err != nil {
		return nil, err
	}

	var podList corev1.PodList
	if err := json.Unmarshal([]byte(out), &podList); err != nil {
		return nil, fmt.Errorf("failed to parse pods json from remote cli: %w", err)
	}

	return mapPodListToSummary(&podList), nil
}

func (s *K8sService) listDeploymentsViaCLI(ctx context.Context, serverID uint, cli, namespace string) ([]models.DeploymentSummary, error) {
	nsFlag := fmt.Sprintf("-n %s", shellEscape(namespace))
	if namespace == metav1.NamespaceAll {
		nsFlag = "--all-namespaces"
	}
	cmd := fmt.Sprintf("%s get deployments %s -o json", cli, nsFlag)

	out, err := s.execRemoteSSH(ctx, serverID, cmd)
	if err != nil {
		return nil, err
	}

	var depList appsv1.DeploymentList
	if err := json.Unmarshal([]byte(out), &depList); err != nil {
		return nil, fmt.Errorf("failed to parse deployments json from remote cli: %w", err)
	}

	return mapDeploymentListToSummary(&depList), nil
}

type sshLogStreamCloser struct {
	reader    io.Reader
	session   *ssh.Session
	sshClient *ssh.Client
}

func (s *sshLogStreamCloser) Read(p []byte) (int, error) {
	return s.reader.Read(p)
}

func (s *sshLogStreamCloser) Close() error {
	var errSession, errClient error
	if s.session != nil {
		errSession = s.session.Close()
	}
	if s.sshClient != nil {
		errClient = s.sshClient.Close()
	}
	if errSession != nil {
		return errSession
	}
	return errClient
}

func (s *K8sService) streamPodLogsViaSSH(ctx context.Context, serverID uint, cli, namespace, podName, containerName string, follow bool, tailLines int64) (io.ReadCloser, error) {
	sshClient, server, err := s.docker.GetSSHClient(ctx, serverID)
	if err != nil {
		return nil, err
	}

	session, err := sshClient.NewSession()
	if err != nil {
		_ = sshClient.Close()
		return nil, fmt.Errorf("failed to create SSH session on %s: %w", server.Name, err)
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		_ = session.Close()
		_ = sshClient.Close()
		return nil, fmt.Errorf("failed to open stdin pipe on %s: %w", server.Name, err)
	}

	stdout, err := session.StdoutPipe()
	if err != nil {
		_ = session.Close()
		_ = sshClient.Close()
		return nil, fmt.Errorf("failed to open stdout pipe: %w", err)
	}

	cmd := fmt.Sprintf("%s logs %s -n %s", cli, shellEscape(podName), shellEscape(namespace))
	if containerName != "" {
		cmd += fmt.Sprintf(" -c %s", shellEscape(containerName))
	}
	if tailLines > 0 {
		cmd += fmt.Sprintf(" --tail=%d", tailLines)
	}
	if follow {
		cmd += " -f"
	}

	// Always launch POSIX sh via "sh -s" so logs command runs reliably regardless of remote shell
	if err := session.Start("sh -s"); err != nil {
		_ = session.Close()
		_ = sshClient.Close()
		return nil, fmt.Errorf("failed to start remote sh for logs command: %w", err)
	}

	go func() {
		_, _ = io.WriteString(stdin, cmd+"\n")
		_ = stdin.Close()
	}()

	return &sshLogStreamCloser{
		reader:    stdout,
		session:   session,
		sshClient: sshClient,
	}, nil
}

// --- Docker-Kubernetes Container Fallback Helpers ---

func (s *K8sService) listPodsViaDocker(ctx context.Context, serverID uint, namespace string) ([]models.PodSummary, error) {
	if s.docker == nil {
		return nil, errors.New("docker service not available")
	}

	cli, err := s.docker.GetClient(ctx, serverID)
	if err != nil || cli == nil {
		return nil, errors.New("docker client unreachable")
	}

	rawList, err := cli.ContainerList(ctx, dockercontainer.ListOptions{All: true})
	if err != nil {
		return nil, err
	}

	type podGroup struct {
		name       string
		namespace  string
		containers []string
		status     string
		created    int64
		node       string
		ip         string
	}
	groups := make(map[string]*podGroup)

	for _, c := range rawList {
		podName, hasPod := c.Labels["io.kubernetes.pod.name"]
		if !hasPod || podName == "" {
			continue
		}
		podNs := c.Labels["io.kubernetes.pod.namespace"]
		if podNs == "" {
			podNs = "default"
		}

		if namespace != "" && namespace != metav1.NamespaceAll && podNs != namespace {
			continue
		}

		containerName := c.Labels["io.kubernetes.container.name"]
		key := fmt.Sprintf("%s/%s", podNs, podName)
		grp, exists := groups[key]
		if !exists {
			grp = &podGroup{
				name:      podName,
				namespace: podNs,
				created:   c.Created,
				status:    "Running",
			}
			groups[key] = grp
		}

		if c.Created < grp.created && c.Created > 0 {
			grp.created = c.Created
		}

		if containerName != "" && containerName != "POD" {
			grp.containers = append(grp.containers, containerName)
		}

		if c.State != "running" && grp.status == "Running" {
			grp.status = strings.Title(c.State)
		}
	}

	result := make([]models.PodSummary, 0, len(groups))
	for _, grp := range groups {
		createdTime := time.Unix(grp.created, 0)
		result = append(result, models.PodSummary{
			Name:       grp.name,
			Namespace:  grp.namespace,
			Status:     grp.status,
			Restarts:   0,
			Age:        formatAge(createdTime),
			Node:       grp.node,
			IP:         grp.ip,
			Containers: grp.containers,
		})
	}

	return result, nil
}

func (s *K8sService) deletePodViaDocker(ctx context.Context, serverID uint, namespace, name string) error {
	if s.docker == nil {
		return errors.New("docker service not available")
	}

	cli, err := s.docker.GetClient(ctx, serverID)
	if err != nil || cli == nil {
		return errors.New("docker client unreachable")
	}

	rawList, err := cli.ContainerList(ctx, dockercontainer.ListOptions{All: true})
	if err != nil {
		return err
	}

	removedAny := false
	for _, c := range rawList {
		pName := c.Labels["io.kubernetes.pod.name"]
		pNs := c.Labels["io.kubernetes.pod.namespace"]
		if pNs == "" {
			pNs = "default"
		}

		if pName == name && pNs == namespace {
			_ = s.docker.RemoveContainerOnServer(ctx, serverID, c.ID, true)
			removedAny = true
		}
	}

	if !removedAny {
		return fmt.Errorf("pod %s/%s not found on server %d", namespace, name, serverID)
	}
	return nil
}

func (s *K8sService) streamPodLogsViaDocker(ctx context.Context, serverID uint, namespace, name, containerName string, follow bool, tailLines int64) (io.ReadCloser, error) {
	if s.docker == nil {
		return nil, errors.New("docker service not available")
	}

	cli, err := s.docker.GetClient(ctx, serverID)
	if err != nil || cli == nil {
		return nil, errors.New("docker client unreachable")
	}

	rawList, err := cli.ContainerList(ctx, dockercontainer.ListOptions{All: true})
	if err != nil {
		return nil, err
	}

	var targetContainerID string
	for _, c := range rawList {
		pName := c.Labels["io.kubernetes.pod.name"]
		pNs := c.Labels["io.kubernetes.pod.namespace"]
		if pNs == "" {
			pNs = "default"
		}
		cName := c.Labels["io.kubernetes.container.name"]

		if pName == name && pNs == namespace {
			if containerName == "" || cName == containerName {
				targetContainerID = c.ID
				break
			}
		}
	}

	if targetContainerID == "" {
		return nil, fmt.Errorf("container for pod %s/%s not found on server %d", namespace, name, serverID)
	}

	tailStr := strconv.FormatInt(tailLines, 10)
	return s.docker.StreamContainerLogsOnServer(ctx, serverID, targetContainerID, follow, tailStr)
}

// --- Common Mapping Helpers ---

func mapPodListToSummary(podList *corev1.PodList) []models.PodSummary {
	result := make([]models.PodSummary, 0, len(podList.Items))
	for _, p := range podList.Items {
		var restarts int32 = 0
		for _, cs := range p.Status.ContainerStatuses {
			restarts += cs.RestartCount
		}

		containerNames := make([]string, 0, len(p.Spec.Containers))
		for _, c := range p.Spec.Containers {
			containerNames = append(containerNames, c.Name)
		}

		status := string(p.Status.Phase)
		for _, cs := range p.Status.ContainerStatuses {
			if cs.State.Waiting != nil && cs.State.Waiting.Reason != "" {
				status = cs.State.Waiting.Reason
				break
			}
		}

		result = append(result, models.PodSummary{
			Name:       p.Name,
			Namespace:  p.Namespace,
			Status:     status,
			Restarts:   restarts,
			Age:        formatAge(p.CreationTimestamp.Time),
			Node:       p.Spec.NodeName,
			IP:         p.Status.PodIP,
			Containers: containerNames,
		})
	}
	return result
}

func mapDeploymentListToSummary(depList *appsv1.DeploymentList) []models.DeploymentSummary {
	result := make([]models.DeploymentSummary, 0, len(depList.Items))
	for _, d := range depList.Items {
		var desired int32 = 1
		if d.Spec.Replicas != nil {
			desired = *d.Spec.Replicas
		}

		images := make([]string, 0, len(d.Spec.Template.Spec.Containers))
		for _, c := range d.Spec.Template.Spec.Containers {
			images = append(images, c.Image)
		}

		result = append(result, models.DeploymentSummary{
			Name:              d.Name,
			Namespace:         d.Namespace,
			ReadyReplicas:     d.Status.ReadyReplicas,
			DesiredReplicas:   desired,
			AvailableReplicas: d.Status.AvailableReplicas,
			Age:               formatAge(d.CreationTimestamp.Time),
			Images:            images,
		})
	}
	return result
}

func formatAge(t time.Time) string {
	d := time.Since(t)
	if d.Hours() >= 24 {
		days := int(d.Hours() / 24)
		return fmt.Sprintf("%dd", days)
	}
	if d.Hours() >= 1 {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	if d.Minutes() >= 1 {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%ds", int(d.Seconds()))
}
