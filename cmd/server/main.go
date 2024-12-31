package main

import (
    "log"
    "net/http"
    "time"

    "github.com/dayquest/cdn/internal/config"
    "github.com/dayquest/cdn/internal/handlers"
    //"github.com/dayquest/cdn/internal/middleware"
    "github.com/dayquest/cdn/internal/storage"
    "github.com/gorilla/mux"
)

func main() {
    cfg, err := config.Load()
    if err != nil {
        log.Fatalf("failed to losad config: %v", err)
    }

    var storageProvider storage.Storage
    switch cfg.StorageType {
    case config.StorageTypeMinio:
        storageProvider, err = storage.NewMinioStorage(cfg)
    case config.StorageTypeLocal:
        storageProvider, err = storage.NewLocalStorage(cfg)
    }

    if err != nil {
        log.Fatalf("Failed to init storage: %v", err)
    }

    videoHandler := handlers.NewVideoHandler(storageProvider, cfg)
    r := mux.NewRouter()

    r.HandleFunc("/video/{video}", videoHandler.StreamVideo).Methods(http.MethodGet)
    //r.Use(middleware.Logging)

    srv := &http.Server{
        Handler:      r,
        Addr:         ":" + cfg.ServerPort,
        WriteTimeout: 15 * time.Second,
        ReadTimeout:  15 * time.Second,
    }

    log.Printf("Server starting on %s with %s storage", ":"+cfg.ServerPort, cfg.StorageType)
    if err := srv.ListenAndServe(); err != nil {
        log.Fatalf("Server failed: %v", err)
    }
}