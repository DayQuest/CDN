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
    "github.com/minio/minio-go/v7"
    "github.com/dayquest/cdn/internal/database"
)

type VideoProcessor struct {
    storage        *MinioStorage
    db             *database.DBHandler
    processedFiles sync.Map
    workerCount    int
}

func NewVideoProcessor(storage *MinioStorage, db *database.DBHandler, workerCount int) *VideoProcessor {
    return &VideoProcessor{
        storage:     storage,
        db:          db,
        workerCount: workerCount,
    }
}

func (vp *VideoProcessor) Start(ctx context.Context) {

    workChan := make(chan minio.ObjectInfo, vp.workerCount)
    var wg sync.WaitGroup

    for i := 0; i < vp.workerCount; i++ {
        wg.Add(1)
        go vp.processWorker(ctx, &wg, workChan)
    }

    for {
        select {
        case <-ctx.Done():
            close(workChan)
            wg.Wait()
            return
        default:
            objects, err := vp.storage.ListObjects(ctx, vp.storage.rawVideosBucket)
            if err != nil {
                time.Sleep(10 * time.Second)
                continue
            }

            for _, obj := range objects {
                if _, exists := vp.processedFiles.Load(obj.Key); exists {
                    continue
                }



                select {
                case workChan <- obj:
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

           status, err := vp.db.GetVideoStatus(obj.Key)
           if err != nil {
               continue
           }

           if status == database.StatusUnknown {
               continue
           }

           if err := vp.processVideo(ctx, obj); err != nil {
               continue
           }
        }
    }
}

func (vp *VideoProcessor) processVideo(ctx context.Context, obj minio.ObjectInfo) error {

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

    compressedFile, err := vp.compressAndConvertVideo(tmpPath)
    if err != nil {
        err := vp.db.UpdateVideoStatus(obj.Key, database.StatusFailed)
        moveErr := vp.moveToFailedBucket(ctx, obj)
        if moveErr != nil {
            fmt.Printf("Failed to move object %s to failed bucket: %v\n", obj.Key, moveErr)
        }
        if err != nil {
            return fmt.Errorf("failed to update status to failed: %w", err)
        }
        return fmt.Errorf("failed to compress and convert video: %w", err)
    }
    defer os.Remove(compressedFile)

    compressedFileReader, err := os.Open(compressedFile)
    if err != nil {
        return fmt.Errorf("failed to open compressed file: %w", err)
    }
    defer compressedFileReader.Close()

    _, err = vp.storage.PutObject(
        ctx,
        vp.storage.videosBucket,
        obj.Key,
        compressedFileReader,
        -1,
        minio.PutObjectOptions{ContentType: "video/mp4"},
    )
    if err != nil {
        return fmt.Errorf("failed to upload video: %w", err)
    }

    err = vp.storage.DeleteObject(ctx, vp.storage.rawVideosBucket, obj.Key)
    if err != nil {
        return fmt.Errorf("failed to delete original video: %w", err)
    }

    thumbnailPath, err := vp.createThumbnail(tmpPath)
    if err != nil {
        return fmt.Errorf("failed to create thumbnail: %w", err)
    }

    err = vp.uploadThumbnail(ctx, thumbnailPath, obj.Key)
    if err != nil {
        return fmt.Errorf("failed to upload thumbnail: %w", err)
    }

    err = vp.db.UpdateVideoStatus(obj.Key, database.StatusCompleted)
    if err != nil {
        return fmt.Errorf("failed to update status to completed: %w", err)
    }

    return nil
}

func (vp *VideoProcessor) compressAndConvertVideo(inputPath string) (string, error) {
    outputPath := fmt.Sprintf("%s-compressed.mp4", inputPath)

    cmdArgs := []string{
        "ffmpeg", "-y", "-i", inputPath,
        "-c:v", "libx264",
        "-preset", "fast",
        "-crf", "23",
        "-c:a", "aac",
        "-b:a", "96k",
        "-ar", "44100",
        "-f", "mp4",
        "-movflags", "+faststart",
        outputPath,
    }
    cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)

    err := cmd.Run()
    if err != nil {
        return "", fmt.Errorf("ffmpeg command fail: %w", err)
    }

    return outputPath, nil
}

func (vp *VideoProcessor) createThumbnail(videoPath string) (string, error) {
    videoBasePath := strings.TrimSuffix(videoPath, ".mp4")

    thumbnailPath := fmt.Sprintf("%s.jpg", videoBasePath)

    cmdArgs := []string{
        "ffmpeg", "-y", "-i", videoPath,
        "-vf", "thumbnail",
        "-frames:v", "1",
        thumbnailPath,
    }

    cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
    err := cmd.Run()
    if err != nil {
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

    _, err = vp.storage.PutObject(
        ctx,
        vp.storage.thumbnailBucket,
        thumbnailKey,
        thumbnailFile,
        -1,
        minio.PutObjectOptions{ContentType: "image/jpeg"},
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

    _, err = vp.storage.PutObject(
        ctx,
        vp.storage.failedBucket,
        obj.Key,
        reader,
        -1,
        minio.PutObjectOptions{ContentType: "application/octet-stream"},
    )
    if err != nil {
        return fmt.Errorf("failed to upload file to failed bucket: %w", err)
    }

    err = vp.storage.DeleteObject(ctx, vp.storage.rawVideosBucket, obj.Key)
    if err != nil {
        return fmt.Errorf("failed to delete original video: %w", err)
    }

    return nil
}
