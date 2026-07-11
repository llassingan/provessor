package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/llassingan/provessor/internal/logger"
	"github.com/llassingan/provessor/internal/model"
	"github.com/llassingan/provessor/internal/repository"
	"github.com/llassingan/provessor/internal/validator"
)

type SettingsHandler struct {
	repo  *repository.SettingsRepository
	log   *logger.Logger
	audit *repository.AuditLogRepository
}

func NewSettingsHandler(repo *repository.SettingsRepository, log *logger.Logger, audit *repository.AuditLogRepository) *SettingsHandler {
	return &SettingsHandler{repo: repo, log: log, audit: audit}
}

type settingsResponse struct {
	ID              int64  `json:"id"`
	TenancyOCID     string `json:"tenancy_ocid"`
	UserOCID        string `json:"user_ocid"`
	Fingerprint     string `json:"fingerprint"`
	PrivateKey      string `json:"private_key"`
	Region          string `json:"region"`
	CompartmentOCID string `json:"compartment_ocid"`
	APIBaseURL      string `json:"api_base_url"`
}

func maskPrivateKey(key string) string {
	if key == "" {
		return ""
	}
	return "********"
}

func (h *SettingsHandler) HandleGetSettings(w http.ResponseWriter, r *http.Request) {
	s, err := h.repo.Get(r.Context())
	if err != nil {
		h.log.Debug("get_settings_failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load settings")
		return
	}
	h.log.Debug("get_settings_loaded", "tenancy", s.TenancyOCID, "region", s.Region, "compartment", s.CompartmentOCID, "has_key", s.PrivateKey != "")
	resp := settingsResponse{
		ID:              s.ID,
		TenancyOCID:     s.TenancyOCID,
		UserOCID:        s.UserOCID,
		Fingerprint:     s.Fingerprint,
		PrivateKey:      maskPrivateKey(s.PrivateKey),
		Region:          s.Region,
		CompartmentOCID: s.CompartmentOCID,
		APIBaseURL:      s.APIBaseURL,
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *SettingsHandler) HandleListRegions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, validator.RegionGroups())
}

type updateSettingsRequest struct {
	TenancyOCID     string `json:"tenancy_ocid"`
	UserOCID        string `json:"user_ocid"`
	Fingerprint     string `json:"fingerprint"`
	PrivateKey      string `json:"private_key"`
	Region          string `json:"region"`
	CompartmentOCID string `json:"compartment_ocid"`
	APIBaseURL      string `json:"api_base_url"`
	APIToken        string `json:"api_token"`
}

func (h *SettingsHandler) HandleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var req updateSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.audit.Log(r.Context(), model.AuditLog{Operation: "settings.update", ResourceType: "settings", Status: "failure", ErrorMessage: "invalid request body"})
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.TenancyOCID = strings.TrimSpace(req.TenancyOCID)
	req.UserOCID = strings.TrimSpace(req.UserOCID)
	req.Fingerprint = strings.TrimSpace(req.Fingerprint)
	req.Region = strings.TrimSpace(req.Region)
	req.CompartmentOCID = strings.TrimSpace(req.CompartmentOCID)
	req.APIBaseURL = strings.TrimSpace(req.APIBaseURL)
	req.APIToken = strings.TrimSpace(req.APIToken)

	if req.TenancyOCID == "" || req.UserOCID == "" || req.Fingerprint == "" ||
		req.Region == "" || req.CompartmentOCID == "" || req.APIToken == "" {
		h.audit.Log(r.Context(), model.AuditLog{Operation: "settings.update", ResourceType: "settings", Status: "failure", ErrorMessage: "all fields except private_key are required"})
		writeError(w, http.StatusBadRequest, "all fields except private_key are required")
		return
	}

	privateKey := strings.TrimSpace(req.PrivateKey)
	if privateKey != "" && privateKey != "********" {
		if !strings.Contains(privateKey, "-----BEGIN PRIVATE KEY-----") {
			h.audit.Log(r.Context(), model.AuditLog{Operation: "settings.update", ResourceType: "settings", Status: "failure", ErrorMessage: "invalid private key format"})
			h.log.Debug("update_settings_invalid_private_key_format")
			writeError(w, http.StatusBadRequest, "private_key must contain -----BEGIN PRIVATE KEY-----")
			return
		}
	}

	h.log.Debug("update_settings_request", "tenancy", req.TenancyOCID, "region", req.Region, "compartment", req.CompartmentOCID, "has_existing_key", privateKey == "" || privateKey == "********", "has_new_key", privateKey != "" && privateKey != "********")

	s, err := h.repo.Get(r.Context())
	if err != nil {
		h.audit.Log(r.Context(), model.AuditLog{Operation: "settings.update", ResourceType: "settings", Status: "failure", ErrorMessage: "failed to load settings"})
		h.log.Debug("update_settings_get_existing_failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load settings")
		return
	}

	s.TenancyOCID = req.TenancyOCID
	s.UserOCID = req.UserOCID
	s.Fingerprint = req.Fingerprint
	s.Region = req.Region
	s.CompartmentOCID = req.CompartmentOCID
	s.APIBaseURL = req.APIBaseURL
	s.APIToken = req.APIToken

	if privateKey != "" && privateKey != "********" {
		s.PrivateKey = privateKey
	}

	if err := h.repo.Update(r.Context(), s); err != nil {
		h.audit.Log(r.Context(), model.AuditLog{Operation: "settings.update", ResourceType: "settings", Status: "failure", ErrorMessage: "failed to update settings"})
		h.log.Debug("update_settings_update_failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update settings")
		return
	}

	h.log.Debug("update_settings_updated")

	s, err = h.repo.Get(r.Context())
	if err != nil {
		h.audit.Log(r.Context(), model.AuditLog{Operation: "settings.update", ResourceType: "settings", Status: "failure", ErrorMessage: "failed to reload settings"})
		writeError(w, http.StatusInternalServerError, "failed to reload settings")
		return
	}

	h.audit.Log(r.Context(), model.AuditLog{Operation: "settings.update", ResourceType: "settings", Status: "success"})
	resp := settingsResponse{
		ID:              s.ID,
		TenancyOCID:     s.TenancyOCID,
		UserOCID:        s.UserOCID,
		Fingerprint:     s.Fingerprint,
		PrivateKey:      maskPrivateKey(s.PrivateKey),
		Region:          s.Region,
		CompartmentOCID: s.CompartmentOCID,
		APIBaseURL:      s.APIBaseURL,
	}
	writeJSON(w, http.StatusOK, resp)
}
