package auth

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/kraken/admin-tool/backend/internal/models"
	"gorm.io/gorm"
)

var (
	// ErrUnauthorized is returned when the request lacks a valid authenticated user context.
	ErrUnauthorized = errors.New("unauthorized: missing user context")
	// ErrServerForbidden is returned when the requested server does not exist or belongs to another user.
	ErrServerForbidden = errors.New("forbidden: server not found or not accessible")
	// ErrNoServerConfigured is returned when the authenticated user has no configured servers.
	ErrNoServerConfigured = errors.New("no server configured for user")
)

// ResolveAuthorizedServerID extracts the requested server ID (from X-Server-ID header or serverId query param)
// and verifies that the authenticated user owns that server (or falls back to the user's default/first server).
func ResolveAuthorizedServerID(db *gorm.DB, r *http.Request) (uint, error) {
	var requestedID uint
	if headerVal := r.Header.Get("X-Server-ID"); headerVal != "" {
		if id, err := strconv.ParseUint(headerVal, 10, 32); err == nil {
			requestedID = uint(id)
		}
	}
	if queryVal := r.URL.Query().Get("serverId"); queryVal != "" && requestedID == 0 {
		if id, err := strconv.ParseUint(queryVal, 10, 32); err == nil {
			requestedID = uint(id)
		}
	}

	claims := GetUserFromContext(r.Context())
	if claims == nil || claims.UserID == 0 {
		if db == nil {
			return requestedID, nil
		}
		return 0, ErrUnauthorized
	}

	if db == nil {
		return requestedID, nil
	}

	if requestedID > 0 {
		var srv models.DockerServer
		if err := db.Where("id = ? AND user_id = ?", requestedID, claims.UserID).First(&srv).Error; err != nil {
			return 0, fmt.Errorf("%w: server %d", ErrServerForbidden, requestedID)
		}
		return srv.ID, nil
	}

	var userSrv models.DockerServer
	if err := db.Where("user_id = ?", claims.UserID).Order("is_default desc, id asc").First(&userSrv).Error; err != nil {
		return 0, ErrNoServerConfigured
	}
	return userSrv.ID, nil
}
