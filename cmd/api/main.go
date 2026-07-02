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

	"vps-store/internal/config"
	"vps-store/internal/db"
	"vps-store/internal/handler"
	"vps-store/internal/logger"
	"vps-store/internal/repository"
	"vps-store/internal/server"
	"vps-store/internal/service"
	"vps-store/internal/sse"
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

	repository.SeedAll(database, ".", cfg.Dev)

	broker := sse.NewEventBroker()

	sseHandler := handler.NewSSEHandler(broker)
	settingsRepo := repository.NewSettingsRepository(database)
	settingsHandler := handler.NewSettingsHandler(settingsRepo)

	networkRepo := repository.NewNetworkRepository(database)
	templateRepo := repository.NewTemplateRepository(database)
	templateHandler := handler.NewTemplateHandler(templateRepo)
	networkService := service.NewNetworkService(settingsRepo, networkRepo, broker, "terraform", appLogger)
	networkHandler := handler.NewNetworkHandler(networkService, networkRepo, settingsRepo, sseHandler, broker)

	vpsRepo := repository.NewVPSRepository(database)
	ociComputeService := service.NewOCIComputeService(settingsRepo, appLogger)
	vpsProvisionService := service.NewVPSProvisionService(ociComputeService, vpsRepo, networkRepo, templateRepo, broker, settingsRepo, cfg.APIURL)
	vpsHandler := handler.NewVPSHandler(vpsRepo, templateRepo, networkRepo, settingsRepo, vpsProvisionService)

	srv := server.New(
		database, cfg, authService, broker,
		sseHandler, settingsHandler, templateHandler, networkHandler, vpsHandler,
	)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		addr := "0.0.0.0:10000"
		log.Printf("server listening on %s", addr)
		if err := srv.ListenAndServe(addr); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("shutdown: %v", err)
	}

	fmt.Println("server stopped")
}
