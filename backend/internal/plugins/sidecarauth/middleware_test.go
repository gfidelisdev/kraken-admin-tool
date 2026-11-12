package sidecarauth

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSidecarContractAcceptanceCriteria(t *testing.T) {
	pluginID := "com.mpt.ad-collaborator"
	installationID := "2c221f06-3b31-4a49-9683-91e5e618cab8"
	primarySecret, err := GenerateInstanceSecret()
	require.NoError(t, err)

	verifier := NewSidecarVerifier(pluginID, installationID, primarySecret)

	handler := SidecarAuthMiddleware(verifier)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := r.Context().Value(ClaimsContextKey).(*SidecarClaims)
		require.True(t, ok)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ok",
			"user":   claims.KrakenUsername,
			"action": claims.Action,
		})
	}))

	baseBody := []byte(`{"username":"admin@mpt.intra","domain_profile":"default"}`)

	t.Run("1. Valid request is accepted", func(t *testing.T) {
		req, _, err := PrepareAndSignRequest(context.Background(), CallParams{
			TargetURL:      "http://localhost:8080/call",
			Path:           "/call",
			PluginID:       pluginID,
			InstallationID: installationID,
			InstanceSecret: primarySecret,
			UserID:         41,
			Username:       "gfidelis",
			SessionID:      "sess-1234",
			Roles:          []string{"operator"},
			Permissions:    MapRoleToPermissions("operator", nil),
			Action:         "ldap.session.open",
			RequestBody:    baseBody,
		})
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
		assert.Contains(t, rr.Body.String(), `"status":"ok"`)
		assert.Contains(t, rr.Body.String(), `"user":"gfidelis"`)
	})

	t.Run("2. Altered signature is rejected", func(t *testing.T) {
		req, _, err := PrepareAndSignRequest(context.Background(), CallParams{
			TargetURL:      "http://localhost:8080/call",
			Path:           "/call",
			PluginID:       pluginID,
			InstallationID: installationID,
			InstanceSecret: primarySecret,
			UserID:         41,
			Username:       "gfidelis",
			SessionID:      "sess-1234",
			Roles:          []string{"operator"},
			Permissions:    MapRoleToPermissions("operator", nil),
			Action:         "ldap.session.open",
			RequestBody:    baseBody,
		})
		require.NoError(t, err)

		// Tamper with signature
		req.Header.Set(HeaderSignature, "v1=invalid_hmac_signature_value")

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusUnauthorized, rr.Code)
		assert.Contains(t, rr.Body.String(), "INVALID_KRAKEN_SIGNATURE")
	})

	t.Run("3. Altered body after signature is rejected", func(t *testing.T) {
		req, _, err := PrepareAndSignRequest(context.Background(), CallParams{
			TargetURL:      "http://localhost:8080/call",
			Path:           "/call",
			PluginID:       pluginID,
			InstallationID: installationID,
			InstanceSecret: primarySecret,
			UserID:         41,
			Username:       "gfidelis",
			SessionID:      "sess-1234",
			Roles:          []string{"operator"},
			Permissions:    MapRoleToPermissions("operator", nil),
			Action:         "ldap.session.open",
			RequestBody:    baseBody,
		})
		require.NoError(t, err)

		// Tamper with body bytes without recomputing headers
		tamperedBody := []byte(`{"username":"hacker@mpt.intra","domain_profile":"evil"}`)
		req.Body = io.NopCloser(bytes.NewReader(tamperedBody))

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusUnauthorized, rr.Code)
		assert.Contains(t, rr.Body.String(), "INVALID_KRAKEN_SIGNATURE")
	})

	t.Run("4. Expired JWT is rejected", func(t *testing.T) {
		pastTime := time.Now().Add(-1 * time.Hour)
		claims, err := NewSidecarClaims(
			pluginID,
			installationID,
			41,
			"gfidelis",
			"sess-123",
			[]string{"operator"},
			MapRoleToPermissions("operator", nil),
			"ldap.session.open",
			GenerateRequestID(),
			pastTime,
		)
		require.NoError(t, err)

		token, err := CreateSidecarJWT(primarySecret, claims)
		require.NoError(t, err)

		_, err = ParseAndVerifySidecarJWT(token, primarySecret, "", nil, pluginID, installationID, time.Now())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "token has expired")
	})

	t.Run("5. Timestamp outside allowable window (>30s) is rejected", func(t *testing.T) {
		skewedTime := time.Now().Add(-45 * time.Second)
		req, _, err := PrepareAndSignRequest(context.Background(), CallParams{
			TargetURL:      "http://localhost:8080/call",
			Path:           "/call",
			PluginID:       pluginID,
			InstallationID: installationID,
			InstanceSecret: primarySecret,
			UserID:         41,
			Username:       "gfidelis",
			SessionID:      "sess-1234",
			Roles:          []string{"operator"},
			Permissions:    MapRoleToPermissions("operator", nil),
			Action:         "ldap.session.open",
			RequestBody:    baseBody,
			CustomTime:     &skewedTime,
		})
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusUnauthorized, rr.Code)
		assert.Contains(t, rr.Body.String(), "KRAKEN_CONTEXT_EXPIRED")
	})

	t.Run("6. Repeated nonce is rejected (REPLAY_DETECTED)", func(t *testing.T) {
		fixedNonce := "fixed-test-nonce-123"
		req1, _, err := PrepareAndSignRequest(context.Background(), CallParams{
			TargetURL:      "http://localhost:8080/call",
			Path:           "/call",
			PluginID:       pluginID,
			InstallationID: installationID,
			InstanceSecret: primarySecret,
			UserID:         41,
			Username:       "gfidelis",
			SessionID:      "sess-1234",
			Roles:          []string{"operator"},
			Permissions:    MapRoleToPermissions("operator", nil),
			Action:         "ldap.session.open",
			RequestBody:    baseBody,
			CustomNonce:    fixedNonce,
		})
		require.NoError(t, err)

		rr1 := httptest.NewRecorder()
		handler.ServeHTTP(rr1, req1)
		assert.Equal(t, http.StatusOK, rr1.Code)

		// Create second request with different request_id but same nonce
		req2, _, err := PrepareAndSignRequest(context.Background(), CallParams{
			TargetURL:      "http://localhost:8080/call",
			Path:           "/call",
			PluginID:       pluginID,
			InstallationID: installationID,
			InstanceSecret: primarySecret,
			UserID:         41,
			Username:       "gfidelis",
			SessionID:      "sess-1234",
			Roles:          []string{"operator"},
			Permissions:    MapRoleToPermissions("operator", nil),
			Action:         "ldap.session.open",
			RequestBody:    baseBody,
			CustomNonce:    fixedNonce,
		})
		require.NoError(t, err)

		rr2 := httptest.NewRecorder()
		handler.ServeHTTP(rr2, req2)
		assert.Equal(t, http.StatusConflict, rr2.Code)
		assert.Contains(t, rr2.Body.String(), "REPLAY_DETECTED")
	})

	t.Run("7. Repeated request_id is rejected (REPLAY_DETECTED)", func(t *testing.T) {
		fixedReqID := GenerateRequestID()
		req1, _, err := PrepareAndSignRequest(context.Background(), CallParams{
			TargetURL:       "http://localhost:8080/call",
			Path:            "/call",
			PluginID:        pluginID,
			InstallationID:  installationID,
			InstanceSecret:  primarySecret,
			UserID:          41,
			Username:        "gfidelis",
			SessionID:       "sess-1234",
			Roles:           []string{"operator"},
			Permissions:     MapRoleToPermissions("operator", nil),
			Action:          "ldap.session.open",
			RequestBody:     baseBody,
			CustomRequestID: fixedReqID,
		})
		require.NoError(t, err)

		rr1 := httptest.NewRecorder()
		handler.ServeHTTP(rr1, req1)
		assert.Equal(t, http.StatusOK, rr1.Code)

		// Replay exact request
		req1Replayed, _, err := PrepareAndSignRequest(context.Background(), CallParams{
			TargetURL:       "http://localhost:8080/call",
			Path:            "/call",
			PluginID:        pluginID,
			InstallationID:  installationID,
			InstanceSecret:  primarySecret,
			UserID:          41,
			Username:        "gfidelis",
			SessionID:       "sess-1234",
			Roles:           []string{"operator"},
			Permissions:     MapRoleToPermissions("operator", nil),
			Action:          "ldap.session.open",
			RequestBody:     baseBody,
			CustomRequestID: fixedReqID,
		})
		require.NoError(t, err)

		rr2 := httptest.NewRecorder()
		handler.ServeHTTP(rr2, req1Replayed)
		assert.Equal(t, http.StatusConflict, rr2.Code)
		assert.Contains(t, rr2.Body.String(), "REPLAY_DETECTED")
	})

	t.Run("8. Token issued for another plugin is rejected", func(t *testing.T) {
		req, _, err := PrepareAndSignRequest(context.Background(), CallParams{
			TargetURL:      "http://localhost:8080/call",
			Path:           "/call",
			PluginID:       "com.kraken.other-plugin",
			InstallationID: installationID,
			InstanceSecret: primarySecret,
			UserID:         41,
			Username:       "gfidelis",
			SessionID:      "sess-1234",
			Roles:          []string{"operator"},
			Permissions:    MapRoleToPermissions("operator", nil),
			Action:         "ldap.session.open",
			RequestBody:    baseBody,
		})
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusForbidden, rr.Code)
		assert.Contains(t, rr.Body.String(), "PLUGIN_AUDIENCE_MISMATCH")
	})

	t.Run("9. Token issued for another installation is rejected", func(t *testing.T) {
		req, _, err := PrepareAndSignRequest(context.Background(), CallParams{
			TargetURL:      "http://localhost:8080/call",
			Path:           "/call",
			PluginID:       pluginID,
			InstallationID: "other-instance-uuid",
			InstanceSecret: primarySecret,
			UserID:         41,
			Username:       "gfidelis",
			SessionID:      "sess-1234",
			Roles:          []string{"operator"},
			Permissions:    MapRoleToPermissions("operator", nil),
			Action:         "ldap.session.open",
			RequestBody:    baseBody,
		})
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusForbidden, rr.Code)
		assert.Contains(t, rr.Body.String(), "PLUGIN_AUDIENCE_MISMATCH")
	})

	t.Run("10. User without permission is rejected by claims validation", func(t *testing.T) {
		viewerClaims, err := NewSidecarClaims(
			pluginID,
			installationID,
			42,
			"guest-viewer",
			"sess-viewer",
			[]string{"viewer"},
			MapRoleToPermissions("viewer", nil),
			"collaborators.create",
			GenerateRequestID(),
			time.Now(),
		)
		require.NoError(t, err)
		assert.False(t, viewerClaims.CheckActionPermission("collaborators.create"))
		assert.False(t, viewerClaims.CheckActionPermission("ldap.session.open"))

		// Operator CAN create collaborator
		operatorClaims, err := NewSidecarClaims(
			pluginID,
			installationID,
			41,
			"gfidelis",
			"sess-operator",
			[]string{"operator"},
			MapRoleToPermissions("operator", nil),
			"collaborators.create",
			GenerateRequestID(),
			time.Now(),
		)
		require.NoError(t, err)
		assert.True(t, operatorClaims.CheckActionPermission("collaborators.create"))
	})

	t.Run("11. Direct connection without signature receives 401", func(t *testing.T) {
		req := httptest.NewRequest("POST", "http://localhost:8080/call", bytes.NewReader(baseBody))
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusUnauthorized, rr.Code)
		assert.Contains(t, rr.Body.String(), "INVALID_KRAKEN_SIGNATURE")
	})

	t.Run("12. Header X-Kraken-Role: admin alone without signature receives 401", func(t *testing.T) {
		req := httptest.NewRequest("POST", "http://localhost:8080/call", bytes.NewReader(baseBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Kraken-Role", "admin")
		req.Header.Set("X-Kraken-User", "hacker")

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusUnauthorized, rr.Code)
		assert.Contains(t, rr.Body.String(), "INVALID_KRAKEN_SIGNATURE")
	})

	t.Run("13. Secret rotation: accepts previous key within 60s window", func(t *testing.T) {
		newSecret, prevSecret, rotatedAt, err := RotateSecret(primarySecret)
		require.NoError(t, err)

		// Create rotated verifier
		rotVerifier := NewSidecarVerifier(pluginID, installationID, newSecret)
		rotVerifier.PreviousSecret = prevSecret
		rotVerifier.SecretRotatedAt = &rotatedAt

		rotHandler := SidecarAuthMiddleware(rotVerifier)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		// Request signed with previous secret
		req, _, err := PrepareAndSignRequest(context.Background(), CallParams{
			TargetURL:      "http://localhost:8080/call",
			Path:           "/call",
			PluginID:       pluginID,
			InstallationID: installationID,
			InstanceSecret: prevSecret,
			UserID:         41,
			Username:       "gfidelis",
			SessionID:      "sess-1234",
			Roles:          []string{"operator"},
			Permissions:    MapRoleToPermissions("operator", nil),
			Action:         "ldap.session.open",
			RequestBody:    baseBody,
		})
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		rotHandler.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code)
	})

	t.Run("14. Secret rotation: rejects previous key after 60s window", func(t *testing.T) {
		newSecret, prevSecret, _, err := RotateSecret(primarySecret)
		require.NoError(t, err)

		oldRotatedAt := time.Now().Add(-65 * time.Second)
		rotVerifier := NewSidecarVerifier(pluginID, installationID, newSecret)
		rotVerifier.PreviousSecret = prevSecret
		rotVerifier.SecretRotatedAt = &oldRotatedAt

		rotHandler := SidecarAuthMiddleware(rotVerifier)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req, _, err := PrepareAndSignRequest(context.Background(), CallParams{
			TargetURL:      "http://localhost:8080/call",
			Path:           "/call",
			PluginID:       pluginID,
			InstallationID: installationID,
			InstanceSecret: prevSecret,
			UserID:         41,
			Username:       "gfidelis",
			SessionID:      "sess-1234",
			Roles:          []string{"operator"},
			Permissions:    MapRoleToPermissions("operator", nil),
			Action:         "ldap.session.open",
			RequestBody:    baseBody,
		})
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		rotHandler.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusUnauthorized, rr.Code)
	})

	t.Run("15. JTI mismatch with request_id is rejected", func(t *testing.T) {
		req, _, err := PrepareAndSignRequest(context.Background(), CallParams{
			TargetURL:      "http://localhost:8080/call",
			Path:           "/call",
			PluginID:       pluginID,
			InstallationID: installationID,
			InstanceSecret: primarySecret,
			UserID:         41,
			Username:       "gfidelis",
			SessionID:      "sess-1234",
			Roles:          []string{"operator"},
			Permissions:    MapRoleToPermissions("operator", nil),
			Action:         "ldap.session.open",
			RequestBody:    baseBody,
		})
		require.NoError(t, err)

		// Mismatch request_id in header
		req.Header.Set(HeaderRequestID, GenerateRequestID())

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusUnauthorized, rr.Code)
	})

	t.Run("16. Redaction of sensitive fields in logging map", func(t *testing.T) {
		payload := map[string]interface{}{
			"username":           "user1",
			"password":           "super-secret-password",
			"temporary_password": "temp-one-time-pwd",
			"token":              "bearer-abc",
			"nested": map[string]interface{}{
				"client_secret": "nested-secret",
				"info":          "public-info",
			},
		}

		redacted := RedactSensitiveMap(payload)
		assert.Equal(t, "user1", redacted["username"])
		assert.Equal(t, "[REDACTED]", redacted["password"])
		assert.Equal(t, "[REDACTED]", redacted["temporary_password"])
		assert.Equal(t, "[REDACTED]", redacted["token"])

		nestedMap := redacted["nested"].(map[string]interface{})
		assert.Equal(t, "[REDACTED]", nestedMap["client_secret"])
		assert.Equal(t, "public-info", nestedMap["info"])
	})

	t.Run("17. Nonce and Request ID generation are valid and unique", func(t *testing.T) {
		nonce1 := GenerateNonce()
		nonce2 := GenerateNonce()
		assert.NotEmpty(t, nonce1)
		assert.NotEmpty(t, nonce2)
		assert.NotEqual(t, nonce1, nonce2)

		reqID1 := GenerateRequestID()
		reqID2 := GenerateRequestID()
		assert.NotEmpty(t, reqID1)
		assert.NotEmpty(t, reqID2)
		assert.NotEqual(t, reqID1, reqID2)
	})

	t.Run("18. Session binding hash is deterministic and non-reversible", func(t *testing.T) {
		sess1 := ComputeKrakenSessionID("session-uuid-1")
		sess1Again := ComputeKrakenSessionID("session-uuid-1")
		sess2 := ComputeKrakenSessionID("session-uuid-2")

		assert.Equal(t, sess1, sess1Again)
		assert.NotEqual(t, sess1, sess2)
		assert.NotContains(t, sess1, "session-uuid-1")
	})

	t.Run("19. network:ldap permission enforcement and claims delegation", func(t *testing.T) {
		// Plugin with network:ldap granted
		withLDAP := MapRoleToPermissions("operator", []string{"network:ldap"})
		claimsWithLDAP, err := NewSidecarClaims(
			pluginID,
			installationID,
			42,
			"ldap-operator",
			"sess-ldap-1",
			[]string{"operator"},
			withLDAP,
			"ldap.session.open",
			GenerateRequestID(),
			time.Now(),
		)
		require.NoError(t, err)
		assert.True(t, claimsWithLDAP.CheckActionPermission("ldap.session.open"))
		assert.True(t, claimsWithLDAP.CheckActionPermission("ldap.bind"))
		assert.True(t, claimsWithLDAP.CheckActionPermission("ad.session.open"))
		assert.True(t, claimsWithLDAP.CheckActionPermission("ad.managers.search"))
		assert.True(t, claimsWithLDAP.CheckActionPermission("collaborators.create"))
		assert.True(t, claimsWithLDAP.CheckActionPermission("network:ldap"))

		// Plugin WITHOUT network:ldap granted (only docker:read)
		withoutLDAP := MapRoleToPermissions("operator", []string{"docker:read"})
		claimsWithoutLDAP, err := NewSidecarClaims(
			pluginID,
			installationID,
			43,
			"non-ldap-user",
			"sess-ldap-2",
			[]string{"operator"},
			withoutLDAP,
			"ldap.session.open",
			GenerateRequestID(),
			time.Now(),
		)
		require.NoError(t, err)
		assert.False(t, claimsWithoutLDAP.CheckActionPermission("ldap.session.open"))
		assert.False(t, claimsWithoutLDAP.CheckActionPermission("ldap.bind"))
		assert.False(t, claimsWithoutLDAP.CheckActionPermission("ad.session.open"))
		assert.False(t, claimsWithoutLDAP.CheckActionPermission("collaborators.create"))
		assert.False(t, claimsWithoutLDAP.CheckActionPermission("network:ldap"))
	})
}
