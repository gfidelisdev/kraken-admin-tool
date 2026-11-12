package sidecarauth

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"
)

type contextKey string

const ClaimsContextKey contextKey = "sidecar_claims"

// ProblemDetails represents an RFC 7807 structured error response
type ProblemDetails struct {
	Type      string `json:"type"`
	Title     string `json:"title"`
	Status    int    `json:"status"`
	Code      string `json:"code"`
	RequestID string `json:"request_id,omitempty"`
	Retryable bool   `json:"retryable"`
}

// SidecarVerifier enforces the cryptographic and anti-replay verification on the sidecar
type SidecarVerifier struct {
	PluginID        string
	InstallationID  string
	PrimarySecret   string
	PreviousSecret  string
	SecretRotatedAt *time.Time
	ReplayCache     *ReplayCache
	TimeSource      func() time.Time
}

// NewSidecarVerifier creates an initialized SidecarVerifier
func NewSidecarVerifier(pluginID, installationID, primarySecret string) *SidecarVerifier {
	return &SidecarVerifier{
		PluginID:       pluginID,
		InstallationID: installationID,
		PrimarySecret:  primarySecret,
		ReplayCache:    NewReplayCache(),
		TimeSource:     time.Now,
	}
}

// WriteProblemResponse sends an application/problem+json response
func WriteProblemResponse(w http.ResponseWriter, status int, code, title, reqID string, retryable bool) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)

	prob := ProblemDetails{
		Type:      "urn:kraken:plugin:error:" + code,
		Title:     title,
		Status:    status,
		Code:      code,
		RequestID: reqID,
		Retryable: retryable,
	}
	_ = json.NewEncoder(w).Encode(prob)
}

// VerifyRequest authenticates and verifies the incoming HTTP request against the Kraken sidecar contract
func (v *SidecarVerifier) VerifyRequest(r *http.Request) (*SidecarClaims, []byte, error) {
	now := v.TimeSource().UTC()

	// 1. Enforce rejection of legacy unauthenticated headers
	// If legacy headers are present without signature or if someone tries to forge identity
	reqSig := r.Header.Get(HeaderSignature)
	if reqSig == "" {
		return nil, nil, &verificationError{
			status: http.StatusUnauthorized,
			code:   "INVALID_KRAKEN_SIGNATURE",
			title:  "Missing mandatory X-Kraken-Signature header",
		}
	}

	// 2. Read mandatory headers
	pluginID := r.Header.Get(HeaderPluginID)
	installationID := r.Header.Get(HeaderInstallationID)
	requestID := r.Header.Get(HeaderRequestID)
	timestampStr := r.Header.Get(HeaderTimestamp)
	nonce := r.Header.Get(HeaderNonce)
	userContextJWT := r.Header.Get(HeaderUserContext)
	contentSHA256 := r.Header.Get(HeaderContentSHA256)

	if pluginID == "" || installationID == "" || requestID == "" || timestampStr == "" || nonce == "" || userContextJWT == "" || contentSHA256 == "" {
		return nil, nil, &verificationError{
			status: http.StatusBadRequest,
			code:   "INVALID_REQUEST",
			title:  "Missing required Kraken authentication headers",
			reqID:  requestID,
		}
	}

	// 3. Match Plugin ID and Installation ID
	if pluginID != v.PluginID {
		return nil, nil, &verificationError{
			status: http.StatusForbidden,
			code:   "PLUGIN_AUDIENCE_MISMATCH",
			title:  "Plugin ID does not match target sidecar",
			reqID:  requestID,
		}
	}
	if installationID != v.InstallationID {
		return nil, nil, &verificationError{
			status: http.StatusForbidden,
			code:   "PLUGIN_AUDIENCE_MISMATCH",
			title:  "Installation ID does not match target sidecar instance",
			reqID:  requestID,
		}
	}

	// 4. Validate timestamp parsing & anti-replay
	ts, err := time.Parse(time.RFC3339, timestampStr)
	if err != nil {
		return nil, nil, &verificationError{
			status: http.StatusBadRequest,
			code:   "INVALID_REQUEST",
			title:  "Malformed timestamp format (must be RFC3339)",
			reqID:  requestID,
		}
	}

	if err := v.ReplayCache.CheckAndRecord(installationID, requestID, nonce, ts, now); err != nil {
		if err == ErrReplayDetected {
			return nil, nil, &verificationError{
				status: http.StatusConflict,
				code:   "REPLAY_DETECTED",
				title:  "Request rejected: replay detected",
				reqID:  requestID,
			}
		}
		return nil, nil, &verificationError{
			status: http.StatusUnauthorized,
			code:   "KRAKEN_CONTEXT_EXPIRED",
			title:  "Timestamp outside allowable window",
			reqID:  requestID,
		}
	}

	// 5. Read and hash request body
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, nil, &verificationError{
			status: http.StatusBadRequest,
			code:   "INVALID_REQUEST",
			title:  "Failed to read request body",
			reqID:  requestID,
		}
	}
	// Restore body for downstream handlers
	r.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	actualContentSHA := HashBytes(bodyBytes)
	if actualContentSHA != contentSHA256 {
		return nil, nil, &verificationError{
			status: http.StatusUnauthorized,
			code:   "INVALID_KRAKEN_SIGNATURE",
			title:  "Content SHA-256 header does not match actual request body",
			reqID:  requestID,
		}
	}

	// 6. Compute Context SHA and build Canonical Request
	actualContextSHA := HashBytes([]byte(userContextJWT))
	canonical := BuildCanonicalRequest(
		r.Method,
		r.URL.Path,
		pluginID,
		installationID,
		requestID,
		timestampStr,
		nonce,
		contentSHA256,
		actualContextSHA,
	)

	// 7. Verify Signature
	if err := VerifySignature(reqSig, canonical, v.PrimarySecret, v.PreviousSecret, v.SecretRotatedAt, now); err != nil {
		return nil, nil, &verificationError{
			status: http.StatusUnauthorized,
			code:   "INVALID_KRAKEN_SIGNATURE",
			title:  "Cryptographic signature verification failed: " + err.Error(),
			reqID:  requestID,
		}
	}

	// 8. Verify and parse internal context JWT
	claims, err := ParseAndVerifySidecarJWT(userContextJWT, v.PrimarySecret, v.PreviousSecret, v.SecretRotatedAt, v.PluginID, v.InstallationID, now)
	if err != nil {
		return nil, nil, &verificationError{
			status: http.StatusUnauthorized,
			code:   "KRAKEN_CONTEXT_EXPIRED",
			title:  "Invalid or expired JWT context: " + err.Error(),
			reqID:  requestID,
		}
	}

	// 9. Verify JTI equals RequestID
	if claims.JTI != requestID {
		return nil, nil, &verificationError{
			status: http.StatusBadRequest,
			code:   "INVALID_REQUEST",
			title:  "Claims JTI must match X-Kraken-Request-ID",
			reqID:  requestID,
		}
	}

	return claims, bodyBytes, nil
}

// SidecarAuthMiddleware returns an HTTP middleware enforcing the sidecar contract
func SidecarAuthMiddleware(verifier *SidecarVerifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, _, err := verifier.VerifyRequest(r)
			if err != nil {
				if vErr, ok := err.(*verificationError); ok {
					WriteProblemResponse(w, vErr.status, vErr.code, vErr.title, vErr.reqID, false)
					return
				}
				WriteProblemResponse(w, http.StatusUnauthorized, "UNAUTHORIZED", err.Error(), "", false)
				return
			}

			// Pass verified claims in context
			ctx := context.WithValue(r.Context(), ClaimsContextKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

type verificationError struct {
	status int
	code   string
	title  string
	reqID  string
}

func (e *verificationError) Error() string {
	return e.title
}
