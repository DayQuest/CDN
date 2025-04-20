package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dayquest/cdn/internal/config"
	"github.com/dayquest/cdn/internal/database"
	"github.com/dayquest/cdn/internal/handlers"
	"github.com/dayquest/cdn/internal/storage"
	"github.com/gorilla/mux"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Initialize database connection
	db, err := database.NewDatabaseConnection(cfg.DatabaseDSN)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// Initialize storage
	storageClient, err := storage.NewMinioStorage(cfg)
	if err != nil {
		log.Fatalf("Failed to initialize storage: %v", err)
	}

	// Initialize router and handlers
	router := mux.NewRouter()

	// Add ping handler for connection testing
	pingHandler := handlers.NewPingHandler()
	router.HandleFunc("/ping", pingHandler.HandlePing).Methods("GET")
	router.HandleFunc("/ping-test.json", pingHandler.ServeTestFile).Methods("GET")

	// API routes for video metadata
	api := router.PathPrefix("/api").Subrouter()
	videoHandler := handlers.NewVideoHandler(storageClient, cfg, db)
	api.HandleFunc("/videos/{video}", videoHandler.GetVideoMetadata).Methods("GET")

	// CDN routes for video streaming
	cdn := router.PathPrefix("/video").Subrouter()
	cdn.HandleFunc("/{video}", videoHandler.StreamVideo).Methods("GET")
	thumbnailHandler := handlers.NewThumbnailHandler(storageClient)
	cdn.HandleFunc("/thumbnail/{thumbnail}", thumbnailHandler.GetThumbnail).Methods("GET")

	// CDN routes for profile pictures
	profileHandler := handlers.NewProfileHandler(storageClient)
	router.HandleFunc("/profile-pictures/{username}", profileHandler.GetProfileImage).Methods("GET")

	// Configure CORS
	router.Use(mux.CORSMethodMiddleware(router))
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}
			next.ServeHTTP(w, r)
		})
	})

	srv := &http.Server{
		Handler:           router,
		Addr:             ":" + cfg.ServerPort,
		WriteTimeout:      30 * time.Second,  // Increased for large video chunks
		ReadTimeout:      30 * time.Second,
		IdleTimeout:      120 * time.Second,  // Keep connections alive longer
		ReadHeaderTimeout: 10 * time.Second,
		MaxHeaderBytes:    1 << 20,          // 1MB header size limit
	}

	// Enable TCP keep-alive
	srv.SetKeepAlivesEnabled(true)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	processor := storage.NewVideoProcessor(storageClient, db, 3)
	ctx, cancel := context.WithCancel(context.Background())
	go processor.Start(ctx)

	log.Printf("Server starting on %s with Minio storage", ":"+cfg.ServerPort)
	log.Printf("Videos Bucket: %s Raw Videos Bucket: %s", cfg.VideosBucket, cfg.RawVideosBucket)
	log.Printf("Video processor started monitoring %s bucket", cfg.RawVideosBucket)

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server Failed: %v", err)
		}
	}()

	<-quit
	log.Println("Shutting down Server and Video processor...")

	cancel()

	if err := srv.Shutdown(context.Background()); err != nil {
		log.Fatalf("server shutdown failed: %v", err)
	}

	log.Println("server and video processor stopped successfully.")
}
