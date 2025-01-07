package storage

import "context"
import "io"



type Storage interface {
    GetObject(ctx context.Context, objectName string, start, end int64) (io.ReadCloser, error)
    GetThumbnail(ctx context.Context, objectName string, start, end int64) (io.ReadCloser, error)

    GetVideo(ctx context.Context, objectName string, start, end int64) (io.ReadCloser, error)
    StatObject(ctx context.Context, objectName string) (ObjectInfo, error)
    StatThumbnail(ctx context.Context, objectName string) (ObjectInfo, error)
    StatVideo(ctx context.Context, objectName string) (ObjectInfo, error)
}

