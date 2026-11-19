package k8s

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/kraken/admin-tool/backend/internal/auth"
	"github.com/kraken/admin-tool/backend/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func setupK8sSecurityTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dbPath := t.TempDir() + "/test_k8s_security.db"
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.AuditLog{}, &models.DockerServer{}, &models.ServerAccessGrant{}); err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	return db
}

func newFakeK8sService(t *testing.T, db *gorm.DB) *K8sService {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/pods", "/api/v1/namespaces/default/pods":
			_ = json.NewEncoder(w).Encode(&corev1.PodList{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "PodList"},
				Items: []corev1.Pod{{
					ObjectMeta: metav1.ObjectMeta{Name: "secret-local-pod", Namespace: "default"},
					Status:     corev1.PodStatus{Phase: corev1.PodRunning},
				}},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	client, err := kubernetes.NewForConfig(&rest.Config{Host: srv.URL})
	if err != nil {
		t.Fatalf("failed to create test k8s client: %v", err)
	}
	return &K8sService{
		client:    client,
		connected: true,
		db:        db,
	}
}

func withAuthUser(r *http.Request, userID uint, username, role string) *http.Request {
	claims := &auth.Claims{
		UserID:   userID,
		Username: username,
		Role:     role,
	}
	ctx := context.WithValue(r.Context(), auth.UserContextKey, claims)
	return r.WithContext(ctx)
}

func TestK8sService_BlocksServerIDZeroWhenDBConfigured(t *testing.T) {
	db := setupK8sSecurityTestDB(t)
	svc := newFakeK8sService(t, db)
	ctx := context.Background()

	if pods, err := svc.ListPodsOnServer(ctx, 0, "default"); err == nil || len(pods) != 0 {
		t.Fatalf("expected ListPodsOnServer(0) to fail with 0 pods when db is set, got %d pods, err=%v", len(pods), err)
	}
	if deps, err := svc.ListDeploymentsOnServer(ctx, 0, "default"); err == nil || len(deps) != 0 {
		t.Fatalf("expected ListDeploymentsOnServer(0) to fail with 0 deployments when db is set, got %d, err=%v", len(deps), err)
	}
	if ver := svc.GetVersionOnServer(ctx, 0); ver != "unavailable" {
		t.Fatalf("expected GetVersionOnServer(0) == 'unavailable', got %q", ver)
	}
	if pods, deps := svc.GetCountsOnServer(ctx, 0); pods != 0 || deps != 0 {
		t.Fatalf("expected GetCountsOnServer(0) == (0, 0), got (%d, %d)", pods, deps)
	}
	if err := svc.DeletePodOnServer(ctx, 0, "default", "secret-local-pod"); err == nil {
		t.Fatal("expected DeletePodOnServer(0) to fail when db is set")
	}
	if err := svc.ScaleDeploymentOnServer(ctx, 0, "default", "secret-local-deploy", 3); err == nil {
		t.Fatal("expected ScaleDeploymentOnServer(0) to fail when db is set")
	}
	if err := svc.RestartDeploymentOnServer(ctx, 0, "default", "secret-local-deploy"); err == nil {
		t.Fatal("expected RestartDeploymentOnServer(0) to fail when db is set")
	}
	if _, err := svc.StreamPodLogsOnServer(ctx, 0, "default", "secret-local-pod", "", false, 10); err == nil {
		t.Fatal("expected StreamPodLogsOnServer(0) to fail when db is set")
	}
}

func TestK8sHandler_AuthorizationAndIsolation(t *testing.T) {
	db := setupK8sSecurityTestDB(t)
	svc := newFakeK8sService(t, db)
	h := NewHandler(svc, db)

	// User 1 has NO servers configured; User 2 owns local host server ID 1.
	localSrv := models.DockerServer{
		UserID:  2,
		Name:    "local-host",
		HostURL: "unix:///var/run/docker.sock",
	}
	if err := db.Create(&localSrv).Error; err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	// 1. User 1 (no servers) listing pods must NOT see local cluster pods.
	req := withAuthUser(httptest.NewRequest(http.MethodGet, "/api/k8s/pods", nil), 1, "admin1", "admin")
	rec := httptest.NewRecorder()
	h.ListPodsHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 empty list for unconfigured user, got %d: %s", rec.Code, rec.Body.String())
	}
	var listResp struct {
		Data []models.PodSummary `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(listResp.Data) != 0 {
		t.Fatalf("expected 0 pods for user without hosts, got %d", len(listResp.Data))
	}

	// 2. User 1 attempting IDOR with User 2's server ID must get 403 Forbidden.
	req = withAuthUser(httptest.NewRequest(http.MethodGet, "/api/k8s/pods?serverId=1", nil), 1, "admin1", "admin")
	rec = httptest.NewRecorder()
	h.ListPodsHandler(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden on IDOR pod list, got %d: %s", rec.Code, rec.Body.String())
	}

	// 3. User 1 attempting DeletePod without server must get 403 Forbidden.
	req = withAuthUser(httptest.NewRequest(http.MethodDelete, "/api/k8s/pods/default/secret-local-pod", nil), 1, "admin1", "admin")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("namespace", "default")
	rctx.URLParams.Add("name", "secret-local-pod")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec = httptest.NewRecorder()
	h.DeletePodHandler(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden on delete without host, got %d: %s", rec.Code, rec.Body.String())
	}

	// 4. User 2 (who owns the local host) CAN list pods.
	req = withAuthUser(httptest.NewRequest(http.MethodGet, "/api/k8s/pods", nil), 2, "admin2", "admin")
	rec = httptest.NewRecorder()
	h.ListPodsHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for authorized owner, got %d: %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(listResp.Data) != 1 || listResp.Data[0].Name != "secret-local-pod" {
		t.Fatalf("expected secret-local-pod for authorized owner, got %+v", listResp.Data)
	}
}
