package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/llassingan/provessor/internal/config"
	"github.com/llassingan/provessor/internal/db"
	"github.com/llassingan/provessor/internal/handler"
	"github.com/llassingan/provessor/internal/logger"
	"github.com/llassingan/provessor/internal/repository"
	"github.com/llassingan/provessor/internal/server"
	"github.com/llassingan/provessor/internal/service"
	"github.com/llassingan/provessor/internal/sse"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	database, err := db.Open("data/provessor.db", cfg.DBEncryptionKey)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer database.Close()

	if err := db.Run(database); err != nil {
		log.Fatalf("migrations: %v", err)
	}

	appLogger, err := logger.New(cfg.Dev, cfg.LogFile)
	if err != nil {
		log.Fatalf("logger: %v", err)
	}
	defer appLogger.Close()
	appLogger.Info("starting provessor", "dev", cfg.Dev, "log_file", cfg.LogFile)

	userRepo := repository.NewUserRepository(database)

	authService, err := service.NewAuthService(userRepo, cfg)
	if err != nil {
		log.Fatalf("auth service: %v", err)
	}

	repository.SeedAll(database, ".", cfg.Dev, appLogger)

	broker := sse.NewEventBroker()

	auditLogRepo := repository.NewAuditLogRepository(database)

	sseHandler := handler.NewSSEHandler(broker)
	settingsRepo := repository.NewSettingsRepository(database)
	settingsHandler := handler.NewSettingsHandler(settingsRepo, appLogger, auditLogRepo)

	networkRepo := repository.NewNetworkRepository(database)
	networkResourceRepo := repository.NewNetworkResourceRepository(database)
	templateRepo := repository.NewTemplateRepository(database)
	templateHandler := handler.NewTemplateHandler(templateRepo, auditLogRepo)
	ociComputeService := service.NewOCIComputeService(settingsRepo, appLogger)
	networkService := service.NewNetworkService(settingsRepo, networkRepo, networkResourceRepo, ociComputeService, broker, appLogger, auditLogRepo)

	vpsRepo := repository.NewVPSRepository(database)

	vpsResourceRepo := repository.NewVPSResourceRepository(database)
	vpsProvisionService := service.NewVPSProvisionService(ociComputeService, vpsRepo, vpsResourceRepo, networkRepo, templateRepo, broker, settingsRepo, cfg.APIURL, appLogger, auditLogRepo)

	jobQueue := service.NewJobQueue(database, networkService, vpsProvisionService, appLogger, auditLogRepo)

	networkHandler := handler.NewNetworkHandler(networkService, networkRepo, settingsRepo, sseHandler, broker, jobQueue, appLogger)
	vpsHandler := handler.NewVPSHandler(vpsRepo, templateRepo, networkRepo, settingsRepo, vpsProvisionService, jobQueue, appLogger, auditLogRepo)

	srv := server.New(
		database, cfg, authService, broker,
		sseHandler, settingsHandler, templateHandler, networkHandler, vpsHandler,
		auditLogRepo,
	)

	reconcileService := service.NewReconcileService(
		networkRepo, vpsRepo, networkResourceRepo, vpsResourceRepo,
		ociComputeService, networkService, vpsProvisionService, broker, appLogger,
	)
	if err := reconcileService.ReconcileOnStartup(context.Background()); err != nil {
		appLogger.Warn("startup_reconciliation_failed", "error", err)
	} else {
		appLogger.Info("startup_reconciliation_complete")
	}

	if err := jobQueue.ResumeOnStartup(context.Background()); err != nil {
		appLogger.Warn("jobqueue_resume_failed", "error", err)
	}

	jobQueue.Start(context.Background())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		addr := "0.0.0.0:10000"
		appLogger.Info("server_listening", "addr", addr)
		if err := srv.ListenAndServe(addr); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server: %v", err)
		}
	}()

	<-ctx.Done()
	appLogger.Info("shutting_down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("shutdown: %v", err)
	}

	jobQueue.Stop()

	fmt.Println("server stopped")
}
