package database

import (
    "database/sql"
    "fmt"
    "log"
    "time"
    _ "github.com/go-sql-driver/mysql"
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

    log.Printf("Attempting to connect to the database with DSN: %s", dsn)

    for {
        db, err = sql.Open("mysql", dsn)
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

        log.Println("Successfully connected to the database.")
        break
    }

    return &DBHandler{db: db}, nil
}

func (h *DBHandler) Close() error {
    return h.db.Close()
}
func (h *DBHandler) UpdateVideoStatus(videoKey string, status VideoStatus) error {
    query := `UPDATE video SET status = ? WHERE file_path = ?`

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
    query := `SELECT status FROM video WHERE file_path = ?`

    err := h.db.QueryRow(query, videoKey).Scan(&status)
    if err == sql.ErrNoRows {
        return StatusUnknown, fmt.Errorf("video not found: %s", videoKey)
    }
    if err != nil {
        return StatusUnknown, fmt.Errorf("failed to get video status: %w", err)
    }

    return status, nil
}