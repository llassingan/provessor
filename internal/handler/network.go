package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"vps-store/internal/repository"
	"vps-store/internal/service"
	"vps-store/internal/sse"
)

type NetworkHandler struct {
	service      *service.NetworkService
	networkRepo  *repository.NetworkRepository
	settingsRepo *repository.SettingsRepository
	sseHandler   *SSEHandler
	broker       *sse.EventBroker
}

func NewNetworkHandler(
	service *service.NetworkService,
	networkRepo *repository.NetworkRepository,
	settingsRepo *repository.SettingsRepository,
	sseHandler *SSEHandler,
	broker *sse.EventBroker,
) *NetworkHandler {
	return &NetworkHandler{
		service:      service,
		networkRepo:  networkRepo,
		settingsRepo: settingsRepo,
		sseHandler:   sseHandler,
		broker:       broker,
	}
}

func (h *NetworkHandler) HandleListNetworks(w http.ResponseWriter, r *http.Request) {
	networks, err := h.networkRepo.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list networks")
		return
	}
	writeJSON(w, http.StatusOK, networks)
}

type createNetworkRequest struct {
	Name   string `json:"name"`
	Region string `json:"region"`
}

func (h *NetworkHandler) HandleCreateNetwork(w http.ResponseWriter, r *http.Request) {
	var req createNetworkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	req.Region = strings.TrimSpace(req.Region)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.Region == "" {
		writeError(w, http.StatusBadRequest, "region is required")
		return
	}

	network, err := h.networkRepo.Create(r.Context(), req.Name, req.Region)
	if err != nil {
		if strings.Contains(err.Error(), "maximum") {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create network")
		return
	}

	writeJSON(w, http.StatusCreated, network)
}

func (h *NetworkHandler) HandleGetNetwork(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid network id")
		return
	}

	network, err := h.networkRepo.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get network")
		return
	}
	if network == nil {
		writeError(w, http.StatusNotFound, "network not found")
		return
	}

	writeJSON(w, http.StatusOK, network)
}

func (h *NetworkHandler) HandleDeleteNetwork(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid network id")
		return
	}

	if err := h.networkRepo.Delete(r.Context(), id); err != nil {
		if strings.Contains(err.Error(), "active VPS") {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to delete network")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *NetworkHandler) HandleNetworkProvision(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid network id")
		return
	}

	go func() {
		_ = h.service.ProvisionNetwork(r.Context(), id)
	}()

	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "network_provisioning_started"})
}

func (h *NetworkHandler) HandleNetworkProvisionEvents(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	channel := "network:" + id
	h.sseHandler.HandleChannelEvents(w, r, channel)
}

func (h *NetworkHandler) HandleNetworkStatus(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid network id")
		return
	}

	network, err := h.networkRepo.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get network")
		return
	}
	if network == nil {
		writeError(w, http.StatusNotFound, "network not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":      network.Status,
		"vcn_ocid":    network.VCNOCID,
		"subnet_ocid": network.SubnetOCID,
	})
}

func (h *NetworkHandler) HandleOldNetworkSetup(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusGone, "network setup has moved to per-network provisioning. Create a network first via POST /api/networks, then POST /api/networks/{id}/provision")
}

func (h *NetworkHandler) HandleOldNetworkStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "migrated",
		"message": "Network status is now per-network. Use GET /api/networks to list networks.",
	})
}
