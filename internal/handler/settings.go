package handler

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"vps-store/internal/repository"
	"vps-store/internal/validator"
)

type SettingsHandler struct {
	repo *repository.SettingsRepository
}

func NewSettingsHandler(repo *repository.SettingsRepository) *SettingsHandler {
	return &SettingsHandler{repo: repo}
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
		log.Printf("[DEBUG] get_settings: failed: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to load settings")
		return
	}
	log.Printf("[DEBUG] get_settings: loaded (tenancy=%s region=%s compartment=%s has_key=%v)",
		s.TenancyOCID, s.Region, s.CompartmentOCID, s.PrivateKey != "")
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
		writeError(w, http.StatusBadRequest, "all fields except private_key are required")
		return
	}

	privateKey := strings.TrimSpace(req.PrivateKey)
	if privateKey != "" && privateKey != "********" {
		if !strings.Contains(privateKey, "-----BEGIN PRIVATE KEY-----") {
			log.Printf("[DEBUG] update_settings: invalid private_key format")
			writeError(w, http.StatusBadRequest, "private_key must contain -----BEGIN PRIVATE KEY-----")
			return
		}
	}

	log.Printf("[DEBUG] update_settings: tenancy=%q region=%q compartment=%q has_existing_key=%v has_new_key=%v",
		req.TenancyOCID, req.Region, req.CompartmentOCID, privateKey == "" || privateKey == "********", privateKey != "" && privateKey != "********")

	s, err := h.repo.Get(r.Context())
	if err != nil {
		log.Printf("[DEBUG] update_settings: get existing failed: %v", err)
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
		log.Printf("[DEBUG] update_settings: update failed: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to update settings")
		return
	}

	log.Printf("[DEBUG] update_settings: updated successfully")

	s, err = h.repo.Get(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reload settings")
		return
	}

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
