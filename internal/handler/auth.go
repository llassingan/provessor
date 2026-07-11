package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/llassingan/provessor/internal/repository"
	"github.com/llassingan/provessor/internal/model"
	"github.com/llassingan/provessor/internal/service"
)

type AuthHandler struct {
	authService *service.AuthService
	audit       *repository.AuditLogRepository
}

func NewAuthHandler(authService *service.AuthService, audit *repository.AuditLogRepository) *AuthHandler {
	return &AuthHandler{authService: authService, audit: audit}
}

func (h *AuthHandler) HandleInit(w http.ResponseWriter, r *http.Request) {
	hasUsers, err := h.authService.HasUsers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check users")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"has_users": hasUsers})
}

type signupRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type userResponse struct {
	ID        int64  `json:"id"`
	Email     string `json:"email"`
	CreatedAt string `json:"created_at"`
}

func (h *AuthHandler) HandleSignup(w http.ResponseWriter, r *http.Request) {
	var req signupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Email = strings.TrimSpace(req.Email)
	if !strings.Contains(req.Email, "@") {
		writeError(w, http.StatusBadRequest, "email must contain @")
		return
	}
	if len(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}

	user, token, err := h.authService.Signup(r.Context(), req.Email, req.Password)
	if err != nil {
		if err == service.ErrAdminAlreadyExists {
			h.audit.Log(r.Context(), model.AuditLog{Operation: "signup", ResourceType: "user", Status: "failure", ErrorMessage: "admin already exists"})
			writeError(w, http.StatusConflict, "admin already exists")
			return
		}
		h.audit.Log(r.Context(), model.AuditLog{Operation: "signup", ResourceType: "user", Status: "failure", ErrorMessage: "signup failed"})
		writeError(w, http.StatusInternalServerError, "signup failed")
		return
	}

	h.audit.Log(r.Context(), model.AuditLog{Operation: "signup", ResourceType: "user", ResourceID: user.ID, Status: "success"})
	setSessionCookie(w, token)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"user": userResponse{
			ID:        user.ID,
			Email:     user.Email,
			CreatedAt: user.CreatedAt.Format("2006-01-02T15:04:05Z"),
		},
	})
}

func (h *AuthHandler) HandleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.audit.Log(r.Context(), model.AuditLog{Operation: "login", ResourceType: "user", Status: "failure", ErrorMessage: "bad request"})
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Email = strings.TrimSpace(req.Email)
	if req.Email == "" || req.Password == "" {
		h.audit.Log(r.Context(), model.AuditLog{Operation: "login", ResourceType: "user", Status: "failure", ErrorMessage: "bad request"})
		writeError(w, http.StatusBadRequest, "email and password are required")
		return
	}

	user, token, err := h.authService.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		if errors.Is(err, service.ErrAccountLocked) {
			h.audit.Log(r.Context(), model.AuditLog{Operation: "login", ResourceType: "user", Status: "failure", ErrorMessage: "account locked"})
			// Extract remaining minutes from error message: "account locked: N"
			msg := strings.TrimPrefix(err.Error(), service.ErrAccountLocked.Error())
			msg = strings.TrimPrefix(msg, ": ")
			minutes, _ := strconv.Atoi(msg)
			writeJSON(w, http.StatusLocked, map[string]interface{}{
				"error": "Account locked. Try again in " + strconv.Itoa(minutes) + " minute" + map[bool]string{true: "s", false: ""}[minutes > 1] + ".",
			})
			return
		}
		if err == service.ErrInvalidCredentials {
			h.audit.Log(r.Context(), model.AuditLog{Operation: "login", ResourceType: "user", Status: "failure", ErrorMessage: "invalid credentials"})
			writeError(w, http.StatusUnauthorized, "invalid credentials")
			return
		}
		h.audit.Log(r.Context(), model.AuditLog{Operation: "login", ResourceType: "user", Status: "failure", ErrorMessage: "login failed"})
		writeError(w, http.StatusInternalServerError, "login failed")
		return
	}

	h.audit.Log(r.Context(), model.AuditLog{Operation: "login", ResourceType: "user", ResourceID: user.ID, Status: "success"})
	setSessionCookie(w, token)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"user": userResponse{
			ID:        user.ID,
			Email:     user.Email,
			CreatedAt: user.CreatedAt.Format("2006-01-02T15:04:05Z"),
		},
	})
}

func (h *AuthHandler) HandleLogout(w http.ResponseWriter, r *http.Request) {
	if userID, ok := UserIDFromContext(r.Context()); ok {
		h.audit.Log(r.Context(), model.AuditLog{Operation: "logout", ResourceType: "user", ResourceID: userID, Status: "success"})
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "session_token",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   0,
	})
	w.WriteHeader(http.StatusNoContent)
}

func setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "session_token",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400,
	})
}
