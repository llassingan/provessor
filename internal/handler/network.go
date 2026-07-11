package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/llassingan/provessor/internal/logger"
	"github.com/llassingan/provessor/internal/repository"
	"github.com/llassingan/provessor/internal/service"
	"github.com/llassingan/provessor/internal/sse"
)

type NetworkHandler struct {
	service      *service.NetworkService
	networkRepo  *repository.NetworkRepository
	settingsRepo *repository.SettingsRepository
	sseHandler   *SSEHandler
	broker       *sse.EventBroker
	jobQueue     *service.JobQueue
	log          *logger.Logger
}

func NewNetworkHandler(
	service *service.NetworkService,
	networkRepo *repository.NetworkRepository,
	settingsRepo *repository.SettingsRepository,
	sseHandler *SSEHandler,
	broker *sse.EventBroker,
	jobQueue *service.JobQueue,
	log *logger.Logger,
) *NetworkHandler {
	return &NetworkHandler{
		service:      service,
		networkRepo:  networkRepo,
		settingsRepo: settingsRepo,
		sseHandler:   sseHandler,
		broker:       broker,
		jobQueue:     jobQueue,
		log:          log,
	}
}

func (h *NetworkHandler) HandleListNetworks(w http.ResponseWriter, r *http.Request) {
	networks, err := h.networkRepo.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list networks")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"networks":     networks,
		"max_networks": repository.MaxNetworks,
	})
}

type createNetworkRequest struct {
	Name   string `json:"name"`
	Region string `json:"region"`
}

func (h *NetworkHandler) HandleCreateNetwork(w http.ResponseWriter, r *http.Request) {
	var req createNetworkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.log.Debug("create_network_invalid_body", "error", err)
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	req.Region = strings.TrimSpace(req.Region)

	h.log.Debug("create_network_request", "name", req.Name, "region", req.Region)

	if req.Name == "" {
		h.log.Debug("create_network_empty_name")
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.Region == "" {
		h.log.Debug("create_network_empty_region")
		writeError(w, http.StatusBadRequest, "region is required")
		return
	}

	network, err := h.networkRepo.Create(r.Context(), req.Name, req.Region)
	if err != nil {
		h.log.Debug("create_network_repo_create_failed", "error", err)
		if strings.Contains(err.Error(), "maximum") {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create network")
		return
	}

	h.log.Debug("create_network_created", "network_id", network.ID, "name", network.Name, "region", network.Region, "status", network.Status)
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

	vpsCount, err := h.networkRepo.CountVPS(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check network usage")
		return
	}
	if vpsCount > 0 {
		writeError(w, http.StatusConflict, fmt.Sprintf("cannot delete network with %d active VPS instances", vpsCount))
		return
	}

	if h.service != nil {
		h.log.Debug("delete_network_destroying", "network_id", id)
		if err := h.service.DestroyNetwork(r.Context(), id); err != nil {
			h.log.Debug("delete_network_destroy_failed", "error", err)
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to destroy network: %v", err))
			return
		}
		h.log.Debug("delete_network_destroyed", "network_id", id)
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *NetworkHandler) HandleNetworkProvision(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid network id")
		return
	}

	if err := h.jobQueue.Enqueue(context.Background(), service.JobProvisionNetwork, service.NetworkJob{ID: id}); err != nil {
		h.log.Error("network_provision_enqueue_failed", "network_id", id, "error", err)
		http.Error(w, "failed to queue provisioning", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]string{"status": "network_provisioning_queued"})
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
