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
	// ErrServerForbidden is returned when the requested server does not exist or is neither owned by nor shared with the user.
	ErrServerForbidden = errors.New("forbidden: server not found or not accessible")
	// ErrNoServerConfigured is returned when the authenticated user has no configured servers.
	ErrNoServerConfigured = errors.New("no server configured for user")
)

// ResolveAuthorizedServerID extracts the requested server ID (from X-Server-ID header or serverId query param)
// and verifies that the authenticated user owns that server or has been granted access to it
// (or falls back to the user's default/first accessible server).
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
		if err := AccessibleServers(db, claims.UserID).Where("id = ?", requestedID).First(&srv).Error; err != nil {
			return 0, fmt.Errorf("%w: server %d", ErrServerForbidden, requestedID)
		}
		return srv.ID, nil
	}

	var userSrv models.DockerServer
	if err := AccessibleServers(db, claims.UserID).Order("is_default desc, id asc").First(&userSrv).Error; err != nil {
		return 0, ErrNoServerConfigured
	}
	return userSrv.ID, nil
}

// AccessibleServers scopes a DockerServer query to the servers a user owns or that were shared with them.
func AccessibleServers(db *gorm.DB, userID uint) *gorm.DB {
	granted := db.Model(&models.ServerAccessGrant{}).Select("server_id").Where("user_id = ?", userID)
	return db.Model(&models.DockerServer{}).Where("user_id = ? OR id IN (?)", userID, granted)
}
