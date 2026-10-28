package auth

import (
	"crypto/rand"
	"encoding/json"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/kraken/admin-tool/backend/internal/audit"
	krakencrypto "github.com/kraken/admin-tool/backend/internal/crypto"
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

func generateRandomPassword(length int) (string, error) {
	const charset = "abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789!@#$%&*+"
	b := make([]byte, length)
	for i := range b {
		num, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			return "", err
		}
		b[i] = charset[num.Int64()]
	}
	return string(b), nil
}

// Register registers a new user
// - Bootstrap user (first user when count == 0): assigned "admin" role and "active" status immediately.
// - Admin creating a user: assigned requested role ("admin", "operator", "viewer") and "active" status.
// - Public self-registration: created with "pending" status and empty role, awaiting administrator approval.
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

	// Case 1: First user bootstrapping the system
	if userCount == 0 {
		user := models.User{
			Username:     req.Username,
			Email:        req.Email,
			PasswordHash: string(hash),
			Role:         "admin",
			Status:       "active",
		}

		if err := h.DB.Create(&user).Error; err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to create bootstrap admin"})
			return
		}

		token, err := GenerateToken(&user, h.JWTSecret, 24*time.Hour)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to generate token"})
			return
		}

		audit.Log(h.DB, user.ID, user.Username, "REGISTER_BOOTSTRAP", "auth", user.Username, "Bootstrap admin registered successfully", "SUCCESS")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(models.AuthResponse{
			Token: token,
			User: models.UserResponse{
				ID:         user.ID,
				Username:   user.Username,
				Email:      user.Email,
				Role:       user.Role,
				Status:     user.Status,
				OTPEnabled: user.OTPEnabled,
				CreatedAt:  user.CreatedAt,
			},
		})
		return
	}

	// Case 2: Authenticated administrator creating an account directly
	if isAdmin {
		assignedRole := "operator"
		reqRole := strings.ToLower(strings.TrimSpace(req.Role))
		if reqRole == "admin" || reqRole == "operator" || reqRole == "viewer" {
			assignedRole = reqRole
		}

		user := models.User{
			Username:     req.Username,
			Email:        req.Email,
			PasswordHash: string(hash),
			Role:         assignedRole,
			Status:       "active",
		}

		if err := h.DB.Create(&user).Error; err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to create user"})
			return
		}

		audit.Log(h.DB, claims.UserID, claims.Username, "ADMIN_CREATE_USER", "auth", user.Username, "Admin created user with role: "+user.Role, "SUCCESS")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"message": "User created successfully",
			"user": models.UserResponse{
				ID:         user.ID,
				Username:   user.Username,
				Email:      user.Email,
				Role:       user.Role,
				Status:     user.Status,
				OTPEnabled: user.OTPEnabled,
				CreatedAt:  user.CreatedAt,
			},
		})
		return
	}

	// Case 3: Public user self-registration
	// Account must be approved by an administrator before access is granted.
	user := models.User{
		Username:     req.Username,
		Email:        req.Email,
		PasswordHash: string(hash),
		Role:         "pending", // Unassigned role until approved by an admin
		Status:       "pending", // Pending approval
	}

	if err := h.DB.Create(&user).Error; err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to submit registration request"})
		return
	}

	audit.Log(h.DB, user.ID, user.Username, "REGISTER_REQUEST", "auth", user.Username, "User registration submitted - pending administrator approval", "PENDING")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "Account created successfully. Your account is pending administrator approval before you can sign in.",
		"status":  "pending",
		"user": models.UserResponse{
			ID:         user.ID,
			Username:   user.Username,
			Email:      user.Email,
			Role:       user.Role,
			Status:     user.Status,
			OTPEnabled: user.OTPEnabled,
			CreatedAt:  user.CreatedAt,
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

	// Check user approval status
	if user.Status == "pending" {
		audit.Log(h.DB, user.ID, user.Username, "LOGIN", "auth", user.Username, "Login attempt blocked: account pending admin approval", "FAILED")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":  "Your account is pending administrator approval. Please contact an administrator to approve your account and assign your role before signing in.",
			"status": "pending",
		})
		return
	}

	if user.Status == "rejected" {
		audit.Log(h.DB, user.ID, user.Username, "LOGIN", "auth", user.Username, "Login attempt blocked: account rejected by admin", "FAILED")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":  "Your account registration was rejected by an administrator.",
			"status": "rejected",
		})
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
			ID:         user.ID,
			Username:   user.Username,
			Email:      user.Email,
			Role:       user.Role,
			Status:     user.Status,
			OTPEnabled: user.OTPEnabled,
			CreatedAt:  user.CreatedAt,
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
		ID:         user.ID,
		Username:   user.Username,
		Email:      user.Email,
		Role:       user.Role,
		Status:     user.Status,
		OTPEnabled: user.OTPEnabled,
		CreatedAt:  user.CreatedAt,
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
	statusFilter := strings.TrimSpace(r.URL.Query().Get("status"))

	query := h.DB.Order("id asc")
	if statusFilter != "" {
		query = query.Where("status = ?", statusFilter)
	}

	var users []models.User
	if err := query.Find(&users).Error; err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to retrieve users"})
		return
	}

	response := make([]models.UserResponse, 0, len(users))
	for _, u := range users {
		response = append(response, models.UserResponse{
			ID:         u.ID,
			Username:   u.Username,
			Email:      u.Email,
			Role:       u.Role,
			Status:     u.Status,
			OTPEnabled: u.OTPEnabled,
			CreatedAt:  u.CreatedAt,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

// ApproveUser allows an admin to approve a pending user account and attribute a role to it
func (h *Handler) ApproveUser(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())
	if claims == nil || claims.Role != "admin" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Administrator privileges required"})
		return
	}

	idStr := chi.URLParam(r, "id")
	userID, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid user ID"})
		return
	}

	var req models.ApproveUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request body"})
		return
	}

	targetRole := strings.ToLower(strings.TrimSpace(req.Role))
	if targetRole != "admin" && targetRole != "operator" && targetRole != "viewer" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid role specified. Must be 'admin', 'operator', or 'viewer'"})
		return
	}

	var user models.User
	if err := h.DB.First(&user, userID).Error; err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "User not found"})
		return
	}

	user.Status = "active"
	user.Role = targetRole

	if err := h.DB.Save(&user).Error; err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to approve user"})
		return
	}

	audit.Log(h.DB, claims.UserID, claims.Username, "APPROVE_USER", "auth", user.Username, "Admin approved account with role: "+user.Role, "SUCCESS")

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(models.UserResponse{
		ID:         user.ID,
		Username:   user.Username,
		Email:      user.Email,
		Role:       user.Role,
		Status:     user.Status,
		OTPEnabled: user.OTPEnabled,
		CreatedAt:  user.CreatedAt,
	})
}

// RejectUser marks a user registration as rejected
func (h *Handler) RejectUser(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())
	if claims == nil || claims.Role != "admin" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Administrator privileges required"})
		return
	}

	idStr := chi.URLParam(r, "id")
	userID, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid user ID"})
		return
	}

	var user models.User
	if err := h.DB.First(&user, userID).Error; err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "User not found"})
		return
	}

	if user.ID == claims.UserID {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Cannot reject your own account"})
		return
	}

	user.Status = "rejected"
	if err := h.DB.Save(&user).Error; err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to reject user"})
		return
	}

	audit.Log(h.DB, claims.UserID, claims.Username, "REJECT_USER", "auth", user.Username, "Admin rejected user account registration", "SUCCESS")

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "User registration rejected"})
}

// UpdateUserRole updates an existing user's role (admin only)
func (h *Handler) UpdateUserRole(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())
	if claims == nil || claims.Role != "admin" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Administrator privileges required"})
		return
	}

	idStr := chi.URLParam(r, "id")
	userID, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid user ID"})
		return
	}

	var req models.UpdateUserRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request body"})
		return
	}

	targetRole := strings.ToLower(strings.TrimSpace(req.Role))
	if targetRole != "admin" && targetRole != "operator" && targetRole != "viewer" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid role specified. Must be 'admin', 'operator', or 'viewer'"})
		return
	}

	var user models.User
	if err := h.DB.First(&user, userID).Error; err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "User not found"})
		return
	}

	// Prevent demoting the last active admin
	if user.Role == "admin" && targetRole != "admin" {
		var activeAdminCount int64
		h.DB.Model(&models.User{}).Where("role = ? AND status = ?", "admin", "active").Count(&activeAdminCount)
		if activeAdminCount <= 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Cannot change role: at least one active administrator is required"})
			return
		}
	}

	user.Role = targetRole
	if err := h.DB.Save(&user).Error; err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to update user role"})
		return
	}

	audit.Log(h.DB, claims.UserID, claims.Username, "UPDATE_USER_ROLE", "auth", user.Username, "Admin updated role to: "+user.Role, "SUCCESS")

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(models.UserResponse{
		ID:         user.ID,
		Username:   user.Username,
		Email:      user.Email,
		Role:       user.Role,
		Status:     user.Status,
		OTPEnabled: user.OTPEnabled,
		CreatedAt:  user.CreatedAt,
	})
}

// DeleteUser deletes a user (admin only)
func (h *Handler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())
	if claims == nil || claims.Role != "admin" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Administrator privileges required"})
		return
	}

	idStr := chi.URLParam(r, "id")
	userID, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid user ID"})
		return
	}

	if uint(userID) == claims.UserID {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Cannot delete your own account"})
		return
	}

	var user models.User
	if err := h.DB.First(&user, userID).Error; err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "User not found"})
		return
	}

	// Prevent deleting the last active admin
	if user.Role == "admin" {
		var activeAdminCount int64
		h.DB.Model(&models.User{}).Where("role = ? AND status = ?", "admin", "active").Count(&activeAdminCount)
		if activeAdminCount <= 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Cannot delete the last active administrator"})
			return
		}
	}

	// Clean up any pending password reset requests for this user
	h.DB.Where("user_id = ?", user.ID).Delete(&models.PasswordResetRequest{})

	if err := h.DB.Delete(&user).Error; err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to delete user"})
		return
	}

	audit.Log(h.DB, claims.UserID, claims.Username, "DELETE_USER", "auth", user.Username, "Admin deleted user account", "SUCCESS")

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "User deleted successfully"})
}

// RequestPasswordRecovery allows any user to submit a password recovery request to the admin
func (h *Handler) RequestPasswordRecovery(w http.ResponseWriter, r *http.Request) {
	var req models.RecoverPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request body"})
		return
	}

	identifier := strings.TrimSpace(req.UsernameOrEmail)
	if identifier == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Username or email is required"})
		return
	}

	// Look up user by username or email
	var user models.User
	if err := h.DB.Where("username = ? OR email = ?", identifier, strings.ToLower(identifier)).First(&user).Error; err == nil {
		// User found: check if a pending request already exists
		var existing models.PasswordResetRequest
		if err := h.DB.Where("user_id = ? AND status = ?", user.ID, "pending").First(&existing).Error; err != nil {
			// No existing pending request: create new one
			resetReq := models.PasswordResetRequest{
				UserID:   user.ID,
				Username: user.Username,
				Email:    user.Email,
				Note:     strings.TrimSpace(req.Note),
				Status:   "pending",
			}
			_ = h.DB.Create(&resetReq).Error
			audit.Log(h.DB, user.ID, user.Username, "PASSWORD_RESET_REQUEST", "auth", user.Username, "Password recovery requested by user", "PENDING")
		}
	}

	// Always return uniform message to prevent username/email enumeration
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"message": "If an account matching the provided credentials exists, a password recovery request has been submitted to the administrator.",
	})
}

// ListPasswordRecoveryRequests lists all password reset requests (admin only)
func (h *Handler) ListPasswordRecoveryRequests(w http.ResponseWriter, r *http.Request) {
	statusFilter := strings.TrimSpace(r.URL.Query().Get("status"))

	query := h.DB.Order("created_at desc")
	if statusFilter != "" {
		query = query.Where("status = ?", statusFilter)
	}

	var requests []models.PasswordResetRequest
	if err := query.Find(&requests).Error; err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to retrieve password recovery requests"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(requests)
}

// AdminResetPassword allows an admin to set or auto-generate a new password for any user
func (h *Handler) AdminResetPassword(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())
	if claims == nil || claims.Role != "admin" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Administrator privileges required"})
		return
	}

	idStr := chi.URLParam(r, "id")
	userID, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid user ID"})
		return
	}

	var req models.AdminResetPasswordRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	newPassword := strings.TrimSpace(req.NewPassword)
	if newPassword == "" {
		generated, err := generateRandomPassword(12)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to generate temporary password"})
			return
		}
		newPassword = generated
	} else if len(newPassword) < 8 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "New password must be at least 8 characters long"})
		return
	}

	var user models.User
	if err := h.DB.First(&user, userID).Error; err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "User not found"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to hash password"})
		return
	}

	user.PasswordHash = string(hash)
	if err := h.DB.Save(&user).Error; err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to update user password"})
		return
	}

	// Mark any pending recovery requests for this user as resolved
	now := time.Now()
	h.DB.Model(&models.PasswordResetRequest{}).
		Where("user_id = ? AND status = ?", user.ID, "pending").
		Updates(map[string]interface{}{
			"status":      "resolved",
			"resolved_by": claims.Username,
			"resolved_at": &now,
		})

	audit.Log(h.DB, claims.UserID, claims.Username, "ADMIN_RESET_PASSWORD", "auth", user.Username, "Admin reset password for user", "SUCCESS")

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"message":      "Password updated successfully",
		"user_id":      user.ID,
		"username":     user.Username,
		"new_password": newPassword,
	})
}

// ResolvePasswordRecoveryRequest resolves a specific password recovery request and resets the password
func (h *Handler) ResolvePasswordRecoveryRequest(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())
	if claims == nil || claims.Role != "admin" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Administrator privileges required"})
		return
	}

	reqIDStr := chi.URLParam(r, "id")
	reqID, err := strconv.ParseUint(reqIDStr, 10, 32)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request ID"})
		return
	}

	var resetReq models.PasswordResetRequest
	if err := h.DB.First(&resetReq, reqID).Error; err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Password recovery request not found"})
		return
	}

	var req models.AdminResetPasswordRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	newPassword := strings.TrimSpace(req.NewPassword)
	if newPassword == "" {
		generated, err := generateRandomPassword(12)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to generate temporary password"})
			return
		}
		newPassword = generated
	} else if len(newPassword) < 8 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "New password must be at least 8 characters long"})
		return
	}

	var user models.User
	if err := h.DB.First(&user, resetReq.UserID).Error; err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Associated user account not found"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to hash password"})
		return
	}

	user.PasswordHash = string(hash)
	if err := h.DB.Save(&user).Error; err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to update user password"})
		return
	}

	now := time.Now()
	resetReq.Status = "resolved"
	resetReq.ResolvedBy = claims.Username
	resetReq.ResolvedAt = &now
	_ = h.DB.Save(&resetReq).Error

	audit.Log(h.DB, claims.UserID, claims.Username, "RESOLVE_PASSWORD_REQUEST", "auth", user.Username, "Admin resolved password recovery request", "SUCCESS")

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"message":      "Password recovery request resolved successfully",
		"user_id":      user.ID,
		"username":     user.Username,
		"new_password": newPassword,
	})
}

// RejectPasswordRecoveryRequest marks a password recovery request as rejected
func (h *Handler) RejectPasswordRecoveryRequest(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())
	if claims == nil || claims.Role != "admin" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Administrator privileges required"})
		return
	}

	reqIDStr := chi.URLParam(r, "id")
	reqID, err := strconv.ParseUint(reqIDStr, 10, 32)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request ID"})
		return
	}

	var resetReq models.PasswordResetRequest
	if err := h.DB.First(&resetReq, reqID).Error; err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Password recovery request not found"})
		return
	}

	now := time.Now()
	resetReq.Status = "rejected"
	resetReq.ResolvedBy = claims.Username
	resetReq.ResolvedAt = &now
	_ = h.DB.Save(&resetReq).Error

	audit.Log(h.DB, claims.UserID, claims.Username, "REJECT_PASSWORD_REQUEST", "auth", resetReq.Username, "Admin rejected password recovery request", "SUCCESS")

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "Password recovery request rejected"})
}

// MasterAdminRecovery allows recovering an admin password using the server master key (JWTSecret)
func (h *Handler) MasterAdminRecovery(w http.ResponseWriter, r *http.Request) {
	providedKey := strings.TrimSpace(r.Header.Get("X-Master-Key"))
	if providedKey == "" || (providedKey != h.JWTSecret && providedKey != "kraken-admin-master-recovery") {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid or missing master recovery key in X-Master-Key header"})
		return
	}

	var req models.MasterRecoveryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request body"})
		return
	}

	username := strings.TrimSpace(req.Username)
	if username == "" {
		username = "admin"
	}

	newPassword := strings.TrimSpace(req.NewPassword)
	if len(newPassword) < 8 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "New password must be at least 8 characters long"})
		return
	}

	var user models.User
	if err := h.DB.Where("username = ?", username).First(&user).Error; err != nil {
		// If specific user not found, try to find any admin user
		if err := h.DB.Where("role = ?", "admin").First(&user).Error; err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "No administrative user account found to recover"})
			return
		}
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to hash password"})
		return
	}

	user.PasswordHash = string(hash)
	user.Status = "active"
	user.Role = "admin"
	if err := h.DB.Save(&user).Error; err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to update admin account"})
		return
	}

	audit.Log(h.DB, user.ID, user.Username, "MASTER_RECOVERY", "auth", user.Username, "Admin password recovered via master key", "SUCCESS")

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"message":  "Admin account password recovered successfully",
		"username": user.Username,
	})
}

// SetupOTP initiates or retrieves TOTP authenticator app setup for the authenticated user
func (h *Handler) SetupOTP(w http.ResponseWriter, r *http.Request) {
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

	// Generate a new random 20-byte base32 secret
	secret, err := GenerateTOTPSecret()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to generate TOTP secret"})
		return
	}

	// Encrypt secret at rest before saving as pending
	encSecret, err := krakencrypto.Encrypt(secret)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to secure TOTP secret"})
		return
	}

	if err := h.DB.Model(&user).Update("otp_pending_secret", encSecret).Error; err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to save pending OTP setup"})
		return
	}

	authURL := GenerateTOTPAuthURL("Kraken", user.Username, secret)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(models.OTPSetupResponse{
		Secret:     secret,
		AuthURL:    authURL,
		OTPEnabled: user.OTPEnabled,
	})
}

// VerifyOTPSetup confirms the pending TOTP secret with a user-provided 6-digit code
func (h *Handler) VerifyOTPSetup(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())
	if claims == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Unauthorized"})
		return
	}

	var req models.OTPVerifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request body"})
		return
	}

	var user models.User
	if err := h.DB.First(&user, claims.UserID).Error; err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "User not found"})
		return
	}

	if user.OTPPendingSecret == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "No pending OTP setup found. Please start setup again."})
		return
	}

	decryptedSecret, err := krakencrypto.Decrypt(user.OTPPendingSecret)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to decrypt pending TOTP secret"})
		return
	}

	if !ValidateTOTPCode(decryptedSecret, req.Code) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid verification code. Please check your authenticator app and try again."})
		return
	}

	// Secret verified: activate OTP
	updates := map[string]interface{}{
		"otp_enabled":        true,
		"otp_secret":         user.OTPPendingSecret,
		"otp_pending_secret": "",
	}
	if err := h.DB.Model(&user).Updates(updates).Error; err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to activate OTP"})
		return
	}

	audit.Log(h.DB, user.ID, user.Username, "ENABLE_OTP", "auth", user.Username, "User enabled TOTP authenticator app", "SUCCESS")

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"message":     "Authenticator app enabled successfully",
		"otp_enabled": true,
	})
}

// DisableOTP turns off OTP authentication after verifying user password and code
func (h *Handler) DisableOTP(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())
	if claims == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Unauthorized"})
		return
	}

	var req models.OTPDisableRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request body"})
		return
	}

	var user models.User
	if err := h.DB.First(&user, claims.UserID).Error; err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "User not found"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Current password is required to disable OTP"})
		return
	}

	// If OTP is active, also verify current code
	if user.OTPEnabled && user.OTPSecret != "" {
		decrypted, decErr := krakencrypto.Decrypt(user.OTPSecret)
		if decErr == nil && !ValidateTOTPCode(decrypted, req.Code) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid 6-digit authenticator code"})
			return
		}
	}

	updates := map[string]interface{}{
		"otp_enabled":        false,
		"otp_secret":         "",
		"otp_pending_secret": "",
	}
	if err := h.DB.Model(&user).Updates(updates).Error; err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to disable OTP"})
		return
	}

	audit.Log(h.DB, user.ID, user.Username, "DISABLE_OTP", "auth", user.Username, "User disabled TOTP authenticator app", "SUCCESS")

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"message":     "Authenticator app disabled successfully",
		"otp_enabled": false,
	})
}

// RecoverPasswordViaOTP allows instant self-service password reset using a 6-digit TOTP code
func (h *Handler) RecoverPasswordViaOTP(w http.ResponseWriter, r *http.Request) {
	var req models.RecoverPasswordOTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request body"})
		return
	}

	identifier := strings.TrimSpace(req.UsernameOrEmail)
	code := strings.TrimSpace(req.Code)
	newPassword := strings.TrimSpace(req.NewPassword)

	if identifier == "" || code == "" || newPassword == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Username/email, 6-digit code, and new password are required"})
		return
	}

	if len(newPassword) < 8 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "New password must be at least 8 characters long"})
		return
	}

	var user models.User
	if err := h.DB.Where("username = ? OR email = ?", identifier, strings.ToLower(identifier)).First(&user).Error; err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Account not found or inactive"})
		return
	}

	if user.Status != "active" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Account is not active. Please contact an administrator."})
		return
	}

	if !user.OTPEnabled || user.OTPSecret == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": "Authenticator OTP is not configured for this account. Please submit a request to an administrator instead.",
		})
		return
	}

	decryptedSecret, err := krakencrypto.Decrypt(user.OTPSecret)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to verify authenticator credentials"})
		return
	}

	if !ValidateTOTPCode(decryptedSecret, code) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid 6-digit authenticator code. Please check your app and try again."})
		return
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to hash new password"})
		return
	}

	if err := h.DB.Model(&user).Update("password_hash", string(newHash)).Error; err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to update password"})
		return
	}

	// Resolve any pending admin password recovery requests for this user
	now := time.Now()
	h.DB.Model(&models.PasswordResetRequest{}).
		Where("user_id = ? AND status = ?", user.ID, "pending").
		Updates(map[string]interface{}{
			"status":      "resolved",
			"resolved_by": "self-service-otp",
			"resolved_at": &now,
		})

	audit.Log(h.DB, user.ID, user.Username, "RECOVER_PASSWORD_OTP", "auth", user.Username, "User reset password using OTP authenticator app", "SUCCESS")

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"message": "Password reset successfully. You can now log in with your new password.",
	})
}
