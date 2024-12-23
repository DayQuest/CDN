package main

import (
	"log"
	"net/http"
	"time"

	"github.com/dayquest/cdn/internal/config"
	"github.com/dayquest/cdn/internal/handlers"
	"github.com/dayquest/cdn/internal/middleware"
	"github.com/dayquest/cdn/internal/storage"
	"github.com/gorilla/mux"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to loaqd config: %v", err)
	}

	storageVar, err := storage.NewMinioStorage(cfg)
	if err != nil {
		log.Fatalf("Failed to initialize storage: %v", err)
	}

	videoHandler := handlers.NewVideoHandler(storageVar, cfg)
	r := mux.NewRouter()

	r.HandleFunc("/videos/{video}", videoHandler.StreamVideo).Methods(http.MethodGet)
	r.Use(middleware.Logging)

	srv := &http.Server{
		Handler:      r,
		Addr:         ":" + cfg.ServerPort,
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
	}

	log.Printf("Server starting on %s", ":"+cfg.ServerPort)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("Server failed: %v ", err)
	}
}
