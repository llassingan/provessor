package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/llassingan/provessor/internal/handler"
)

func (s *Server) mountRoutes() {
	r := s.router
	authHandler := handler.NewAuthHandler(s.authService)

	r.Use(RateLimitAPIByIP(s.limiters.globalAPI))

	r.Get("/api/health", handleHealth)
	r.Get("/api/auth/init", authHandler.HandleInit)
	r.Get("/api/auth/csrf", HandleCSRFToken(s.csrfSecret, !s.config.Dev))

	r.With(RateLimitByIP(s.limiters.signup)).Post("/api/auth/signup", authHandler.HandleSignup)
	r.With(RateLimitByIP(s.limiters.login)).Post("/api/auth/login", authHandler.HandleLogin)
	r.With(CSRFMiddleware(s.csrfSecret)).Post("/api/auth/logout", authHandler.HandleLogout)

	r.Group(func(r chi.Router) {
		r.Use(AuthMiddleware(s.authService))

		if s.templateHandler != nil {
			r.Get("/api/shapes", s.templateHandler.HandleListShapes)
		} else {
			r.Get("/api/shapes", handleListShapesStub)
		}

		r.With(CSRFMiddleware(s.csrfSecret), RateLimitByUser(s.limiters.userActions)).Post("/api/vps", s.vpsHandler.HandleCreateVPS)
		r.Get("/api/vps", s.vpsHandler.HandleListVPS)
		r.Get("/api/vps/{id}", s.vpsHandler.HandleGetVPS)
		r.With(CSRFMiddleware(s.csrfSecret), RateLimitByUser(s.limiters.userActions)).Delete("/api/vps/{id}", s.vpsHandler.HandleDeleteVPS)
		r.With(CSRFMiddleware(s.csrfSecret), RateLimitByUser(s.limiters.userActions)).Post("/api/vps/{id}/start", s.vpsHandler.HandleStartVPS)
		r.With(CSRFMiddleware(s.csrfSecret), RateLimitByUser(s.limiters.userActions)).Post("/api/vps/{id}/stop", s.vpsHandler.HandleStopVPS)
		r.With(CSRFMiddleware(s.csrfSecret), RateLimitByUser(s.limiters.userActions)).Post("/api/vps/{id}/restart", s.vpsHandler.HandleRestartVPS)
		r.With(CSRFMiddleware(s.csrfSecret), RateLimitByUser(s.limiters.userActions)).Post("/api/vps/{id}/reset", s.vpsHandler.HandleResetVPS)
		r.With(CSRFMiddleware(s.csrfSecret), RateLimitByUser(s.limiters.userActions)).Post("/api/vps/{id}/reset-password", s.vpsHandler.HandleResetPasswordVPS)
		r.With(CSRFMiddleware(s.csrfSecret), RateLimitByUser(s.limiters.userActions)).Post("/api/vps/{id}/terminate", s.vpsHandler.HandleTerminateVPS)
		r.Get("/api/vps/{id}/firewall", s.vpsHandler.HandleGetFirewall)
		r.With(CSRFMiddleware(s.csrfSecret), RateLimitByUser(s.limiters.userActions)).Post("/api/vps/{id}/firewall", s.vpsHandler.HandleUpdateFirewall)
		r.With(CSRFMiddleware(s.csrfSecret), RateLimitByUser(s.limiters.userActions)).Post("/api/vps/{id}/refresh-ips", s.vpsHandler.HandleRefreshIPs)

		if s.sseHandler != nil {
			r.Get("/api/vps/{id}/events", s.sseHandler.HandleVPSEvents)
			r.Get("/api/network/events", s.sseHandler.HandleNetworkEvents)
		} else {
			r.Get("/api/vps/{id}/events", handleSSEEventsStub)
			r.Get("/api/network/events", handleNetworkSSEStub)
		}

		if s.templateHandler != nil {
			r.Get("/api/templates", s.templateHandler.HandleListTemplates)
			r.With(CSRFMiddleware(s.csrfSecret), RateLimitByUser(s.limiters.userActions)).Post("/api/templates", s.templateHandler.HandleCreateTemplate)
		} else {
			r.Get("/api/templates", handleListTemplatesStub)
			r.With(CSRFMiddleware(s.csrfSecret), RateLimitByUser(s.limiters.userActions)).Post("/api/templates", handleCreateTemplateStub)
		}

		if s.settingsHandler != nil {
			r.Get("/api/settings", s.settingsHandler.HandleGetSettings)
			r.With(CSRFMiddleware(s.csrfSecret), RateLimitByUser(s.limiters.userActions)).Put("/api/settings", s.settingsHandler.HandleUpdateSettings)
			r.Get("/api/regions", s.settingsHandler.HandleListRegions)
		} else {
			r.Get("/api/settings", handleGetSettingsStub)
			r.With(CSRFMiddleware(s.csrfSecret), RateLimitByUser(s.limiters.userActions)).Put("/api/settings", handleUpdateSettingsStub)
			r.Get("/api/regions", handleListRegionsStub)
		}

		if s.networkHandler != nil {
			r.With(CSRFMiddleware(s.csrfSecret), RateLimitByUser(s.limiters.userActions)).Post("/api/networks", s.networkHandler.HandleCreateNetwork)
			r.Get("/api/networks", s.networkHandler.HandleListNetworks)
			r.Get("/api/networks/{id}", s.networkHandler.HandleGetNetwork)
			r.With(CSRFMiddleware(s.csrfSecret), RateLimitByUser(s.limiters.userActions)).Delete("/api/networks/{id}", s.networkHandler.HandleDeleteNetwork)
			r.With(CSRFMiddleware(s.csrfSecret), RateLimitByUser(s.limiters.userActions)).Post("/api/networks/{id}/provision", s.networkHandler.HandleNetworkProvision)
			r.Get("/api/networks/{id}/events", s.networkHandler.HandleNetworkProvisionEvents)

			r.With(CSRFMiddleware(s.csrfSecret), RateLimitByUser(s.limiters.userActions)).Post("/api/network/setup", s.networkHandler.HandleOldNetworkSetup)
			r.Get("/api/network/status", s.networkHandler.HandleOldNetworkStatus)
		} else {
			r.With(CSRFMiddleware(s.csrfSecret), RateLimitByUser(s.limiters.userActions)).Post("/api/networks", handleCreateNetworkStub)
			r.Get("/api/networks", handleListNetworksStub)
			r.Get("/api/networks/{id}", handleGetNetworkStub)
			r.With(CSRFMiddleware(s.csrfSecret), RateLimitByUser(s.limiters.userActions)).Delete("/api/networks/{id}", handleDeleteNetworkStub)
			r.With(CSRFMiddleware(s.csrfSecret), RateLimitByUser(s.limiters.userActions)).Post("/api/networks/{id}/provision", handleNetworkProvisionStub)
			r.Get("/api/networks/{id}/events", handleNetworkEventsStub)

			r.With(CSRFMiddleware(s.csrfSecret), RateLimitByUser(s.limiters.userActions)).Post("/api/network/setup", handleNetworkSetupStub)
			r.Get("/api/network/status", handleNetworkStatusStub)
		}
	})

	r.With(RateLimitByIP(s.limiters.credentialsCallback)).Post("/api/vps/{id}/credentials", s.vpsHandler.HandleCredentialsCallback)
}

func handleListTemplatesStub(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "not implemented yet"})
}

func handleListShapesStub(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, []interface{}{})
}

func handleCreateTemplateStub(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "not implemented yet"})
}

func handleCreateNetworkStub(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "not implemented yet"})
}

func handleListNetworksStub(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, []interface{}{})
}

func handleGetNetworkStub(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "not implemented yet"})
}

func handleDeleteNetworkStub(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func handleNetworkProvisionStub(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "network_provisioning_started"})
}

func handleNetworkEventsStub(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "not implemented yet"})
}

func handleGetSettingsStub(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "not implemented yet"})
}

func handleUpdateSettingsStub(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "not implemented yet"})
}

func handleListRegionsStub(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, []interface{}{})
}

func handleNetworkSetupStub(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "not implemented yet"})
}

func handleNetworkStatusStub(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "not implemented yet"})
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status":    "ok",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}

func handleCreateVPSStub(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "not implemented yet"})
}

func handleListVPSStub(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "not implemented yet"})
}

func handleGetVPSStub(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "not implemented yet"})
}

func handleDeleteVPSStub(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "not implemented yet"})
}

func handleSSEEventsStub(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "not implemented yet"})
}

func handleNetworkSSEStub(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "not implemented yet"})
}

func handleCredentialsCallbackStub(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "not implemented yet"})
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
