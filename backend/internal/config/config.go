// Package config provides configuration management for the DroidCam Sentry application.
package config

import (
	"os"
	"strconv"
	"sync"

	"gopkg.in/yaml.v3"
)

// Config holds the application configuration with thread-safe access.
type Config struct {
	Server      ServerConfig   `yaml:"server"`
	Cameras     []CameraConfig `yaml:"cameras"`
	Motion      MotionConfig   `yaml:"motion"`
	Health      HealthConfig   `yaml:"health"`
	Storage     StorageConfig  `yaml:"storage"`
	mu          sync.RWMutex
	subscribers []func(*Config)
}

// Snapshot is a thread-safe snapshot of Config without mutex
// Snapshot is a read-only snapshot of the current configuration.
type Snapshot struct {
	Server  ServerConfig   `yaml:"server" json:"server"`
	Cameras []CameraConfig `yaml:"cameras" json:"cameras"`
	Motion  MotionConfig   `yaml:"motion" json:"motion"`
	Health  HealthConfig   `yaml:"health" json:"health"`
	Storage StorageConfig  `yaml:"storage" json:"storage"`
}

// ServerConfig contains HTTP server settings.
type ServerConfig struct {
	Host string `yaml:"host" json:"host"`
	Port int    `yaml:"port" json:"port"`
}

// CameraConfig contains individual camera settings.
type CameraConfig struct {
	Name            string          `yaml:"name" json:"name"`
	Description     string          `yaml:"description" json:"description"`
	URL             string          `yaml:"url" json:"url"`
	Enabled         bool            `yaml:"enabled" json:"enabled"`
	MotionThreshold float64         `yaml:"motion_threshold" json:"motion_threshold"`
	Recording       RecordingConfig `yaml:"recording" json:"recording"`
}

// RecordingConfig contains video recording settings.
type RecordingConfig struct {
	Path              string `yaml:"path" json:"path"`
	Format            string `yaml:"format" json:"format"`
	PreBufferSeconds  int    `yaml:"pre_buffer_seconds" json:"pre_buffer_seconds"`
	PostBufferSeconds int    `yaml:"post_buffer_seconds" json:"post_buffer_seconds"`
}

// MotionConfig contains motion detection settings.
type MotionConfig struct {
	DetectionIntervalMs int `yaml:"detection_interval_ms" json:"detection_interval_ms"`
	MinArea             int `yaml:"min_area" json:"min_area"`
}

// HealthConfig contains health check settings.
type HealthConfig struct {
	CheckIntervalSeconds int `yaml:"check_interval_seconds" json:"check_interval_seconds"`
	TimeoutSeconds       int `yaml:"timeout_seconds" json:"timeout_seconds"`
}

// StorageConfig contains storage management settings.
type StorageConfig struct {
	MaxRecordingSizeMB int `yaml:"max_recording_size_mb" json:"max_recording_size_mb"`
	RetentionDays      int `yaml:"retention_days" json:"retention_days"`
}

// Load reads configuration from a YAML file and applies env var overrides
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	cfg.subscribers = make([]func(*Config), 0)

	// Apply environment variable overrides
	cfg.applyEnvOverrides()
	// Set defaults for any missing config values
	cfg.setDefaults()

	return &cfg, nil
}

func (c *Config) applyEnvOverrides() {
	// Server overrides
	if host := os.Getenv("DROIDCAM_SENTRY_HOST"); host != "" {
		c.Server.Host = host
	}
	if port := os.Getenv("DROIDCAM_SENTRY_PORT"); port != "" {
		if p, err := strconv.Atoi(port); err == nil {
			c.Server.Port = p
		}
	}

	// Recording path override (applies to all cameras)
	if recordingPath := os.Getenv("DROIDCAM_SENTRY_RECORDING_PATH"); recordingPath != "" {
		for i := range c.Cameras {
			c.Cameras[i].Recording.Path = recordingPath
		}
	}

	// Motion threshold override (applies to all cameras)
	if threshold := os.Getenv("DROIDCAM_SENTRY_MOTION_THRESHOLD"); threshold != "" {
		if t, err := strconv.ParseFloat(threshold, 64); err == nil {
			for i := range c.Cameras {
				c.Cameras[i].MotionThreshold = t
			}
		}
	}

	// Motion detection interval override
	if interval := os.Getenv("DROIDCAM_SENTRY_DETECTION_INTERVAL_MS"); interval != "" {
		if i, err := strconv.Atoi(interval); err == nil {
			c.Motion.DetectionIntervalMs = i
		}
	}

	// Min area override
	if minArea := os.Getenv("DROIDCAM_SENTRY_MIN_AREA"); minArea != "" {
		if a, err := strconv.Atoi(minArea); err == nil {
			c.Motion.MinArea = a
		}
	}

	// Pre-buffer override
	if preBuffer := os.Getenv("DROIDCAM_SENTRY_PRE_BUFFER_SECONDS"); preBuffer != "" {
		if pb, err := strconv.Atoi(preBuffer); err == nil {
			for i := range c.Cameras {
				c.Cameras[i].Recording.PreBufferSeconds = pb
			}
		}
	}

	// Post-buffer override
	if postBuffer := os.Getenv("DROIDCAM_SENTRY_POST_BUFFER_SECONDS"); postBuffer != "" {
		if pb, err := strconv.Atoi(postBuffer); err == nil {
			for i := range c.Cameras {
				c.Cameras[i].Recording.PostBufferSeconds = pb
			}
		}
	}
}

// Update atomically updates the configuration
func (c *Config) Update(updater func(*Config)) {
	c.mu.Lock()
	defer c.mu.Unlock()

	updater(c)
	c.notifySubscribers()
}

// Get safely retrieves a snapshot of the config without mutex
func (c *Config) Get() Snapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Deep copy cameras to avoid shared references
	cameras := make([]CameraConfig, len(c.Cameras))
	copy(cameras, c.Cameras)

	return Snapshot{
		Server:  c.Server,
		Cameras: cameras,
		Motion:  c.Motion,
		Storage: c.Storage,
		Health:  c.Health,
	}
}

// Subscribe registers a callback for config changes
func (c *Config) Subscribe(callback func(*Config)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.subscribers = append(c.subscribers, callback)
}

func (c *Config) notifySubscribers() {
	for _, callback := range c.subscribers {
		go callback(c)
	}
}

// Save writes the current configuration to a file
func (c *Config) Save(path string) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0o644)
}

func (c *Config) setDefaults() {
	// Set default health check values if not configured
	if c.Health.CheckIntervalSeconds <= 0 {
		c.Health.CheckIntervalSeconds = 30
	}
	if c.Health.TimeoutSeconds <= 0 {
		c.Health.TimeoutSeconds = 5
	}
}
