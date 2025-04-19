package storage

import (
    "fmt"
    "io"
    "os"
    "os/exec"
    "path/filepath"
    "strings"
    "sync"
    "time"
    "context"
    "runtime"
    "github.com/minio/minio-go/v7"
    "github.com/dayquest/cdn/internal/database"
)

type VideoProcessor struct {
    storage        *MinioStorage
    db             *database.DBHandler
    processedFiles sync.Map
    workerCount    int
    maxRetries     int
}

func NewVideoProcessor(storage *MinioStorage, db *database.DBHandler, workerCount int) *VideoProcessor {
    if workerCount <= 0 {
        workerCount = runtime.NumCPU() // Nutze verfügbare CPU-Kerne
    }
    return &VideoProcessor{
        storage:     storage,
        db:          db,
        workerCount: workerCount,
        maxRetries:  3,
    }
}

func (vp *VideoProcessor) Start(ctx context.Context) {
    workChan := make(chan minio.ObjectInfo, vp.workerCount*2) // Doppelte Buffer-Größe
    var wg sync.WaitGroup

    // Starte Worker
    for i := 0; i < vp.workerCount; i++ {
        wg.Add(1)
        go vp.processWorker(ctx, &wg, workChan)
    }

    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            close(workChan)
            wg.Wait()
            return
        case <-ticker.C:
            objects, err := vp.storage.ListObjects(ctx, vp.storage.rawVideosBucket)
            if err != nil {
                continue
            }

            for _, obj := range objects {
                if _, exists := vp.processedFiles.Load(obj.Key); exists {
                    continue
                }

                select {
                case workChan <- obj:
                    // Video zur Verarbeitung gesendet
                case <-ctx.Done():
                    close(workChan)
                    wg.Wait()
                    return
                }
            }
        }
    }
}

func (vp *VideoProcessor) processWorker(ctx context.Context, wg *sync.WaitGroup, workChan <-chan minio.ObjectInfo) {
    defer wg.Done()

    for {
        select {
        case <-ctx.Done():
            return
        case obj, ok := <-workChan:
            if !ok {
                return
            }

            retryCount := 0
            for retryCount < vp.maxRetries {
                status, err := vp.db.GetVideoStatus(obj.Key)
                if err != nil {
                    retryCount++
                    time.Sleep(time.Second * time.Duration(1<<uint(retryCount)))
                    continue
                }

                if status == database.StatusUnknown {
                    break
                }

                err = vp.processVideo(ctx, obj)
                if err == nil {
                    break
                }

                retryCount++
                if retryCount < vp.maxRetries {
                    time.Sleep(time.Second * time.Duration(1<<uint(retryCount)))
                }
            }
        }
    }
}

func (vp *VideoProcessor) processVideo(ctx context.Context, obj minio.ObjectInfo) error {
    startTime := time.Now()
    defer func() {
        duration := time.Since(startTime)
        fmt.Printf("Video processing completed in %v: %s\n", duration, obj.Key)
    }()

    err := vp.db.UpdateVideoStatus(obj.Key, database.StatusProcessing)
    if err != nil {
        return fmt.Errorf("failed to update status to processing: %w", err)
    }

    tmpFile, err := os.CreateTemp("", "video-*.mp4")
    if err != nil {
        return fmt.Errorf("Failed to create temp file: %w", err)
    }
    tmpPath := tmpFile.Name()
    defer os.Remove(tmpPath)
    defer tmpFile.Close()

    reader, err := vp.storage.GetObject(ctx, obj.Key, 0, -1)
    if err != nil {
        return fmt.Errorf("failed to get object: %w", err)
    }
    defer reader.Close()

    _, err = io.Copy(tmpFile, reader)
    if err != nil {
        return fmt.Errorf("failed to write to temp file: %w", err)
    }

    // Parallel processing für Video und Thumbnail
    var wg sync.WaitGroup
    var compressedFile, thumbnailPath string
    var compressErr, thumbnailErr error

    wg.Add(2)
    go func() {
        defer wg.Done()
        compressedFile, compressErr = vp.compressAndConvertVideo(tmpPath)
    }()

    go func() {
        defer wg.Done()
        thumbnailPath, thumbnailErr = vp.createThumbnail(tmpPath)
    }()

    wg.Wait()

    if compressErr != nil {
        err := vp.db.UpdateVideoStatus(obj.Key, database.StatusFailed)
        moveErr := vp.moveToFailedBucket(ctx, obj)
        if moveErr != nil {
            fmt.Printf("Failed to move object %s to failed bucket: %v\n", obj.Key, moveErr)
        }
        if err != nil {
            return fmt.Errorf("failed to update status to failed: %w", err)
        }
        return fmt.Errorf("failed to compress and convert video: %w", compressErr)
    }
    defer os.Remove(compressedFile)

    if thumbnailErr == nil {
        defer os.Remove(thumbnailPath)
        // Upload thumbnail im Hintergrund
        go func() {
            if err := vp.uploadThumbnail(ctx, thumbnailPath, obj.Key); err != nil {
                fmt.Printf("Failed to upload thumbnail for %s: %v\n", obj.Key, err)
            }
        }()
    }

    compressedFileReader, err := os.Open(compressedFile)
    if err != nil {
        return fmt.Errorf("failed to open compressed file: %w", err)
    }
    defer compressedFileReader.Close()

    // Optimierte PutObject Optionen
    putOpts := minio.PutObjectOptions{
        ContentType: "video/mp4",
        UserMetadata: map[string]string{
            "original-size": fmt.Sprintf("%d", obj.Size),
            "processed-at": time.Now().Format(time.RFC3339),
        },
    }

    _, err = vp.storage.PutObject(
        ctx,
        vp.storage.videosBucket,
        obj.Key,
        compressedFileReader,
        -1,
        putOpts,
    )
    if err != nil {
        return fmt.Errorf("failed to upload video: %w", err)
    }

    // Cleanup im Hintergrund
    go func() {
        if err := vp.storage.DeleteObject(ctx, vp.storage.rawVideosBucket, obj.Key); err != nil {
            fmt.Printf("Failed to delete original video %s: %v\n", obj.Key, err)
        }
    }()

    err = vp.db.UpdateVideoStatus(obj.Key, database.StatusCompleted)
    if err != nil {
        return fmt.Errorf("failed to update status to completed: %w", err)
    }

    return nil
}

func (vp *VideoProcessor) compressAndConvertVideo(inputPath string) (string, error) {
    outputPath := fmt.Sprintf("%s-compressed.mp4", inputPath)

    // Optimierte FFmpeg-Einstellungen für bessere Qualität und Performance
    cmdArgs := []string{
        "ffmpeg", "-y", "-i", inputPath,
        "-c:v", "libx264",
        "-preset", "medium", // Balancierter Preset
        "-crf", "23",       // Gute Qualität (0-51, niedriger = besser)
        "-profile:v", "high",
        "-level", "4.0",
        "-movflags", "+faststart",
        "-c:a", "aac",
        "-b:a", "128k",     // Höhere Audio-Bitrate
        "-ar", "48000",     // Bessere Audio-Sampling-Rate
        "-threads", fmt.Sprintf("%d", runtime.NumCPU()),
        outputPath,
    }

    cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
    if err := cmd.Run(); err != nil {
        return "", fmt.Errorf("ffmpeg command failed: %w", err)
    }

    return outputPath, nil
}

func (vp *VideoProcessor) createThumbnail(videoPath string) (string, error) {
    videoBasePath := strings.TrimSuffix(videoPath, ".mp4")
    thumbnailPath := fmt.Sprintf("%s.jpg", videoBasePath)

    // Verbesserte Thumbnail-Generierung
    cmdArgs := []string{
        "ffmpeg", "-y", "-i", videoPath,
        "-vf", "select=eq(n\\,0),scale=480:-1",  // Erstes Frame, 480p Breite
        "-frames:v", "1",
        "-q:v", "2",                             // Hohe JPEG-Qualität
        thumbnailPath,
    }

    cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
    if err := cmd.Run(); err != nil {
        return "", fmt.Errorf("failed to extract thumbnail: %w", err)
    }

    return thumbnailPath, nil
}

func (vp *VideoProcessor) uploadThumbnail(ctx context.Context, thumbnailPath, videoKey string) error {
    thumbnailFile, err := os.Open(thumbnailPath)
    if err != nil {
        return fmt.Errorf("failed to open thumbnail file: %w", err)
    }
    defer thumbnailFile.Close()

    thumbnailKey := fmt.Sprintf("%s.jpg", strings.TrimSuffix(filepath.Base(videoKey), filepath.Ext(videoKey)))

    // Optimierte PutObject Optionen für Thumbnails
    putOpts := minio.PutObjectOptions{
        ContentType: "image/jpeg",
        UserMetadata: map[string]string{
            "video-key": videoKey,
            "created-at": time.Now().Format(time.RFC3339),
        },
    }

    _, err = vp.storage.PutObject(
        ctx,
        vp.storage.thumbnailBucket,
        thumbnailKey,
        thumbnailFile,
        -1,
        putOpts,
    )
    if err != nil {
        return fmt.Errorf("failed to upload thumbnail to bucket: %w", err)
    }

    return nil
}

func (vp *VideoProcessor) moveToFailedBucket(ctx context.Context, obj minio.ObjectInfo) error {
    reader, err := vp.storage.GetObject(ctx, obj.Key, 0, -1)
    if err != nil {
        return fmt.Errorf("failed to get object: %w", err)
    }
    defer reader.Close()

    // Optimierte PutObject Optionen für fehlgeschlagene Videos
    putOpts := minio.PutObjectOptions{
        ContentType: "application/octet-stream",
        UserMetadata: map[string]string{
            "original-size": fmt.Sprintf("%d", obj.Size),
            "failed-at": time.Now().Format(time.RFC3339),
        },
    }

    _, err = vp.storage.PutObject(
        ctx,
        vp.storage.failedBucket,
        obj.Key,
        reader,
        -1,
        putOpts,
    )
    if err != nil {
        return fmt.Errorf("failed to upload file to failed bucket: %w", err)
    }

    // Cleanup im Hintergrund
    go func() {
        if err := vp.storage.DeleteObject(ctx, vp.storage.rawVideosBucket, obj.Key); err != nil {
            fmt.Printf("Failed to delete failed video %s: %v\n", obj.Key, err)
        }
    }()

    return nil
}
