package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"
)

// PingHandler verarbeitet Ping-Anfragen für Verbindungsqualitatätsmessungen
type PingHandler struct{}

// NewPingHandler erstellt einen neuen PingHandler
func NewPingHandler() *PingHandler {
	return &PingHandler{}
}

// HandlePing bearbeitet einfache Ping-Anfragen
func (h *PingHandler) HandlePing(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	// Delay-Parameter für Tests (simuliert langsame Verbindungen)
	delayParam := r.URL.Query().Get("delay")
	if delayParam != "" {
		if delay, err := strconv.Atoi(delayParam); err == nil && delay > 0 && delay <= 5000 {
			time.Sleep(time.Duration(delay) * time.Millisecond)
		}
	}

	// Einige Statistik-Daten erfassen
	elapsed := time.Since(startTime)
	ua := r.Header.Get("User-Agent")
	ip := r.RemoteAddr

	// Für Debug-Zwecke loggen
	log.Printf("Ping request from %s (%s) processed in %vms", ip, ua, elapsed.Milliseconds())

	// JSON-Antwort mit Zeitstempel und Server-Zeit
	response := map[string]interface{}{
		"status":     "ok",
		"timestamp":  time.Now().UnixMilli(),
		"serverTime": elapsed.Milliseconds(),
	}

	// Cache-Header setzen
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-CDN-Server", "CDN-1")

	// CORS-Header für browserübergreifende Anfragen
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")

	json.NewEncoder(w).Encode(response)
}

// ServeTestFile liefert eine kleine JSON-Datei für Ping-Tests
func (h *PingHandler) ServeTestFile(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	// Prevent caching for accurate timing measurements
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-CDN-Server", "CDN-1")

	// Allow cross-origin requests
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")

	// Generate a minimal JSON response with a timestamp to prevent caching
	response := map[string]interface{}{
		"timestamp":      time.Now().UnixMilli(),
		"status":         "ok",
		"processingTime": time.Since(start).Milliseconds(),
	}

	json.NewEncoder(w).Encode(response)
}
