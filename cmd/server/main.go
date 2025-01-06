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
    "github.com/dayquest/cdn/internal/handlers"
    "github.com/dayquest/cdn/internal/storage"
    "github.com/gorilla/mux"
)

func main() {
    cfg, err := config.Load()
    if err != nil {
        log.Fatalf("failed to load config: %v", err)
    }

    storageProvider, err := storage.NewMinioStorage(cfg)
    if err != nil {
        log.Fatalf("failed to intialize storage: %v", err)
    }

    videoHandler := handlers.NewVideoHandler(storageProvider, cfg)

    r := mux.NewRouter()

    r.HandleFunc("/video/{video}", videoHandler.StreamVideo).Methods(http.MethodGet)

    srv := &http.Server{
        Handler:      r,
        Addr:         ":" + cfg.ServerPort,
        WriteTimeout: 15 * time.Second,
        ReadTimeout:  15 * time.Second,
    }

    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

    processor := storage.NewVideoProcessor(storageProvider, 3)
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
