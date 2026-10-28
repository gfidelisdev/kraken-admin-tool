package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/time/rate"

	"github.com/kraken/admin-tool/backend/internal/audit"
	"github.com/kraken/admin-tool/backend/internal/auth"
	"github.com/kraken/admin-tool/backend/internal/config"
	"github.com/kraken/admin-tool/backend/internal/database"
	"github.com/kraken/admin-tool/backend/internal/docker"
	"github.com/kraken/admin-tool/backend/internal/host"
	"github.com/kraken/admin-tool/backend/internal/k8s"
	"github.com/kraken/admin-tool/backend/internal/models"
	"github.com/kraken/admin-tool/backend/internal/plugins"
	"github.com/kraken/admin-tool/backend/internal/source"
	"github.com/kraken/admin-tool/backend/internal/system"
	krakews "github.com/kraken/admin-tool/backend/internal/websocket"
)

func main() {
	// Parse optional CLI commands for administrative recovery
	resetAdminPass := flag.String("reset-admin-password", "", "Emergency password reset for admin user")
	resetUser := flag.String("reset-user", "", "Emergency password reset: target username")
	newPassword := flag.String("new-password", "", "Emergency password reset: new password for specified user")
	flag.Parse()

	startTime := time.Now()
	log.Println("[KRAKEN] Starting Kraken Admin Tool Backend...")

	// 1. Configuration
	cfg := config.Load()
	log.Printf("[KRAKEN] Loaded configuration. Environment: %s, Port: %s", cfg.Environment, cfg.Port)

	// 2. Database Initialization (PostgreSQL with SQLite fallback)
	dbHandle, err := database.InitDB(cfg)
	if err != nil {
		log.Fatalf("[KRAKEN] Critical: failed to initialize database: %v", err)
	}

	// CLI Emergency Password Recovery
	if *resetAdminPass != "" || (*resetUser != "" && *newPassword != "") {
		targetUser := "admin"
		targetPass := *resetAdminPass
		if *resetUser != "" {
			targetUser = *resetUser
			targetPass = *newPassword
		}

		if len(targetPass) < 8 {
			log.Fatalf("[RECOVERY] Error: Password must be at least 8 characters long")
		}

		hash, err := bcrypt.GenerateFromPassword([]byte(targetPass), bcrypt.DefaultCost)
		if err != nil {
			log.Fatalf("[RECOVERY] Error hashing password: %v", err)
		}

		var user models.User
		if err := dbHandle.DB.Where("username = ?", targetUser).First(&user).Error; err != nil {
			log.Fatalf("[RECOVERY] Target user '%s' not found: %v", targetUser, err)
		}

		user.PasswordHash = string(hash)
		user.Status = "active"
		if err := dbHandle.DB.Save(&user).Error; err != nil {
			log.Fatalf("[RECOVERY] Failed to update password for '%s': %v", targetUser, err)
		}

		log.Printf("[RECOVERY] SUCCESS: Password for user '%s' has been reset and account set to active.", targetUser)
		os.Exit(0)
	}

	// 3. Connect to Docker & Kubernetes (with graceful degradation)
	dockerService := docker.NewDockerService(cfg.DockerHost, dbHandle.DB)
	k8sService := k8s.NewK8sService(cfg.Kubeconfig)
	k8sService.SetDB(dbHandle.DB)
	k8sService.SetDockerService(dockerService)

	sourceService := source.NewSourceService(dbHandle.DB, dockerService)
	sourceService.StartScheduler(context.Background())
	defer sourceService.StopScheduler()

	// 4. Initialize Handlers
	authHandler := auth.NewHandler(dbHandle.DB, cfg.JWTSecret, cfg.AllowRegistration)
	dockerHandler := docker.NewHandler(dockerService, dbHandle.DB)
	hostHandler := host.NewHandler(dbHandle.DB, dockerService)
	sourceHandler := source.NewHandler(sourceService, dbHandle.DB)
	k8sHandler := k8s.NewHandler(k8sService, dbHandle.DB)
	auditHandler := audit.NewHandler(dbHandle.DB)
	systemHandler := system.NewHandler(dbHandle, dockerService, k8sService, startTime)

	pluginsManager, pErr := plugins.NewManager(context.Background(), dbHandle.DB, dockerService, cfg.PluginsDir)
	if pErr != nil {
		log.Printf("[PLUGINS] Warning: failed to initialize plugins manager: %v", pErr)
	}
	pluginsHandler := plugins.NewHandler(pluginsManager)

	// Rate limiter for sensitive auth endpoints (5 burst, 1 request per 2s per IP)
	authRateLimiter := auth.NewIPRateLimiter(rate.Every(2*time.Second), 5)

	// 5. Setup Chi Router & Middleware
	r := chi.NewRouter()

	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)

	// Security Headers & Request Body Size Limiting
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
			w.Header().Set("X-XSS-Protection", "1; mode=block")

			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, 10<<20) // 10MB maximum
			}
			next.ServeHTTP(w, r)
		})
	})

	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   cfg.CorsOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token", "X-Server-ID", "X-Host-Password", "X-Host-Key", "X-Host-Key-Passphrase", "X-Sudo-Password"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	krakews.SetAllowedOrigins(cfg.CorsOrigins)

	// Health check endpoint
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// Public AI Guide for Plugin Developers
	r.Get("/api/plugins/ai-guide", pluginsHandler.GetAIGuideHandler)

	// Public Auth endpoints
	r.Route("/api/auth", func(r chi.Router) {
		r.Post("/register", authHandler.Register)
		r.With(authRateLimiter.Middleware("Too many login attempts. Please wait before trying again.")).Post("/login", authHandler.Login)
		r.With(authRateLimiter.Middleware("Too many recovery attempts. Please wait before trying again.")).Post("/recover-password", authHandler.RequestPasswordRecovery)
		r.With(authRateLimiter.Middleware("Too many recovery attempts. Please wait before trying again.")).Post("/recover-password/otp", authHandler.RecoverPasswordViaOTP)
		r.Post("/admin-recovery", authHandler.MasterAdminRecovery)
	})

	// Protected Endpoints
	r.Group(func(r chi.Router) {
		r.Use(auth.AuthMiddleware(cfg.JWTSecret))

		// Profile & Ticket & Account (All authenticated users: admin, operator, viewer)
		r.Get("/api/auth/me", authHandler.Me)
		r.Post("/api/auth/change-password", authHandler.ChangePassword)
		r.Post("/api/auth/ws-ticket", authHandler.CreateWSTicket)
		r.Get("/api/auth/otp/setup", authHandler.SetupOTP)
		r.Post("/api/auth/otp/verify-setup", authHandler.VerifyOTPSetup)
		r.Post("/api/auth/otp/disable", authHandler.DisableOTP)

		// User & Access Management (Admin only)
		r.Group(func(adminAuth chi.Router) {
			adminAuth.Use(auth.RequireRole("admin"))
			adminAuth.Get("/api/auth/users", authHandler.ListUsers)
			adminAuth.Post("/api/auth/users/{id}/approve", authHandler.ApproveUser)
			adminAuth.Post("/api/auth/users/{id}/reject", authHandler.RejectUser)
			adminAuth.Put("/api/auth/users/{id}/role", authHandler.UpdateUserRole)
			adminAuth.Delete("/api/auth/users/{id}", authHandler.DeleteUser)
			adminAuth.Post("/api/auth/users/{id}/reset-password", authHandler.AdminResetPassword)

			// Password recovery requests
			adminAuth.Get("/api/auth/password-recovery-requests", authHandler.ListPasswordRecoveryRequests)
			adminAuth.Post("/api/auth/password-recovery-requests/{id}/resolve", authHandler.ResolvePasswordRecoveryRequest)
			adminAuth.Post("/api/auth/password-recovery-requests/{id}/reject", authHandler.RejectPasswordRecoveryRequest)
		})

		// System Status (All authenticated users)
		r.Get("/api/system/status", systemHandler.StatusHandler)

		// Audit Logs (Restricted to Admin only)
		r.With(auth.RequireRole("admin")).Get("/api/audit-logs", auditHandler.ListLogsHandler)

		// Docker Endpoints
		r.Route("/api/docker", func(r chi.Router) {
			// Servers: Read-only for all, Server management restricted to Admin only
			r.Get("/servers", dockerHandler.ListServersHandler)

			r.Group(func(adminRouter chi.Router) {
				adminRouter.Use(auth.RequireRole("admin"))
				adminRouter.Post("/servers", dockerHandler.CreateServerHandler)
				adminRouter.Post("/servers/test", dockerHandler.TestServerHandler)
				adminRouter.Post("/servers/test/stream", dockerHandler.TestServerStreamHandler)
				adminRouter.Put("/servers/{id}", dockerHandler.UpdateServerHandler)
				adminRouter.Delete("/servers/{id}", dockerHandler.DeleteServerHandler)
				adminRouter.Post("/servers/{id}/default", dockerHandler.SetDefaultServerHandler)
			})

			// Containers: List for all, mutations restricted to Admin & Operator
			r.Get("/containers", dockerHandler.ListContainersHandler)

			r.Group(func(opRouter chi.Router) {
				opRouter.Use(auth.RequireRoles("admin", "operator"))
				opRouter.Post("/containers/{id}/start", dockerHandler.StartContainerHandler)
				opRouter.Post("/containers/{id}/stop", dockerHandler.StopContainerHandler)
				opRouter.Post("/containers/{id}/restart", dockerHandler.RestartContainerHandler)
				opRouter.Delete("/containers/{id}", dockerHandler.RemoveContainerHandler)
			})

			// Compose Stacks: List & Detail for all, mutations restricted to Admin & Operator
			r.Get("/stacks", dockerHandler.ListComposeStacksHandler)
			r.Get("/stacks/{name}", dockerHandler.GetComposeStackHandler)
			r.Get("/stacks/{name}/yaml", dockerHandler.GetComposeStackYAMLHandler)

			r.Group(func(opRouter chi.Router) {
				opRouter.Use(auth.RequireRoles("admin", "operator"))
				opRouter.Post("/stacks/{name}/start", dockerHandler.StartComposeStackHandler)
				opRouter.Post("/stacks/{name}/stop", dockerHandler.StopComposeStackHandler)
				opRouter.Post("/stacks/{name}/restart", dockerHandler.RestartComposeStackHandler)
				opRouter.Delete("/stacks/{name}", dockerHandler.RemoveComposeStackHandler)
				opRouter.Post("/stacks/deploy", dockerHandler.DeployComposeStackHandler)
				opRouter.Put("/stacks/{name}/yaml", dockerHandler.UpdateComposeStackYAMLHandler)
			})

			// Images: List & Search for all, Pull & Delete restricted to Admin & Operator
			r.Get("/images", dockerHandler.ListImagesHandler)
			r.Get("/images/search", dockerHandler.SearchImagesHandler)
			r.Get("/images/updates", dockerHandler.CheckImageUpdatesHandler)
			r.Get("/images/dockerfile", dockerHandler.GetImageDockerfileHandler)
			r.Get("/images/{id}/dockerfile", dockerHandler.GetImageDockerfileHandler)

			r.Group(func(opRouter chi.Router) {
				opRouter.Use(auth.RequireRoles("admin", "operator"))
				opRouter.Post("/images/pull", dockerHandler.PullImageHandler)
				opRouter.Post("/images/pull/stream", dockerHandler.PullImageStreamHandler)
				opRouter.Delete("/images/{id}", dockerHandler.RemoveImageHandler)
				opRouter.Put("/images/dockerfile", dockerHandler.SaveImageDockerfileHandler)
				opRouter.Post("/images/build/stream", dockerHandler.BuildImageStreamHandler)
				opRouter.Post("/images/update", dockerHandler.UpdateImageHandler)
			})
		})

		// Host & Server Management Endpoints (Package updates, Sudo policy, Source directories)
		r.Route("/api/hosts", func(r chi.Router) {
			// Read-only package info and active upgrade queries for all authenticated users
			r.Get("/packages/upgrade/active", hostHandler.ListActiveUpgradeJobsHandler)
			r.Get("/{id}/packages", hostHandler.GetPackageStatusHandler)
			r.Get("/{id}/packages/upgrade/active", hostHandler.GetActiveUpgradeJobHandler)
			r.Get("/{id}/source-dirs", sourceHandler.ListSearchDirsHandler)
			r.Get("/{id}/sources/links", sourceHandler.ListSourceLinksHandler)
			r.Get("/{id}/sources/files", sourceHandler.ListDiscoveredFilesHandler)
			r.Get("/{id}/sources/content", sourceHandler.GetSourceContentHandler)

			// Package & workload source operations restricted to Admin & Operator
			r.Group(func(opRouter chi.Router) {
				opRouter.Use(auth.RequireRoles("admin", "operator"))
				opRouter.Post("/{id}/packages/check", hostHandler.CheckPackagesHandler)
				opRouter.Post("/{id}/packages/upgrade", hostHandler.UpgradePackagesHandler)
				opRouter.Post("/{id}/packages/upgrade/stream", hostHandler.UpgradePackagesStreamHandler)
				opRouter.Get("/{id}/packages/upgrade/stream", hostHandler.UpgradePackagesStreamHandler)
				opRouter.Put("/{id}/package-manager", hostHandler.UpdatePackageManagerHandler)
				opRouter.Post("/{id}/sources/links/{linkId}/confirm", sourceHandler.ConfirmSourceLinkHandler)
				opRouter.Post("/{id}/sources/links/{linkId}/reject", sourceHandler.RejectSourceLinkHandler)
				opRouter.Post("/{id}/sources/bind", sourceHandler.BindSourceHandler)
				opRouter.Post("/{id}/sources/unbind", sourceHandler.UnbindSourceHandler)
			})

			// Host credential and sudo policy management restricted to Admin only
			r.Group(func(adminRouter chi.Router) {
				adminRouter.Use(auth.RequireRole("admin"))
				adminRouter.Post("/{id}/sudo-policy", hostHandler.UpdateSudoPolicyHandler)
				adminRouter.Delete("/{id}/sudo-password", hostHandler.ClearSudoPasswordHandler)
				adminRouter.Post("/provision-key", hostHandler.ProvisionKeyHandler)
				adminRouter.Post("/{id}/source-dirs", sourceHandler.CreateSearchDirHandler)
				adminRouter.Put("/{id}/source-dirs/{dirId}", sourceHandler.UpdateSearchDirHandler)
				adminRouter.Delete("/{id}/source-dirs/{dirId}", sourceHandler.DeleteSearchDirHandler)
				adminRouter.Post("/{id}/sources/scan", sourceHandler.ScanSourcesHandler)
			})
		})

		// Kubernetes Endpoints
		r.Route("/api/k8s", func(r chi.Router) {
			// Read-only for all
			r.Get("/pods", k8sHandler.ListPodsHandler)
			r.Get("/deployments", k8sHandler.ListDeploymentsHandler)

			// Workload mutations restricted to Admin & Operator
			r.Group(func(opRouter chi.Router) {
				opRouter.Use(auth.RequireRoles("admin", "operator"))
				opRouter.Delete("/pods/{namespace}/{name}", k8sHandler.DeletePodHandler)
				opRouter.Post("/deployments/{namespace}/{name}/scale", k8sHandler.ScaleDeploymentHandler)
				opRouter.Post("/deployments/{namespace}/{name}/restart", k8sHandler.RestartDeploymentHandler)
			})
		})

		// Plugins & Store Endpoints
		r.Route("/api/plugins", func(r chi.Router) {
			// Read-only / execution / AI guide
			r.Get("/", pluginsHandler.ListPluginsHandler)
			r.Get("/store", pluginsHandler.ListStorePluginsHandler)
			r.Get("/ai-guide", pluginsHandler.GetAIGuideHandler)
			r.Get("/{id}/worker.js", pluginsHandler.GetWorkerJSHandler)
			r.Post("/{id}/call", pluginsHandler.CallPluginHandler)

			// Admin-only operations: Upload, URL install, enable/disable, delete, catalog & key management
			r.Group(func(adminRouter chi.Router) {
				adminRouter.Use(auth.RequireRole("admin"))
				adminRouter.Post("/inspect-upload", pluginsHandler.InspectUploadHandler)
				adminRouter.Post("/inspect-url", pluginsHandler.InspectURLHandler)
				adminRouter.Post("/install-upload", pluginsHandler.InstallUploadHandler)
				adminRouter.Post("/install-url", pluginsHandler.InstallURLHandler)
				adminRouter.Post("/{id}/enable", pluginsHandler.EnablePluginHandler)
				adminRouter.Post("/{id}/disable", pluginsHandler.DisablePluginHandler)
				adminRouter.Post("/{id}/rebuild", pluginsHandler.RebuildPluginHandler)
				adminRouter.Delete("/{id}", pluginsHandler.DeletePluginHandler)

				adminRouter.Get("/catalogs", pluginsHandler.ListCatalogSourcesHandler)
				adminRouter.Post("/catalogs", pluginsHandler.CreateCatalogSourceHandler)
				adminRouter.Delete("/catalogs/{id}", pluginsHandler.DeleteCatalogSourceHandler)
				adminRouter.Post("/catalogs/{id}/sync", pluginsHandler.SyncCatalogSourceHandler)

				adminRouter.Get("/trusted-keys", pluginsHandler.ListTrustedKeysHandler)
				adminRouter.Post("/trusted-keys", pluginsHandler.CreateTrustedKeyHandler)
				adminRouter.Delete("/trusted-keys/{id}", pluginsHandler.DeleteTrustedKeyHandler)
			})
		})

		// WebSockets (Protected by AuthMiddleware via Bearer header or ?token=)
		r.Route("/api/ws", func(r chi.Router) {
			r.Get("/docker/logs", krakews.HandleDockerLogs(dockerService))
			r.Get("/k8s/logs", krakews.HandleK8sLogs(k8sService))
		})
	})

	// 6. HTTP Server & Graceful Shutdown
	server := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 0, // 0 for unbounded streaming / WebSockets
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Printf("[KRAKEN] Server listening on http://0.0.0.0:%s", cfg.Port)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("[KRAKEN] Server failure: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	log.Println("[KRAKEN] Shutting down server gracefully...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Printf("[KRAKEN] Server forced to shutdown: %v", err)
	}

	log.Println("[KRAKEN] Server stopped cleanly.")
}
