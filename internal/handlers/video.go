package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

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
	
	// Handle .temp file requests
	isTemp := strings.HasSuffix(videoName, ".temp")
	if isTemp {
		baseVideoName := strings.TrimSuffix(videoName, ".temp")
		if strings.HasSuffix(baseVideoName, ".mp4") {
			// Check if the mp4 exists
			obj, err := h.storage.StatVideo(r.Context(), baseVideoName)
			if err == nil {
				// Return metadata for the mp4 file
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]interface{}{
					"status":  "completed",
					"message": "Video is ready",
					"data": map[string]interface{}{
						"size":        obj.Size,
						"contentType": "video/mp4",
						"filePath":    baseVideoName,
						"cdnUrl":      fmt.Sprintf("/video/%s", baseVideoName),
					},
				})
				return
			}
		}
	}

	// Check video processing status
	videoID := strings.TrimSuffix(strings.TrimSuffix(videoName, ".temp"), ".mp4")
	status, err := h.db.GetVideoStatus(videoID)
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

	// Check if this is a .temp file request
	isTemp := strings.HasSuffix(videoName, ".temp")
	if isTemp {
		baseVideoName := strings.TrimSuffix(videoName, ".temp")
		
		// First check if the processed mp4 exists
		if strings.HasSuffix(baseVideoName, ".mp4") {
			_, err := h.storage.StatVideo(ctx, baseVideoName)
			if err == nil {
				// Redirect to the mp4 file
				http.Redirect(w, r, "/video/"+baseVideoName, http.StatusTemporaryRedirect)
				return
			}
		}

		// Check if the file exists in the raw videos bucket
		rawVideoName := strings.TrimSuffix(baseVideoName, ".mp4")
		status, err := h.db.GetVideoStatus(rawVideoName)
		if err == nil {
			switch status {
			case database.StatusPending, database.StatusProcessing:
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusAccepted)
				json.NewEncoder(w).Encode(map[string]string{
					"status": "processing",
					"message": "Video is being processed",
					"videoId": rawVideoName,
				})
				return
			case database.StatusFailed:
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnprocessableEntity)
				json.NewEncoder(w).Encode(map[string]string{
					"status": "failed",
					"message": "Video processing failed",
					"videoId": rawVideoName,
				})
				return
			}
		}

		// If we get here, the video doesn't exist in either bucket
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{
			"status": "not_found",
			"message": "Video not found or upload incomplete",
			"videoId": rawVideoName,
		})
		return
	}

	// Get video info
	obj, err := h.storage.StatVideo(ctx, videoName)
	if err != nil {
		// If the original request was for a .temp file and we got here,
		// it means neither the .temp nor the .mp4 exists
		if isTemp {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{
				"status": "not_found",
				"message": "Video not found or still processing",
			})
			return
		}
		log.Printf("Error getting video info: %v", err)
		http.Error(w, "Video not found", http.StatusNotFound)
		return
	}

	// Set essential headers first
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Length", strconv.FormatInt(obj.Size, 10))

	// Determine and set content type
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
		case ".temp":
			// For .temp files, check the base file extension
			baseExt := strings.ToLower(filepath.Ext(strings.TrimSuffix(videoName, ".temp")))
			if baseExt == ".mp4" {
				contentType = "video/mp4"
			} else {
				contentType = "application/octet-stream"
			}
		default:
			contentType = mime.TypeByExtension(ext)
			if contentType == "" {
				contentType = "application/octet-stream"
			}
		}
	}
	w.Header().Set("Content-Type", contentType)

	// Handle range request for video streaming
	start, end := parseRangeHeader(r, obj.Size)
	if start >= obj.Size {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", obj.Size))
		http.Error(w, "Requested range not satisfiable", http.StatusRequestedRangeNotSatisfiable)
		return
	}

	// Set content length for the actual range being served
	rangeSize := end - start + 1
	w.Header().Set("Content-Length", strconv.FormatInt(rangeSize, 10))

	// For MP4 files, ensure we have all required headers for iOS
	if strings.HasSuffix(strings.ToLower(videoName), ".mp4") {
		w.Header().Set("Content-Type", "video/mp4")
		if start > 0 || end < obj.Size-1 {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, obj.Size))
			w.WriteHeader(http.StatusPartialContent)
		}
	} else if start > 0 || end < obj.Size-1 {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, obj.Size))
		w.WriteHeader(http.StatusPartialContent)
	}

	reader, err := h.storage.GetVideo(ctx, videoName, start, end)
	if err != nil {
		http.Error(w, "Error reading video", http.StatusInternalServerError)
		return
	}
	defer reader.Close()

	// Use larger buffer for better streaming performance
	buffer := make([]byte, 256*1024) // 256KB buffer
	_, err = io.CopyBuffer(w, reader, buffer)
	if err != nil {
		log.Printf("Error streaming video: %v", err)
	}
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
