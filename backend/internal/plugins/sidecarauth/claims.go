package sidecarauth

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	IssuerKrakenControlPlane = "kraken-control-plane"
	MaxTokenLifetimeSecs    = 30
	ClockToleranceSecs      = 5
)

// InternalSessionPepper is a backend secret pepper used to hash session identifiers
var InternalSessionPepper = "kraken-sidecar-session-pepper-v1"

// SidecarClaims represents the cryptographically signed, short-lived JWT context claims
type SidecarClaims struct {
	Issuer           string   `json:"iss"`
	Audience         string   `json:"aud"`
	Subject          string   `json:"sub"`
	JTI              string   `json:"jti"`
	IssuedAt         int64    `json:"iat"`
	NotBefore        int64    `json:"nbf"`
	ExpiresAt        int64    `json:"exp"`
	PluginID         string   `json:"plugin_id"`
	InstallationID   string   `json:"installation_id"`
	KrakenUserID     uint     `json:"kraken_user_id"`
	KrakenUsername   string   `json:"kraken_username"`
	KrakenSessionID  string   `json:"kraken_session_id"`
	Roles            []string `json:"roles"`
	Permissions      []string `json:"permissions"`
	Action           string   `json:"action"`
	RequestID        string   `json:"request_id"`
	jwt.RegisteredClaims
}

// ComputeKrakenSessionID computes the non-reversible, session-bound hash identifier:
// base64url(SHA-256(internal_pepper || session_id))
func ComputeKrakenSessionID(rawSessionID string) string {
	if rawSessionID == "" {
		rawSessionID = "anonymous-session"
	}
	h := sha256.New()
	h.Write([]byte(InternalSessionPepper))
	h.Write([]byte(rawSessionID))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

// MapRoleToPermissions resolves effective internal permissions for a given Kraken role and manifest permissions
func MapRoleToPermissions(role string, grantedPermissions []string) []string {
	permSet := make(map[string]bool)

	// Add granted permissions from manifest
	for _, p := range grantedPermissions {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			permSet[trimmed] = true
		}
	}

	normRole := strings.ToLower(strings.TrimSpace(role))
	// If the plugin has network:ldap granted or no permissions restriction was passed (e.g. testing defaults)
	hasLDAP := permSet["network:ldap"] || len(grantedPermissions) == 0

	switch normRole {
	case "admin":
		if hasLDAP {
			permSet["ad:authenticate"] = true
			permSet["ad:read"] = true
			permSet["ad:create-collaborator"] = true
			permSet["ad:session:close"] = true
			permSet["ad:config:read"] = true
			permSet["ad:config:write"] = true
			permSet["ad:audit:read"] = true
		}
	case "operator":
		if hasLDAP {
			permSet["ad:authenticate"] = true
			permSet["ad:read"] = true
			permSet["ad:create-collaborator"] = true
			permSet["ad:session:close"] = true
			permSet["ad:config:read"] = true
		}
	case "viewer":
		// Viewer has no administrative or AD action permissions
	default:
		// Default to viewer privileges
	}

	perms := make([]string, 0, len(permSet))
	for p := range permSet {
		perms = append(perms, p)
	}
	return perms
}

// CheckActionPermission verifies whether the given action is authorized by the claims' permissions
func (c *SidecarClaims) CheckActionPermission(action string) bool {
	permMap := make(map[string]bool, len(c.Permissions))
	for _, p := range c.Permissions {
		permMap[p] = true
	}

	hasAdmin := false
	for _, r := range c.Roles {
		if strings.EqualFold(r, "admin") {
			hasAdmin = true
			break
		}
	}

	// Internal AD permission mapping rules
	switch action {
	case "ldap.session.open", "ldap.bind", "ad.session.open":
		return permMap["ad:authenticate"] || permMap["network:ldap"]
	case "ad.managers.search", "ad.ous.list", "ad.logins.check", "ad.collaborators.validate", "ad.collaborators.review", "ad.read":
		return permMap["ad:read"] || permMap["network:ldap"]
	case "collaborators.create", "ad.collaborator.create":
		return permMap["ad:create-collaborator"] || permMap["network:ldap"]
	case "ldap.session.close", "ad.session.close":
		return permMap["ad:session:close"] || permMap["network:ldap"]
	case "config.read", "ad.config.read":
		return permMap["ad:config:read"] || permMap["network:ldap"]
	case "config.write", "ad.config.write":
		return permMap["ad:config:write"] || (permMap["network:ldap"] && hasAdmin)
	default:
		// For other actions, allow if explicitly granted or if user is admin
		if permMap[action] {
			return true
		}
		// Allow LDAP/AD related operations if network:ldap is explicitly granted
		if (strings.HasPrefix(action, "ldap.") || strings.HasPrefix(action, "ad.") || strings.HasPrefix(action, "network.ldap")) && permMap["network:ldap"] {
			return true
		}
		return hasAdmin
	}
}

// NewSidecarClaims creates validated claims conforming to the specification
func NewSidecarClaims(
	pluginID string,
	installationID string,
	userID uint,
	username string,
	rawSessionID string,
	roles []string,
	permissions []string,
	action string,
	requestID string,
	now time.Time,
) (*SidecarClaims, error) {
	if pluginID == "" {
		return nil, errors.New("pluginID is required")
	}
	if installationID == "" {
		return nil, errors.New("installationID is required")
	}
	if requestID == "" {
		return nil, errors.New("requestID is required")
	}

	nowUnix := now.Unix()
	claims := &SidecarClaims{
		Issuer:          IssuerKrakenControlPlane,
		Audience:        "plugin:" + pluginID,
		Subject:         "user:" + strconv.Itoa(int(userID)),
		JTI:             requestID,
		IssuedAt:        nowUnix,
		NotBefore:       nowUnix - ClockToleranceSecs,
		ExpiresAt:       nowUnix + MaxTokenLifetimeSecs,
		PluginID:        pluginID,
		InstallationID:  installationID,
		KrakenUserID:    userID,
		KrakenUsername:  username,
		KrakenSessionID: ComputeKrakenSessionID(rawSessionID),
		Roles:           roles,
		Permissions:     permissions,
		Action:          action,
		RequestID:       requestID,
	}

	return claims, nil
}

// CreateSidecarJWT signs claims with HS256 using the instance secret
func CreateSidecarJWT(secret string, claims *SidecarClaims) (string, error) {
	if secret == "" {
		return "", errors.New("secret cannot be empty")
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// ParseAndVerifySidecarJWT validates and parses the short-lived JWT, supporting secret rotation
func ParseAndVerifySidecarJWT(
	tokenStr string,
	primarySecret string,
	previousSecret string,
	rotatedAt *time.Time,
	expectedPluginID string,
	expectedInstallationID string,
	now time.Time,
) (*SidecarClaims, error) {
	if tokenStr == "" {
		return nil, errors.New("token is empty")
	}

	keyFunc := func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(primarySecret), nil
	}

	claims := &SidecarClaims{}
	parsedToken, err := jwt.ParseWithClaims(tokenStr, claims, keyFunc)

	// If primary secret failed and rotation is active, attempt verification with previous secret
	if err != nil && previousSecret != "" && IsSecretRotationGraceActive(rotatedAt, now) {
		altKeyFunc := func(token *jwt.Token) (interface{}, error) {
			return []byte(previousSecret), nil
		}
		claims = &SidecarClaims{}
		parsedToken, err = jwt.ParseWithClaims(tokenStr, claims, altKeyFunc)
	}

	if err != nil || !parsedToken.Valid {
		return nil, fmt.Errorf("invalid or expired JWT context: %w", err)
	}

	// Validate audience
	expectedAud := "plugin:" + expectedPluginID
	if claims.Audience != expectedAud {
		return nil, fmt.Errorf("plugin audience mismatch: expected '%s', got '%s'", expectedAud, claims.Audience)
	}

	// Validate installation ID
	if claims.InstallationID != expectedInstallationID {
		return nil, fmt.Errorf("installation ID mismatch: expected '%s', got '%s'", expectedInstallationID, claims.InstallationID)
	}

	// Validate issuer
	if claims.Issuer != IssuerKrakenControlPlane {
		return nil, fmt.Errorf("invalid issuer: %s", claims.Issuer)
	}

	// Validate lifetime
	if claims.ExpiresAt-claims.IssuedAt > MaxTokenLifetimeSecs+ClockToleranceSecs {
		return nil, errors.New("token lifetime exceeds maximum allowed duration")
	}

	// Validate clock expiration with tolerance
	nowUnix := now.Unix()
	if nowUnix > claims.ExpiresAt+ClockToleranceSecs {
		return nil, errors.New("token has expired")
	}
	if nowUnix < claims.NotBefore-ClockToleranceSecs {
		return nil, errors.New("token is not yet valid")
	}

	// Validate JTI matches RequestID
	if claims.JTI != claims.RequestID {
		return nil, errors.New("jti does not match request_id")
	}

	return claims, nil
}
