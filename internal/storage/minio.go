package storage

import (
	"context"
	"fmt"
	"io"
	"log"
	"path/filepath"
	"time"

	"github.com/dayquest/cdn/internal/config"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type MinioStorage struct {
	client             *minio.Client
	videosBucket       string
	rawVideosBucket    string
	failedBucket       string
	thumbnailBucket    string
	profileImageBucket string
}

func NewMinioStorage(cfg *config.Config) (*MinioStorage, error) {
	minioClient, err := minio.New(cfg.MinioEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.MinioRootUser, cfg.MinioRootPassword, ""),
		Secure: false,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Minio client: %w", err)
	}

	storage := &MinioStorage{
		client:             minioClient,
		videosBucket:       cfg.VideosBucket,
		rawVideosBucket:    cfg.RawVideosBucket,
		failedBucket:       cfg.FailedBucket,
		thumbnailBucket:    cfg.ThumbnailBucket,
		profileImageBucket: cfg.ProfileImageBucket,
	}

	// Ensure the buckets exist
	ctx := context.Background()
	buckets := []string{
		storage.videosBucket,
		storage.rawVideosBucket,
		storage.failedBucket,
		storage.thumbnailBucket,
		storage.profileImageBucket,
	}

	for _, bucket := range buckets {
		err := storage.ensureBucketExists(ctx, bucket)
		if err != nil {
			return nil, fmt.Errorf("failed to ensure bucket %s exists: %w", bucket, err)
		}
	}

	return storage, nil
}

func (s *MinioStorage) ensureBucketExists(ctx context.Context, bucketName string) error {
	exists, err := s.client.BucketExists(ctx, bucketName)
	if err != nil {
		return fmt.Errorf("failed to check if bucket %s exists: %w", bucketName, err)
	}

	if !exists {
		err = s.client.MakeBucket(ctx, bucketName, minio.MakeBucketOptions{})
		if err != nil {
			return fmt.Errorf("failed to create bucket %s: %w", bucketName, err)
		}

		// Set bucket policy to allow public read access
		policy := fmt.Sprintf(`{
            "Version": "2012-10-17",
            "Statement": [
                {
                    "Effect": "Allow",
                    "Principal": {"AWS": ["*"]},
                    "Action": ["s3:GetObject"],
                    "Resource": ["arn:aws:s3:::%s/*"]
                }
            ]
        }`, bucketName)

		err = s.client.SetBucketPolicy(ctx, bucketName, policy)
		if err != nil {
			return fmt.Errorf("failed to set bucket policy for %s: %w", bucketName, err)
		}

		log.Printf("Created bucket %s with public read access", bucketName)
	}

	return nil
}

func (s *MinioStorage) ListObjects(ctx context.Context, bucketName string) ([]minio.ObjectInfo, error) {
	var objects []minio.ObjectInfo

	opts := minio.ListObjectsOptions{
		Recursive: true,
	}

	for object := range s.client.ListObjects(ctx, bucketName, opts) {
		if object.Err != nil {
			return nil, fmt.Errorf("error listing objects: %w", object.Err)
		}
		objects = append(objects, object)
	}

	return objects, nil
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

func (s *MinioStorage) GetThumbnail(ctx context.Context, objectName string, start, end int64) (io.ReadCloser, error) {
	opts := minio.GetObjectOptions{}
	if start > 0 || end >= 0 {
		opts.SetRange(start, end)
	}
	return s.client.GetObject(ctx, s.thumbnailBucket, objectName, opts)
}

func (s *MinioStorage) GetProfileImage(ctx context.Context, imagePath string) (io.ReadCloser, error) {
	opts := minio.GetObjectOptions{}
	return s.client.GetObject(ctx, s.profileImageBucket, imagePath, opts)
}

func (s *MinioStorage) UploadVideo(ctx context.Context, objectName string, reader io.Reader, contentType string) error {
	_, err := s.client.PutObject(ctx, s.videosBucket, objectName, reader, -1, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return fmt.Errorf("error uploading video %s: %w", objectName, err)
	}
	return nil
}

func (s *MinioStorage) UploadThumbnail(ctx context.Context, objectName string, reader io.Reader, contentType string) error {
	_, err := s.client.PutObject(ctx, s.thumbnailBucket, objectName, reader, -1, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return fmt.Errorf("error uploading thumbnail %s: %w", objectName, err)
	}
	return nil
}

func (s *MinioStorage) UploadProfileImage(ctx context.Context, objectName string, reader io.Reader, contentType string) error {
	_, err := s.client.PutObject(ctx, s.profileImageBucket, objectName, reader, -1, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return fmt.Errorf("error uploading profile image %s: %w", objectName, err)
	}
	return nil
}

func (s *MinioStorage) UploadFile(ctx context.Context, bucketName, objectName string, reader io.Reader, contentType string) error {
	_, err := s.client.PutObject(ctx, bucketName, objectName, reader, -1, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return fmt.Errorf("error uploading file %s to bucket %s: %w", objectName, bucketName, err)
	}
	return nil
}

func (s *MinioStorage) GetStats(ctx context.Context, bucketName, objectName string) (minio.ObjectInfo, error) {
	objInfo, err := s.client.StatObject(ctx, bucketName, objectName, minio.StatObjectOptions{})
	if err != nil {
		return minio.ObjectInfo{}, fmt.Errorf("error getting stats for %s from bucket %s: %w", objectName, bucketName, err)
	}
	return objInfo, nil
}

func (s *MinioStorage) DeleteObject(ctx context.Context, bucketName, objectName string) error {
	err := s.client.RemoveObject(ctx, bucketName, objectName, minio.RemoveObjectOptions{})
	if err != nil {
		return fmt.Errorf("error deleting object %s from bucket %s: %w", objectName, bucketName, err)
	}
	return nil
}

func (s *MinioStorage) DeleteProfileImage(ctx context.Context, profileImageID string) error {
	return s.DeleteObject(ctx, s.profileImageBucket, profileImageID)
}

func (s *MinioStorage) GetPresignedURL(ctx context.Context, bucketName, objectName string, expiry time.Duration) (string, error) {
	presignedURL, err := s.client.PresignedGetObject(ctx, bucketName, objectName, expiry, nil)
	if err != nil {
		return "", fmt.Errorf("error generating presigned URL for %s in bucket %s: %w", objectName, bucketName, err)
	}
	return presignedURL.String(), nil
}

func (s *MinioStorage) GetProfileImageURL(ctx context.Context, profileImageID string, expiry time.Duration) (string, error) {
	return s.GetPresignedURL(ctx, s.profileImageBucket, profileImageID, expiry)
}

func GetFileExtension(filename string) string {
	return filepath.Ext(filename)
}

func (s *MinioStorage) GetHLSContent(ctx context.Context, objectName string) (io.ReadCloser, error) {
	opts := minio.GetObjectOptions{}
	return s.client.GetObject(ctx, s.videosBucket, objectName, opts)
}

func (s *MinioStorage) StatObject(ctx context.Context, objectName string) (ObjectInfo, error) {
	info, err := s.client.StatObject(ctx, s.rawVideosBucket, objectName, minio.StatObjectOptions{})
	if err != nil {
		return ObjectInfo{}, fmt.Errorf("failed to stat object: %w", err)
	}
	return ObjectInfo{Size: info.Size, ContentType: info.ContentType}, nil
}

func (s *MinioStorage) StatThumbnail(ctx context.Context, objectName string) (ObjectInfo, error) {
	info, err := s.client.StatObject(ctx, s.thumbnailBucket, objectName, minio.StatObjectOptions{})
	if err != nil {
		return ObjectInfo{}, fmt.Errorf("failed to stat thumbnail: %w", err)
	}
	return ObjectInfo{Size: info.Size, ContentType: info.ContentType}, nil
}

func (s *MinioStorage) StatVideo(ctx context.Context, objectName string) (ObjectInfo, error) {
	info, err := s.client.StatObject(ctx, s.videosBucket, objectName, minio.StatObjectOptions{})
	if err != nil {
		return ObjectInfo{}, fmt.Errorf("failed to stat video: %w", err)
	}
	return ObjectInfo{Size: info.Size, ContentType: info.ContentType}, nil
}

func (s *MinioStorage) StatProfileImage(ctx context.Context, imagePath string) (ObjectInfo, error) {
	info, err := s.client.StatObject(ctx, s.profileImageBucket, imagePath, minio.StatObjectOptions{})
	if err != nil {
		return ObjectInfo{}, fmt.Errorf("failed to stat profile image: %w", err)
	}
	return ObjectInfo{Size: info.Size, ContentType: info.ContentType}, nil
}

// PutObject für Kompatibilität mit dem VideoProcessor
func (s *MinioStorage) PutObject(ctx context.Context, bucketName, objectName string, reader io.Reader, size int64, opts minio.PutObjectOptions) (minio.UploadInfo, error) {
	return s.client.PutObject(ctx, bucketName, objectName, reader, size, opts)
}
