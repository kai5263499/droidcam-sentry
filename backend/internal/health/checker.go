// Package health provides health check utilities for camera endpoints.
package health

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"
)

// CheckResult contains the results of health checks
type CheckResult struct {
	HostReachable bool   `json:"host_reachable"`
	HostError     string `json:"host_error,omitempty"`
	URLAccessible bool   `json:"url_accessible"`
	URLError      string `json:"url_error,omitempty"`
	ResponseTime  int64  `json:"response_time_ms"`
	LastChecked   string `json:"last_checked"`
}

// Checker performs health checks on camera endpoints
type Checker struct {
	timeout time.Duration
}

// NewChecker creates a new health checker
func NewChecker(timeout time.Duration) *Checker {
	return &Checker{
		timeout: timeout,
	}
}

// Check performs both TCP and HTTP health checks on a camera URL
func (c *Checker) Check(cameraURL string) CheckResult {
	result := CheckResult{
		LastChecked: time.Now().Format(time.RFC3339),
	}

	// Parse URL to extract host
	parsedURL, err := url.Parse(cameraURL)
	if err != nil {
		result.HostError = fmt.Sprintf("Invalid URL: %v", err)
		result.URLError = result.HostError
		return result
	}

	// TCP ping check
	result.HostReachable, result.HostError = c.tcpPing(parsedURL.Host)

	// HTTP GET check
	if result.HostReachable {
		start := time.Now()
		result.URLAccessible, result.URLError = c.httpCheck(cameraURL)
		result.ResponseTime = time.Since(start).Milliseconds()
	} else {
		result.URLError = "Host unreachable"
	}

	return result
}

// tcpPing attempts to establish a TCP connection to the host
func (c *Checker) tcpPing(host string) (bool, string) {
	// Add default port if not specified
	if _, _, err := net.SplitHostPort(host); err != nil {
		host = net.JoinHostPort(host, "80")
	}

	conn, err := net.DialTimeout("tcp", host, c.timeout)
	if err != nil {
		return false, fmt.Sprintf("TCP connection failed: %v", err)
	}
	_ = conn.Close()
	return true, ""
}

// httpCheck performs an HTTP GET request to verify URL accessibility
func (c *Checker) httpCheck(urlStr string) (bool, string) {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return false, fmt.Sprintf("Request creation failed: %v", err)
	}

	client := &http.Client{
		Timeout: c.timeout,
	}

	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Sprintf("HTTP request failed: %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		return true, ""
	}

	return false, fmt.Sprintf("HTTP %d", resp.StatusCode)
}
