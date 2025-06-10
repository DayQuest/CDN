package database

import (
    "database/sql"
    "fmt"
    "log"
    "time"
    _ "github.com/lib/pq"
)

type VideoStatus int

const (
    StatusUnknown    VideoStatus = 0
    StatusPending    VideoStatus = 1
    StatusProcessing VideoStatus = 2
    StatusCompleted  VideoStatus = 3
    StatusFailed     VideoStatus = 4
)

type DBHandler struct {
    db *sql.DB
}

func NewDatabaseConnection(dsn string) (*DBHandler, error) {
    var db *sql.DB
    var err error
    
    log.Printf("Attempting to connect to PostgreSQL database with DSN: %s", dsn)
    
    for {
        db, err = sql.Open("postgres", dsn)
        if err != nil {
            log.Printf("Failed to open database connection: %v. Retrying in 5 seconds...", err)
            time.Sleep(5 * time.Second)
            continue
        }
        
        err = db.Ping()
        if err != nil {
            log.Printf("Failed to ping database: %v. Retrying in 5 seconds...", err)
            time.Sleep(5 * time.Second)
            continue
        }
        
        log.Println("Successfully connected to PostgreSQL database.")
        break
    }
    
    return &DBHandler{db: db}, nil
}

func (h *DBHandler) Close() error {
    return h.db.Close()
}

func (h *DBHandler) UpdateVideoStatus(videoKey string, status VideoStatus) error {
    query := `UPDATE video SET status = $1 WHERE file_path = $2`
    result, err := h.db.Exec(query, status, videoKey)
    if err != nil {
        return fmt.Errorf("failed to update video status: %w", err)
    }
    
    rowsAffected, err := result.RowsAffected()
    if err != nil {
        return fmt.Errorf("failed to get rows affected: %w", err)
    }
    
    if rowsAffected == 0 {
        return fmt.Errorf("no video found with key: %s", videoKey)
    }
    
    return nil
}

func (h *DBHandler) GetVideoStatus(videoKey string) (VideoStatus, error) {
    var status VideoStatus
    query := `SELECT status FROM video WHERE file_path = $1`
    err := h.db.QueryRow(query, videoKey).Scan(&status)
    
    if err == sql.ErrNoRows {
        return StatusUnknown, fmt.Errorf("video not found: %s", videoKey)
    }
    if err != nil {
        return StatusUnknown, fmt.Errorf("failed to get video status: %w", err)
    }
    
    return status, nil
}

func (h *DBHandler) InsertVideo(videoKey string, status VideoStatus) error {
    query := `INSERT INTO video (file_path, status, created_at) VALUES ($1, $2, NOW())
              ON CONFLICT (file_path) DO UPDATE SET status = $2`
    _, err := h.db.Exec(query, videoKey, status)
    if err != nil {
        return fmt.Errorf("failed to insert video: %w", err)
    }
    return nil
}

func (h *DBHandler) GetVideosByStatus(status VideoStatus) ([]string, error) {
    query := `SELECT file_path FROM video WHERE status = $1`
    rows, err := h.db.Query(query, status)
    if err != nil {
        return nil, fmt.Errorf("failed to query videos by status: %w", err)
    }
    defer rows.Close()
    
    var videoKeys []string
    for rows.Next() {
        var videoKey string
        if err := rows.Scan(&videoKey); err != nil {
            return nil, fmt.Errorf("failed to scan video key: %w", err)
        }
        videoKeys = append(videoKeys, videoKey)
    }
    
    if err := rows.Err(); err != nil {
        return nil, fmt.Errorf("error iterating over rows: %w", err)
    }
    
    return videoKeys, nil
}
