package auth

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/kraken/admin-tool/backend/internal/audit"
	"github.com/kraken/admin-tool/backend/internal/models"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type Handler struct {
	DB                *gorm.DB
	JWTSecret         string
	AllowRegistration bool
}

func NewHandler(db *gorm.DB, secret string, allowRegistration bool) *Handler {
	return &Handler{
		DB:                db,
		JWTSecret:         secret,
		AllowRegistration: allowRegistration,
	}
}

// Register registers a new user (initial bootstrap admin, admin-created users, or public if allowed)
func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var req models.RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request body"})
		return
	}

	req.Username = strings.TrimSpace(req.Username)
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	if req.Username == "" || req.Email == "" || req.Password == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Username, email, and password are required"})
		return
	}

	if len(req.Password) < 8 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Password must be at least 8 characters long"})
		return
	}

	// Security Gate: Check existing user count
	var userCount int64
	if err := h.DB.Model(&models.User{}).Count(&userCount).Error; err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Database error checking authorization"})
		return
	}

	claims := GetUserFromContext(r.Context())
	if claims == nil {
		claims = ExtractTokenClaims(r, h.JWTSecret)
	}

	isAdmin := claims != nil && claims.Role == "admin"

	// If users already exist, registration requires admin privileges unless public registration is enabled
	if userCount > 0 && !isAdmin && !h.AllowRegistration {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Public registration is disabled. Please contact an administrator or log in as admin to create users."})
		return
	}

	// Role Assignment:
	// Initial setup user is always "admin".
	// For subsequent users, an admin can specify "admin", "operator", or "viewer" (default: "operator").
	// Public self-registration (if enabled) is always assigned "viewer" for safety.
	assignedRole := "admin"
	if userCount > 0 {
		if isAdmin {
			assignedRole = "operator"
			reqRole := strings.ToLower(strings.TrimSpace(req.Role))
			if reqRole == "admin" || reqRole == "operator" || reqRole == "viewer" {
				assignedRole = reqRole
			}
		} else {
			assignedRole = "viewer"
		}
	}

	// Check if username or email already exists
	var existing models.User
	if err := h.DB.Where("username = ? OR email = ?", req.Username, req.Email).First(&existing).Error; err == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Username or email already in use"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to hash password"})
		return
	}

	user := models.User{
		Username:     req.Username,
		Email:        req.Email,
		PasswordHash: string(hash),
		Role:         assignedRole,
	}

	if err := h.DB.Create(&user).Error; err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to create user"})
		return
	}

	token, err := GenerateToken(&user, h.JWTSecret, 24*time.Hour)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to generate token"})
		return
	}

	audit.Log(h.DB, user.ID, user.Username, "REGISTER", "auth", user.Username, "User registered successfully", "SUCCESS")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(models.AuthResponse{
		Token: token,
		User: models.UserResponse{
			ID:        user.ID,
			Username:  user.Username,
			Email:     user.Email,
			Role:      user.Role,
			CreatedAt: user.CreatedAt,
		},
	})
}

// Login authenticates a user and returns a signed JWT
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req models.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request body"})
		return
	}

	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || req.Password == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Username and password are required"})
		return
	}

	var user models.User
	if err := h.DB.Where("username = ?", req.Username).First(&user).Error; err != nil {
		audit.Log(h.DB, 0, req.Username, "LOGIN", "auth", req.Username, "Invalid credentials - user not found", "FAILED")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid credentials"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		audit.Log(h.DB, user.ID, user.Username, "LOGIN", "auth", user.Username, "Invalid password attempt", "FAILED")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid credentials"})
		return
	}

	token, err := GenerateToken(&user, h.JWTSecret, 24*time.Hour)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to generate token"})
		return
	}

	audit.Log(h.DB, user.ID, user.Username, "LOGIN", "auth", user.Username, "User logged in successfully", "SUCCESS")

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(models.AuthResponse{
		Token: token,
		User: models.UserResponse{
			ID:        user.ID,
			Username:  user.Username,
			Email:     user.Email,
			Role:      user.Role,
			CreatedAt: user.CreatedAt,
		},
	})
}

// Me returns the currently authenticated user's profile
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())
	if claims == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Unauthorized"})
		return
	}

	var user models.User
	if err := h.DB.First(&user, claims.UserID).Error; err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "User not found"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(models.UserResponse{
		ID:        user.ID,
		Username:  user.Username,
		Email:     user.Email,
		Role:      user.Role,
		CreatedAt: user.CreatedAt,
	})
}

// CreateWSTicket generates a short-lived single-use ticket for establishing WebSocket connections
func (h *Handler) CreateWSTicket(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())
	if claims == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Unauthorized"})
		return
	}

	ticket, err := DefaultTicketStore.CreateTicket(claims, 30*time.Second)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to generate WebSocket ticket"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ticket":    ticket,
		"expiresIn": 30,
	})
}

// ChangePassword updates the authenticated user's password
func (h *Handler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())
	if claims == nil {
		claims = ExtractTokenClaims(r, h.JWTSecret)
	}
	if claims == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Unauthorized"})
		return
	}

	var req models.ChangePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request body"})
		return
	}

	if req.CurrentPassword == "" || req.NewPassword == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Current password and new password are required"})
		return
	}

	if len(req.NewPassword) < 8 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "New password must be at least 8 characters long"})
		return
	}

	var user models.User
	if err := h.DB.First(&user, claims.UserID).Error; err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "User not found"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.CurrentPassword)); err != nil {
		audit.Log(h.DB, user.ID, user.Username, "CHANGE_PASSWORD", "auth", user.Username, "Invalid current password attempt", "FAILED")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid current password"})
		return
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to hash new password"})
		return
	}

	user.PasswordHash = string(newHash)
	if err := h.DB.Save(&user).Error; err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to update password"})
		return
	}

	audit.Log(h.DB, user.ID, user.Username, "CHANGE_PASSWORD", "auth", user.Username, "Password updated successfully", "SUCCESS")

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "Password changed successfully"})
}

// ListUsers lists all registered users (admin only)
func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request) {
	var users []models.User
	if err := h.DB.Order("id asc").Find(&users).Error; err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to retrieve users"})
		return
	}

	var response []models.UserResponse
	for _, u := range users {
		response = append(response, models.UserResponse{
			ID:        u.ID,
			Username:  u.Username,
			Email:     u.Email,
			Role:      u.Role,
			CreatedAt: u.CreatedAt,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

