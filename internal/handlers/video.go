package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/dayquest/cdn/internal/config"
	"github.com/dayquest/cdn/internal/storage"
	"github.com/gorilla/mux"
	"github.com/minio/minio-go/v7"
)

type VideoHandler struct {
	storage *storage.MinioStorage
	config  *config.Config
}

func NewVideoHandler(storage *storage.MinioStorage, cfg *config.Config) *VideoHandler {
	return &VideoHandler{
		storage: storage,
		config:  cfg,
	}
}

func (h *VideoHandler) StreamVideo(w http.ResponseWriter, r *http.Request) {
	videoName := mux.Vars(r)["video"]

	obj, err := h.storage.StatObject(context.Background(), videoName)
	if err != nil {
		http.Error(w, "Video not found", http.StatusNotFound)
		return
	}

	start, end := parseRangeHeader(r, obj.Size)
	setVideoHeaders(w, start, end, obj.Size)

	opts := minio.GetObjectOptions{}
	if start > 0 || end < obj.Size-1 {
		opts.SetRange(start, end)
	}

	object, err := h.storage.GetObject(context.Background(), videoName, opts)
	if err != nil {
		http.Error(w, "Error reading video", http.StatusInternalServerError)
		return
	}
	defer object.Close()

	buf := make([]byte, 32*1024)
	for {
		n, err := object.Read(buf)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				http.Error(w, "Error while streaming video", http.StatusInternalServerError)
				return
			}
		}
		if err != nil {
			if err.Error() != "EOF" {
				http.Error(w, "Error while streaming video", http.StatusInternalServerError)
			}
			break
		}
	}
}

func parseRangeHeader(r *http.Request, fileSize int64) (start, end int64) {
	rangeHeader := r.Header.Get("Range")
	if rangeHeader == "" {
		return 0, fileSize - 1
	}
	fmt.Sscanf(rangeHeader, "bytes=%d-%d", &start, &end)
	if end == 0 {
		end = fileSize - 1
	}
	return start, end
}

func setVideoHeaders(w http.ResponseWriter, start, end, fileSize int64) {
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
	if start > 0 || end < fileSize-1 {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, fileSize))
		w.WriteHeader(http.StatusPartialContent)
	}
}
