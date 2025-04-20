package handlers

import (
	"encoding/json"
	"net/http"
	"time"
)

// PingHandler handles connection testing requests
type PingHandler struct{}

// NewPingHandler creates a new PingHandler
func NewPingHandler() *PingHandler {
	return &PingHandler{}
}

// HandlePing returns a simple JSON response for connection testing
// It deliberately has minimal processing to accurately measure network latency
func (h *PingHandler) HandlePing(w http.ResponseWriter, r *http.Request) {
	// Set cache headers to prevent caching
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	
	// Set content type
	w.Header().Set("Content-Type", "application/json")
	
	// Create a response with current timestamp to ensure freshness
	response := map[string]interface{}{
		"status": "ok",
		"time":   time.Now().UnixNano() / int64(time.Millisecond),
	}
	
	// Write response
	json.NewEncoder(w).Encode(response)
}

// ServeTestFile serves the static ping-test.json file
func (h *PingHandler) ServeTestFile(w http.ResponseWriter, r *http.Request) {
	// Set cache headers to prevent caching
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	
	// Set content type
	w.Header().Set("Content-Type", "application/json")
	
	// Simple static response
	w.Write([]byte(`{"status":"ok"}`))
} 