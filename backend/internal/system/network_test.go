package system

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/kraken/admin-tool/backend/internal/auth"
	"github.com/kraken/admin-tool/backend/internal/database"
	"github.com/kraken/admin-tool/backend/internal/models"
)

type mockSystemK8sServer struct {
	mu          sync.RWMutex
	secrets     map[string]*corev1.Secret
	configMaps  map[string]*corev1.ConfigMap
	services    map[string]*corev1.Service
	ingresses   map[string]*networkingv1.Ingress
	deployments map[string]*appsv1.Deployment
	server      *httptest.Server
	client      kubernetes.Interface
}

func newMockSystemK8sServer(t *testing.T) *mockSystemK8sServer {
	t.Helper()
	m := &mockSystemK8sServer{
		secrets:     make(map[string]*corev1.Secret),
		configMaps:  make(map[string]*corev1.ConfigMap),
		services:    make(map[string]*corev1.Service),
		ingresses:   make(map[string]*networkingv1.Ingress),
		deployments: make(map[string]*appsv1.Deployment),
	}

	m.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		bodyBytes, _ := io.ReadAll(r.Body)

		var trimmed string
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/v1/namespaces/"):
			trimmed = strings.TrimPrefix(r.URL.Path, "/api/v1/namespaces/")
		case strings.HasPrefix(r.URL.Path, "/apis/networking.k8s.io/v1/namespaces/"):
			trimmed = strings.TrimPrefix(r.URL.Path, "/apis/networking.k8s.io/v1/namespaces/")
		case strings.HasPrefix(r.URL.Path, "/apis/apps/v1/namespaces/"):
			trimmed = strings.TrimPrefix(r.URL.Path, "/apis/apps/v1/namespaces/")
		default:
			writeSystemK8sNotFound(w, "unknown", r.URL.Path)
			return
		}

		parts := strings.Split(strings.Trim(trimmed, "/"), "/")
		if len(parts) < 2 {
			writeSystemK8sNotFound(w, "unknown", r.URL.Path)
			return
		}
		ns := parts[0]
		resource := parts[1]
		name := ""
		if len(parts) >= 3 {
			name = parts[2]
		}
		key := ns + "/" + name

		m.mu.Lock()
		defer m.mu.Unlock()

		switch resource {
		case "secrets":
			switch r.Method {
			case http.MethodGet:
				obj, ok := m.secrets[key]
				if !ok {
					writeSystemK8sNotFound(w, "secrets", name)
					return
				}
				_ = json.NewEncoder(w).Encode(obj)
			case http.MethodPost:
				var obj corev1.Secret
				_ = json.Unmarshal(bodyBytes, &obj)
				obj.Namespace = ns
				m.secrets[ns+"/"+obj.Name] = &obj
				w.WriteHeader(http.StatusCreated)
				_ = json.NewEncoder(w).Encode(&obj)
			case http.MethodPut:
				var obj corev1.Secret
				_ = json.Unmarshal(bodyBytes, &obj)
				obj.Namespace = ns
				m.secrets[key] = &obj
				_ = json.NewEncoder(w).Encode(&obj)
			}

		case "configmaps":
			switch r.Method {
			case http.MethodGet:
				obj, ok := m.configMaps[key]
				if !ok {
					writeSystemK8sNotFound(w, "configmaps", name)
					return
				}
				_ = json.NewEncoder(w).Encode(obj)
			case http.MethodPost:
				var obj corev1.ConfigMap
				_ = json.Unmarshal(bodyBytes, &obj)
				obj.Namespace = ns
				m.configMaps[ns+"/"+obj.Name] = &obj
				w.WriteHeader(http.StatusCreated)
				_ = json.NewEncoder(w).Encode(&obj)
			case http.MethodPut:
				var obj corev1.ConfigMap
				_ = json.Unmarshal(bodyBytes, &obj)
				obj.Namespace = ns
				m.configMaps[key] = &obj
				_ = json.NewEncoder(w).Encode(&obj)
			}

		case "services":
			switch r.Method {
			case http.MethodGet:
				obj, ok := m.services[key]
				if !ok {
					writeSystemK8sNotFound(w, "services", name)
					return
				}
				_ = json.NewEncoder(w).Encode(obj)
			case http.MethodPut:
				var obj corev1.Service
				_ = json.Unmarshal(bodyBytes, &obj)
				obj.Namespace = ns
				m.services[key] = &obj
				_ = json.NewEncoder(w).Encode(&obj)
			}

		case "ingresses":
			switch r.Method {
			case http.MethodGet:
				obj, ok := m.ingresses[key]
				if !ok {
					writeSystemK8sNotFound(w, "ingresses", name)
					return
				}
				_ = json.NewEncoder(w).Encode(obj)
			case http.MethodPost:
				var obj networkingv1.Ingress
				_ = json.Unmarshal(bodyBytes, &obj)
				obj.Namespace = ns
				m.ingresses[ns+"/"+obj.Name] = &obj
				w.WriteHeader(http.StatusCreated)
				_ = json.NewEncoder(w).Encode(&obj)
			case http.MethodPut:
				var obj networkingv1.Ingress
				_ = json.Unmarshal(bodyBytes, &obj)
				obj.Namespace = ns
				m.ingresses[key] = &obj
				_ = json.NewEncoder(w).Encode(&obj)
			case http.MethodDelete:
				obj, ok := m.ingresses[key]
				if !ok {
					writeSystemK8sNotFound(w, "ingresses", name)
					return
				}
				delete(m.ingresses, key)
				_ = json.NewEncoder(w).Encode(obj)
			}

		case "deployments":
			switch r.Method {
			case http.MethodGet:
				obj, ok := m.deployments[key]
				if !ok {
					writeSystemK8sNotFound(w, "deployments", name)
					return
				}
				_ = json.NewEncoder(w).Encode(obj)
			case http.MethodPut:
				var obj appsv1.Deployment
				_ = json.Unmarshal(bodyBytes, &obj)
				obj.Namespace = ns
				m.deployments[key] = &obj
				_ = json.NewEncoder(w).Encode(&obj)
			}

		default:
			writeSystemK8sNotFound(w, resource, name)
		}
	}))

	cli, err := kubernetes.NewForConfig(&rest.Config{Host: m.server.URL})
	if err != nil {
		t.Fatalf("failed to create mock k8s client: %v", err)
	}
	m.client = cli
	t.Cleanup(m.server.Close)
	return m
}

func writeSystemK8sNotFound(w http.ResponseWriter, resource, name string) {
	w.WriteHeader(http.StatusNotFound)
	_ = json.NewEncoder(w).Encode(metav1.Status{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"},
		Status:   metav1.StatusFailure,
		Message:  resource + " " + name + " not found",
		Reason:   metav1.StatusReasonNotFound,
		Code:     http.StatusNotFound,
	})
}

func TestNormalizeDomains(t *testing.T) {
	domains, err := NormalizeDomains([]string{
		"https://KAT.MyCompany.com:443/login",
		"kraken.mycompany.com, *.internal.corp",
		"kat.mycompany.com",
	})
	if err != nil {
		t.Fatalf("NormalizeDomains failed: %v", err)
	}
	expected := []string{"kat.mycompany.com", "kraken.mycompany.com", "*.internal.corp"}
	if len(domains) != len(expected) {
		t.Fatalf("expected %v, got %v", expected, domains)
	}
	for i := range expected {
		if domains[i] != expected[i] {
			t.Errorf("domain[%d]: expected %q, got %q", i, expected[i], domains[i])
		}
	}

	if _, err := NormalizeDomains([]string{"not valid host!"}); err == nil {
		t.Errorf("expected error for invalid hostname")
	}
}

func TestGenerateMultiSANCertificate(t *testing.T) {
	domains := []string{"kat.mycompany.com", "kraken.mycompany.com"}
	certPEM, keyPEM, fp, expiresAt, sanList, err := GenerateMultiSANCertificate(domains)
	if err != nil {
		t.Fatalf("GenerateMultiSANCertificate failed: %v", err)
	}
	if certPEM == "" || keyPEM == "" || fp == "" || expiresAt.IsZero() {
		t.Fatalf("expected non-empty cert, key, fingerprint, and expiration")
	}

	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		t.Fatalf("failed to decode generated cert PEM")
	}
	parsed, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("failed to parse generated x509 cert: %v", err)
	}
	if parsed.Subject.CommonName != "kat.mycompany.com" {
		t.Errorf("expected CommonName 'kat.mycompany.com', got %q", parsed.Subject.CommonName)
	}

	dnsSet := make(map[string]bool)
	for _, d := range parsed.DNSNames {
		dnsSet[d] = true
	}
	for _, mustHave := range []string{"kat.mycompany.com", "kraken.mycompany.com", "localhost", "proxy"} {
		if !dnsSet[mustHave] {
			t.Errorf("expected DNS SAN %q in certificate, got %v", mustHave, sanList)
		}
	}
}

func TestUpdateNetworkConfig_ReconcilesK8sIngressSecretConfigMapsAndRolloutRestart(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:memnettest?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	if err := db.AutoMigrate(&models.SystemNetworkConfig{}, &models.AuditLog{}); err != nil {
		t.Fatalf("failed to migrate tables: %v", err)
	}

	mockAPI := newMockSystemK8sServer(t)

	// Seed baseline Kubernetes resources in namespace "kraken"
	mockAPI.configMaps["kraken/kraken-config"] = &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "kraken-config", Namespace: "kraken"},
		Data:       map[string]string{"ENVIRONMENT": "production", "KRAKEN_RUNTIME_MODE": "kubernetes"},
	}
	mockAPI.services["kraken/proxy"] = &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "proxy", Namespace: "kraken"},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{Name: "http", Port: 8000, TargetPort: intstr.FromInt(8000), NodePort: 30800},
				{Name: "https", Port: 8443, TargetPort: intstr.FromInt(8443), NodePort: 30443},
			},
		},
	}
	mockAPI.deployments["kraken/proxy"] = &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "proxy", Namespace: "kraken"},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{{Name: "generate-tls-certs", Image: "alpine:3.21"}},
					Containers:     []corev1.Container{{Name: "proxy", Image: "nginx:alpine"}},
					Volumes: []corev1.Volume{{
						Name:         "tls-certs",
						VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
					}},
				},
			},
		},
	}
	mockAPI.deployments["kraken/frontend"] = &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "frontend", Namespace: "kraken"},
	}
	mockAPI.deployments["kraken/backend"] = &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "backend", Namespace: "kraken"},
	}

	h := NewHandler(&database.Handle{DB: db, DBType: "SQLite"}, nil, nil, time.Now())
	h.SetK8sClient(mockAPI.client, "kraken")

	var updatedOrigins []string
	h.SetOnOriginsUpdated(func(origins []string) {
		updatedOrigins = origins
	})

	payload, _ := json.Marshal(models.UpdateNetworkConfigRequest{
		IngressEnabled:   true,
		Domains:          []string{"kat.mycompany.com", "kraken.mycompany.com"},
		IngressClassName: "nginx",
	})
	req := httptest.NewRequest("PUT", "/api/system/network-config", bytes.NewReader(payload))
	req = req.WithContext(auth.WithUserContext(req.Context(), &auth.Claims{
		UserID:   1,
		Username: "admin",
		Role:     "admin",
	}))
	rec := httptest.NewRecorder()

	h.UpdateNetworkConfigHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp models.NetworkConfigResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.LastApplyStatus != "success" {
		t.Fatalf("expected LastApplyStatus='success', got %q (%s)", resp.LastApplyStatus, resp.LastApplyMessage)
	}
	if len(resp.Domains) != 2 || resp.Domains[0] != "kat.mycompany.com" {
		t.Fatalf("unexpected domains in response: %v", resp.Domains)
	}

	// Verify TLS Secret contains both FQDNs in cert SANs
	sec := mockAPI.secrets["kraken/kraken-tls-secret"]
	if sec == nil || len(sec.Data["tls.crt"]) == 0 || len(sec.Data["tls.key"]) == 0 {
		t.Fatalf("expected Secret kraken-tls-secret created with tls.crt and tls.key")
	}
	_, _, sans, err := InspectCertificatePEM(string(sec.Data["tls.crt"]), string(sec.Data["tls.key"]))
	if err != nil {
		t.Fatalf("Secret TLS keypair invalid: %v", err)
	}
	sanText := strings.Join(sans, ",")
	if !strings.Contains(sanText, "kat.mycompany.com") || !strings.Contains(sanText, "kraken.mycompany.com") {
		t.Errorf("expected both FQDNs in TLS Secret SANs, got %v", sans)
	}

	// Verify ConfigMap kraken-config & kraken-nginx-template
	cm := mockAPI.configMaps["kraken/kraken-config"]
	if cm.Data["KRAKEN_FQDNS"] != "kat.mycompany.com,kraken.mycompany.com" || cm.Data["KRAKEN_INGRESS_ENABLED"] != "true" {
		t.Errorf("unexpected kraken-config data: %+v", cm.Data)
	}
	nginxCM := mockAPI.configMaps["kraken/kraken-nginx-template"]
	if nginxCM == nil || !strings.Contains(nginxCM.Data["default.conf.template"], "server_name kat.mycompany.com kraken.mycompany.com _;") {
		t.Errorf("expected server_name with both FQDNs in kraken-nginx-template")
	}

	// Verify Ingress kraken-ingress created for both FQDNs on TLS port 443
	ing := mockAPI.ingresses["kraken/kraken-ingress"]
	if ing == nil {
		t.Fatalf("expected Ingress kraken-ingress to be created")
	}
	if len(ing.Spec.TLS) != 1 || ing.Spec.TLS[0].SecretName != "kraken-tls-secret" || len(ing.Spec.TLS[0].Hosts) != 2 {
		t.Errorf("unexpected Ingress TLS spec: %+v", ing.Spec.TLS)
	}
	if len(ing.Spec.Rules) != 2 || ing.Spec.Rules[0].Host != "kat.mycompany.com" || ing.Spec.Rules[1].Host != "kraken.mycompany.com" {
		t.Errorf("unexpected Ingress rules: %+v", ing.Spec.Rules)
	}

	// Verify Service/proxy exposes standard ports 80 and 443
	svc := mockAPI.services["kraken/proxy"]
	portSet := make(map[int32]bool)
	for _, p := range svc.Spec.Ports {
		portSet[p.Port] = true
	}
	if !portSet[80] || !portSet[443] || !portSet[8000] || !portSet[8443] {
		t.Errorf("expected ports 80, 443, 8000, 8443 on Service/proxy, got %+v", svc.Spec.Ports)
	}

	// Verify Deployments proxy, frontend, and backend received rollout restart annotation
	proxyDep := mockAPI.deployments["kraken/proxy"]
	if proxyDep.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] == "" {
		t.Errorf("expected proxy Deployment rollout restart annotation")
	}
	if len(proxyDep.Spec.Template.Spec.InitContainers) != 0 {
		t.Errorf("expected generate-tls-certs initContainer removed after switching to kraken-tls-secret")
	}
	if proxyDep.Spec.Template.Spec.Volumes[0].VolumeSource.Secret == nil || proxyDep.Spec.Template.Spec.Volumes[0].VolumeSource.Secret.SecretName != "kraken-tls-secret" {
		t.Errorf("expected proxy Deployment volume switched to kraken-tls-secret")
	}
	if mockAPI.deployments["kraken/frontend"].Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] == "" {
		t.Errorf("expected frontend Deployment rollout restart annotation")
	}
	if mockAPI.deployments["kraken/backend"].Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] == "" {
		t.Errorf("expected backend Deployment rollout restart annotation")
	}
	if len(updatedOrigins) == 0 || updatedOrigins[0] != "https://kat.mycompany.com" {
		t.Errorf("expected onOriginsUpdated invoked with new FQDNs, got %v", updatedOrigins)
	}

	// Verify disabling Ingress deletes kraken-ingress cleanly
	if _, err := h.ApplyNetworkConfig(context.Background(), models.UpdateNetworkConfigRequest{
		IngressEnabled: false,
		Domains:        []string{"kat.mycompany.com"},
	}, "admin", 1); err != nil {
		t.Fatalf("disabling ingress failed: %v", err)
	}
	if mockAPI.ingresses["kraken/kraken-ingress"] != nil {
		t.Errorf("expected Ingress kraken-ingress deleted when IngressEnabled=false")
	}
}
