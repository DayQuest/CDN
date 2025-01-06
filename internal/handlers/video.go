package handlers

import (
    "fmt"
    "io"
    "net/http"
    "strconv"
    "time"
    "github.com/dayquest/cdn/internal/config"
    "github.com/dayquest/cdn/internal/storage"
    "github.com/gorilla/mux"
)

type VideoHandler struct {
    storage storage.Storage
    config  *config.Config
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

func NewVideoHandler(storage storage.Storage, cfg *config.Config) *VideoHandler {
    return &VideoHandler{
        storage: storage,
        config:  cfg,
    }
}
func (h *VideoHandler) StreamVideo(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()
    videoName := mux.Vars(r)["video"]


    obj, err := h.storage.StatVideo(ctx, videoName)
    if err != nil {
        http.Error(w, "Video not found", http.StatusNotFound)
        return
    }

    start, end := parseRangeHeader(r, obj.Size)

    reader, err := h.storage.GetVideo(ctx, videoName, start, end)
    if err != nil {
        http.Error(w, "Error reading video", http.StatusInternalServerError)
        return
    }
    defer reader.Close()

    seekable := &seekableReadCloser{
        ReadCloser: reader,
        size:      obj.Size,
        offset:    start,
    }

    w.Header().Set("Content-Type", "video/mp4")
    w.Header().Set("Accept-Ranges", "bytes")
    w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))

    if start > 0 || end < obj.Size-1 {
        w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, obj.Size))
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
