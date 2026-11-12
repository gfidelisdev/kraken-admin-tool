package sidecarauth

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
)

const CanonicalVersionPrefix = "KRKN1"

// HashBytes computes the raw Base64URL-encoded SHA-256 hash of a byte slice
func HashBytes(b []byte) string {
	h := sha256.Sum256(b)
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// BuildCanonicalRequest formats the canonical request string according to the specification:
// KRKN1
// <HTTP_METHOD>
// <PATH>
// plugin_id=<plugin_id>
// installation_id=<installation_id>
// request_id=<request_id>
// timestamp=<timestamp>
// nonce=<nonce>
// content_sha256=<content_sha256>
// context_sha256=<context_sha256>
func BuildCanonicalRequest(
	httpMethod string,
	path string,
	pluginID string,
	installationID string,
	requestID string,
	timestamp string,
	nonce string,
	contentSHA256 string,
	contextSHA256 string,
) string {
	normMethod := strings.ToUpper(strings.TrimSpace(httpMethod))
	normPath := path
	if !strings.HasPrefix(normPath, "/") {
		normPath = "/" + normPath
	}

	lines := []string{
		CanonicalVersionPrefix,
		normMethod,
		normPath,
		fmt.Sprintf("plugin_id=%s", pluginID),
		fmt.Sprintf("installation_id=%s", installationID),
		fmt.Sprintf("request_id=%s", requestID),
		fmt.Sprintf("timestamp=%s", timestamp),
		fmt.Sprintf("nonce=%s", nonce),
		fmt.Sprintf("content_sha256=%s", contentSHA256),
		fmt.Sprintf("context_sha256=%s", contextSHA256),
	}

	return strings.Join(lines, "\n")
}
