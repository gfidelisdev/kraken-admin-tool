package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"golang.org/x/time/rate"

	"github.com/kraken/admin-tool/backend/internal/audit"
	"github.com/kraken/admin-tool/backend/internal/auth"
	"github.com/kraken/admin-tool/backend/internal/config"
	"github.com/kraken/admin-tool/backend/internal/database"
	"github.com/kraken/admin-tool/backend/internal/docker"
	"github.com/kraken/admin-tool/backend/internal/k8s"
	"github.com/kraken/admin-tool/backend/internal/system"
	krakews "github.com/kraken/admin-tool/backend/internal/websocket"
)

func main() {
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

	// 3. Connect to Docker & Kubernetes (with graceful degradation)
	dockerService := docker.NewDockerService(cfg.DockerHost, dbHandle.DB)
	k8sService := k8s.NewK8sService(cfg.Kubeconfig)
	k8sService.SetDB(dbHandle.DB)
	k8sService.SetDockerService(dockerService)

	// 4. Initialize Handlers
	authHandler := auth.NewHandler(dbHandle.DB, cfg.JWTSecret, cfg.AllowRegistration)
	dockerHandler := docker.NewHandler(dockerService, dbHandle.DB)
	k8sHandler := k8s.NewHandler(k8sService, dbHandle.DB)
	auditHandler := audit.NewHandler(dbHandle.DB)
	systemHandler := system.NewHandler(dbHandle, dockerService, k8sService, startTime)

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
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token", "X-Server-ID"},
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

	// Public Auth endpoints
	r.Route("/api/auth", func(r chi.Router) {
		r.Post("/register", authHandler.Register)
		r.With(authRateLimiter.Middleware("Too many login attempts. Please wait before trying again.")).Post("/login", authHandler.Login)
	})

	// Protected Endpoints
	r.Group(func(r chi.Router) {
		r.Use(auth.AuthMiddleware(cfg.JWTSecret))

		// Profile & Ticket & Account (All authenticated users: admin, operator, viewer)
		r.Get("/api/auth/me", authHandler.Me)
		r.Post("/api/auth/change-password", authHandler.ChangePassword)
		r.Post("/api/auth/ws-ticket", authHandler.CreateWSTicket)

		// User Management (Admin only)
		r.With(auth.RequireRole("admin")).Get("/api/auth/users", authHandler.ListUsers)

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

			// Images: List & Search for all, Pull & Delete restricted to Admin & Operator
			r.Get("/images", dockerHandler.ListImagesHandler)
			r.Get("/images/search", dockerHandler.SearchImagesHandler)

			r.Group(func(opRouter chi.Router) {
				opRouter.Use(auth.RequireRoles("admin", "operator"))
				opRouter.Post("/images/pull", dockerHandler.PullImageHandler)
				opRouter.Post("/images/pull/stream", dockerHandler.PullImageStreamHandler)
				opRouter.Delete("/images/{id}", dockerHandler.RemoveImageHandler)
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
