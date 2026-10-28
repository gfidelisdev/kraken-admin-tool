package registry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNormalizeRepo(t *testing.T) {
	tests := []struct {
		input    string
		wantRepo string
		wantTag  string
	}{
		{"redis", "library/redis", "latest"},
		{"redis:7.0.12-alpine", "library/redis", "7.0.12-alpine"},
		{"grafana/grafana:10.2.0", "grafana/grafana", "10.2.0"},
		{"docker.io/library/nginx:1.25.4", "library/nginx", "1.25.4"},
	}

	for _, tt := range tests {
		repo, tag := NormalizeRepo(tt.input)
		if repo != tt.wantRepo || tag != tt.wantTag {
			t.Errorf("NormalizeRepo(%q) = (%q, %q), want (%q, %q)", tt.input, repo, tag, tt.wantRepo, tt.wantTag)
		}
	}
}

func TestParseSemverTag(t *testing.T) {
	st, ok := ParseSemverTag("7.0.12-alpine", "sha256:123", nil)
	if !ok {
		t.Fatalf("expected 7.0.12-alpine to parse successfully")
	}
	if st.Major != 7 || st.Minor != 0 || st.Patch != 12 || st.Variant != "-alpine" {
		t.Errorf("unexpected parsed semver: %+v", st)
	}

	st2, ok2 := ParseSemverTag("1.25.4", "", nil)
	if !ok2 || st2.Major != 1 || st2.Minor != 25 || st2.Patch != 4 || st2.Variant != "" {
		t.Errorf("unexpected parsed semver: %+v", st2)
	}

	_, okRC := ParseSemverTag("8.0-rc1", "", nil)
	if okRC {
		t.Errorf("expected release candidate to be ignored")
	}
}

func TestCompareVersions(t *testing.T) {
	v1, _ := ParseSemverTag("7.0.12", "", nil)
	v2, _ := ParseSemverTag("7.0.15", "", nil)
	v3, _ := ParseSemverTag("7.2.4", "", nil)

	if CompareVersions(v1, v2) >= 0 {
		t.Errorf("expected v1 < v2")
	}
	if CompareVersions(v2, v3) >= 0 {
		t.Errorf("expected v2 < v3")
	}
}

func TestCheckUpdates(t *testing.T) {
	mockTags := `{
		"count": 4,
		"results": [
			{"name": "7.0.12-alpine", "digest": "sha256:current"},
			{"name": "7.0.15-alpine", "digest": "sha256:patch"},
			{"name": "7.2.4-alpine", "digest": "sha256:major"},
			{"name": "7.2.4-bullseye", "digest": "sha256:diffvariant"}
		]
	}`

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(mockTags))
	}))
	defer ts.Close()

	client := &Client{
		httpClient: ts.Client(),
		baseURL:    ts.URL,
	}

	info, err := client.CheckUpdates(context.Background(), "redis:7.0.12-alpine", "sha256:current", time.Now())
	if err != nil {
		t.Fatalf("CheckUpdates failed: %v", err)
	}

	if !info.RetainHasUpdate || info.RetainTag != "7.0.15-alpine" {
		t.Errorf("expected RetainTag 7.0.15-alpine, got %s (hasUpdate: %v)", info.RetainTag, info.RetainHasUpdate)
	}

	if !info.FullHasUpdate || info.FullTag != "7.2.4-alpine" {
		t.Errorf("expected FullTag 7.2.4-alpine, got %s (hasUpdate: %v)", info.FullTag, info.FullHasUpdate)
	}
}
