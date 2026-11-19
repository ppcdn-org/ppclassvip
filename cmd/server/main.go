package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"ppclassvip/internal/api"
	"ppclassvip/internal/auth"
	"ppclassvip/internal/config"
	"ppclassvip/internal/database"
	"ppclassvip/internal/logger"
	"ppclassvip/internal/middleware"
	"ppclassvip/internal/model"
	"ppclassvip/internal/ppcdn"
	"ppclassvip/internal/realtime"
	"ppclassvip/internal/reaper"
	"ppclassvip/internal/settings"
	"ppclassvip/internal/storage"
	"ppclassvip/internal/store"

	"github.com/joho/godotenv"
)

func main() {
	configPath := flag.String("config", "configs/config.yaml", "path to config file")
	flag.Parse()

	_ = godotenv.Load()
	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("load config failed", "error", err)
		os.Exit(2)
	}

	log := logger.New(cfg.Log)
	slog.SetDefault(log)

	if cfg.Auth.JWTSecret == config.DefaultDevJWTSecret {
		log.Warn("using development JWT secret; set PPCLASS_JWT_SECRET before production")
	}

	db, err := database.Open(cfg.MySQL, log)
	if err != nil {
		log.Error("database init failed", "error", err)
		os.Exit(3)
	}
	defer func() {
		if err := database.Close(db); err != nil {
			log.Warn("close database failed", "error", err)
		}
	}()

	if cfg.MySQL.AutoMigrate {
		if err := database.AutoMigrate(db, model.AllModels()...); err != nil {
			log.Error("database migrate failed", "error", err)
			os.Exit(4)
		}
		log.Info("database migrated")
	}

	tokens, err := auth.NewTokenManager(cfg.Auth.JWTSecret, cfg.Auth.Issuer, cfg.Auth.AccessTTL.Std())
	if err != nil {
		log.Error("token manager init failed", "error", err)
		os.Exit(5)
	}

	fileStorage, err := storage.NewLocal(cfg.Storage)
	if err != nil {
		log.Error("storage init failed", "error", err)
		os.Exit(6)
	}

	var mediaProvider ppcdn.Provider
	if cfg.PPCDN.Enabled {
		mediaProvider = ppcdn.NewClient(cfg.PPCDN)
		log.Info("ppcdn media provider enabled", "base_url", cfg.PPCDN.BaseURL, "app_id", cfg.PPCDN.AppID)
	} else {
		log.Warn("ppcdn media provider disabled; classroom media credentials will be unavailable")
	}

	settingsService, err := settings.NewService(context.Background(), cfg.Classroom, store.NewSettingsStore(db))
	if err != nil {
		log.Error("settings init failed", "error", err)
		os.Exit(7)
	}

	classrooms := store.NewClassroomStore(db)
	recordings := store.NewRecordingStore(db)
	attachments := store.NewAttachmentStore(db)
	audits := store.NewAuditStore(db)
	mediaStats := store.NewMediaStatStore(db)
	adminStore := store.NewAdminStore(db)
	remoteDesktop := store.NewRemoteDesktopStore(db)
	hub := realtime.NewHub()
	controlHub := realtime.NewControlHub()

	router := api.NewRouter(api.Dependencies{
		Config:      cfg,
		DB:          db,
		Logger:      log,
		Tokens:      tokens,
		Users:       store.NewUserStore(db),
		Classrooms:  classrooms,
		Hub:         hub,
		Media:       mediaProvider,
		Storage:     fileStorage,
		Attachments: attachments,
		Recordings:  recordings,
		Audits:      audits,
		MediaStats:  mediaStats,
		Limiter:     middleware.NewRateLimiter(time.Minute),
		Settings:    settingsService,
		Admin:       adminStore,
		Remote:      remoteDesktop,
		Control:     controlHub,
	})

	server := &http.Server{
		Addr:              cfg.HTTP.Addr,
		Handler:           router,
		ReadTimeout:       cfg.HTTP.ReadTimeout.Std(),
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      cfg.HTTP.WriteTimeout.Std(),
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("server starting", "addr", cfg.HTTP.Addr, "env", cfg.App.Env)
		errCh <- server.ListenAndServe()
	}()

	stopCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	classroomReaper := reaper.NewClassroomReaper(cfg, settingsService, classrooms, recordings, attachments, mediaStats, remoteDesktop, controlHub, fileStorage, hub, log)
	go classroomReaper.Run(stopCtx)

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server stopped unexpectedly", "error", err)
			os.Exit(1)
		}
	case <-stopCtx.Done():
		log.Info("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeout.Std())
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Error("server shutdown failed", "error", err)
		os.Exit(1)
	}
	log.Info("server stopped")
}
