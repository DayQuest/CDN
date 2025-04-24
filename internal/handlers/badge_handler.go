package handlers

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gorilla/mux"
)

// BadgeHandler handles badge image requests
func BadgeHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	badgeID := vars["id"]

	// Try different image extensions
	extensions := []string{".jpg", ".png", ".jpeg"}
	var imagePath string
	var found bool

	for _, ext := range extensions {
		testPath := filepath.Join("static", "badges", fmt.Sprintf("%s%s", badgeID, ext))
		if _, err := os.Stat(testPath); err == nil {
			imagePath = testPath
			found = true
			break
		}
	}

	if !found {
		http.NotFound(w, r)
		return
	}

	// Set appropriate content type
	ext := filepath.Ext(imagePath)
	switch ext {
	case ".jpg", ".jpeg":
		w.Header().Set("Content-Type", "image/jpeg")
	case ".png":
		w.Header().Set("Content-Type", "image/png")
	}

	// Serve the file
	http.ServeFile(w, r, imagePath)
}
