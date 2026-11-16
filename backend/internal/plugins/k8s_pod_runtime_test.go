package plugins

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/kraken/admin-tool/backend/internal/models"
	"github.com/kraken/admin-tool/backend/internal/plugins/sidecarauth"
)

type mockK8sAPIServer struct {
	mu       sync.RWMutex
	pods     map[string]*corev1.Pod
	services map[string]*corev1.Service
	secrets  map[string]*corev1.Secret
	server   *httptest.Server
	client   kubernetes.Interface
}

func newMockK8sAPIServer(t *testing.T) *mockK8sAPIServer {
	t.Helper()
	m := &mockK8sAPIServer{
		pods:     make(map[string]*corev1.Pod),
		services: make(map[string]*corev1.Service),
		secrets:  make(map[string]*corev1.Secret),
	}

	m.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		trimmed := strings.TrimPrefix(r.URL.Path, "/api/v1/namespaces/")
		parts := strings.Split(strings.Trim(trimmed, "/"), "/")
		if len(parts) < 2 {
			writeK8sNotFound(w, "unknown", r.URL.Path)
			return
		}
		ns := parts[0]
		resource := parts[1]
		name := ""
		subresource := ""
		if len(parts) >= 3 {
			name = parts[2]
		}
		if len(parts) >= 4 {
			subresource = parts[3]
		}

		bodyBytes, _ := io.ReadAll(r.Body)
		key := ns + "/" + name

		m.mu.Lock()
		defer m.mu.Unlock()

		switch resource {
		case "pods":
			switch r.Method {
			case http.MethodGet:
				pod, ok := m.pods[key]
				if !ok {
					writeK8sNotFound(w, "pods", name)
					return
				}
				_ = json.NewEncoder(w).Encode(pod)
			case http.MethodPost:
				var pod corev1.Pod
				if err := json.Unmarshal(bodyBytes, &pod); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				pod.Namespace = ns
				m.pods[ns+"/"+pod.Name] = &pod
				w.WriteHeader(http.StatusCreated)
				_ = json.NewEncoder(w).Encode(&pod)
			case http.MethodPut:
				var pod corev1.Pod
				if err := json.Unmarshal(bodyBytes, &pod); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				pod.Namespace = ns
				if subresource == "status" {
					if existing, ok := m.pods[key]; ok {
						existing.Status = pod.Status
						_ = json.NewEncoder(w).Encode(existing)
						return
					}
				}
				m.pods[key] = &pod
				_ = json.NewEncoder(w).Encode(&pod)
			case http.MethodDelete:
				pod, ok := m.pods[key]
				if !ok {
					writeK8sNotFound(w, "pods", name)
					return
				}
				delete(m.pods, key)
				_ = json.NewEncoder(w).Encode(pod)
			}

		case "services":
			switch r.Method {
			case http.MethodGet:
				svc, ok := m.services[key]
				if !ok {
					writeK8sNotFound(w, "services", name)
					return
				}
				_ = json.NewEncoder(w).Encode(svc)
			case http.MethodPost:
				var svc corev1.Service
				if err := json.Unmarshal(bodyBytes, &svc); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				svc.Namespace = ns
				m.services[ns+"/"+svc.Name] = &svc
				w.WriteHeader(http.StatusCreated)
				_ = json.NewEncoder(w).Encode(&svc)
			case http.MethodPut:
				var svc corev1.Service
				if err := json.Unmarshal(bodyBytes, &svc); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				svc.Namespace = ns
				m.services[key] = &svc
				_ = json.NewEncoder(w).Encode(&svc)
			case http.MethodDelete:
				svc, ok := m.services[key]
				if !ok {
					writeK8sNotFound(w, "services", name)
					return
				}
				delete(m.services, key)
				_ = json.NewEncoder(w).Encode(svc)
			}

		case "secrets":
			switch r.Method {
			case http.MethodGet:
				sec, ok := m.secrets[key]
				if !ok {
					writeK8sNotFound(w, "secrets", name)
					return
				}
				_ = json.NewEncoder(w).Encode(sec)
			case http.MethodPost:
				var sec corev1.Secret
				if err := json.Unmarshal(bodyBytes, &sec); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				sec.Namespace = ns
				m.secrets[ns+"/"+sec.Name] = &sec
				w.WriteHeader(http.StatusCreated)
				_ = json.NewEncoder(w).Encode(&sec)
			case http.MethodPut:
				var sec corev1.Secret
				if err := json.Unmarshal(bodyBytes, &sec); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				sec.Namespace = ns
				m.secrets[key] = &sec
				_ = json.NewEncoder(w).Encode(&sec)
			case http.MethodDelete:
				sec, ok := m.secrets[key]
				if !ok {
					writeK8sNotFound(w, "secrets", name)
					return
				}
				delete(m.secrets, key)
				_ = json.NewEncoder(w).Encode(sec)
			}

		default:
			writeK8sNotFound(w, resource, name)
		}
	}))

	cli, err := kubernetes.NewForConfig(&rest.Config{
		Host: m.server.URL,
	})
	if err != nil {
		t.Fatalf("failed to construct k8s client against mock server: %v", err)
	}
	m.client = cli
	t.Cleanup(m.server.Close)
	return m
}

func writeK8sNotFound(w http.ResponseWriter, resource, name string) {
	w.WriteHeader(http.StatusNotFound)
	_ = json.NewEncoder(w).Encode(metav1.Status{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"},
		Status:   metav1.StatusFailure,
		Message:  resource + " " + name + " not found",
		Reason:   metav1.StatusReasonNotFound,
		Code:     http.StatusNotFound,
	})
}

func TestManifest_NormalizesDockerRuntimeToContainer(t *testing.T) {
	raw := []byte(`{
		"id": "com.example.docker-plugin",
		"name": "Docker Legacy Plugin",
		"version": "1.2.0",
		"runtime": {
			"type": "docker",
			"entrypoint": "nginx:alpine",
			"port": 9090
		}
	}`)

	manifest, err := ParseAndValidateManifest(raw)
	if err != nil {
		t.Fatalf("expected docker runtime type to parse cleanly, got error: %v", err)
	}
	if manifest.Runtime.Type != "container" {
		t.Errorf("expected runtime type normalized to 'container', got %q", manifest.Runtime.Type)
	}
	if manifest.Runtime.Port != 9090 {
		t.Errorf("expected port 9090, got %d", manifest.Runtime.Port)
	}
}

func TestInspectDirectory_AutoPromotesDockerfilePluginToContainer(t *testing.T) {
	dir := t.TempDir()
	manifestJSON := `{
		"id": "com.example.auto-docker",
		"name": "Auto Docker Plugin",
		"version": "0.1.0",
		"runtime": { "type": "none" }
	}`
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(manifestJSON), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM python:3.11-slim\nCMD [\"python\", \"app.py\"]\n"), 0644); err != nil {
		t.Fatal(err)
	}

	insp, err := InspectDirectory(dir, nil)
	if err != nil {
		t.Fatalf("InspectDirectory failed: %v", err)
	}
	if insp.Manifest.Runtime.Type != "container" {
		t.Errorf("expected runtime auto-promoted to 'container', got %q", insp.Manifest.Runtime.Type)
	}
	if insp.Manifest.Runtime.Dockerfile != "Dockerfile" {
		t.Errorf("expected Dockerfile detected, got %q", insp.Manifest.Runtime.Dockerfile)
	}
	if !insp.NeedsCompilation || !insp.SourceDetected {
		t.Errorf("expected NeedsCompilation=true and SourceDetected=true")
	}
}

func TestContainerRuntimeManager_ShouldUseKubernetes(t *testing.T) {
	ctx := context.Background()
	crm := NewContainerRuntimeManager(nil)

	if crm.ShouldUseKubernetes(ctx) {
		t.Fatalf("expected ShouldUseKubernetes=false when no k8s client configured")
	}

	mockAPI := newMockK8sAPIServer(t)
	crm.SetK8sClient(mockAPI.client, "kraken")
	if !crm.ShouldUseKubernetes(ctx) {
		t.Fatalf("expected ShouldUseKubernetes=true when explicit k8s client configured")
	}
}

func TestKubernetesPodRuntime_LifecycleWithPrebuiltImageAndSignedCall(t *testing.T) {
	ctx := context.Background()
	mockAPI := newMockK8sAPIServer(t)
	crm := NewContainerRuntimeManager(nil)
	crm.SetK8sClient(mockAPI.client, "kraken")

	manifest := &models.PluginManifest{
		ID:      "com.example.pod-sidecar",
		Name:    "Pod Sidecar",
		Version: "1.0.0",
		Runtime: models.PluginRuntimeConfig{
			Type:       "container",
			Entrypoint: "ghcr.io/example/pod-sidecar:1.0.0",
			Port:       8085,
		},
	}
	plugin := &models.InstalledPlugin{
		PluginID: manifest.ID,
		Name:     manifest.Name,
		Version:  manifest.Version,
	}

	// 1. StartContainer transparently creates Kubernetes Pod + Service
	podName, err := crm.StartContainer(ctx, plugin, manifest)
	if err != nil {
		t.Fatalf("StartContainer in k8s mode failed: %v", err)
	}
	if podName != "kraken-plugin-com-example-pod-sidecar" {
		t.Fatalf("unexpected pod name %q", podName)
	}

	pod, err := mockAPI.client.CoreV1().Pods("kraken").Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("expected Pod %q in namespace 'kraken', got error: %v", podName, err)
	}
	if len(pod.Spec.Containers) != 1 || pod.Spec.Containers[0].Image != "ghcr.io/example/pod-sidecar:1.0.0" {
		t.Fatalf("unexpected container spec: %+v", pod.Spec.Containers)
	}

	envMap := make(map[string]string)
	for _, ev := range pod.Spec.Containers[0].Env {
		envMap[ev.Name] = ev.Value
	}
	if envMap["KRAKEN_PLUGIN_ID"] != manifest.ID {
		t.Errorf("expected KRAKEN_PLUGIN_ID=%q, got %q", manifest.ID, envMap["KRAKEN_PLUGIN_ID"])
	}
	if envMap["KRAKEN_PLUGIN_INSTALLATION_ID"] == "" || envMap["KRAKEN_PLUGIN_INSTANCE_SECRET"] == "" {
		t.Errorf("expected installation ID and instance secret injected into Pod env")
	}
	if envMap["PORT"] != "8085" {
		t.Errorf("expected PORT='8085', got %q", envMap["PORT"])
	}

	svc, err := mockAPI.client.CoreV1().Services("kraken").Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("expected Service %q created, got error: %v", podName, err)
	}
	if len(svc.Spec.Ports) != 1 || svc.Spec.Ports[0].Port != 8085 {
		t.Errorf("expected Service port 8085, got %+v", svc.Spec.Ports)
	}

	// 2. Spin up mock Pod HTTP server verifying sidecarauth signature and update PodIP
	verifier := sidecarauth.NewSidecarVerifier(plugin.PluginID, plugin.InstallationID, plugin.InstanceSecret)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, body, vErr := verifier.VerifyRequest(r)
		if vErr != nil {
			t.Errorf("sidecarauth verification failed: %v", vErr)
			http.Error(w, vErr.Error(), http.StatusUnauthorized)
			return
		}
		if claims.KrakenUsername != "alice" {
			t.Errorf("expected username 'alice', got %q", claims.KrakenUsername)
		}
		var req map[string]interface{}
		_ = json.Unmarshal(body, &req)
		if req["action"] != "ping" {
			t.Errorf("expected action 'ping', got %v", req["action"])
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"pong": true})
	}))
	defer ts.Close()

	parsedURL, _ := url.Parse(ts.URL)
	portInt, _ := strconv.Atoi(parsedURL.Port())
	manifest.Runtime.Port = portInt

	pod.Status.Phase = corev1.PodRunning
	pod.Status.PodIP = parsedURL.Hostname()
	if _, err := mockAPI.client.CoreV1().Pods("kraken").UpdateStatus(ctx, pod, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("failed to update mock pod IP: %v", err)
	}

	res, err := crm.ExecuteCall(ctx, plugin, manifest, "ping", map[string]string{"hello": "k8s"}, "alice", "admin", 1, "sess-1", []string{"docker:read"})
	if err != nil {
		t.Fatalf("ExecuteCall to Pod failed: %v", err)
	}
	resMap, ok := res.(map[string]interface{})
	if !ok || resMap["pong"] != true {
		t.Fatalf("unexpected ExecuteCall response: %#v", res)
	}

	// 3. StopContainer deletes Pod and Service
	if err := crm.StopContainer(ctx, plugin); err != nil {
		t.Fatalf("StopContainer failed: %v", err)
	}
	if _, err := mockAPI.client.CoreV1().Pods("kraken").Get(ctx, podName, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Errorf("expected Pod to be deleted, got err=%v", err)
	}
	if _, err := mockAPI.client.CoreV1().Services("kraken").Get(ctx, podName, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Errorf("expected Service to be deleted, got err=%v", err)
	}
}

func TestKubernetesPodRuntime_ConvertsMultiStageDockerfilePluginWithoutDockerDaemon(t *testing.T) {
	ctx := context.Background()
	mockAPI := newMockK8sAPIServer(t)
	crm := NewContainerRuntimeManager(nil)
	crm.SetK8sClient(mockAPI.client, "kraken")

	pluginDir := t.TempDir()
	dockerfile := `
FROM golang:1.22-alpine AS builder
WORKDIR /src
COPY . .
RUN go build -o /out/plugin-server .

FROM alpine:3.21
WORKDIR /app
COPY --from=builder /out/plugin-server /app/plugin-server
EXPOSE 9001
ENTRYPOINT ["/app/plugin-server"]
`
	if err := os.WriteFile(filepath.Join(pluginDir, "Dockerfile"), []byte(dockerfile), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "main.go"), []byte("package main\nfunc main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	manifest := &models.PluginManifest{
		ID:      "com.example.dockerfile-plugin",
		Name:    "Dockerfile Plugin",
		Version: "2.0.0",
		Runtime: models.PluginRuntimeConfig{
			Type:       "container",
			Dockerfile: "Dockerfile",
		},
	}

	// 1. PrepareKubernetesPluginBuild succeeds cleanly without Docker daemon
	buildRes, err := PrepareKubernetesPluginBuild(ctx, nil, pluginDir, manifest)
	if err != nil {
		t.Fatalf("PrepareKubernetesPluginBuild failed: %v", err)
	}
	if buildRes.Compiler != "k8s-pod-converter" {
		t.Errorf("expected compiler 'k8s-pod-converter', got %q", buildRes.Compiler)
	}
	if manifest.Runtime.Port != 9001 {
		t.Errorf("expected EXPOSE 9001 picked up into manifest.Runtime.Port, got %d", manifest.Runtime.Port)
	}

	plugin := &models.InstalledPlugin{
		PluginID:    manifest.ID,
		Name:        manifest.Name,
		Version:     manifest.Version,
		InstallPath: pluginDir,
	}

	// 2. StartContainer converts multi-stage Dockerfile into Secret + InitContainer + Runtime Container Pod
	podName, err := crm.StartContainer(ctx, plugin, manifest)
	if err != nil {
		t.Fatalf("StartContainer for Dockerfile plugin failed: %v", err)
	}

	secretName := k8sPluginContextSecretName(plugin.PluginID)
	sec, err := mockAPI.client.CoreV1().Secrets("kraken").Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("expected context Secret %q created, got error: %v", secretName, err)
	}
	if len(sec.Data["context.tar.gz"]) == 0 {
		t.Errorf("expected non-empty context.tar.gz in Secret")
	}

	pod, err := mockAPI.client.CoreV1().Pods("kraken").Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("expected converted Pod %q created, got error: %v", podName, err)
	}
	if len(pod.Spec.InitContainers) != 1 || pod.Spec.InitContainers[0].Image != "golang:1.22-alpine" {
		t.Fatalf("expected builder initContainer with golang:1.22-alpine, got %+v", pod.Spec.InitContainers)
	}
	if len(pod.Spec.Containers) != 1 || pod.Spec.Containers[0].Image != "alpine:3.21" {
		t.Fatalf("expected runtime container with alpine:3.21, got %+v", pod.Spec.Containers)
	}
	if !strings.Contains(pod.Spec.Containers[0].Command[2], "exec '/app/plugin-server'") {
		t.Errorf("expected runtime startup script to exec entrypoint, got %q", pod.Spec.Containers[0].Command[2])
	}

	// 3. StopContainer cleans up Pod, Service, and context Secret
	if err := crm.StopContainer(ctx, plugin); err != nil {
		t.Fatalf("StopContainer failed: %v", err)
	}
	if _, err := mockAPI.client.CoreV1().Secrets("kraken").Get(ctx, secretName, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Errorf("expected context Secret to be deleted, got err=%v", err)
	}
}
