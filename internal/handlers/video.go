package handlers

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dayquest/cdn/internal/config"
	"github.com/dayquest/cdn/internal/database"
	"github.com/dayquest/cdn/internal/storage"
	"github.com/gorilla/mux"
)

type VideoHandler struct {
	storage storage.Storage
	config  *config.Config
	db      *database.DBHandler
}

type seekableReadCloser struct {
	io.ReadCloser
	size   int64
	offset int64
}

func (s *seekableReadCloser) Read(p []byte) (n int, err error) {
	return s.ReadCloser.Read(p)
}

func (s *seekableReadCloser) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		s.offset = offset
	case io.SeekCurrent:
		s.offset += offset
	case io.SeekEnd:
		s.offset = s.size + offset
	}
	return s.offset, nil
}

func NewVideoHandler(storage storage.Storage, cfg *config.Config, db *database.DBHandler) *VideoHandler {
	return &VideoHandler{
		storage: storage,
		config:  cfg,
		db:      db,
	}
}

func (h *VideoHandler) GetVideoMetadata(w http.ResponseWriter, r *http.Request) {
	videoName := mux.Vars(r)["video"]

	// Check video processing status
	status, err := h.db.GetVideoStatus(videoName)
	if err != nil {
		log.Printf("Error checking video status: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "error",
			"message": "Error checking video status",
		})
		return
	}

	switch status {
	case database.StatusPending:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "pending",
			"message": "Video is pending processing",
		})
		return
	case database.StatusProcessing:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "processing",
			"message": "Video is currently being processed",
		})
		return
	case database.StatusFailed:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "failed",
			"message": "Video processing failed",
		})
		return
	case database.StatusUnknown:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "not_found",
			"message": "Video not found",
		})
		return
	case database.StatusCompleted:
		// Continue with video info
	}

	// Get video info
	obj, err := h.storage.StatVideo(r.Context(), videoName)
	if err != nil {
		log.Printf("Error getting video info: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "not_found",
			"message": "Video not found",
		})
		return
	}

	// Return video metadata
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "completed",
		"message": "Video is ready",
		"data": map[string]interface{}{
			"size":        obj.Size,
			"contentType": obj.ContentType,
			"filePath":    videoName,
			"cdnUrl":      fmt.Sprintf("/video/%s", videoName),
		},
	})
}

func (h *VideoHandler) StreamVideo(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	videoName := mux.Vars(r)["video"]

	// Detect client type for optimized delivery
	userAgent := r.Header.Get("User-Agent")
	isApp := strings.Contains(userAgent, "DayQuest-App") || strings.Contains(userAgent, "Flutter")

	// Add debug logging for performance analysis
	requestStart := time.Now()
	defer func() {
		log.Printf("Video request: %s, app: %v, took: %vms", videoName, isApp, time.Since(requestStart).Milliseconds())
	}()

	// Get video info
	obj, err := h.storage.StatVideo(ctx, videoName)
	if err != nil {
		log.Printf("Error getting video info: %v", err)
		http.Error(w, "Video not found", http.StatusNotFound)
		return
	}

	// Determine content type
	contentType := obj.ContentType
	if contentType == "" {
		ext := strings.ToLower(filepath.Ext(videoName))
		switch ext {
		case ".mp4":
			contentType = "video/mp4"
		case ".m3u8":
			contentType = "application/vnd.apple.mpegurl"
		case ".ts":
			contentType = "video/MP2T"
		default:
			contentType = mime.TypeByExtension(ext)
			if contentType == "" {
				contentType = "application/octet-stream"
			}
		}
	}

	// Optimize cache settings based on client type and content
	var cacheControl string

	// App-specific optimizations
	if isApp {
		// Longer cache for apps, which handle their own refresh policy
		cacheControl = "public, max-age=604800, immutable" // 1 week
		w.Header().Set("X-CDN-Cache", "true")
		w.Header().Set("X-App-Optimized", "true")
	} else if r.URL.Query().Get("cache") == "true" {
		// Explicit caching requested
		cacheControl = "public, max-age=604800, immutable" // 1 week
		w.Header().Set("X-CDN-Cache", "true")
	} else if r.URL.Query().Get("nocache") != "" {
		// No caching requested
		cacheControl = "no-store, no-cache, must-revalidate, max-age=0"
		w.Header().Set("X-CDN-Cache", "false")
	} else if strings.HasSuffix(videoName, ".m3u8") {
		// HLS manifests need frequent refreshing
		cacheControl = "public, max-age=10, must-revalidate"
	} else if strings.HasSuffix(videoName, ".ts") {
		// HLS segments are immutable
		cacheControl = "public, max-age=604800, immutable"
	} else {
		// Default for other static content
		cacheControl = "public, max-age=31536000, immutable" // 1 year
	}

	// Set response headers for performance
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", cacheControl)
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Connection", "keep-alive")

	// Set expires header for better caching
	if !strings.Contains(cacheControl, "no-cache") {
		expiryDuration := 24 * time.Hour // Default 1 day
		if strings.Contains(cacheControl, "max-age=604800") {
			expiryDuration = 7 * 24 * time.Hour // 1 week
		} else if strings.Contains(cacheControl, "max-age=31536000") {
			expiryDuration = 365 * 24 * time.Hour // 1 year
		}
		w.Header().Set("Expires", time.Now().Add(expiryDuration).Format(time.RFC1123))
	}

	// Handle HLS content with compression for manifests
	if strings.HasSuffix(videoName, ".m3u8") || strings.HasSuffix(videoName, ".ts") {
		reader, err := h.storage.GetHLSContent(ctx, videoName)
		if err != nil {
			http.Error(w, "Error reading HLS content", http.StatusInternalServerError)
			return
		}
		defer reader.Close()

		// Apply compression for manifest files
		if strings.HasSuffix(videoName, ".m3u8") && strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			w.Header().Set("Content-Encoding", "gzip")
			gz := gzip.NewWriter(w)
			defer gz.Close()
			io.Copy(gz, reader)
			return
		}

		// Optimize transfer for TS segments
		if strings.HasSuffix(videoName, ".ts") {
			w.Header().Del("Content-Length")
			w.Header().Set("Transfer-Encoding", "chunked")
		}

		io.Copy(w, reader)
		return
	}

	// Handle video streaming with optimized chunk sizes for mobile
	rangeHeader := r.Header.Get("Range")
	start, end := parseRangeHeader(r, obj.Size)

	// App-specific range optimizations
	if isApp && rangeHeader != "" {
		log.Printf("App range request: %s bytes=%d-%d", videoName, start, end)

		// For mobile apps, we use smaller chunks to improve initial load time
		// but only if the requested range is large
		maxChunkSize := int64(2 * 1024 * 1024) // 2MB for mobile
		if end-start > maxChunkSize {
			// Mobile devices do better with smaller chunks
			end = start + maxChunkSize
			log.Printf("Optimized range for app: bytes=%d-%d", start, end)
		}
	}

	// Validate range
	if start >= obj.Size {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", obj.Size))
		http.Error(w, "Requested range not satisfiable", http.StatusRequestedRangeNotSatisfiable)
		return
	}

	if end >= obj.Size {
		end = obj.Size - 1
	}

	reader, err := h.storage.GetVideo(ctx, videoName, start, end)
	if err != nil {
		http.Error(w, "Error reading video", http.StatusInternalServerError)
		return
	}
	defer reader.Close()

	seekable := &seekableReadCloser{
		ReadCloser: reader,
		size:       obj.Size,
		offset:     start,
	}

	// Set content length and range headers
	contentLength := end - start + 1
	w.Header().Set("Content-Length", strconv.FormatInt(contentLength, 10))

	if start > 0 || end < obj.Size-1 {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, obj.Size))
		w.WriteHeader(http.StatusPartialContent)
	}

	// Use efficient serving method with proper cache control
	http.ServeContent(w, r, videoName, time.Now(), seekable)
}

func parseRangeHeader(r *http.Request, fileSize int64) (start, end int64) {
	rangeHeader := r.Header.Get("Range")
	if rangeHeader == "" {
		return 0, fileSize - 1
	}

	_, err := fmt.Sscanf(rangeHeader, "bytes=%d-%d", &start, &end)
	if err != nil {
		_, err = fmt.Sscanf(rangeHeader, "bytes=%d-", &start)
		if err != nil {
			return 0, fileSize - 1
		}
		end = fileSize - 1
	}

	if start < 0 {
		start = 0
	}
	if end >= fileSize {
		end = fileSize - 1
	}
	return start, end
}
