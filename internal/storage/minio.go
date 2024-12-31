package storage

import (
    "context"
    "io"
    "fmt"
    "time"
    "github.com/dayquest/cdn/internal/config"
    "github.com/minio/minio-go/v7"
    "github.com/minio/minio-go/v7/pkg/credentials"
)

type MinioStorage struct {
    client     *minio.Client
    bucketName string
}

func NewMinioStorage(cfg *config.Config) (*MinioStorage, error) {
    client, err := minio.New(cfg.MinioEndpoint, &minio.Options{
        Creds:  credentials.NewStaticV4(cfg.MinioRootUser, cfg.MinioRootPassword, ""),
        Secure: false,
    })
    if err != nil {
        return nil, err
    }
   err = ensureBucketExists(client, cfg.BucketName)
    if err != nil {
        return nil, fmt.Errorf("failed to ensure bucket exists: %w", err)
    }
    return &MinioStorage{
        client:     client,
        bucketName: cfg.BucketName,
    }, nil
}

func ensureBucketExists(client *minio.Client, bucketName string) error {
    retryInterval := 2 * time.Second
    timeout := 30 * time.Second
    startTime := time.Now()

    for time.Since(startTime) < timeout {
        exists, err := client.BucketExists(context.Background(), bucketName)
        if err == nil && exists {
            return nil
        }



        if err == nil && !exists {
            err = client.MakeBucket(context.Background(), bucketName, minio.MakeBucketOptions{Region: ""})
            if err == nil {

                return nil
            }
            fmt.Printf("failed to Create Bucket: %v. Retrying...\n", err)
        }

        time.Sleep(retryInterval)
    }

    return fmt.Errorf("no bucket %v", timeout)
}
func (s *MinioStorage) GetObject(ctx context.Context, objectName string, start, end int64) (io.ReadCloser, error) {
    opts := minio.GetObjectOptions{}
    if start > 0 || end >= 0 {
        opts.SetRange(start, end)
    }
    return s.client.GetObject(ctx, s.bucketName, objectName, opts)
}

func (s *MinioStorage) StatObject(ctx context.Context, objectName string) (ObjectInfo, error) {
    info, err := s.client.StatObject(ctx, s.bucketName, objectName, minio.StatObjectOptions{})
    if err != nil {
        return ObjectInfo{}, err
    }
    return ObjectInfo{Size: info.Size}, nil
}
