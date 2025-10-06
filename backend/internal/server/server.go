//go:build opencv

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	httpSwagger "github.com/swaggo/http-swagger"
	"github.com/kai5263499/droidcam-sentry/backend/internal/config"
	"github.com/kai5263499/droidcam-sentry/backend/internal/surveillance"
)

type Server struct {
	cfg     *config.Config
	survMgr *surveillance.Manager
	srv     *http.Server
}

func New(cfg *config.Config, survMgr *surveillance.Manager) *Server {

	return &Server{
		cfg:     cfg,
		survMgr: survMgr,
	}
}

func (s *Server) Start() error {
	mux := http.NewServeMux()

	// API routes
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/cameras", s.handleCameras)
	mux.HandleFunc("/api/cameras/", s.handleCameraUpdate)
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/recordings", s.handleRecordings)

	// Camera control routes
	mux.HandleFunc("/api/cameras/start/", s.handleCameraStart)
	mux.HandleFunc("/api/cameras/stop/", s.handleCameraStop)
	mux.HandleFunc("/api/cameras/live/", s.handleCameraLive)

	// Motion detection control routes
	mux.HandleFunc("/api/motion-detection/enable/", s.handleMotionDetectionEnable)
	mux.HandleFunc("/api/motion-detection/disable/", s.handleMotionDetectionDisable)

	// Video serving routes
	mux.HandleFunc("/api/recordings/play", s.handleRecordingPlay)
	mux.HandleFunc("/api/recordings/download", s.handleRecordingDownload)
	mux.HandleFunc("/api/recordings/delete", s.handleRecordingDelete)

	// Swagger UI
	mux.HandleFunc("/swagger/", httpSwagger.WrapHandler)

	// Serve frontend
	mux.HandleFunc("/", s.handleFrontend)

	// Health check
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	addr := fmt.Sprintf("%s:%d", s.cfg.Server.Host, s.cfg.Server.Port)
	s.srv = &http.Server{
		Addr:    addr,
		Handler: s.corsMiddleware(mux),
	}

	log.Printf("Starting API server on %s", addr)
	return s.srv.ListenAndServe()
}

func (s *Server) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.srv.Shutdown(ctx)
}

// CORS middleware for web app
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// Serve frontend files
func (s *Server) handleFrontend(w http.ResponseWriter, r *http.Request) {
	// Determine frontend directory (sibling to backend)
	frontendDir := "frontend"

	if r.URL.Path == "/" {
		http.ServeFile(w, r, filepath.Join(frontendDir, "index.html"))
		return
	}

	// Serve static files
	filePath := filepath.Join(frontendDir, r.URL.Path)
	if _, err := os.Stat(filePath); err == nil {
		http.ServeFile(w, r, filePath)
		return
	}

	// 404
	http.NotFound(w, r)
}

// handleRecordingPlay godoc
// @Summary Stream recording for playback
// @Tags Recordings
// @Param file query string true "Recording file path"
// @Produce video/mp4
// @Success 200
// @Failure 400 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /api/recordings/play [get]
func (s *Server) handleRecordingPlay(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	filePath := r.URL.Query().Get("file")
	if filePath == "" {
		respondError(w, http.StatusBadRequest, "file parameter required")
		return
	}

	// Security check: ensure file exists and is within allowed paths
	if !s.isValidRecordingPath(filePath) {
		respondError(w, http.StatusForbidden, "Access denied")
		return
	}

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		respondError(w, http.StatusNotFound, "Recording not found")
		return
	}

	// Serve video with proper content type
	w.Header().Set("Content-Type", "video/mp4")
	http.ServeFile(w, r, filePath)
}

// handleRecordingDownload godoc
// @Summary Download recording file
// @Tags Recordings
// @Param file query string true "Recording file path"
// @Produce application/octet-stream
// @Success 200
// @Failure 400 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /api/recordings/download [get]
func (s *Server) handleRecordingDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	filePath := r.URL.Query().Get("file")
	if filePath == "" {
		respondError(w, http.StatusBadRequest, "file parameter required")
		return
	}

	// Security check
	if !s.isValidRecordingPath(filePath) {
		respondError(w, http.StatusForbidden, "Access denied")
		return
	}

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		respondError(w, http.StatusNotFound, "Recording not found")
		return
	}

	// Force download with filename
	filename := filepath.Base(filePath)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))

	file, err := os.Open(filePath)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to open file")
		return
	}
	defer file.Close()

	io.Copy(w, file)
}

// Validate recording path is within configured recording directories
func (s *Server) isValidRecordingPath(filePath string) bool {
	cfg := s.cfg.Get()

	// Get absolute path
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return false
	}

	// Check if path is within any camera recording directory
	for _, camCfg := range cfg.Cameras {
		recordingPath, err := filepath.Abs(camCfg.Recording.Path)
		if err != nil {
			continue
		}

		// Check if file is within recording directory
		if strings.HasPrefix(absPath, recordingPath) {
			return true
		}
	}

	return false
}

// handleCameraStart godoc
// @Summary Start camera monitoring
// @Tags Cameras
// @Param name path string true "Camera name"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /api/cameras/start/{name} [post]
func (s *Server) handleCameraStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	cameraName := strings.TrimPrefix(r.URL.Path, "/api/cameras/start/")
	if cameraName == "" {
		respondError(w, http.StatusBadRequest, "Camera name required")
		return
	}

	if err := s.survMgr.StartCamera(cameraName); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{
		"status":  "started",
		"camera":  cameraName,
		"message": fmt.Sprintf("Camera %s started successfully", cameraName),
	})
}

// handleCameraStop godoc
// @Summary Stop camera monitoring
// @Tags Cameras
// @Param name path string true "Camera name"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /api/cameras/stop/{name} [post]
func (s *Server) handleCameraStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	cameraName := strings.TrimPrefix(r.URL.Path, "/api/cameras/stop/")
	if cameraName == "" {
		respondError(w, http.StatusBadRequest, "Camera name required")
		return
	}

	if err := s.survMgr.StopCamera(cameraName); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{
		"status":  "stopped",
		"camera":  cameraName,
		"message": fmt.Sprintf("Camera %s stopped successfully", cameraName),
	})
}

// handleMotionDetectionEnable godoc
// @Summary Enable motion detection
// @Tags Motion Detection
// @Param name path string true "Camera name"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /api/motion-detection/enable/{name} [post]
func (s *Server) handleMotionDetectionEnable(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	cameraName := strings.TrimPrefix(r.URL.Path, "/api/motion-detection/enable/")
	if cameraName == "" {
		respondError(w, http.StatusBadRequest, "Camera name required")
		return
	}

	if err := s.survMgr.EnableMotionDetection(cameraName); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{
		"status":  "enabled",
		"camera":  cameraName,
		"message": fmt.Sprintf("Motion detection enabled for camera %s", cameraName),
	})
}

// handleMotionDetectionDisable godoc
// @Summary Disable motion detection
// @Tags Motion Detection
// @Param name path string true "Camera name"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /api/motion-detection/disable/{name} [post]
func (s *Server) handleMotionDetectionDisable(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	cameraName := strings.TrimPrefix(r.URL.Path, "/api/motion-detection/disable/")
	if cameraName == "" {
		respondError(w, http.StatusBadRequest, "Camera name required")
		return
	}

	if err := s.survMgr.DisableMotionDetection(cameraName); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{
		"status":  "disabled",
		"camera":  cameraName,
		"message": fmt.Sprintf("Motion detection disabled for camera %s", cameraName),
	})
}

// handleConfig godoc
// @Summary Get or update configuration
// @Tags Configuration
// @Accept json
// @Produce json
// @Success 200 {object} config.Snapshot
// @Failure 400 {object} map[string]string
// @Router /api/config [get]
// @Router /api/config [put]
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg := s.cfg.Get()
		respondJSON(w, http.StatusOK, cfg)

	case http.MethodPut:
		var updates map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
			respondError(w, http.StatusBadRequest, "Invalid JSON")
			return
		}

		s.cfg.Update(func(c *config.Config) {
			s.applyConfigUpdates(c, updates)
		})

		if err := s.cfg.Save("config.yaml"); err != nil {
			log.Printf("Warning: failed to save config: %v", err)
		}

		respondJSON(w, http.StatusOK, map[string]string{"status": "updated"})

	default:
		respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// handleCameras godoc
// @Summary List all cameras
// @Tags Cameras
// @Produce json
// @Success 200 {array} config.CameraConfig
// @Router /api/cameras [get]
func (s *Server) handleCameras(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	cfg := s.cfg.Get()
	respondJSON(w, http.StatusOK, cfg.Cameras)
}

// handleCameraUpdate godoc
// @Summary Update camera configuration
// @Tags Cameras
// @Param name path string true "Camera name"
// @Accept json
// @Produce json
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /api/cameras/{name} [put]
func (s *Server) handleCameraUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Extract camera name from path
	name := r.URL.Path[len("/api/cameras/"):]

	var updates map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}

	found := false
	s.cfg.Update(func(c *config.Config) {
		for i := range c.Cameras {
			if c.Cameras[i].Name == name {
				s.applyCameraUpdates(&c.Cameras[i], updates)
				found = true
				break
			}
		}
	})

	if !found {
		respondError(w, http.StatusNotFound, "Camera not found")
		return
	}

	s.cfg.Save("config.yaml")
	respondJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// handleStatus godoc
// @Summary Get system status
// @Tags System
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /api/status [get]
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	status := s.survMgr.GetStatus()
	respondJSON(w, http.StatusOK, status)
}

// handleRecordings godoc
// @Summary List all recordings
// @Tags Recordings
// @Produce json
// @Success 200 {array} map[string]interface{}
// @Router /api/recordings [get]
func (s *Server) handleRecordings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	recordings := s.survMgr.GetRecordings()
	respondJSON(w, http.StatusOK, recordings)
}

func (s *Server) applyConfigUpdates(c *config.Config, updates map[string]interface{}) {
	// Apply motion config updates
	if motion, ok := updates["motion"].(map[string]interface{}); ok {
		if interval, ok := motion["detection_interval_ms"].(float64); ok {
			c.Motion.DetectionIntervalMs = int(interval)
		}
		if minArea, ok := motion["min_area"].(float64); ok {
			c.Motion.MinArea = int(minArea)
		}
	}

	// Apply storage config updates
	if storage, ok := updates["storage"].(map[string]interface{}); ok {
		if maxSize, ok := storage["max_recording_size_mb"].(float64); ok {
			c.Storage.MaxRecordingSizeMB = int(maxSize)
		}
		if retention, ok := storage["retention_days"].(float64); ok {
			c.Storage.RetentionDays = int(retention)
		}
	}
}

func (s *Server) applyCameraUpdates(cam *config.CameraConfig, updates map[string]interface{}) {
	if enabled, ok := updates["enabled"].(bool); ok {
		cam.Enabled = enabled
	}
	if url, ok := updates["url"].(string); ok {
		cam.URL = url
	}
	if threshold, ok := updates["motion_threshold"].(float64); ok {
		cam.MotionThreshold = threshold
	}
}

func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func respondError(w http.ResponseWriter, status int, message string) {
	respondJSON(w, status, map[string]string{"error": message})
}

// handleCameraLive godoc
// @Summary Stream live MJPEG video
// @Tags Cameras
// @Param name path string true "Camera name"
// @Produce multipart/x-mixed-replace
// @Success 200
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /api/cameras/live/{name} [get]
func (s *Server) handleCameraLive(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	cameraName := strings.TrimPrefix(r.URL.Path, "/api/cameras/live/")
	if cameraName == "" {
		respondError(w, http.StatusBadRequest, "Camera name required")
		return
	}

	// Subscribe to camera frames
	frameChan, err := s.survMgr.Subscribe(cameraName)
	if err != nil {
		respondError(w, http.StatusNotFound, err.Error())
		return
	}
	defer s.survMgr.Unsubscribe(cameraName, frameChan)

	// Set MJPEG stream headers
	w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=frame")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "close")

	// Stream frames
	for {
		select {
		case frameBytes, ok := <-frameChan:
			if !ok {
				return
			}

			// Write MJPEG multipart frame
			fmt.Fprintf(w, "--frame\r\n")
			fmt.Fprintf(w, "Content-Type: image/jpeg\r\n")
			fmt.Fprintf(w, "Content-Length: %d\r\n\r\n", len(frameBytes))
			w.Write(frameBytes)
			fmt.Fprintf(w, "\r\n")

			// Flush to client
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}

		case <-r.Context().Done():
			// Client disconnected
			return
		}
	}
}

// handleRecordingDelete godoc
// @Summary Delete recording file
// @Tags Recordings
// @Param file query string true "Recording file path"
// @Produce json
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /api/recordings/delete [delete]
func (s *Server) handleRecordingDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	filePath := r.URL.Query().Get("file")
	if filePath == "" {
		respondError(w, http.StatusBadRequest, "file parameter required")
		return
	}

	// Security check
	if !s.isValidRecordingPath(filePath) {
		respondError(w, http.StatusForbidden, "Access denied")
		return
	}

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		respondError(w, http.StatusNotFound, "Recording not found")
		return
	}

	// Delete the file
	if err := os.Remove(filePath); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to delete file: %v", err))
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{
		"status": "deleted",
		"file":   filepath.Base(filePath),
	})
}
