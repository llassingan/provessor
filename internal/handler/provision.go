package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"vps-store/internal/model"
	"vps-store/internal/repository"
	"vps-store/internal/service"
)

type VPSHandler struct {
	vpsRepo          *repository.VPSRepository
	tmplRepo         *repository.TemplateRepository
	networkRepo      *repository.NetworkRepository
	settingsRepo     *repository.SettingsRepository
	provisionService *service.VPSProvisionService
}

func NewVPSHandler(
	vpsRepo *repository.VPSRepository,
	tmplRepo *repository.TemplateRepository,
	networkRepo *repository.NetworkRepository,
	settingsRepo *repository.SettingsRepository,
	provisionService *service.VPSProvisionService,
) *VPSHandler {
	return &VPSHandler{
		vpsRepo:          vpsRepo,
		tmplRepo:         tmplRepo,
		networkRepo:      networkRepo,
		settingsRepo:     settingsRepo,
		provisionService: provisionService,
	}
}

type createVPSRequest struct {
	TemplateID         int64    `json:"template_id"`
	NetworkID          int64    `json:"network_id"`
	DisplayName        string   `json:"display_name"`
	Shape              *string  `json:"shape,omitempty"`
	OCPU               *float64 `json:"ocpu,omitempty"`
	MemoryGB           *float64 `json:"memory_gb,omitempty"`
	BootVolumeSizeGB   *int     `json:"boot_volume_size_gb,omitempty"`
}

func (h *VPSHandler) HandleCreateVPS(w http.ResponseWriter, r *http.Request) {
	var req createVPSRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("[DEBUG] create_vps: invalid body: %v", err)
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	log.Printf("[DEBUG] create_vps: display_name=%q template_id=%d network_id=%d", req.DisplayName, req.TemplateID, req.NetworkID)

	if req.DisplayName == "" {
		log.Printf("[DEBUG] create_vps: display_name is empty")
		writeError(w, http.StatusBadRequest, "display_name is required")
		return
	}

	if req.NetworkID == 0 {
		log.Printf("[DEBUG] create_vps: network_id is 0")
		writeError(w, http.StatusBadRequest, "network_id is required")
		return
	}

	network, err := h.networkRepo.Get(r.Context(), req.NetworkID)
	if err != nil {
		log.Printf("[DEBUG] create_vps: get network failed: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to load network")
		return
	}
	if network == nil {
		log.Printf("[DEBUG] create_vps: network %d not found", req.NetworkID)
		writeError(w, http.StatusNotFound, "network not found")
		return
	}
	if network.Status != "ready" {
		log.Printf("[DEBUG] create_vps: network %d not ready (status=%s)", req.NetworkID, network.Status)
		writeError(w, http.StatusBadRequest, "network is not ready for provisioning")
		return
	}

	log.Printf("[DEBUG] create_vps: network %d is ready (region=%s)", req.NetworkID, network.Region)

	template, err := h.tmplRepo.Get(r.Context(), req.TemplateID)
	if err != nil {
		log.Printf("[DEBUG] create_vps: get template failed: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to load template")
		return
	}
	if template == nil {
		log.Printf("[DEBUG] create_vps: template %d not found", req.TemplateID)
		writeError(w, http.StatusNotFound, "template not found")
		return
	}

	log.Printf("[DEBUG] create_vps: template %q loaded (shape=%s ocpu=%.1f mem=%.1f boot=%d)", template.Name, template.Shape, template.DefaultOCPU, template.DefaultMemory, template.BootVolumeSizeGB)

	vps := &model.VPS{
		DisplayName:      req.DisplayName,
		TemplateID:        req.TemplateID,
		NetworkID:         model.NullInt64{NullInt64: sql.NullInt64{Int64: req.NetworkID, Valid: true}},
		Shape:             template.Shape,
		OCPU:              template.DefaultOCPU,
		MemoryGB:          template.DefaultMemory,
		BootVolumeSizeGB:  template.BootVolumeSizeGB,
		Status:            "pending",
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
		log.Printf("[DEBUG] create_vps: repo.Create failed: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to create VPS")
		return
	}

	log.Printf("[DEBUG] create_vps: created vps id=%d display_name=%q shape=%s ocpu=%.1f mem=%.1f", created.ID, created.DisplayName, created.Shape, created.OCPU, created.MemoryGB)

	if h.provisionService != nil {
		go func() {
			log.Printf("[DEBUG] provision_vps: starting provisioning for vps %d", created.ID)
			if err := h.provisionService.ProvisionVPS(context.Background(), created.ID); err != nil {
				log.Printf("[DEBUG] provision_vps: vps %d provisioning failed: %v", created.ID, err)
			}
		}()
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
		log.Printf("[DEBUG] terminate_vps: invalid id: %v", err)
		writeError(w, http.StatusBadRequest, "invalid VPS id")
		return
	}

	log.Printf("[DEBUG] terminate_vps: vps_id=%d", id)

	vps, err := h.vpsRepo.Get(r.Context(), id)
	if err != nil {
		log.Printf("[DEBUG] terminate_vps: vps %d get failed: %v", id, err)
		writeError(w, http.StatusInternalServerError, "failed to get VPS")
		return
	}
	if vps == nil {
		log.Printf("[DEBUG] terminate_vps: vps %d not found", id)
		writeError(w, http.StatusNotFound, "VPS not found")
		return
	}

	log.Printf("[DEBUG] terminate_vps: vps %d current status=%s oci_instance_id=%s", id, vps.Status, vps.OCIInstanceID.String)

	if vps.Status == "terminated" {
		log.Printf("[DEBUG] terminate_vps: vps %d already terminated", id)
		writeError(w, http.StatusConflict, "VPS is already terminated")
		return
	}

	if vps.OCIInstanceID.Valid && vps.OCIInstanceID.String != "" && h.provisionService != nil {
		region, err := h.provisionService.VPSRegionForDelete(r.Context(), id)
		if err != nil {
			log.Printf("[DEBUG] terminate_vps: cannot determine region for vps %d: %v", id, err)
			writeError(w, http.StatusInternalServerError, "failed to determine VPS region")
			return
		}
		go func() {
			log.Printf("[DEBUG] terminate_vps: terminating OCI instance %s in region %s", vps.OCIInstanceID.String, region)
			if err := h.provisionService.TerminateInstance(context.Background(), id, region, vps.OCIInstanceID.String); err != nil {
				log.Printf("[DEBUG] terminate_vps: terminate failed: %v", err)
			}
		}()
	}

	if err := h.vpsRepo.UpdateStatus(r.Context(), id, "terminated"); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to terminate VPS")
		return
	}

	log.Printf("[DEBUG] terminate_vps: vps %d terminated", id)
	writeJSON(w, http.StatusOK, map[string]string{"status": "terminated"})
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

	if vps.Status != "terminated" && vps.Status != "failed" {
		writeError(w, http.StatusConflict, "VPS must be terminated before deleting. Terminate it first.")
		return
	}

	if err := h.vpsRepo.Delete(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete VPS")
		return
	}

	log.Printf("[DEBUG] delete_vps: vps %d deleted from database", id)
	w.WriteHeader(http.StatusNoContent)
}

func (h *VPSHandler) HandleCredentialsCallback(w http.ResponseWriter, r *http.Request) {
	_, ok := UserIDFromContext(r.Context())
	if !ok {
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid VPS id")
			return
		}

		var creds map[string]any
		if err := json.NewDecoder(r.Body).Decode(&creds); err != nil {
			writeError(w, http.StatusBadRequest, "invalid credentials body")
			return
		}

		credsJSON, err := json.Marshal(creds)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to marshal credentials")
			return
		}

		if err := h.vpsRepo.UpdateCredentials(r.Context(), id, string(credsJSON)); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update credentials")
			return
		}

		w.WriteHeader(http.StatusNoContent)
		return
	}

	writeError(w, http.StatusForbidden, "forbidden")
}

func (h *VPSHandler) HandleStartVPS(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		log.Printf("[DEBUG] start_vps: invalid id: %v", err)
		writeError(w, http.StatusBadRequest, "invalid VPS id")
		return
	}

	log.Printf("[DEBUG] start_vps: vps_id=%d", id)

	vps, err := h.vpsRepo.Get(r.Context(), id)
	if err != nil {
		log.Printf("[DEBUG] start_vps: vps %d get failed: %v", id, err)
		writeError(w, http.StatusInternalServerError, "failed to get VPS")
		return
	}
	if vps == nil {
		log.Printf("[DEBUG] start_vps: vps %d not found", id)
		writeError(w, http.StatusNotFound, "VPS not found")
		return
	}

	log.Printf("[DEBUG] start_vps: vps %d current status=%s oci_instance_id=%s", id, vps.Status, vps.OCIInstanceID.String)

	if h.provisionService == nil {
		log.Printf("[DEBUG] start_vps: provisionService is nil")
		writeError(w, http.StatusServiceUnavailable, "provisioning service not available")
		return
	}

	if err := h.provisionService.StartInstance(r.Context(), id); err != nil {
		log.Printf("[DEBUG] start_vps: vps %d start failed: %v", id, err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	vps, err = h.vpsRepo.Get(r.Context(), id)
	if err != nil {
		log.Printf("[DEBUG] start_vps: vps %d re-fetch after start failed: %v", id, err)
		writeError(w, http.StatusInternalServerError, "failed to get updated VPS")
		return
	}

	log.Printf("[DEBUG] start_vps: vps %d started successfully, new status=%s", id, vps.Status)
	writeJSON(w, http.StatusOK, vps)
}

func (h *VPSHandler) HandleStopVPS(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		log.Printf("[DEBUG] stop_vps: invalid id: %v", err)
		writeError(w, http.StatusBadRequest, "invalid VPS id")
		return
	}

	log.Printf("[DEBUG] stop_vps: vps_id=%d", id)

	vps, err := h.vpsRepo.Get(r.Context(), id)
	if err != nil {
		log.Printf("[DEBUG] stop_vps: vps %d get failed: %v", id, err)
		writeError(w, http.StatusInternalServerError, "failed to get VPS")
		return
	}
	if vps == nil {
		log.Printf("[DEBUG] stop_vps: vps %d not found", id)
		writeError(w, http.StatusNotFound, "VPS not found")
		return
	}

	log.Printf("[DEBUG] stop_vps: vps %d current status=%s oci_instance_id=%s", id, vps.Status, vps.OCIInstanceID.String)

	if h.provisionService == nil {
		log.Printf("[DEBUG] stop_vps: provisionService is nil")
		writeError(w, http.StatusServiceUnavailable, "provisioning service not available")
		return
	}

	if err := h.provisionService.StopInstance(r.Context(), id); err != nil {
		log.Printf("[DEBUG] stop_vps: vps %d stop failed: %v", id, err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	vps, err = h.vpsRepo.Get(r.Context(), id)
	if err != nil {
		log.Printf("[DEBUG] stop_vps: vps %d re-fetch after stop failed: %v", id, err)
		writeError(w, http.StatusInternalServerError, "failed to get updated VPS")
		return
	}

	log.Printf("[DEBUG] stop_vps: vps %d stopped successfully, new status=%s", id, vps.Status)
	writeJSON(w, http.StatusOK, vps)
}

func (h *VPSHandler) HandleRestartVPS(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		log.Printf("[DEBUG] restart_vps: invalid id: %v", err)
		writeError(w, http.StatusBadRequest, "invalid VPS id")
		return
	}

	log.Printf("[DEBUG] restart_vps: vps_id=%d", id)

	vps, err := h.vpsRepo.Get(r.Context(), id)
	if err != nil {
		log.Printf("[DEBUG] restart_vps: vps %d get failed: %v", id, err)
		writeError(w, http.StatusInternalServerError, "failed to get VPS")
		return
	}
	if vps == nil {
		log.Printf("[DEBUG] restart_vps: vps %d not found", id)
		writeError(w, http.StatusNotFound, "VPS not found")
		return
	}

	log.Printf("[DEBUG] restart_vps: vps %d current status=%s oci_instance_id=%s", id, vps.Status, vps.OCIInstanceID.String)

	if h.provisionService == nil {
		log.Printf("[DEBUG] restart_vps: provisionService is nil")
		writeError(w, http.StatusServiceUnavailable, "provisioning service not available")
		return
	}

	if err := h.provisionService.RestartInstance(r.Context(), id); err != nil {
		log.Printf("[DEBUG] restart_vps: vps %d restart failed: %v", id, err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	vps, err = h.vpsRepo.Get(r.Context(), id)
	if err != nil {
		log.Printf("[DEBUG] restart_vps: vps %d re-fetch after restart failed: %v", id, err)
		writeError(w, http.StatusInternalServerError, "failed to get updated VPS")
		return
	}

	log.Printf("[DEBUG] restart_vps: vps %d restarted successfully, new status=%s", id, vps.Status)
	writeJSON(w, http.StatusOK, vps)
}

func (h *VPSHandler) HandleResetVPS(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		log.Printf("[DEBUG] reset_vps: invalid id: %v", err)
		writeError(w, http.StatusBadRequest, "invalid VPS id")
		return
	}

	log.Printf("[DEBUG] reset_vps: vps_id=%d", id)

	vps, err := h.vpsRepo.Get(r.Context(), id)
	if err != nil {
		log.Printf("[DEBUG] reset_vps: vps %d get failed: %v", id, err)
		writeError(w, http.StatusInternalServerError, "failed to get VPS")
		return
	}
	if vps == nil {
		log.Printf("[DEBUG] reset_vps: vps %d not found", id)
		writeError(w, http.StatusNotFound, "VPS not found")
		return
	}

	log.Printf("[DEBUG] reset_vps: vps %d current status=%s oci_instance_id=%s", id, vps.Status, vps.OCIInstanceID.String)

	if h.provisionService == nil {
		log.Printf("[DEBUG] reset_vps: provisionService is nil")
		writeError(w, http.StatusServiceUnavailable, "provisioning service not available")
		return
	}

	if err := h.provisionService.ResetInstance(r.Context(), id); err != nil {
		log.Printf("[DEBUG] reset_vps: vps %d reset failed: %v", id, err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	vps, err = h.vpsRepo.Get(r.Context(), id)
	if err != nil {
		log.Printf("[DEBUG] reset_vps: vps %d re-fetch after reset failed: %v", id, err)
		writeError(w, http.StatusInternalServerError, "failed to get updated VPS")
		return
	}

	log.Printf("[DEBUG] reset_vps: vps %d reset successfully, new status=%s", id, vps.Status)
	writeJSON(w, http.StatusOK, vps)
}

type resetPasswordRequest struct {
	Password string `json:"password"`
}

func (h *VPSHandler) HandleResetPasswordVPS(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		log.Printf("[DEBUG] reset_password: invalid id: %v", err)
		writeError(w, http.StatusBadRequest, "invalid VPS id")
		return
	}

	log.Printf("[DEBUG] reset_password: vps_id=%d", id)

	var req resetPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("[DEBUG] reset_password: vps %d invalid body: %v", id, err)
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Password == "" {
		log.Printf("[DEBUG] reset_password: vps %d password is empty", id)
		writeError(w, http.StatusBadRequest, "password is required")
		return
	}

	log.Printf("[DEBUG] reset_password: vps %d password length=%d", id, len(req.Password))

	vps, err := h.vpsRepo.Get(r.Context(), id)
	if err != nil {
		log.Printf("[DEBUG] reset_password: vps %d get failed: %v", id, err)
		writeError(w, http.StatusInternalServerError, "failed to get VPS")
		return
	}
	if vps == nil {
		log.Printf("[DEBUG] reset_password: vps %d not found", id)
		writeError(w, http.StatusNotFound, "VPS not found")
		return
	}

	log.Printf("[DEBUG] reset_password: vps %d current status=%s oci_instance_id=%s", id, vps.Status, vps.OCIInstanceID.String)

	if h.provisionService == nil {
		log.Printf("[DEBUG] reset_password: provisionService is nil")
		writeError(w, http.StatusServiceUnavailable, "provisioning service not available")
		return
	}

	if err := h.provisionService.ResetPassword(r.Context(), id, req.Password); err != nil {
		log.Printf("[DEBUG] reset_password: vps %d reset password failed: %v", id, err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	vps, err = h.vpsRepo.Get(r.Context(), id)
	if err != nil {
		log.Printf("[DEBUG] reset_password: vps %d re-fetch after reset failed: %v", id, err)
		writeError(w, http.StatusInternalServerError, "failed to get updated VPS")
		return
	}

	log.Printf("[DEBUG] reset_password: vps %d password reset successfully", id)
	writeJSON(w, http.StatusOK, vps)
}

func (h *VPSHandler) HandleGetFirewall(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		log.Printf("[DEBUG] firewall_get: invalid id: %v", err)
		writeError(w, http.StatusBadRequest, "invalid VPS id")
		return
	}

	log.Printf("[DEBUG] firewall_get: vps_id=%d", id)

	vps, err := h.vpsRepo.Get(r.Context(), id)
	if err != nil {
		log.Printf("[DEBUG] firewall_get: vps %d get failed: %v", id, err)
		writeError(w, http.StatusInternalServerError, "failed to get VPS")
		return
	}
	if vps == nil {
		log.Printf("[DEBUG] firewall_get: vps %d not found", id)
		writeError(w, http.StatusNotFound, "VPS not found")
		return
	}

	log.Printf("[DEBUG] firewall_get: vps %d found (status=%s network_id=%d)", id, vps.Status, vps.NetworkID.Int64)

	if h.provisionService == nil {
		log.Printf("[DEBUG] firewall_get: provisionService is nil")
		writeError(w, http.StatusServiceUnavailable, "provisioning service not available")
		return
	}

	rules, err := h.provisionService.GetFirewallRules(r.Context(), id)
	if err != nil {
		log.Printf("[DEBUG] firewall_get: GetFirewallRules failed: %v", err)
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

	log.Printf("[DEBUG] firewall_get: vps %d returning %d ingress + %d egress rules", id, len(ingress), len(egress))
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ingress": ingress,
		"egress":  egress,
	})
}

func (h *VPSHandler) HandleUpdateFirewall(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		log.Printf("[DEBUG] firewall_update: invalid id: %v", err)
		writeError(w, http.StatusBadRequest, "invalid VPS id")
		return
	}

	log.Printf("[DEBUG] firewall_update: vps_id=%d", id)

	var req struct {
		Rules []service.FirewallRule `json:"rules"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("[DEBUG] firewall_update: vps %d invalid body: %v", id, err)
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	log.Printf("[DEBUG] firewall_update: vps %d received %d rules", id, len(req.Rules))
	for i, rule := range req.Rules {
		log.Printf("[DEBUG] firewall_update: rule[%d] port=%d name=%q direction=%s source=%q dest=%q", i, rule.Port, rule.Name, rule.Direction, rule.Source, rule.Destination)
	}

	vps, err := h.vpsRepo.Get(r.Context(), id)
	if err != nil {
		log.Printf("[DEBUG] firewall_update: vps %d get failed: %v", id, err)
		writeError(w, http.StatusInternalServerError, "failed to get VPS")
		return
	}
	if vps == nil {
		log.Printf("[DEBUG] firewall_update: vps %d not found", id)
		writeError(w, http.StatusNotFound, "VPS not found")
		return
	}

	log.Printf("[DEBUG] firewall_update: vps %d found (status=%s network_id=%d)", id, vps.Status, vps.NetworkID.Int64)

	if h.provisionService == nil {
		log.Printf("[DEBUG] firewall_update: provisionService is nil")
		writeError(w, http.StatusServiceUnavailable, "provisioning service not available")
		return
	}

	if err := h.provisionService.UpdateFirewallRules(r.Context(), id, req.Rules); err != nil {
		log.Printf("[DEBUG] firewall_update: vps %d UpdateFirewallRules failed: %v", id, err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	log.Printf("[DEBUG] firewall_update: vps %d update successful, re-fetching rules", id)

	rules, err := h.provisionService.GetFirewallRules(r.Context(), id)
	if err != nil {
		log.Printf("[DEBUG] firewall_update: vps %d re-fetch rules failed: %v", id, err)
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

	log.Printf("[DEBUG] firewall_update: vps %d returning %d ingress + %d egress rules", id, len(ingress), len(egress))
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ingress": ingress,
		"egress":  egress,
	})
}
