package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/llassingan/provessor/internal/logger"
	"github.com/llassingan/provessor/internal/model"
	"github.com/llassingan/provessor/internal/repository"
	"github.com/llassingan/provessor/internal/service"
)

type VPSHandler struct {
	vpsRepo          *repository.VPSRepository
	tmplRepo         *repository.TemplateRepository
	networkRepo      *repository.NetworkRepository
	settingsRepo     *repository.SettingsRepository
	provisionService vpsProvisioner
	jobQueue         *service.JobQueue
	log              *logger.Logger
	audit            *repository.AuditLogRepository
}

type vpsProvisioner interface {
	VPSRegionForDelete(ctx context.Context, vpsID int64) (string, error)
	StartInstance(ctx context.Context, vpsID int64) error
	StopInstance(ctx context.Context, vpsID int64) error
	RestartInstance(ctx context.Context, vpsID int64) error
	ResetInstance(ctx context.Context, vpsID int64) error
	ResetPassword(ctx context.Context, vpsID int64, newPassword string) error
	GetFirewallRules(ctx context.Context, vpsID int64) ([]service.FirewallRule, error)
	UpdateFirewallRules(ctx context.Context, vpsID int64, rules []service.FirewallRule) error
	RefreshInstanceIPs(ctx context.Context, vpsID int64) error
}

func NewVPSHandler(
	vpsRepo *repository.VPSRepository,
	tmplRepo *repository.TemplateRepository,
	networkRepo *repository.NetworkRepository,
	settingsRepo *repository.SettingsRepository,
	provisionService vpsProvisioner,
	jobQueue *service.JobQueue,
	log *logger.Logger,
	audit *repository.AuditLogRepository,
) *VPSHandler {
	return &VPSHandler{
		vpsRepo:          vpsRepo,
		tmplRepo:         tmplRepo,
		networkRepo:      networkRepo,
		settingsRepo:     settingsRepo,
		provisionService: provisionService,
		jobQueue:         jobQueue,
		log:              log,
		audit:            audit,
	}
}

type createVPSRequest struct {
	TemplateID       int64    `json:"template_id"`
	NetworkID        int64    `json:"network_id"`
	DisplayName      string   `json:"display_name"`
	Shape            *string  `json:"shape,omitempty"`
	OCPU             *float64 `json:"ocpu,omitempty"`
	MemoryGB         *float64 `json:"memory_gb,omitempty"`
	BootVolumeSizeGB *int     `json:"boot_volume_size_gb,omitempty"`
}

func (h *VPSHandler) HandleCreateVPS(w http.ResponseWriter, r *http.Request) {
	var req createVPSRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.log.Debug("create_vps_invalid_body", "error", err)
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	h.log.Debug("create_vps_request", "display_name", req.DisplayName, "template_id", req.TemplateID, "network_id", req.NetworkID)

	if req.DisplayName == "" {
		h.log.Debug("create_vps_empty_display_name")
		writeError(w, http.StatusBadRequest, "display_name is required")
		return
	}

	if req.NetworkID == 0 {
		h.log.Debug("create_vps_empty_network_id")
		writeError(w, http.StatusBadRequest, "network_id is required")
		return
	}

	network, err := h.networkRepo.Get(r.Context(), req.NetworkID)
	if err != nil {
		h.log.Debug("create_vps_get_network_failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load network")
		return
	}
	if network == nil {
		h.log.Debug("create_vps_network_not_found", "network_id", req.NetworkID)
		writeError(w, http.StatusNotFound, "network not found")
		return
	}
	if network.Status != "ready" {
		h.log.Debug("create_vps_network_not_ready", "network_id", req.NetworkID, "status", network.Status)
		writeError(w, http.StatusBadRequest, "network is not ready for provisioning")
		return
	}

	h.log.Debug("create_vps_network_ready", "network_id", req.NetworkID, "region", network.Region)

	template, err := h.tmplRepo.Get(r.Context(), req.TemplateID)
	if err != nil {
		h.log.Debug("create_vps_get_template_failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load template")
		return
	}
	if template == nil {
		h.log.Debug("create_vps_template_not_found", "template_id", req.TemplateID)
		writeError(w, http.StatusNotFound, "template not found")
		return
	}

	h.log.Debug("create_vps_template_loaded", "name", template.Name, "shape", template.Shape, "ocpu", template.DefaultOCPU, "memory_gb", template.DefaultMemory, "boot_volume_gb", template.BootVolumeSizeGB)

	vps := &model.VPS{
		DisplayName:      req.DisplayName,
		TemplateID:       req.TemplateID,
		NetworkID:        model.NullInt64{NullInt64: sql.NullInt64{Int64: req.NetworkID, Valid: true}},
		Shape:            template.Shape,
		OCPU:             template.DefaultOCPU,
		MemoryGB:         template.DefaultMemory,
		BootVolumeSizeGB: template.BootVolumeSizeGB,
		Status:           "pending",
	}

	if req.Shape != nil {
		vps.Shape = *req.Shape
	}
	if req.OCPU != nil {
		vps.OCPU = *req.OCPU
	}
	if req.MemoryGB != nil {
		vps.MemoryGB = *req.MemoryGB
	}
	if req.BootVolumeSizeGB != nil {
		vps.BootVolumeSizeGB = *req.BootVolumeSizeGB
	}

	created, err := h.vpsRepo.Create(r.Context(), vps)
	if err != nil {
		h.log.Debug("create_vps_repo_create_failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create VPS")
		return
	}

	h.log.Debug("create_vps_created", "vps_id", created.ID, "display_name", created.DisplayName, "shape", created.Shape, "ocpu", created.OCPU, "memory_gb", created.MemoryGB)

	if err := h.jobQueue.Enqueue(context.Background(), service.JobProvisionVPS, service.VPSJob{ID: created.ID}); err != nil {
		h.log.Error("provision_vps_enqueue_failed", "vps_id", created.ID, "error", err)
	}

	writeJSON(w, http.StatusOK, created)
}

func (h *VPSHandler) HandleListVPS(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	vpsList, err := h.vpsRepo.List(r.Context(), status)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list VPS")
		return
	}

	writeJSON(w, http.StatusOK, vpsList)
}

func (h *VPSHandler) HandleGetVPS(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid VPS id")
		return
	}

	vps, err := h.vpsRepo.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get VPS")
		return
	}
	if vps == nil {
		writeError(w, http.StatusNotFound, "VPS not found")
		return
	}

	writeJSON(w, http.StatusOK, vps)
}

func (h *VPSHandler) HandleTerminateVPS(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		h.log.Debug("terminate_vps_invalid_id", "error", err)
		writeError(w, http.StatusBadRequest, "invalid VPS id")
		return
	}

	h.log.Debug("terminate_vps_request", "vps_id", id)

	vps, err := h.vpsRepo.Get(r.Context(), id)
	if err != nil {
		h.log.Debug("terminate_vps_get_failed", "vps_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to get VPS")
		return
	}
	if vps == nil {
		h.log.Debug("terminate_vps_not_found", "vps_id", id)
		writeError(w, http.StatusNotFound, "VPS not found")
		return
	}

	h.log.Debug("terminate_vps_status", "vps_id", id, "status", vps.Status, "oci_instance_id", vps.OCIInstanceID.String)

	if vps.Status == "terminated" {
		h.log.Debug("terminate_vps_already_terminated", "vps_id", id)
		writeError(w, http.StatusConflict, "VPS is already terminated")
		return
	}

	if vps.Status == "terminating" {
		h.log.Debug("terminate_vps_already_terminating", "vps_id", id)
		writeError(w, http.StatusConflict, "VPS termination is already in progress")
		return
	}

	if err := h.vpsRepo.UpdateStatus(r.Context(), id, "terminating"); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update VPS status")
		return
	}

	if vps.OCIInstanceID.Valid && vps.OCIInstanceID.String != "" && h.provisionService != nil {
		region, err := h.provisionService.VPSRegionForDelete(r.Context(), id)
		if err != nil {
			h.log.Debug("terminate_vps_region_failed", "vps_id", id, "error", err)
			_ = h.vpsRepo.UpdateStatus(r.Context(), id, vps.Status)
			writeError(w, http.StatusInternalServerError, "failed to determine VPS region")
			return
		}
		if err := h.jobQueue.Enqueue(context.Background(), service.JobTerminateVPS, service.TerminateJob{
			ID:         id,
			Region:     region,
			InstanceID: vps.OCIInstanceID.String,
		}); err != nil {
			h.log.Error("terminate_vps_enqueue_failed", "vps_id", id, "error", err)
			_ = h.vpsRepo.UpdateStatus(r.Context(), id, vps.Status)
			writeError(w, http.StatusInternalServerError, "failed to enqueue termination job")
			return
		}
	} else {
		_ = h.vpsRepo.UpdateStatus(r.Context(), id, "terminated")
	}

	h.log.Debug("terminate_vps_queued", "vps_id", id)
	writeJSON(w, http.StatusOK, map[string]string{"status": "terminating"})
}

func (h *VPSHandler) HandleDeleteVPS(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid VPS id")
		return
	}

	vps, err := h.vpsRepo.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get VPS")
		return
	}
	if vps == nil {
		writeError(w, http.StatusNotFound, "VPS not found")
		return
	}

	if vps.Status != "terminated" {
		writeError(w, http.StatusConflict, "VPS must be terminated before deleting. Terminate it first.")
		return
	}

	if err := h.vpsRepo.Delete(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete VPS")
		return
	}

	h.log.Debug("delete_vps_deleted", "vps_id", id)
	w.WriteHeader(http.StatusNoContent)
}

const credentialsCallbackMaxBodyBytes = 64 * 1024

func (h *VPSHandler) HandleCredentialsCallback(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		h.audit.Log(r.Context(), model.AuditLog{
			Operation:    "vps.credentials_callback",
			ResourceType: "vps",
			ResourceID:   id,
			Status:       "failure",
			ErrorMessage: "invalid VPS id",
		})
		writeError(w, http.StatusBadRequest, "invalid VPS id")
		return
	}

	token, ok := credentialsCallbackBearerToken(r.Header.Get("Authorization"))
	if !ok {
		h.audit.Log(r.Context(), model.AuditLog{
			Operation:    "vps.credentials_callback",
			ResourceType: "vps",
			ResourceID:   id,
			Status:       "failure",
			ErrorMessage: "invalid token",
		})
		writeError(w, http.StatusUnauthorized, "invalid callback token")
		return
	}

	var creds map[string]any
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, credentialsCallbackMaxBodyBytes))
	if err := decoder.Decode(&creds); err != nil {
		h.audit.Log(r.Context(), model.AuditLog{
			Operation:    "vps.credentials_callback",
			ResourceType: "vps",
			ResourceID:   id,
			Status:       "failure",
			ErrorMessage: "malformed JSON",
		})
		writeError(w, http.StatusBadRequest, "invalid credentials body")
		return
	}
	if creds == nil {
		h.audit.Log(r.Context(), model.AuditLog{
			Operation:    "vps.credentials_callback",
			ResourceType: "vps",
			ResourceID:   id,
			Status:       "failure",
			ErrorMessage: "malformed JSON",
		})
		writeError(w, http.StatusBadRequest, "invalid credentials body")
		return
	}
	if agentStatus, ok := creds["_agent"].(string); ok {
		h.log.Debug("credentials_callback_agent", "vps_id", id, "agent_status", agentStatus)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		h.audit.Log(r.Context(), model.AuditLog{
			Operation:    "vps.credentials_callback",
			ResourceType: "vps",
			ResourceID:   id,
			Status:       "failure",
			ErrorMessage: "malformed JSON",
		})
		writeError(w, http.StatusBadRequest, "invalid credentials body")
		return
	}

	credsJSON, err := json.Marshal(creds)
	if err != nil {
		h.audit.Log(r.Context(), model.AuditLog{
			Operation:    "vps.credentials_callback",
			ResourceType: "vps",
			ResourceID:   id,
			Status:       "failure",
			ErrorMessage: "internal error",
		})
		writeError(w, http.StatusInternalServerError, "failed to marshal credentials")
		return
	}

	consumed, err := h.vpsRepo.ConsumeCredentialsCallback(r.Context(), id, repository.HashCredentialsCallbackToken(token), string(credsJSON))
	if err != nil {
		h.audit.Log(r.Context(), model.AuditLog{
			Operation:    "vps.credentials_callback",
			ResourceType: "vps",
			ResourceID:   id,
			Status:       "failure",
			ErrorMessage: "internal error",
		})
		writeError(w, http.StatusInternalServerError, "failed to update credentials")
		return
	}
	if !consumed {
		h.audit.Log(r.Context(), model.AuditLog{
			Operation:    "vps.credentials_callback",
			ResourceType: "vps",
			ResourceID:   id,
			Status:       "failure",
			ErrorMessage: "expired token",
		})
		writeError(w, http.StatusUnauthorized, "invalid callback token")
		return
	}

	h.audit.Log(r.Context(), model.AuditLog{
		Operation:    "vps.credentials_callback",
		ResourceType: "vps",
		ResourceID:   id,
		Status:       "success",
	})
	w.WriteHeader(http.StatusNoContent)
}

func credentialsCallbackBearerToken(header string) (string, bool) {
	if !strings.HasPrefix(header, "Bearer ") {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
	if token == "" || strings.ContainsAny(token, " \t\r\n") {
		return "", false
	}
	return token, true
}

func (h *VPSHandler) HandleStartVPS(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		h.log.Debug("start_vps_invalid_id", "error", err)
		writeError(w, http.StatusBadRequest, "invalid VPS id")
		return
	}

	h.log.Debug("start_vps_request", "vps_id", id)

	vps, err := h.vpsRepo.Get(r.Context(), id)
	if err != nil {
		h.log.Debug("start_vps_get_failed", "vps_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to get VPS")
		return
	}
	if vps == nil {
		h.log.Debug("start_vps_not_found", "vps_id", id)
		writeError(w, http.StatusNotFound, "VPS not found")
		return
	}

	h.log.Debug("start_vps_status", "vps_id", id, "status", vps.Status, "oci_instance_id", vps.OCIInstanceID.String)

	if h.provisionService == nil {
		h.log.Debug("start_vps_no_provision_service")
		writeError(w, http.StatusServiceUnavailable, "provisioning service not available")
		return
	}

	if err := h.provisionService.StartInstance(r.Context(), id); err != nil {
		h.log.Debug("start_vps_start_failed", "vps_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	vps, err = h.vpsRepo.Get(r.Context(), id)
	if err != nil {
		h.log.Debug("start_vps_refetch_failed", "vps_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to get updated VPS")
		return
	}

	h.log.Debug("start_vps_success", "vps_id", id, "status", vps.Status)
	writeJSON(w, http.StatusOK, vps)
}

func (h *VPSHandler) HandleStopVPS(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		h.log.Debug("stop_vps_invalid_id", "error", err)
		writeError(w, http.StatusBadRequest, "invalid VPS id")
		return
	}

	h.log.Debug("stop_vps_request", "vps_id", id)

	vps, err := h.vpsRepo.Get(r.Context(), id)
	if err != nil {
		h.log.Debug("stop_vps_get_failed", "vps_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to get VPS")
		return
	}
	if vps == nil {
		h.log.Debug("stop_vps_not_found", "vps_id", id)
		writeError(w, http.StatusNotFound, "VPS not found")
		return
	}

	h.log.Debug("stop_vps_status", "vps_id", id, "status", vps.Status, "oci_instance_id", vps.OCIInstanceID.String)

	if h.provisionService == nil {
		h.log.Debug("stop_vps_no_provision_service")
		writeError(w, http.StatusServiceUnavailable, "provisioning service not available")
		return
	}

	if err := h.provisionService.StopInstance(r.Context(), id); err != nil {
		h.log.Debug("stop_vps_stop_failed", "vps_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	vps, err = h.vpsRepo.Get(r.Context(), id)
	if err != nil {
		h.log.Debug("stop_vps_refetch_failed", "vps_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to get updated VPS")
		return
	}

	h.log.Debug("stop_vps_success", "vps_id", id, "status", vps.Status)
	writeJSON(w, http.StatusOK, vps)
}

func (h *VPSHandler) HandleRestartVPS(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		h.log.Debug("restart_vps_invalid_id", "error", err)
		writeError(w, http.StatusBadRequest, "invalid VPS id")
		return
	}

	h.log.Debug("restart_vps_request", "vps_id", id)

	vps, err := h.vpsRepo.Get(r.Context(), id)
	if err != nil {
		h.log.Debug("restart_vps_get_failed", "vps_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to get VPS")
		return
	}
	if vps == nil {
		h.log.Debug("restart_vps_not_found", "vps_id", id)
		writeError(w, http.StatusNotFound, "VPS not found")
		return
	}

	h.log.Debug("restart_vps_status", "vps_id", id, "status", vps.Status, "oci_instance_id", vps.OCIInstanceID.String)

	if h.provisionService == nil {
		h.log.Debug("restart_vps_no_provision_service")
		writeError(w, http.StatusServiceUnavailable, "provisioning service not available")
		return
	}

	if err := h.provisionService.RestartInstance(r.Context(), id); err != nil {
		h.log.Debug("restart_vps_restart_failed", "vps_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	vps, err = h.vpsRepo.Get(r.Context(), id)
	if err != nil {
		h.log.Debug("restart_vps_refetch_failed", "vps_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to get updated VPS")
		return
	}

	h.log.Debug("restart_vps_success", "vps_id", id, "status", vps.Status)
	writeJSON(w, http.StatusOK, vps)
}

func (h *VPSHandler) HandleResetVPS(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		h.log.Debug("reset_vps_invalid_id", "error", err)
		writeError(w, http.StatusBadRequest, "invalid VPS id")
		return
	}

	h.log.Debug("reset_vps_request", "vps_id", id)

	vps, err := h.vpsRepo.Get(r.Context(), id)
	if err != nil {
		h.log.Debug("reset_vps_get_failed", "vps_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to get VPS")
		return
	}
	if vps == nil {
		h.log.Debug("reset_vps_not_found", "vps_id", id)
		writeError(w, http.StatusNotFound, "VPS not found")
		return
	}

	h.log.Debug("reset_vps_status", "vps_id", id, "status", vps.Status, "oci_instance_id", vps.OCIInstanceID.String)

	if h.provisionService == nil {
		h.log.Debug("reset_vps_no_provision_service")
		writeError(w, http.StatusServiceUnavailable, "provisioning service not available")
		return
	}

	if err := h.provisionService.ResetInstance(r.Context(), id); err != nil {
		h.log.Debug("reset_vps_reset_failed", "vps_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	vps, err = h.vpsRepo.Get(r.Context(), id)
	if err != nil {
		h.log.Debug("reset_vps_refetch_failed", "vps_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to get updated VPS")
		return
	}

	h.log.Debug("reset_vps_success", "vps_id", id, "status", vps.Status)
	writeJSON(w, http.StatusOK, vps)
}

type resetPasswordRequest struct {
	Password string `json:"password"`
}

func (h *VPSHandler) HandleResetPasswordVPS(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		h.log.Debug("reset_password_invalid_id", "error", err)
		writeError(w, http.StatusBadRequest, "invalid VPS id")
		return
	}

	h.log.Debug("reset_password_request", "vps_id", id)

	var req resetPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.log.Debug("reset_password_invalid_body", "vps_id", id, "error", err)
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := service.ValidateResetPassword(req.Password); err != nil {
		h.log.Debug("reset_password_policy_failed", "vps_id", id, "error", err)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	h.log.Debug("reset_password_password_length", "vps_id", id, "length", len(req.Password))

	vps, err := h.vpsRepo.Get(r.Context(), id)
	if err != nil {
		h.log.Debug("reset_password_get_failed", "vps_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to get VPS")
		return
	}
	if vps == nil {
		h.log.Debug("reset_password_not_found", "vps_id", id)
		writeError(w, http.StatusNotFound, "VPS not found")
		return
	}

	h.log.Debug("reset_password_status", "vps_id", id, "status", vps.Status, "oci_instance_id", vps.OCIInstanceID.String)

	if h.provisionService == nil {
		h.log.Debug("reset_password_no_provision_service")
		writeError(w, http.StatusServiceUnavailable, "provisioning service not available")
		return
	}

	if err := h.provisionService.ResetPassword(r.Context(), id, req.Password); err != nil {
		h.log.Debug("reset_password_reset_failed", "vps_id", id, "error", err)
		if errors.Is(err, service.ErrResetPasswordPolicy) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	vps, err = h.vpsRepo.Get(r.Context(), id)
	if err != nil {
		h.log.Debug("reset_password_refetch_failed", "vps_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to get updated VPS")
		return
	}

	h.log.Debug("reset_password_success", "vps_id", id)
	writeJSON(w, http.StatusOK, vps)
}

func (h *VPSHandler) HandleGetFirewall(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		h.log.Debug("firewall_get_invalid_id", "error", err)
		writeError(w, http.StatusBadRequest, "invalid VPS id")
		return
	}

	h.log.Debug("firewall_get_request", "vps_id", id)

	vps, err := h.vpsRepo.Get(r.Context(), id)
	if err != nil {
		h.log.Debug("firewall_get_get_failed", "vps_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to get VPS")
		return
	}
	if vps == nil {
		h.log.Debug("firewall_get_not_found", "vps_id", id)
		writeError(w, http.StatusNotFound, "VPS not found")
		return
	}

	h.log.Debug("firewall_get_found", "vps_id", id, "status", vps.Status, "network_id", vps.NetworkID.Int64)

	if h.provisionService == nil {
		h.log.Debug("firewall_get_no_provision_service")
		writeError(w, http.StatusServiceUnavailable, "provisioning service not available")
		return
	}

	rules, err := h.provisionService.GetFirewallRules(r.Context(), id)
	if err != nil {
		h.log.Debug("firewall_get_rules_failed", "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	ingress := make([]service.FirewallRule, 0)
	egress := make([]service.FirewallRule, 0)
	for _, r := range rules {
		if r.Direction == "ingress" {
			ingress = append(ingress, r)
		} else {
			egress = append(egress, r)
		}
	}

	h.log.Debug("firewall_get_rules_count", "vps_id", id, "ingress_count", len(ingress), "egress_count", len(egress))
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ingress": ingress,
		"egress":  egress,
	})
}

func (h *VPSHandler) HandleUpdateFirewall(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		h.log.Debug("firewall_update_invalid_id", "error", err)
		writeError(w, http.StatusBadRequest, "invalid VPS id")
		return
	}

	h.log.Debug("firewall_update_request", "vps_id", id)

	var req struct {
		Rules []service.FirewallRule `json:"rules"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.log.Debug("firewall_update_invalid_body", "vps_id", id, "error", err)
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	h.log.Debug("firewall_update_rules_received", "vps_id", id, "rule_count", len(req.Rules))
	for i, rule := range req.Rules {
		h.log.Debug("firewall_update_rule", "index", i, "port", rule.Port, "name", rule.Name, "direction", rule.Direction, "source", rule.Source, "destination", rule.Destination)
	}

	vps, err := h.vpsRepo.Get(r.Context(), id)
	if err != nil {
		h.log.Debug("firewall_update_get_failed", "vps_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to get VPS")
		return
	}
	if vps == nil {
		h.log.Debug("firewall_update_not_found", "vps_id", id)
		writeError(w, http.StatusNotFound, "VPS not found")
		return
	}

	h.log.Debug("firewall_update_found", "vps_id", id, "status", vps.Status, "network_id", vps.NetworkID.Int64)

	if h.provisionService == nil {
		h.log.Debug("firewall_update_no_provision_service")
		writeError(w, http.StatusServiceUnavailable, "provisioning service not available")
		return
	}

	if err := h.provisionService.UpdateFirewallRules(r.Context(), id, req.Rules); err != nil {
		h.log.Debug("firewall_update_rules_failed", "vps_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.log.Debug("firewall_update_success_refetching", "vps_id", id)

	rules, err := h.provisionService.GetFirewallRules(r.Context(), id)
	if err != nil {
		h.log.Debug("firewall_update_refetch_failed", "vps_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	ingress := make([]service.FirewallRule, 0)
	egress := make([]service.FirewallRule, 0)
	for _, r := range rules {
		if r.Direction == "ingress" {
			ingress = append(ingress, r)
		} else {
			egress = append(egress, r)
		}
	}

	h.log.Debug("firewall_update_rules_count", "vps_id", id, "ingress_count", len(ingress), "egress_count", len(egress))
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ingress": ingress,
		"egress":  egress,
	})
}

func (h *VPSHandler) HandleRefreshIPs(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		h.log.Debug("refresh_ips_invalid_id", "error", err)
		writeError(w, http.StatusBadRequest, "invalid VPS id")
		return
	}

	h.log.Debug("refresh_ips_request", "vps_id", id)

	if h.provisionService == nil {
		h.log.Debug("refresh_ips_no_provision_service")
		writeError(w, http.StatusServiceUnavailable, "provisioning service not available")
		return
	}

	if err := h.provisionService.RefreshInstanceIPs(r.Context(), id); err != nil {
		h.log.Debug("refresh_ips_refresh_failed", "vps_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	vps, err := h.vpsRepo.Get(r.Context(), id)
	if err != nil {
		h.log.Debug("refresh_ips_refetch_failed", "vps_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to get updated VPS")
		return
	}

	h.log.Debug("refresh_ips_success", "vps_id", id, "public_ip", vps.PublicIP.String, "private_ip", vps.PrivateIP.String)
	writeJSON(w, http.StatusOK, vps)
}
