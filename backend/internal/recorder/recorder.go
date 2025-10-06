//go:build opencv

package recorder

import (
	"container/ring"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gocv.io/x/gocv"
)

type VideoRecorder struct {
	Name              string
	OutputPath        string
	FPS               float64
	PreBufferSeconds  int
	PostBufferSeconds int

	writer            *gocv.VideoWriter
	preBuffer         *ring.Ring
	isRecording       bool
	recordingStart    time.Time
	framesSinceMotion int
	currentFile       string
	mu                sync.Mutex
}

func NewRecorder(name, outputPath string, fps float64, preBuffer, postBuffer int) *VideoRecorder {
	// Create pre-buffer ring
	bufferSize := int(fps) * preBuffer
	preBufferRing := ring.New(bufferSize)

	return &VideoRecorder{
		Name:              name,
		OutputPath:        outputPath,
		FPS:               fps,
		PreBufferSeconds:  preBuffer,
		PostBufferSeconds: postBuffer,
		preBuffer:         preBufferRing,
	}
}

func (r *VideoRecorder) AddFrame(frame gocv.Mat) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// CRITICAL FIX: Close old Mat before overwriting to prevent memory leak
	if r.preBuffer.Value != nil {
		if oldMat, ok := r.preBuffer.Value.(gocv.Mat); ok && !oldMat.Empty() {
			oldMat.Close()
		}
	}

	// Clone and store new frame in circular buffer
	r.preBuffer.Value = frame.Clone()
	r.preBuffer = r.preBuffer.Next()

	// If recording, write frame
	if r.isRecording && r.writer != nil && !frame.Empty() {
		r.writer.Write(frame)
	}
}

func (r *VideoRecorder) StartRecording() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.isRecording {
		return nil // Already recording
	}

	// Create output directory
	if err := os.MkdirAll(r.OutputPath, 0o755); err != nil {
		return fmt.Errorf("failed to create output dir: %w", err)
	}

	// Generate filename with timestamp
	timestamp := time.Now().Format("20060102_150405")
	filename := filepath.Join(r.OutputPath, fmt.Sprintf("%s_%s.avi", r.Name, timestamp))
	r.currentFile = filename

	// Get frame dimensions from pre-buffer
	var width, height int
	r.preBuffer.Do(func(val interface{}) {
		if val != nil {
			if mat, ok := val.(gocv.Mat); ok && !mat.Empty() && width == 0 {
				width = mat.Cols()
				height = mat.Rows()
			}
		}
	})

	if width == 0 || height == 0 {
		return fmt.Errorf("no valid frames in pre-buffer")
	}

	// Open video writer with MJPEG codec (most compatible)
	writer, err := gocv.VideoWriterFile(filename, "MJPG", r.FPS, width, height, true)
	if err != nil {
		return fmt.Errorf("failed to open video writer: %w", err)
	}

	if !writer.IsOpened() {
		return fmt.Errorf("video writer not opened")
	}

	r.writer = writer
	r.isRecording = true
	r.recordingStart = time.Now()
	r.framesSinceMotion = 0

	log.Printf("[%s] Started recording: %s", r.Name, filename)

	// Write pre-buffered frames
	frameCount := 0
	r.preBuffer.Do(func(val interface{}) {
		if val != nil {
			if mat, ok := val.(gocv.Mat); ok && !mat.Empty() {
				r.writer.Write(mat)
				frameCount++
			}
		}
	})
	log.Printf("[%s] Wrote %d pre-buffered frames", r.Name, frameCount)

	return nil
}

func (r *VideoRecorder) OnMotion() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.framesSinceMotion = 0
}

func (r *VideoRecorder) Update() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.isRecording {
		return false
	}

	r.framesSinceMotion++
	maxFrames := int(r.FPS) * r.PostBufferSeconds

	// Stop recording if post-buffer period exceeded
	if r.framesSinceMotion >= maxFrames {
		r.stopRecording()
		return true // Recording stopped
	}

	return false
}

// convertAVItoMP4 converts an AVI file to MP4 using ffmpeg
func (r *VideoRecorder) convertAVItoMP4(aviPath string) error {
	// Generate MP4 filename
	mp4Path := strings.TrimSuffix(aviPath, ".avi") + ".mp4"

	log.Printf("[%s] Converting %s to MP4...", r.Name, filepath.Base(aviPath))

	// ffmpeg command: convert AVI to MP4 with H.264 codec
	// -i input.avi: input file
	// -c:v libx264: use H.264 video codec
	// -preset fast: encoding speed/compression tradeoff
	// -crf 23: constant rate factor (quality, 18-28 range, lower = better)
	// -c:a aac: use AAC audio codec (if there's audio)
	// -y: overwrite output file if exists
	cmd := exec.Command("ffmpeg",
		"-i", aviPath,
		"-c:v", "libx264",
		"-preset", "fast",
		"-crf", "23",
		"-c:a", "aac",
		"-y",
		mp4Path,
	)

	// Capture output for debugging
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("[%s] FFmpeg conversion failed: %v\nOutput: %s", r.Name, err, string(output))
		return fmt.Errorf("ffmpeg conversion failed: %w", err)
	}

	log.Printf("[%s] Successfully converted to %s", r.Name, filepath.Base(mp4Path))

	// Delete original AVI file after successful conversion
	if err := os.Remove(aviPath); err != nil {
		log.Printf("[%s] Warning: failed to delete AVI file: %v", r.Name, err)
	} else {
		log.Printf("[%s] Deleted original AVI file", r.Name)
	}

	return nil
}

func (r *VideoRecorder) stopRecording() {
	if r.writer != nil {
		r.writer.Close()
		r.writer = nil
	}

	duration := time.Since(r.recordingStart)
	log.Printf("[%s] Stopped recording (duration: %s, file: %s)", r.Name, duration, r.currentFile)
	r.isRecording = false

	// Convert AVI to MP4 in background (non-blocking)
	if r.currentFile != "" {
		aviFile := r.currentFile
		go func() {
			if err := r.convertAVItoMP4(aviFile); err != nil {
				log.Printf("[%s] Failed to convert recording to MP4: %v", r.Name, err)
			}
		}()
	}
}

func (r *VideoRecorder) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopRecording()
}

func (r *VideoRecorder) IsRecording() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.isRecording
}

func (r *VideoRecorder) Close() {
	r.Stop()

	// CRITICAL FIX: Properly clean up all Mats in pre-buffer
	r.preBuffer.Do(func(val interface{}) {
		if val != nil {
			if mat, ok := val.(gocv.Mat); ok && !mat.Empty() {
				mat.Close()
			}
		}
	})
}
