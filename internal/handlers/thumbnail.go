package handlers

import (
    "io"
    "net/http"
    "strconv"

    "github.com/dayquest/cdn/internal/storage"
    "github.com/gorilla/mux"
)

type ThumbnailHandler struct {
    storage storage.Storage
}

func NewThumbnailHandler(s storage.Storage) *ThumbnailHandler {
    return &ThumbnailHandler{storage: s}
}

func (h *ThumbnailHandler) GetThumbnail(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()

    thumbnailName := mux.Vars(r)["thumbnail"]

    objInfo, err := h.storage.StatThumbnail(ctx, thumbnailName)
    if err != nil {
        http.Error(w, "Thumbnail not found", http.StatusNotFound)
        return
    }

    w.Header().Set("Content-Type", "image/jpeg")
    w.Header().Set("Content-Length", strconv.FormatInt(objInfo.Size, 10))

    reader, err := h.storage.GetThumbnail(ctx, thumbnailName, 0, objInfo.Size-1)
    if err != nil {
        http.Error(w, "Failed to retrieve thumbnail", http.StatusInternalServerError)
        return
    }
    defer reader.Close()

    if _, err := io.Copy(w, reader); err != nil {
        http.Error(w, "Failed to stream thumbnail", http.StatusInternalServerError)
    }
}
