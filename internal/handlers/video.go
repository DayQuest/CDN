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
	
	if strings.HasSuffix(videoName, ".temp") {
		obj, err := h.storage.StatVideo(r.Context(), videoName)
		if err != nil {
			log.Printf("Error getting temp video info: %v", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{
				"status":  "not_found",
				"message": "Video not found",
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "completed",
			"message": "Video is ready",
			"data": map[string]interface{}{
				"size":        obj.Size,
				"contentType": "video/mp4", 
				"filePath":    videoName,
				"cdnUrl":      fmt.Sprintf("/video/%s", videoName),
			},
		})
		return
	}

	videoID := strings.TrimSuffix(videoName, ".mp4")
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
	}

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

	if strings.HasSuffix(videoName, ".temp") {
		obj, err := h.storage.StatVideo(ctx, videoName)
		if err != nil {
			log.Printf("Error getting temp video info: %v", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{
				"status":  "not_found",
				"message": "Video not found",
			})
			return
		}

		h.streamVideoFile(w, r, videoName, obj)
		return
	}

	obj, err := h.storage.StatVideo(ctx, videoName)
	if err != nil {
		log.Printf("Error getting video info: %v", err)
		http.Error(w, "Video not found", http.StatusNotFound)
		return
	}

	h.streamVideoFile(w, r, videoName, obj)
}

func (h *VideoHandler) streamVideoFile(w http.ResponseWriter, r *http.Request, videoName string, obj *storage.VideoInfo) {
	ctx := r.Context()

	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Length", strconv.FormatInt(obj.Size, 10))

	contentType := obj.ContentType
	if contentType == "" {
		ext := strings.ToLower(filepath.Ext(videoName))
		switch ext {
		case ".mp4":
			contentType = "video/mp4"
		case ".temp":
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
	w.Header().Set("Content-Type", contentType)

	start, end := parseRangeHeader(r, obj.Size)
	if start >= obj.Size {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", obj.Size))
		http.Error(w, "Requested range not satisfiable", http.StatusRequestedRangeNotSatisfiable)
		return
	}

	rangeSize := end - start + 1
	w.Header().Set("Content-Length", strconv.FormatInt(rangeSize, 10))

	if strings.HasSuffix(strings.ToLower(videoName), ".mp4") || strings.HasSuffix(strings.ToLower(videoName), ".temp") {
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

	buffer := make([]byte, 256*1024) 
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
