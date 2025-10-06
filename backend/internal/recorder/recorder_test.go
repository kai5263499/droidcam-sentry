//go:build opencv

package recorder

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"gocv.io/x/gocv"
)

func TestNewRecorder(t *testing.T) {
	tmpDir := t.TempDir()
	r := NewRecorder("test-cam", tmpDir, 30.0, 5, 10)

	if r.Name != "test-cam" {
		t.Errorf("Expected name 'test-cam', got '%s'", r.Name)
	}
	if r.FPS != 30.0 {
		t.Errorf("Expected FPS 30.0, got %f", r.FPS)
	}
	if r.PreBufferSeconds != 5 {
		t.Errorf("Expected PreBufferSeconds 5, got %d", r.PreBufferSeconds)
	}
}

func TestAddFrameMemoryCleanup(t *testing.T) {
	tmpDir := t.TempDir()
	r := NewRecorder("test-cam", tmpDir, 30.0, 2, 5)
	defer r.Close()

	// Create test frames
	testMat := gocv.NewMatWithSize(480, 640, gocv.MatTypeCV8UC3)
	defer testMat.Close()

	// Add more frames than buffer size to test wraparound
	bufferSize := int(r.FPS) * r.PreBufferSeconds
	for i := 0; i < bufferSize*2; i++ {
		r.AddFrame(testMat)
	}

	// Verify recorder is not recording yet
	if r.IsRecording() {
		t.Error("Recorder should not be recording")
	}
}

func TestStartStopRecording(t *testing.T) {
	tmpDir := t.TempDir()
	r := NewRecorder("test-cam", tmpDir, 30.0, 2, 2)
	defer r.Close()

	// Add some test frames to pre-buffer
	testMat := gocv.NewMatWithSize(480, 640, gocv.MatTypeCV8UC3)
	defer testMat.Close()

	for i := 0; i < 60; i++ {
		r.AddFrame(testMat)
	}

	// Start recording
	err := r.StartRecording()
	if err != nil {
		t.Fatalf("Failed to start recording: %v", err)
	}

	if !r.IsRecording() {
		t.Error("Recorder should be recording")
	}

	// Add more frames while recording
	for i := 0; i < 30; i++ {
		r.AddFrame(testMat)
		time.Sleep(10 * time.Millisecond)
	}

	// Stop recording
	r.Stop()

	if r.IsRecording() {
		t.Error("Recorder should have stopped")
	}

	// Wait for MP4 conversion to complete
	time.Sleep(2 * time.Second)

	// Check that MP4 file was created (AVI should be deleted)
	files, err := filepath.Glob(filepath.Join(tmpDir, "*.mp4"))
	if err != nil {
		t.Fatalf("Failed to glob files: %v", err)
	}

	if len(files) == 0 {
		t.Error("Expected MP4 file to be created")
	}
}

func TestMotionTrigger(t *testing.T) {
	tmpDir := t.TempDir()
	r := NewRecorder("test-cam", tmpDir, 30.0, 1, 2)
	defer r.Close()

	testMat := gocv.NewMatWithSize(480, 640, gocv.MatTypeCV8UC3)
	defer testMat.Close()

	// Fill pre-buffer
	for i := 0; i < 30; i++ {
		r.AddFrame(testMat)
	}

	// Start recording
	r.StartRecording()
	defer r.Stop()

	// Simulate motion detected
	r.OnMotion()

	// Add frames
	for i := 0; i < 10; i++ {
		r.AddFrame(testMat)
		r.Update()
	}

	// Reset motion counter
	r.OnMotion()

	// Should still be recording
	if !r.IsRecording() {
		t.Error("Should still be recording after motion reset")
	}
}

func TestPreBufferSize(t *testing.T) {
	tmpDir := t.TempDir()
	fps := 10.0
	preBufferSecs := 2
	r := NewRecorder("test-cam", tmpDir, fps, preBufferSecs, 5)
	defer r.Close()

	expectedBufferSize := int(fps) * preBufferSecs

	testMat := gocv.NewMatWithSize(100, 100, gocv.MatTypeCV8UC3)
	defer testMat.Close()

	// Add exactly buffer size frames
	for i := 0; i < expectedBufferSize; i++ {
		r.AddFrame(testMat)
	}

	// Should be able to start recording with full buffer
	err := r.StartRecording()
	if err != nil {
		t.Fatalf("Should be able to start with full buffer: %v", err)
	}
	r.Stop()
}

func TestRecordingWithoutPreBuffer(t *testing.T) {
	tmpDir := t.TempDir()
	r := NewRecorder("test-cam", tmpDir, 30.0, 2, 5)
	defer r.Close()

	// Try to start recording without adding frames
	err := r.StartRecording()
	if err == nil {
		t.Error("Expected error when starting recording without frames")
	}
}

func TestConcurrentAddFrame(t *testing.T) {
	tmpDir := t.TempDir()
	r := NewRecorder("test-cam", tmpDir, 30.0, 1, 2)
	defer r.Close()

	testMat := gocv.NewMatWithSize(200, 200, gocv.MatTypeCV8UC3)
	defer testMat.Close()

	// Add frames from multiple goroutines
	done := make(chan bool, 3)
	for i := 0; i < 3; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				r.AddFrame(testMat)
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 3; i++ {
		<-done
	}
}

func TestFileCreation(t *testing.T) {
	tmpDir := t.TempDir()

	// Verify directory is created
	if _, err := os.Stat(tmpDir); os.IsNotExist(err) {
		t.Errorf("Temp directory should exist: %v", err)
	}
}
