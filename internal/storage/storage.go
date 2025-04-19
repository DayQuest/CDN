package storage

import "context"
import "io"

type ObjectInfo struct {
    Size        int64
    ContentType string
}

type Storage interface {
    GetObject(ctx context.Context, objectName string, start, end int64) (io.ReadCloser, error)
    GetThumbnail(ctx context.Context, objectName string, start, end int64) (io.ReadCloser, error)
    GetVideo(ctx context.Context, objectName string, start, end int64) (io.ReadCloser, error)
    GetHLSContent(ctx context.Context, objectName string) (io.ReadCloser, error)
    StatObject(ctx context.Context, objectName string) (ObjectInfo, error)
    StatThumbnail(ctx context.Context, objectName string) (ObjectInfo, error)
    StatVideo(ctx context.Context, objectName string) (ObjectInfo, error)
}
