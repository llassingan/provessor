package server

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"

	"github.com/llassingan/provessor/internal/config"
	"github.com/llassingan/provessor/internal/handler"
	"github.com/llassingan/provessor/internal/service"
	"github.com/llassingan/provessor/internal/sse"
)

type Server struct {
	router      *chi.Mux
	httpServer  *http.Server
	db          *sql.DB
	config      *config.Config
	authService *service.AuthService
	broker      *sse.EventBroker

	sseHandler      *handler.SSEHandler
	settingsHandler *handler.SettingsHandler
	templateHandler *handler.TemplateHandler
	networkHandler  *handler.NetworkHandler
	vpsHandler      *handler.VPSHandler
}

func New(
	db *sql.DB,
	cfg *config.Config,
	authService *service.AuthService,
	broker *sse.EventBroker,
	sseHandler *handler.SSEHandler,
	settingsHandler *handler.SettingsHandler,
	templateHandler *handler.TemplateHandler,
	networkHandler *handler.NetworkHandler,
	vpsHandler *handler.VPSHandler,
) *Server {
	s := &Server{
		router:          chi.NewRouter(),
		db:              db,
		config:          cfg,
		authService:     authService,
		broker:          broker,
		sseHandler:      sseHandler,
		settingsHandler: settingsHandler,
		templateHandler: templateHandler,
		networkHandler:  networkHandler,
		vpsHandler:      vpsHandler,
	}

	s.setupCORS()
	s.mountRoutes()

	return s
}

func (s *Server) ListenAndServe(addr string) error {
	s.httpServer = &http.Server{
		Addr:        addr,
		Handler:     s.router,
		ReadTimeout: 15 * time.Second,
		IdleTimeout: 60 * time.Second,
	}
	return s.httpServer.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	if s.broker != nil {
		s.broker.Close()
	}
	if s.httpServer == nil {
		return nil
	}
	return s.httpServer.Shutdown(ctx)
}

func (s *Server) setupCORS() {
	s.router.Use(cors.Handler(cors.Options{
		AllowedOrigins:   s.config.CORSOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: true,
		MaxAge:           300,
	}))
}
