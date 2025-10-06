//go:build opencv

package camera

import (
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"gocv.io/x/gocv"
)

type Stream struct {
	Name       string
	URL        string
	capture    *gocv.VideoCapture
	frame      gocv.Mat
	isOpen     bool
	lastError  error
	frameCount int64
	mu         sync.RWMutex
}

type StreamInfo struct {
	Width      int
	Height     int
	FPS        float64
	Codec      string
	Resolution string
}

func NewStream(name, url string) *Stream {
	return &Stream{
		Name:  name,
		URL:   url,
		frame: gocv.NewMat(),
	}
}

func (s *Stream) Open() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	log.Info().Str("camera", s.Name).Str("url", s.URL).Msg("Opening stream")

	capture, err := gocv.OpenVideoCapture(s.URL)
	if err != nil {
		s.lastError = fmt.Errorf("failed to open stream: %w", err)
		return s.lastError
	}

	if !capture.IsOpened() {
		s.lastError = fmt.Errorf("stream opened but not ready")
		capture.Close()
		return s.lastError
	}

	s.capture = capture
	s.isOpen = true
	log.Info().Str("camera", s.Name).Msg("Stream opened successfully")
	return nil
}

func (s *Stream) GetInfo() StreamInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.isOpen || s.capture == nil {
		return StreamInfo{}
	}

	width := int(s.capture.Get(gocv.VideoCaptureFrameWidth))
	height := int(s.capture.Get(gocv.VideoCaptureFrameHeight))
	fps := s.capture.Get(gocv.VideoCaptureFPS)

	// DroidCam uses MJPEG codec
	codec := "MJPEG"
	resolution := fmt.Sprintf("%dx%d", width, height)

	return StreamInfo{
		Width:      width,
		Height:     height,
		FPS:        fps,
		Codec:      codec,
		Resolution: resolution,
	}
}

func (s *Stream) ReadFrame() (gocv.Mat, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.isOpen || s.capture == nil {
		return gocv.NewMat(), fmt.Errorf("stream not open")
	}

	// Create a temporary frame for reading
	tempFrame := gocv.NewMat()
	defer tempFrame.Close()

	if !s.capture.Read(&tempFrame) {
		s.lastError = fmt.Errorf("failed to read frame")
		return gocv.NewMat(), s.lastError
	}

	if tempFrame.Empty() {
		s.lastError = fmt.Errorf("empty frame")
		return gocv.NewMat(), s.lastError
	}

	s.frameCount++
	return tempFrame.Clone(), nil
}

func (s *Stream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.capture != nil {
		s.capture.Close()
		s.capture = nil
	}
	if !s.frame.Empty() {
		s.frame.Close()
	}
	s.isOpen = false
	log.Info().Str("camera", s.Name).Int64("frames", s.frameCount).Msg("Stream closed")
	return nil
}

func (s *Stream) IsOpen() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isOpen
}

func (s *Stream) Reconnect() error {
	log.Info().Str("camera", s.Name).Msg("Reconnecting stream")
	_ = s.Close()
	time.Sleep(2 * time.Second)
	return s.Open()
}
