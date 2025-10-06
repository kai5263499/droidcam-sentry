//go:build opencv

package motion

import (
	"image"
	"log"
	"time"

	"gocv.io/x/gocv"
)

type Detector struct {
	Name              string
	Threshold         float64
	MinArea           int
	mog2              gocv.BackgroundSubtractorMOG2
	detectionInterval time.Duration
	lastDetection     time.Time
}

type Detection struct {
	Timestamp time.Time
	Area      int
	Frame     gocv.Mat
}

func NewDetector(name string, threshold float64, minArea int, intervalMs int) *Detector {
	return &Detector{
		Name:              name,
		Threshold:         threshold,
		MinArea:           minArea,
		mog2:              gocv.NewBackgroundSubtractorMOG2(),
		detectionInterval: time.Duration(intervalMs) * time.Millisecond,
	}
}

func (d *Detector) Detect(frame gocv.Mat) (*Detection, bool) {
	now := time.Now()
	if now.Sub(d.lastDetection) < d.detectionInterval {
		return nil, false
	}

	// Create mask using background subtraction
	mask := gocv.NewMat()
	defer mask.Close()
	d.mog2.Apply(frame, &mask)

	// Noise reduction
	kernel := gocv.GetStructuringElement(gocv.MorphRect, image.Pt(5, 5))
	defer kernel.Close()
	gocv.Dilate(mask, &mask, kernel)

	// Find contours
	contours := gocv.FindContours(mask, gocv.RetrievalExternal, gocv.ChainApproxSimple)

	totalArea := 0
	for i := 0; i < contours.Size(); i++ {
		area := gocv.ContourArea(contours.At(i))
		if area > float64(d.MinArea) {
			totalArea += int(area)
		}
	}

	// Motion detected if total area exceeds threshold
	if totalArea > int(d.Threshold) {
		d.lastDetection = now
		log.Printf("[%s] Motion detected! Area: %d pixels", d.Name, totalArea)

		return &Detection{
			Timestamp: now,
			Area:      totalArea,
			Frame:     frame.Clone(),
		}, true
	}

	return nil, false
}

func (d *Detector) Close() {
	d.mog2.Close()
}
