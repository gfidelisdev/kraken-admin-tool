package database

import (
	"errors"
	"log"
	"os"
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

// InitDB attempts to connect to PostgreSQL. If unavailable, falls back gracefully to SQLite.
func InitDB(cfg *config.Config) (*Handle, error) {
	var gormDB *gorm.DB
	var err error
	dbType := "PostgreSQL"

	gormConfig := &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	}

	log.Printf("[DB] Attempting connection to PostgreSQL (%s:%s/%s)...", cfg.DBHost, cfg.DBPort, cfg.DBName)
	gormDB, err = gorm.Open(postgres.Open(cfg.PostgresDSN()), gormConfig)

	if err != nil {
		log.Printf("[DB] Warning: PostgreSQL unavailable (%v). Falling back to SQLite (kraken_data.db)...", err)
		dbType = "SQLite"
		gormDB, err = gorm.Open(sqlite.Open("kraken_data.db"), gormConfig)
		if err != nil {
			return nil, err
		}
		log.Printf("[DB] Connected to SQLite fallback successfully.")
	} else {
		log.Printf("[DB] Connected to PostgreSQL successfully.")
	}

	sqlDB, err := gormDB.DB()
	if err == nil {
		sqlDB.SetMaxIdleConns(10)
		sqlDB.SetMaxOpenConns(50)
		sqlDB.SetConnMaxLifetime(time.Hour)
	}

	// Auto-migration
	log.Printf("[DB] Running database migrations...")
	if err := gormDB.AutoMigrate(&models.User{}, &models.AuditLog{}, &models.DockerServer{}, &models.HostPackageUpdate{}); err != nil {
		return nil, err
	}

	// Auto-seed admin user and default docker server
	seedAdmin(gormDB)
	seedDefaultServer(gormDB, cfg.DockerHost)

	return &Handle{
		DB:     gormDB,
		DBType: dbType,
	}, nil
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
		}

		if err := db.Create(&admin).Error; err != nil {
			log.Printf("[DB] Error seeding default admin user: %v", err)
		} else {
			log.Printf("[DB] Successfully seeded initial admin user.")
		}
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
