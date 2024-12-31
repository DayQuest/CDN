package storage

import (
    "context"
    "io"
)

type ObjectInfo struct {
    Size int64
}

type Storage interface {
    GetObject(ctx context.Context, objectName string, start, end int64) (io.ReadCloser, error)
    StatObject(ctx context.Context, objectName string) (ObjectInfo, error)
}
