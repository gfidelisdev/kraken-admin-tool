package database

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/kraken/admin-tool/backend/internal/config"
	"github.com/kraken/admin-tool/backend/internal/models"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Handle represents the database wrapper holding the GORM instance and metadata
type Handle struct {
	DB     *gorm.DB
	DBType string
}

// InitDB attempts to connect to PostgreSQL with retries.
// In production or when DB_HOST is set to a dedicated database host (like "postgres"),
// it retries until PostgreSQL is ready and does not silently fall back to an empty SQLite database.
func InitDB(cfg *config.Config) (*Handle, error) {
	var gormDB *gorm.DB
	var err error
	dbType := "PostgreSQL"

	gormConfig := &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	}

	maxRetries := 1
	retryInterval := 500 * time.Millisecond
	isDedicatedPostgres := cfg.Environment == "production" || cfg.DBHost == "postgres"
	if isDedicatedPostgres {
		maxRetries = 30
		retryInterval = 1 * time.Second
	}

	if envRetries := os.Getenv("DB_MAX_RETRIES"); envRetries != "" {
		if r, convErr := strconv.Atoi(envRetries); convErr == nil && r > 0 {
			maxRetries = r
		}
	}
	if envInterval := os.Getenv("DB_RETRY_INTERVAL_MS"); envInterval != "" {
		if ms, convErr := strconv.Atoi(envInterval); convErr == nil && ms > 0 {
			retryInterval = time.Duration(ms) * time.Millisecond
		}
	}

	for attempt := 1; attempt <= maxRetries; attempt++ {
		if maxRetries > 1 {
			log.Printf("[DB] Connecting to PostgreSQL (%s:%s/%s) [attempt %d/%d]...", cfg.DBHost, cfg.DBPort, cfg.DBName, attempt, maxRetries)
		} else {
			log.Printf("[DB] Attempting connection to PostgreSQL (%s:%s/%s)...", cfg.DBHost, cfg.DBPort, cfg.DBName)
		}

		gormDB, err = gorm.Open(postgres.Open(cfg.PostgresDSN()), gormConfig)
		if err == nil {
			var sqlDB *gorm.DB
			_ = sqlDB
			rawDB, dbErr := gormDB.DB()
			if dbErr == nil {
				if pingErr := rawDB.Ping(); pingErr == nil {
					log.Printf("[DB] Connected to PostgreSQL successfully.")
					break
				} else {
					err = pingErr
				}
			} else {
				err = dbErr
			}
		}

		if attempt < maxRetries {
			log.Printf("[DB] PostgreSQL not ready yet (%v). Retrying in %v...", err, retryInterval)
			time.Sleep(retryInterval)
		}
	}

	if err != nil {
		if isDedicatedPostgres {
			return nil, fmt.Errorf("critical: PostgreSQL is configured but unavailable after %d attempts: %w", maxRetries, err)
		}

		log.Printf("[DB] Warning: PostgreSQL unavailable (%v). Falling back to SQLite (kraken_data.db)...", err)
		dbType = "SQLite"
		gormDB, err = gorm.Open(sqlite.Open("kraken_data.db"), gormConfig)
		if err != nil {
			return nil, err
		}
		log.Printf("[DB] Connected to SQLite fallback successfully.")
	}

	sqlDB, err := gormDB.DB()
	if err == nil {
		sqlDB.SetMaxIdleConns(10)
		sqlDB.SetMaxOpenConns(50)
		sqlDB.SetConnMaxLifetime(time.Hour)
	}

	// Auto-migration
	log.Printf("[DB] Running database migrations...")
	if gormDB.Migrator().HasColumn(&models.SourceLink{}, "file_m_time") && !gormDB.Migrator().HasColumn(&models.SourceLink{}, "file_mtime") {
		_ = gormDB.Migrator().RenameColumn(&models.SourceLink{}, "file_m_time", "file_mtime")
	}

	if err := gormDB.AutoMigrate(
		&models.User{},
		&models.AuditLog{},
		&models.DockerServer{},
		&models.HostPackageUpdate{},
		&models.PasswordResetRequest{},
		&models.ComposeStackConfig{},
		&models.ImageDockerfileConfig{},
		&models.SourceSearchDir{},
		&models.SourceLink{},
		&models.InstalledPlugin{},
		&models.PluginKVStore{},
		&models.PluginCatalogSource{},
		&models.PluginTrustedKey{},
	); err != nil {
		return nil, err
	}

	// Backfill any empty user status to 'active'
	gormDB.Model(&models.User{}).Where("status = '' OR status IS NULL").Update("status", "active")

	// Auto-seed admin user, default docker server, and default plugin catalog
	seedAdmin(gormDB)
	checkEnvAdminPasswordReset(gormDB)
	seedDefaultServer(gormDB, cfg.DockerHost)
	seedPluginCatalog(gormDB)

	return &Handle{
		DB:     gormDB,
		DBType: dbType,
	}, nil
}

func seedPluginCatalog(db *gorm.DB) {
	var count int64
	db.Model(&models.PluginCatalogSource{}).Count(&count)
	if count == 0 {
		defaultCatalog := models.PluginCatalogSource{
			Name:    "Kraken Community & Official Catalog",
			URL:     "https://raw.githubusercontent.com/kraken-admin-tool/plugins-catalog/main/index.json",
			Enabled: true,
		}
		if err := db.Create(&defaultCatalog).Error; err != nil {
			log.Printf("[DB] Warning: failed to seed default plugin catalog: %v", err)
		} else {
			log.Printf("[DB] Seeded default plugin catalog: %s", defaultCatalog.Name)
		}
	}
}

func seedAdmin(db *gorm.DB) {
	var count int64
	db.Model(&models.User{}).Where("username = ?", "admin").Count(&count)
	if count == 0 {
		initialPass := os.Getenv("INITIAL_ADMIN_PASSWORD")
		if initialPass == "" {
			env := strings.ToLower(os.Getenv("ENVIRONMENT"))
			if env == "production" {
				log.Printf("[SECURITY] Production notice: No default admin account seeded. First user can register securely via setup wizard or configure INITIAL_ADMIN_PASSWORD.")
				return
			}
			initialPass = "admin123"
			log.Printf("[SECURITY] WARNING: Seeded default admin credentials in development environment (admin / admin123). Never use default credentials in production!")
		}

		hash, err := bcrypt.GenerateFromPassword([]byte(initialPass), bcrypt.DefaultCost)
		if err != nil {
			log.Printf("[DB] Error generating admin password hash: %v", err)
			return
		}

		admin := models.User{
			Username:     "admin",
			Email:        "admin@kraken.local",
			PasswordHash: string(hash),
			Role:         "admin",
			Status:       "active",
		}

		if err := db.Create(&admin).Error; err != nil {
			log.Printf("[DB] Error seeding default admin user: %v", err)
		} else {
			log.Printf("[DB] Successfully seeded initial admin user.")
		}
	}
}

func checkEnvAdminPasswordReset(db *gorm.DB) {
	resetPass := strings.TrimSpace(os.Getenv("RESET_ADMIN_PASSWORD"))
	if resetPass == "" {
		return
	}
	if len(resetPass) < 8 {
		log.Printf("[SECURITY] Warning: RESET_ADMIN_PASSWORD must be at least 8 characters long. Skipping reset.")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(resetPass), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("[SECURITY] Error hashing RESET_ADMIN_PASSWORD: %v", err)
		return
	}

	var admin models.User
	if err := db.Where("username = ?", "admin").First(&admin).Error; err != nil {
		// If admin user doesn't exist, try first user with admin role
		if err := db.Where("role = ?", "admin").First(&admin).Error; err != nil {
			log.Printf("[SECURITY] Warning: RESET_ADMIN_PASSWORD set, but no admin user found in database.")
			return
		}
	}

	admin.PasswordHash = string(hash)
	admin.Status = "active"
	admin.Role = "admin"
	if err := db.Save(&admin).Error; err != nil {
		log.Printf("[SECURITY] Failed to reset admin password from environment: %v", err)
	} else {
		log.Printf("[SECURITY] Successfully recovered/reset admin password for user '%s' via RESET_ADMIN_PASSWORD.", admin.Username)
	}
}

func seedDefaultServer(db *gorm.DB, defaultHost string) {
	var count int64
	db.Model(&models.DockerServer{}).Count(&count)
	if count == 0 {
		host := defaultHost
		if host == "" {
			host = "unix:///var/run/docker.sock"
		}
		server := models.DockerServer{
			Name:      "Local Daemon",
			HostURL:   host,
			IsDefault: true,
			Status:    "CONNECTED",
		}
		if err := db.Create(&server).Error; err != nil {
			log.Printf("[DB] Error seeding default docker server: %v", err)
		} else {
			log.Printf("[DB] Successfully seeded default docker server (%s).", host)
		}
	}
}

// CheckHealth checks if the database is responding
func (h *Handle) CheckHealth() error {
	if h.DB == nil {
		return errors.New("database not initialized")
	}
	sqlDB, err := h.DB.DB()
	if err != nil {
		return err
	}
	return sqlDB.Ping()
}
