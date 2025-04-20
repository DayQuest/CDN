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

	// Set optimized Cache-Control headers based on content type
	cacheControl := "public, max-age=31536000, immutable" // 1 year for static content

	// Check if client specifically requests caching with cache=true parameter
	if r.URL.Query().Get("cache") == "true" {
		// Extended caching for explicitly cacheable requests
		cacheControl = "public, max-age=604800, immutable" // 1 week
	} else if r.URL.Query().Get("nocache") != "" {
		// No caching for testing requests with nocache parameter
		cacheControl = "no-store, no-cache, must-revalidate, max-age=0"
	} else if strings.HasSuffix(videoName, ".m3u8") {
		cacheControl = "public, max-age=1, must-revalidate" // Short cache for HLS manifests
	} else if strings.HasSuffix(videoName, ".ts") {
		cacheControl = "public, max-age=604800, immutable" // 1 week for HLS segments
	}

	// Add custom CDN caching header if requested
	if r.Header.Get("X-CDN-Cache-Control") == "true" {
		w.Header().Set("X-CDN-Cache", "true")
	}

	w.Header().Set("Cache-Control", cacheControl)

	// Handle HLS content
	if strings.HasSuffix(videoName, ".m3u8") || strings.HasSuffix(videoName, ".ts") {
		reader, err := h.storage.GetHLSContent(ctx, videoName)
		if err != nil {
			http.Error(w, "Error reading HLS content", http.StatusInternalServerError)
			return
		}
		defer reader.Close()

		w.Header().Set("Content-Type", contentType)

		// Enable compression for HLS manifests only
		if strings.HasSuffix(videoName, ".m3u8") && strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			w.Header().Set("Content-Encoding", "gzip")
			gz := gzip.NewWriter(w)
			defer gz.Close()
			io.Copy(gz, reader)
			return
		}

		// For TS segments, use chunked transfer
		if strings.HasSuffix(videoName, ".ts") {
			w.Header().Del("Content-Length")
			w.Header().Set("Transfer-Encoding", "chunked")
		}

		io.Copy(w, reader)
		return
	}

	// Handle video streaming
	start, end := parseRangeHeader(r, obj.Size)
	if start >= obj.Size {
		http.Error(w, "Requested range not satisfiable", http.StatusRequestedRangeNotSatisfiable)
		return
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

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Accept-Ranges", "bytes")

	// Enable chunked transfer for video streaming
	if start == 0 && end == obj.Size-1 {
		w.Header().Del("Content-Length")
		w.Header().Set("Transfer-Encoding", "chunked")
	} else {
		w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
	}

	if start > 0 || end < obj.Size-1 {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, obj.Size))
		w.WriteHeader(http.StatusPartialContent)
	}

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
