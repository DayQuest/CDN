package storage

import (
	"context"

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
		Creds:  credentials.NewStaticV4(cfg.MinioAccessKey, cfg.MinioSecretKey, ""),
		Secure: false,
	})
	if err != nil {
		return nil, err
	}

	return &MinioStorage{
		client:     client,
		bucketName: cfg.BucketName,
	}, nil
}

func (s *MinioStorage) GetObject(ctx context.Context, objectName string, opts minio.GetObjectOptions) (*minio.Object, error) {
	return s.client.GetObject(ctx, s.bucketName, objectName, opts)
}

func (s *MinioStorage) StatObject(ctx context.Context, objectName string) (minio.ObjectInfo, error) {
	return s.client.StatObject(ctx, s.bucketName, objectName, minio.StatObjectOptions{})
}
