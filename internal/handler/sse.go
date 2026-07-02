package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"vps-store/internal/sse"
)

type SSEHandler struct {
	broker *sse.EventBroker
}

func NewSSEHandler(broker *sse.EventBroker) *SSEHandler {
	return &SSEHandler{broker: broker}
}

// HandleVPSEvents — GET /api/vps/{id}/events
func (h *SSEHandler) HandleVPSEvents(w http.ResponseWriter, r *http.Request) {
	instanceID := chi.URLParam(r, "id")
	if instanceID == "" {
		writeError(w, http.StatusBadRequest, "missing instance id")
		return
	}
	h.streamEvents(w, r, "vps:"+instanceID)
}

// HandleNetworkEvents — GET /api/network/events
func (h *SSEHandler) HandleNetworkEvents(w http.ResponseWriter, r *http.Request) {
	h.streamEvents(w, r, "network")
}

// HandleChannelEvents — generic SSE stream for a named channel
func (h *SSEHandler) HandleChannelEvents(w http.ResponseWriter, r *http.Request, channel string) {
	h.streamEvents(w, r, channel)
}

func (h *SSEHandler) streamEvents(w http.ResponseWriter, r *http.Request, channelID string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ch := h.broker.Subscribe(channelID)
	defer h.broker.Unsubscribe(channelID, ch)

	// Replay buffered history so reconnecting clients see the full log
	for _, evt := range h.broker.History(channelID) {
		data, err := json.Marshal(evt)
		if err != nil {
			continue
		}
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	ctx := r.Context()

	for {
		select {
		case evt, ok := <-ch:
			if !ok {
				return
			}
			data, err := json.Marshal(evt)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
				return
			}
			flusher.Flush()

		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()

		case <-ctx.Done():
			return
		}
	}
}
