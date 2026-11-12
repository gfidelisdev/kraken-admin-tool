package auth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kraken/admin-tool/backend/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createDummyJWT generates a dummy base64url-encoded JWT with given payload claims
func createDummyJWT(claims map[string]interface{}) string {
	headerJSON := []byte(`{"alg":"none","typ":"JWT"}`)
	payloadJSON, _ := json.Marshal(claims)

	h64 := base64.RawURLEncoding.EncodeToString(headerJSON)
	p64 := base64.RawURLEncoding.EncodeToString(payloadJSON)
	return h64 + "." + p64 + "."
}

func TestMicrosoftSessionBroker_DeviceLoginFlow(t *testing.T) {
	db := setupTestDB(t)
	jwtSecret := "test-jwt-secret-key-32bytes-long!"
	handler := NewHandler(db, jwtSecret, false)

	// Mock Microsoft Entra HTTP server
	pollCount := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/common/oauth2/v2.0/devicecode":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"device_code":      "mock-device-code-123",
				"user_code":        "ABCD-EFGH",
				"verification_uri": "https://microsoft.com/devicelogin",
				"expires_in":       900,
				"interval":         5,
				"message":          "To sign in, open https://microsoft.com/devicelogin and enter ABCD-EFGH",
			})
		case r.URL.Path == "/common/oauth2/v2.0/token", r.URL.Path == "/mock-tenant-id-789/oauth2/v2.0/token":
			_ = r.ParseForm()
			grantType := r.FormValue("grant_type")
			if grantType == "urn:ietf:params:oauth:grant-type:device_code" {
				pollCount++
				if pollCount == 1 {
					// Case 1a: authorization_pending
					w.WriteHeader(http.StatusBadRequest)
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"error":             "authorization_pending",
						"error_description": "User has not yet authorized",
					})
					return
				} else if pollCount == 2 {
					// Case 1b: slow_down
					w.WriteHeader(http.StatusBadRequest)
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"error":             "slow_down",
						"error_description": "Polling too fast",
					})
					return
				}

				// Completed
				idToken := createDummyJWT(map[string]interface{}{
					"email": "alice@contoso.com",
					"name":  "Alice Contoso",
					"tid":   "mock-tenant-id-789",
					"oid":   "user-oid-111",
				})
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"access_token":  "mock-access-token-vault",
					"refresh_token": "mock-refresh-token-456",
					"id_token":      idToken,
					"expires_in":    3600,
				})
			} else if grantType == "refresh_token" {
				// Scoped token exchange
				scope := r.FormValue("scope")
				respData, _ := json.Marshal(map[string]interface{}{
					"access_token":  "mock-scoped-token-for-" + scope,
					"refresh_token": "mock-rotated-refresh-token-999",
					"expires_in":    3600,
				})
				_, _ = w.Write(respData)
				return
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer mockServer.Close()

	handler.MSBroker.EntraBaseURL = mockServer.URL
	handler.MSBroker.HTTPClient = mockServer.Client()

	// 1. Device Code Initiation
	initReq := httptest.NewRequest(http.MethodPost, "/api/auth/microsoft/device-code", strings.NewReader(`{}`))
	initRec := httptest.NewRecorder()
	handler.StartMicrosoftDeviceLogin(initRec, initReq)

	assert.Equal(t, http.StatusOK, initRec.Code)
	var initResp models.MicrosoftDeviceCodeResponse
	err := json.Unmarshal(initRec.Body.Bytes(), &initResp)
	require.NoError(t, err)
	assert.Equal(t, "mock-device-code-123", initResp.DeviceCode)
	assert.Equal(t, "ABCD-EFGH", initResp.UserCode)

	// 2. Poll #1 -> pending_authorization
	pollBody, _ := json.Marshal(models.MicrosoftPollRequest{DeviceCode: "mock-device-code-123"})
	pollReq1 := httptest.NewRequest(http.MethodPost, "/api/auth/microsoft/poll", bytes.NewReader(pollBody))
	pollRec1 := httptest.NewRecorder()
	handler.PollMicrosoftDeviceLogin(pollRec1, pollReq1)

	assert.Equal(t, http.StatusOK, pollRec1.Code)
	var pollResp1 models.MicrosoftPollResponse
	require.NoError(t, json.Unmarshal(pollRec1.Body.Bytes(), &pollResp1))
	assert.Equal(t, "pending_authorization", pollResp1.Status)

	// 3. Poll #2 -> slow_down
	pollReq2 := httptest.NewRequest(http.MethodPost, "/api/auth/microsoft/poll", bytes.NewReader(pollBody))
	pollRec2 := httptest.NewRecorder()
	handler.PollMicrosoftDeviceLogin(pollRec2, pollReq2)

	assert.Equal(t, http.StatusOK, pollRec2.Code)
	var pollResp2 models.MicrosoftPollResponse
	require.NoError(t, json.Unmarshal(pollRec2.Body.Bytes(), &pollResp2))
	assert.Equal(t, "slow_down", pollResp2.Status)
	assert.Equal(t, 5, pollResp2.Interval)

	// 4. Poll #3 -> Unrecognized email provisions user with Status = "pending", returns HTTP 403
	pollReq3 := httptest.NewRequest(http.MethodPost, "/api/auth/microsoft/poll", bytes.NewReader(pollBody))
	pollRec3 := httptest.NewRecorder()
	handler.PollMicrosoftDeviceLogin(pollRec3, pollReq3)

	assert.Equal(t, http.StatusForbidden, pollRec3.Code)
	var pollResp3 models.MicrosoftPollResponse
	require.NoError(t, json.Unmarshal(pollRec3.Body.Bytes(), &pollResp3))
	assert.Equal(t, "pending", pollResp3.Status)

	var createdUser models.User
	err = db.Where("email = ?", "alice@contoso.com").First(&createdUser).Error
	require.NoError(t, err)
	assert.Equal(t, "pending", createdUser.Status)
	assert.Equal(t, "pending", createdUser.Role)

	// 5. Admin approves user -> Status = "active"
	createdUser.Status = "active"
	createdUser.Role = "operator"
	require.NoError(t, db.Save(&createdUser).Error)

	// Reset pollCount to complete immediately
	pollCount = 2
	pollReq4 := httptest.NewRequest(http.MethodPost, "/api/auth/microsoft/poll", bytes.NewReader(pollBody))
	pollRec4 := httptest.NewRecorder()
	handler.PollMicrosoftDeviceLogin(pollRec4, pollReq4)

	assert.Equal(t, http.StatusOK, pollRec4.Code)
	var pollResp4 models.MicrosoftPollResponse
	require.NoError(t, json.Unmarshal(pollRec4.Body.Bytes(), &pollResp4))
	assert.Equal(t, "authenticated", pollResp4.Status)
	assert.NotEmpty(t, pollResp4.Token)
	require.NotNil(t, pollResp4.User)
	assert.Equal(t, "microsoft", pollResp4.User.AuthProvider)
	require.NotNil(t, pollResp4.User.MicrosoftAccount)
	assert.Equal(t, "alice@contoso.com", pollResp4.User.MicrosoftAccount.Email)
	assert.Equal(t, "kraken_session", pollResp4.User.MicrosoftAccount.Source)

	// Validate JWT claims
	claims, err := ValidateToken(pollResp4.Token, jwtSecret)
	require.NoError(t, err)
	assert.Equal(t, "microsoft", claims.AuthProvider)
	assert.NotEmpty(t, claims.ID) // Session ID / jti

	// 6. Test GET /api/auth/me returns microsoft_account metadata
	meReq := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	ctx := context.WithValue(meReq.Context(), UserContextKey, claims)
	meRec := httptest.NewRecorder()
	handler.Me(meRec, meReq.WithContext(ctx))

	assert.Equal(t, http.StatusOK, meRec.Code)
	var meUser models.UserResponse
	require.NoError(t, json.Unmarshal(meRec.Body.Bytes(), &meUser))
	assert.Equal(t, "microsoft", meUser.AuthProvider)
	require.NotNil(t, meUser.MicrosoftAccount)
	assert.Equal(t, "alice@contoso.com", meUser.MicrosoftAccount.Email)

	// 7. Test GetScopedToken
	token, acc, err := handler.MSBroker.GetScopedToken(context.Background(), claims.ID, "https://management.azure.com/.default")
	require.NoError(t, err)
	assert.Contains(t, token, "mock-scoped-token-for-https://management.azure.com/.default")
	require.NotNil(t, acc)
	assert.Equal(t, "alice@contoso.com", acc.Email)

	// 7b. Test GetScopedTokenForUser (resolves token even without knowing session ID, e.g. for background host tasks)
	tokenUser, accUser, errUser := handler.MSBroker.GetScopedTokenForUser(context.Background(), claims.UserID, "https://management.azure.com/.default")
	require.NoError(t, errUser)
	assert.Equal(t, token, tokenUser)
	require.NotNil(t, accUser)
	assert.Equal(t, "alice@contoso.com", accUser.Email)

	// 8. Test Logout evicts session
	logoutReq := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	logoutRec := httptest.NewRecorder()
	handler.Logout(logoutRec, logoutReq.WithContext(ctx))
	assert.Equal(t, http.StatusOK, logoutRec.Code)

	// After logout, GetAccountMetadata returns nil
	assert.Nil(t, handler.MSBroker.GetAccountMetadata(claims.ID))

	// And GET /api/auth/me downgrades AuthProvider to "local"
	meRec2 := httptest.NewRecorder()
	handler.Me(meRec2, meReq.WithContext(ctx))
	assert.Equal(t, http.StatusOK, meRec2.Code)
	var meUser2 models.UserResponse
	require.NoError(t, json.Unmarshal(meRec2.Body.Bytes(), &meUser2))
	assert.Equal(t, "local", meUser2.AuthProvider)
	assert.Nil(t, meUser2.MicrosoftAccount)
}
