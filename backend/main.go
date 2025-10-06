//go:build opencv

package main

import (
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"syscall"

	"github.com/felixge/fgprof"
	"github.com/rs/zerolog/log"
	_ "github.com/kai5263499/droidcam-sentry/backend/docs" // Swagger docs
	"github.com/kai5263499/droidcam-sentry/backend/internal/config"
	"github.com/kai5263499/droidcam-sentry/backend/internal/logger"
	"github.com/kai5263499/droidcam-sentry/backend/internal/server"
	"github.com/kai5263499/droidcam-sentry/backend/internal/surveillance"
)

// @title DroidCam Sentry API
// @version 0.1.0
// @description Surveillance system API for controlling DroidCam IP cameras with motion detection and recording
// @termsOfService http://swagger.io/terms/

// @contact.name API Support
// @contact.url https://github.com/kai5263499/droidcam-sentry

// @license.name MIT
// @license.url https://opensource.org/licenses/MIT

// @host 192.168.2.149:8080
// @BasePath /
// @schemes http

// @tag.name Cameras
// @tag.description Camera monitoring and control operations

// @tag.name Recordings
// @tag.description Recording control and playback operations

// @tag.name System
// @tag.description System status and configuration

func main() {
	// Initialize logger with INFO level
	logger.Init("info")

	// Start pprof and fgprof server on separate port for performance profiling
	go func() {
		log.Info().Msg("Starting profiling server on :6060")
		log.Info().Str("pprof", "http://localhost:6060/debug/pprof").Msg("Standard pprof available")
		log.Info().Str("fgprof", "http://localhost:6060/debug/fgprof").Msg("Full goroutine profiler available")

		// Register fgprof handler
		http.DefaultServeMux.Handle("/debug/fgprof", fgprof.Handler())

		if err := http.ListenAndServe(":6060", nil); err != nil {
			log.Error().Err(err).Msg("Profiling server error")
		}
	}()

	// Load configuration
	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load configuration")
	}

	log.Info().Str("version", "0.1.0").Msg("Starting droidcam-sentry")
	log.Info().Int("cameras", len(cfg.Cameras)).Msg("Loaded cameras")

	// Initialize surveillance manager
	survMgr := surveillance.NewManager(cfg)
	if err := survMgr.Start(); err != nil {
		log.Fatal().Err(err).Msg("Failed to start surveillance")
	}
	defer survMgr.Stop()

	// Start HTTP API server
	apiServer := server.New(cfg, survMgr)
	go func() {
		if err := apiServer.Start(); err != nil {
			log.Fatal().Err(err).Msg("Failed to start API server")
		}
	}()

	log.Info().Str("url", "http://192.168.2.149:8080/swagger/index.html").Msg("Swagger UI available")

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Info().Msg("Shutting down gracefully...")
	_ = apiServer.Stop()
}
