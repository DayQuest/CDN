package storage

import (
    "context"
    "fmt"
    "io"
    "os"
    "os/exec"
    "strings"
    "sync"
    "time"
    "github.com/minio/minio-go/v7"
)

type VideoProcessor struct {
    storage        *MinioStorage
    processedFiles sync.Map
    workerCount    int
}

func NewVideoProcessor(storage *MinioStorage, workerCount int) *VideoProcessor {
    return &VideoProcessor{
        storage:     storage,
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

                if !strings.HasSuffix(obj.Key, ".mp4") {
                    err := vp.moveToFailedBucket(ctx, obj)
                    if err != nil {
                        continue
                    }
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

            if err := vp.processVideo(ctx, obj); err != nil {
                errMove := vp.moveToFailedBucket(ctx, obj)
                if errMove != nil {
                    continue
                }
            }
        }
    }
}

func (vp *VideoProcessor) processVideo(ctx context.Context, obj minio.ObjectInfo) error {
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

    return nil
}

func (vp *VideoProcessor) compressAndConvertVideo(inputPath string) (string, error) {
    outputPath := fmt.Sprintf("%s-compressed.mp4", inputPath)

    cmdArgs := []string{
        "ffmpeg", "-y", "-i", inputPath,
        "-c:v", "libx264",
        "-preset", "medium",
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
