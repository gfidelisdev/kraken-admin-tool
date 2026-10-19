package audit

import (
	"log"
	"strings"
	"time"

	"github.com/kraken/admin-tool/backend/internal/models"
	"gorm.io/gorm"
)

func sanitizeLogField(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}

// Log records an administrative action into the AuditLog table
func Log(db *gorm.DB, userID uint, username, action, resourceType, resourceID, details, status string) {
	if db == nil {
		return
	}

	entry := models.AuditLog{
		UserID:       userID,
		Username:     sanitizeLogField(username),
		Action:       sanitizeLogField(action),
		ResourceType: sanitizeLogField(resourceType),
		ResourceID:   sanitizeLogField(resourceID),
		Details:      sanitizeLogField(details),
		Status:       sanitizeLogField(status),
		CreatedAt:    time.Now().UTC(),
	}

	go func() {
		if err := db.Create(&entry).Error; err != nil {
			log.Printf("[AUDIT] Failed to save audit log: %v", err)
		}
	}()
}

// List retrieves paginated audit logs ordered by creation date descending
func List(db *gorm.DB, page, limit int) ([]models.AuditLog, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}

	offset := (page - 1) * limit
	var logs []models.AuditLog
	var total int64

	if err := db.Model(&models.AuditLog{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := db.Order("created_at DESC").Limit(limit).Offset(offset).Find(&logs).Error
	return logs, total, err
}
