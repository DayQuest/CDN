package storage

import (
    "context"
    "fmt"
    "io"
    "github.com/minio/minio-go/v7"
    "github.com/minio/minio-go/v7/pkg/credentials"
    "github.com/dayquest/cdn/internal/config"
    "time"
)

type MinioStorage struct {
    client          *minio.Client
    videosBucket    string
    rawVideosBucket string
    failedBucket    string
    thumbnailBucket string
}

func NewMinioStorage(cfg *config.Config) (*MinioStorage, error) {
    client, err := minio.New(cfg.MinioEndpoint, &minio.Options{
        Creds:  credentials.NewStaticV4(cfg.MinioRootUser, cfg.MinioRootPassword, ""),
        Secure: false,
    })
    if err != nil {
        return nil, fmt.Errorf("failed to initialize MinIO client: %w", err)
    }

    err = ensureBucketExists(client, cfg.VideosBucket)
    if err != nil {
        return nil, fmt.Errorf("failed to ensure VideosBucket exists: %w", err)
    }

    err = ensureBucketExists(client, cfg.FailedBucket)
    if err != nil {
        return nil, fmt.Errorf("failed to ensure FailedBucket exists: %w", err)
    }

    err = ensureBucketExists(client, cfg.ThumbnailBucket)
    if err != nil {
        return nil, fmt.Errorf("failed to ensure ThumbnailBucket exists: %w", err)
    }

    err = ensureBucketExists(client, cfg.RawVideosBucket)
    if err != nil {
        return nil, fmt.Errorf("failed to ensure RawVideosBucket exists: %w", err)
    }

    return &MinioStorage{
        client:          client,
        videosBucket:    cfg.VideosBucket,
        rawVideosBucket: cfg.RawVideosBucket,
        failedBucket:    cfg.FailedBucket,
        thumbnailBucket: cfg.ThumbnailBucket,
    }, nil
}

func ensureBucketExists(client *minio.Client, bucketName string) error {
    retryInterval := 2 * time.Second
    timeout := 30 * time.Second
    startTime := time.Now()

    for time.Since(startTime) < timeout {
        exists, err := client.BucketExists(context.Background(), bucketName)
        if err == nil && exists {
            fmt.Printf("Bucket %s already exists.\n", bucketName)
            return nil
        }

        if err == nil && !exists {
            err = client.MakeBucket(context.Background(), bucketName, minio.MakeBucketOptions{Region: ""})
            if err == nil {
                fmt.Printf("Bucket %s created successfully.\n", bucketName)
                return nil
            } else {
                fmt.Printf("Failed to create bucket %s: %v. Retrying...\n", bucketName, err)
            }
        } else {
            fmt.Printf("Error checking bucket %s: %v. Retrying...\n", bucketName, err)
        }

        time.Sleep(retryInterval)
        retryInterval *= 2
    }

    return fmt.Errorf("failed to ensure bucket %s exists within the timeout period", bucketName)
}

func (s *MinioStorage) ListObjects(ctx context.Context, bucketName string) ([]minio.ObjectInfo, error) {
    objects := s.client.ListObjects(ctx, bucketName, minio.ListObjectsOptions{Recursive: true})
    var objectList []minio.ObjectInfo
    for object := range objects {
        if object.Err != nil {
            return nil, fmt.Errorf("error listing objects: %v", object.Err)
        }
        objectList = append(objectList, object)
    }
    return objectList, nil
}

func (s *MinioStorage) GetObject(ctx context.Context, objectName string, start, end int64) (io.ReadCloser, error) {
    opts := minio.GetObjectOptions{}
    if start > 0 || end >= 0 {
        opts.SetRange(start, end)
    }
    return s.client.GetObject(ctx, s.rawVideosBucket, objectName, opts)
}

func (s *MinioStorage) GetVideo(ctx context.Context, objectName string, start, end int64) (io.ReadCloser, error) {
    opts := minio.GetObjectOptions{}
    if start > 0 || end >= 0 {
        opts.SetRange(start, end)
    }
    return s.client.GetObject(ctx, s.videosBucket, objectName, opts)
}

func (s *MinioStorage) GetHLSContent(ctx context.Context, objectName string) (io.ReadCloser, error) {
    opts := minio.GetObjectOptions{}
    return s.client.GetObject(ctx, s.videosBucket, objectName, opts)
}

func (s *MinioStorage) StatVideo(ctx context.Context, objectName string) (ObjectInfo, error) {
    info, err := s.client.StatObject(ctx, s.videosBucket, objectName, minio.StatObjectOptions{})
    if err != nil {
        return ObjectInfo{}, fmt.Errorf("failed to stat video: %w", err)
    }
    return ObjectInfo{Size: info.Size, ContentType: info.ContentType}, nil
}

func (s *MinioStorage) GetThumbnail(ctx context.Context, objectName string, start, end int64) (io.ReadCloser, error) {
    opts := minio.GetObjectOptions{}
    if start >= 0 && end >= 0 {
        opts.SetRange(start, end)
    }
    return s.client.GetObject(ctx, s.thumbnailBucket, objectName, opts)
}

func (s *MinioStorage) StatThumbnail(ctx context.Context, objectName string) (ObjectInfo, error) {
    info, err := s.client.StatObject(ctx, s.thumbnailBucket, objectName, minio.StatObjectOptions{})
    if err != nil {
        return ObjectInfo{}, fmt.Errorf("failed to stat thumbnail: %w", err)
    }
    return ObjectInfo{Size: info.Size, ContentType: info.ContentType}, nil
}

func (s *MinioStorage) StatObject(ctx context.Context, objectName string) (ObjectInfo, error) {
    info, err := s.client.StatObject(ctx, s.rawVideosBucket, objectName, minio.StatObjectOptions{})
    if err != nil {
        return ObjectInfo{}, fmt.Errorf("failed to stat object: %w", err)
    }
    return ObjectInfo{Size: info.Size, ContentType: info.ContentType}, nil
}

func (s *MinioStorage) DeleteObject(ctx context.Context, bucketName, objectName string) error {
    return s.client.RemoveObject(ctx, bucketName, objectName, minio.RemoveObjectOptions{})
}

func (s *MinioStorage) PutObject(ctx context.Context, bucketName, objectName string, reader io.Reader, objectSize int64, opts minio.PutObjectOptions) (minio.UploadInfo, error) {
    return s.client.PutObject(ctx, bucketName, objectName, reader, objectSize, opts)
}
