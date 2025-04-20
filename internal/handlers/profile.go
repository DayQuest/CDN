package handlers

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/dayquest/cdn/internal/storage"
	"github.com/gorilla/mux"
)

// ProfileHandler handles profile image requests
type ProfileHandler struct {
	storage storage.Storage
}

// NewProfileHandler creates a new ProfileHandler
func NewProfileHandler(storage storage.Storage) *ProfileHandler {
	return &ProfileHandler{
		storage: storage,
	}
}

// GetProfileImage serves profile image files
func (h *ProfileHandler) GetProfileImage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	username := mux.Vars(r)["username"]

	// Sanitize username
	username = strings.ReplaceAll(username, "/", "")
	username = strings.ReplaceAll(username, "\\", "")

	// Define possible image filename patterns
	patterns := []string{
		fmt.Sprintf("%s.jpg", username),
		fmt.Sprintf("%s.png", username),
		fmt.Sprintf("%s.jpeg", username),
		fmt.Sprintf("user_%s.jpg", username),
		fmt.Sprintf("user_%s.png", username),
		fmt.Sprintf("user_%s.jpeg", username),
	}

	// Try to find the profile image with one of the patterns
	var imageReader io.ReadCloser
	var imageFile string
	var imageFound bool

	for _, pattern := range patterns {
		imagePath := filepath.Join("profile-pictures", pattern)
		reader, err := h.storage.GetProfileImage(ctx, imagePath)
		if err == nil {
			imageReader = reader
			imageFile = pattern
			imageFound = true
			break
		}
	}

	// If no image was found, serve the default image
	if !imageFound {
		defaultImagePath := filepath.Join("profile-pictures", "default.jpg")
		reader, err := h.storage.GetProfileImage(ctx, defaultImagePath)
		if err != nil {
			log.Printf("Error getting default profile image: %v", err)
			http.Error(w, "Default profile image not found", http.StatusInternalServerError)
			return
		}
		imageReader = reader
		imageFile = "default.jpg"
	}

	defer imageReader.Close()

	// Determine content type
	contentType := "image/jpeg"
	if strings.HasSuffix(imageFile, ".png") {
		contentType = "image/png"
	}

	// Set cache headers
	cacheControl := "public, max-age=86400" // 24 hours
	if r.URL.Query().Get("nocache") != "" {
		cacheControl = "no-store, no-cache, must-revalidate, max-age=0"
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", cacheControl)

	// Set a far-future expiration date if not no-cache
	if r.URL.Query().Get("nocache") == "" {
		expiresTime := time.Now().Add(24 * time.Hour)
		w.Header().Set("Expires", expiresTime.Format(time.RFC1123))
	}

	// Copy the image data to the response
	io.Copy(w, imageReader)
}
