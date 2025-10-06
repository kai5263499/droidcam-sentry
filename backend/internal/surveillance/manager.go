//go:build opencv

package surveillance

import (
	"fmt"

	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/kai5263499/droidcam-sentry/backend/internal/config"
	"github.com/kai5263499/droidcam-sentry/backend/internal/health"
	"github.com/kai5263499/droidcam-sentry/backend/internal/motion"
	"github.com/kai5263499/droidcam-sentry/backend/internal/recorder"
	"github.com/kai5263499/droidcam-sentry/backend/pkg/camera"
	"github.com/rs/zerolog/log"
	"gocv.io/x/gocv"
)

type Manager struct {
	cfg           *config.Config
	monitors      map[string]*CameraMonitor
	mu            sync.RWMutex
	durationCache map[string]string
	cacheMu       sync.RWMutex
	healthChecker *health.Checker
	healthCache   map[string]health.CheckResult
	healthMu      sync.RWMutex
}

type CameraMonitor struct {
	Name                string
	Enabled             bool
	MotionDetectEnabled bool
	stream              *camera.Stream
	detector            *motion.Detector
	recorder            *recorder.VideoRecorder
	stopChan            chan struct{}
	running             bool
	subscribers         []chan []byte
	mu                  sync.RWMutex
}

func NewManager(cfg *config.Config) *Manager {
	log.Info().Int("health_check_interval", cfg.Health.CheckIntervalSeconds).Int("health_timeout", cfg.Health.TimeoutSeconds).Msg("Health config loaded")
	mgr := &Manager{
		cfg:           cfg,
		monitors:      make(map[string]*CameraMonitor),
		durationCache: make(map[string]string),
		healthChecker: health.NewChecker(time.Duration(cfg.Health.TimeoutSeconds) * time.Second),
		healthCache:   make(map[string]health.CheckResult),
	}

	cfg.Subscribe(mgr.onConfigChange)

	// Start background duration scanner
	go mgr.scanVideoDurations()

	// Start background health checker
	go mgr.runHealthChecks()

	return mgr
}

func (m *Manager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cfg := m.cfg.Get()
	for _, camCfg := range cfg.Cameras {
		if camCfg.Enabled {
			if err := m.startMonitor(camCfg); err != nil {
				log.Error().Str("camera", camCfg.Name).Err(err).Msg("Failed to start monitor")
				continue
			}
		}
	}

	return nil
}

func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for name, monitor := range m.monitors {
		log.Info().Str("monitor", name).Msg("Stopping monitor")
		m.stopMonitor(monitor)
	}
}

// StartCamera starts monitoring for a specific camera
func (m *Manager) StartCamera(cameraName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if already running
	if monitor, exists := m.monitors[cameraName]; exists && monitor.running {
		return fmt.Errorf("camera %s is already running", cameraName)
	}

	// Find camera config
	cfg := m.cfg.Get()
	var camCfg *config.CameraConfig
	for i := range cfg.Cameras {
		if cfg.Cameras[i].Name == cameraName {
			camCfg = &cfg.Cameras[i]
			break
		}
	}

	if camCfg == nil {
		return fmt.Errorf("camera %s not found in configuration", cameraName)
	}

	return m.startMonitor(*camCfg)
}

// StopCamera stops monitoring for a specific camera
func (m *Manager) StopCamera(cameraName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	monitor, exists := m.monitors[cameraName]
	if !exists {
		return fmt.Errorf("camera %s not found", cameraName)
	}

	if !monitor.running {
		return fmt.Errorf("camera %s is not running", cameraName)
	}

	m.stopMonitor(monitor)
	delete(m.monitors, cameraName)

	log.Info().Str("camera", cameraName).Msg("Camera stopped")
	return nil
}

// EnableMotionDetection enables motion detection for a specific camera
func (m *Manager) EnableMotionDetection(cameraName string) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	monitor, exists := m.monitors[cameraName]
	if !exists {
		return fmt.Errorf("camera %s not found", cameraName)
	}

	if !monitor.running {
		return fmt.Errorf("camera %s is not running", cameraName)
	}

	monitor.mu.Lock()
	monitor.MotionDetectEnabled = true
	monitor.mu.Unlock()

	log.Info().Str("camera", cameraName).Msg("Motion detection enabled")
	return nil
}

// DisableMotionDetection disables motion detection for a specific camera
func (m *Manager) DisableMotionDetection(cameraName string) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	monitor, exists := m.monitors[cameraName]
	if !exists {
		return fmt.Errorf("camera %s not found", cameraName)
	}

	if !monitor.running {
		return fmt.Errorf("camera %s is not running", cameraName)
	}

	monitor.mu.Lock()
	monitor.MotionDetectEnabled = false
	monitor.mu.Unlock()

	// Stop any active recording
	if monitor.recorder.IsRecording() {
		monitor.recorder.Stop()
	}

	log.Info().Str("camera", cameraName).Msg("Motion detection disabled")
	return nil
}

func (m *Manager) startMonitor(camCfg config.CameraConfig) error {
	log.Info().Str("camera", camCfg.Name).Str("url", camCfg.URL).Msg("Starting monitor")

	cfg := m.cfg.Get()

	stream := camera.NewStream(camCfg.Name, camCfg.URL)
	if err := stream.Open(); err != nil {
		return err
	}

	detector := motion.NewDetector(
		camCfg.Name,
		camCfg.MotionThreshold,
		cfg.Motion.MinArea,
		cfg.Motion.DetectionIntervalMs,
	)

	rec := recorder.NewRecorder(
		camCfg.Name,
		camCfg.Recording.Path,
		30.0, // FPS
		camCfg.Recording.PreBufferSeconds,
		camCfg.Recording.PostBufferSeconds,
	)

	monitor := &CameraMonitor{
		Name:                camCfg.Name,
		Enabled:             true,
		MotionDetectEnabled: true,
		stream:              stream,
		detector:            detector,
		recorder:            rec,
		stopChan:            make(chan struct{}),
		running:             true,
	}

	m.monitors[camCfg.Name] = monitor

	// Start monitoring loop
	go m.monitorLoop(monitor)

	return nil
}

func (m *Manager) monitorLoop(monitor *CameraMonitor) {
	log.Info().Str("camera", monitor.Name).Msg("Monitor loop started")
	defer log.Info().Str("camera", monitor.Name).Msg("Monitor loop stopped")

	ticker := time.NewTicker(33 * time.Millisecond) // ~30 FPS
	defer ticker.Stop()

	for {
		select {
		case <-monitor.stopChan:
			return
		case <-ticker.C:
			frame, err := monitor.stream.ReadFrame()
			if err != nil {
				log.Error().Str("camera", monitor.Name).Err(err).Msg("Error reading frame")

				// Try to reconnect
				if err := monitor.stream.Reconnect(); err != nil {
					log.Error().Str("camera", monitor.Name).Err(err).Msg("Reconnect failed")
					time.Sleep(5 * time.Second)
				}
				continue
			}

			// Add frame to recorder buffer
			monitor.recorder.AddFrame(frame)

			// Broadcast frame to live viewers (clone frame first to avoid race)
			monitor.mu.RLock()
			hasSubscribers := len(monitor.subscribers) > 0
			monitor.mu.RUnlock()

			if hasSubscribers && !frame.Empty() {
				// Clone frame before encoding to avoid race with frame.Close()
				clonedFrame := frame.Clone()
				buf, err := gocv.IMEncode(".jpg", clonedFrame)
				clonedFrame.Close() // Clean up clone immediately

				if err == nil {
					frameBytes := buf.GetBytes()
					if len(frameBytes) > 0 {
						monitor.mu.RLock()
						for _, sub := range monitor.subscribers {
							select {
							case sub <- frameBytes:
							default:
								// Skip if channel is full
							}
						}
						monitor.mu.RUnlock()
					}
					buf.Close()
				}
			}

			// Detect motion only if enabled
			monitor.mu.RLock()
			motionEnabled := monitor.MotionDetectEnabled
			monitor.mu.RUnlock()

			if motionEnabled {
				detection, motionDetected := monitor.detector.Detect(frame)
				if motionDetected {
					// Start recording if not already recording
					if !monitor.recorder.IsRecording() {
						if err := monitor.recorder.StartRecording(); err != nil {
							log.Error().Str("camera", monitor.Name).Err(err).Msg("Failed to start recording")
						}
					}

					// Reset post-buffer timer
					monitor.recorder.OnMotion()

					// Clean up detection frame
					if detection != nil && !detection.Frame.Empty() {
						detection.Frame.Close()
					}
				}
			}

			// Update recorder (check if post-buffer expired)
			monitor.recorder.Update()

			// Clean up frame
			if !frame.Empty() {
				frame.Close()
			}
		}
	}
}

func (m *Manager) stopMonitor(monitor *CameraMonitor) {
	if !monitor.running {
		return
	}

	close(monitor.stopChan)
	monitor.running = false

	if monitor.stream != nil {
		monitor.stream.Close()
	}
	if monitor.detector != nil {
		monitor.detector.Close()
	}
	if monitor.recorder != nil {
		monitor.recorder.Close()
	}
}

func (m *Manager) onConfigChange(cfg *config.Config) {
	log.Info().Msg("Configuration changed, reloading cameras...")
	// TODO: Handle camera enable/disable, URL changes, etc.
}

func (m *Manager) GetStatus() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	status := make(map[string]interface{})
	cameras := make([]map[string]interface{}, 0)

	cfg := m.cfg.Get()

	// Loop through ALL configured cameras, not just running monitors
	for _, camCfg := range cfg.Cameras {
		name := camCfg.Name
		monitor, monitorExists := m.monitors[name]

		camStatus := map[string]interface{}{
			"name":             name,
			"enabled":          camCfg.Enabled,
			"running":          false,
			"is_open":          false,
			"recording":        false,
			"motion_detection": false,
		}

		// If monitor exists and is running, get runtime status
		if monitorExists {
			monitor.mu.RLock()
			motionEnabled := monitor.MotionDetectEnabled
			monitor.mu.RUnlock()

			camStatus["running"] = monitor.running
			camStatus["is_open"] = monitor.stream.IsOpen()
			camStatus["recording"] = monitor.recorder.IsRecording()
			camStatus["motion_detection"] = motionEnabled

			// Add stream info if available
			if monitor.stream != nil && monitor.stream.IsOpen() {
				info := monitor.stream.GetInfo()
				if info.Width > 0 {
					camStatus["resolution"] = info.Resolution
					camStatus["width"] = info.Width
					camStatus["height"] = info.Height
					camStatus["fps"] = info.FPS
					camStatus["codec"] = info.Codec
				}
			}
		}

		// Add health check results (available for all cameras)
		m.healthMu.RLock()
		if healthResult, exists := m.healthCache[name]; exists {
			camStatus["health"] = healthResult
		}
		m.healthMu.RUnlock()

		cameras = append(cameras, camStatus)
	}

	status["cameras"] = cameras
	status["version"] = "0.1.0"

	// Get disk space for recording directory
	if len(cfg.Cameras) > 0 {
		recordPath := cfg.Cameras[0].Recording.Path
		var stat syscall.Statfs_t
		if err := syscall.Statfs(recordPath, &stat); err == nil {
			// Available blocks * block size
			availableBytes := stat.Bavail * uint64(stat.Bsize)
			totalBytes := stat.Blocks * uint64(stat.Bsize)

			availableGB := float64(availableBytes) / (1024 * 1024 * 1024)
			totalGB := float64(totalBytes) / (1024 * 1024 * 1024)
			recordingsGB := m.calculateRecordingsSize()

			status["storage"] = map[string]interface{}{
				"available_gb":  fmt.Sprintf("%.2f", availableGB),
				"total_gb":      fmt.Sprintf("%.2f", totalGB),
				"recordings_gb": fmt.Sprintf("%.2f", recordingsGB),
				"path":          recordPath,
			}
		}
	}

	return status
}

// Subscribe to live frames from a camera
func (m *Manager) Subscribe(cameraName string) (chan []byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	monitor, exists := m.monitors[cameraName]
	if !exists {
		return nil, fmt.Errorf("camera %s not found", cameraName)
	}

	if !monitor.running {
		return nil, fmt.Errorf("camera %s is not running", cameraName)
	}

	ch := make(chan []byte, 5) // Buffer 5 frames
	monitor.mu.Lock()
	monitor.subscribers = append(monitor.subscribers, ch)
	monitor.mu.Unlock()

	return ch, nil
}

// Unsubscribe from live frames
func (m *Manager) Unsubscribe(cameraName string, ch chan []byte) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	monitor, exists := m.monitors[cameraName]
	if !exists {
		return
	}

	monitor.mu.Lock()
	for i, sub := range monitor.subscribers {
		if sub == ch {
			monitor.subscribers = append(monitor.subscribers[:i], monitor.subscribers[i+1:]...)
			close(ch)
			break
		}
	}
	monitor.mu.Unlock()
}

// scanVideoDurations runs in background to probe video durations
func (m *Manager) scanVideoDurations() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// Initial scan
	m.updateDurations()

	for range ticker.C {
		m.updateDurations()
	}
}

// updateDurations scans all recordings and updates duration cache
func (m *Manager) updateDurations() {
	cfg := m.cfg.Get()

	for _, camCfg := range cfg.Cameras {
		recordingPath := camCfg.Recording.Path
		if _, err := os.Stat(recordingPath); os.IsNotExist(err) {
			continue
		}

		filepath.Walk(recordingPath, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}

			if !strings.HasSuffix(strings.ToLower(info.Name()), ".mp4") {
				return nil
			}

			// Check if we already have duration cached
			m.cacheMu.RLock()
			_, exists := m.durationCache[path]
			m.cacheMu.RUnlock()

			if exists {
				return nil
			}

			// Probe video duration using ffprobe
			duration := m.probeDuration(path)

			m.cacheMu.Lock()
			m.durationCache[path] = duration
			m.cacheMu.Unlock()

			return nil
		})
	}
}

// probeDuration uses ffprobe to get video duration
func (m *Manager) probeDuration(filepath string) string {
	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		filepath)

	output, err := cmd.Output()
	if err != nil {
		return "Unknown"
	}

	durationStr := strings.TrimSpace(string(output))
	seconds, err := strconv.ParseFloat(durationStr, 64)
	if err != nil {
		return "Unknown"
	}

	// Format as MM:SS
	mins := int(seconds) / 60
	secs := int(seconds) % 60
	return fmt.Sprintf("%d:%02d", mins, secs)
}

func (m *Manager) GetRecordings() []map[string]interface{} {
	cfg := m.cfg.Get()
	recordings := make([]map[string]interface{}, 0)

	// Scan all camera recording directories
	for _, camCfg := range cfg.Cameras {
		recordingPath := camCfg.Recording.Path

		// Check if directory exists
		if _, err := os.Stat(recordingPath); os.IsNotExist(err) {
			continue
		}

		// Walk directory and find MP4 files
		filepath.Walk(recordingPath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}

			// Only include MP4 files
			if !info.IsDir() && strings.HasSuffix(strings.ToLower(info.Name()), ".mp4") {
				sizeMB := float64(info.Size()) / (1024 * 1024)

				// Get duration from cache
				m.cacheMu.RLock()
				duration, exists := m.durationCache[path]
				m.cacheMu.RUnlock()

				if !exists {
					duration = "Unknown"
				}

				recordings = append(recordings, map[string]interface{}{
					"name":      info.Name(),
					"path":      path,
					"size":      fmt.Sprintf("%.2f MB", sizeMB),
					"timestamp": info.ModTime().Format("2006-01-02 15:04:05"),
					"camera":    camCfg.Name,
					"duration":  duration,
				})
			}

			return nil
		})
	}

	return recordings
}

// calculateRecordingsSize calculates total size of all recordings
func (m *Manager) calculateRecordingsSize() float64 {
	cfg := m.cfg.Get()
	var totalBytes int64

	for _, camCfg := range cfg.Cameras {
		recordingPath := camCfg.Recording.Path
		if _, err := os.Stat(recordingPath); os.IsNotExist(err) {
			continue
		}

		filepath.Walk(recordingPath, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}

			if strings.HasSuffix(strings.ToLower(info.Name()), ".mp4") {
				totalBytes += info.Size()
			}
			return nil
		})
	}

	return float64(totalBytes) / (1024 * 1024 * 1024) // Convert to GB
}

// runHealthChecks performs periodic health checks on all configured cameras
func (m *Manager) runHealthChecks() {
	ticker := time.NewTicker(time.Duration(m.cfg.Get().Health.CheckIntervalSeconds) * time.Second)
	defer ticker.Stop()

	// Run initial check immediately
	m.performHealthChecks()

	for range ticker.C {
		m.performHealthChecks()
	}
}

// performHealthChecks checks all configured cameras
func (m *Manager) performHealthChecks() {
	cfg := m.cfg.Get()

	for _, camCfg := range cfg.Cameras {
		result := m.healthChecker.Check(camCfg.URL)

		m.healthMu.Lock()
		m.healthCache[camCfg.Name] = result
		m.healthMu.Unlock()

		log.Debug().
			Str("camera", camCfg.Name).
			Bool("host_reachable", result.HostReachable).
			Bool("url_accessible", result.URLAccessible).
			Int64("response_time_ms", result.ResponseTime).
			Msg("Health check")
	}
}
