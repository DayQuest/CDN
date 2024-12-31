package storage

import (
    "context"
    "io"
    "os"
    "path/filepath"

    "github.com/dayquest/cdn/internal/config"
)

type LocalStorage struct {
    basePath string
}

func NewLocalStorage(cfg *config.Config) (*LocalStorage, error) {
    if err := os.MkdirAll(cfg.LocalStoragePath, 0755); err != nil {
        return nil, err
    }
    return &LocalStorage{basePath: cfg.LocalStoragePath}, nil
}

func (s *LocalStorage) GetObject(ctx context.Context, objectName string, start, end int64) (io.ReadCloser, error) {
    file, err := os.Open(filepath.Join(s.basePath, objectName))
    if err != nil {
        return nil, err
    }

    if start > 0 {
        _, err = file.Seek(start, io.SeekStart)
        if err != nil {
            file.Close()
            return nil, err
        }
    }

    if end >= 0 {
        return &limitedReadCloser{
            ReadCloser: file,
            remaining: end - start + 1,
        }, nil
    }

    return file, nil
}

func (s *LocalStorage) StatObject(ctx context.Context, objectName string) (ObjectInfo, error) {
    info, err := os.Stat(filepath.Join(s.basePath, objectName))
    if err != nil {
        return ObjectInfo{}, err
    }
    return ObjectInfo{Size: info.Size()}, nil
}

type limitedReadCloser struct {
    io.ReadCloser
    remaining int64
}

func (l *limitedReadCloser) Read(p []byte) (n int, err error) {
    if l.remaining <= 0 {
        return 0, io.EOF
    }
    if int64(len(p)) > l.remaining {
        p = p[0:l.remaining]
    }
    n, err = l.ReadCloser.Read(p)
    l.remaining -= int64(n)
    return
}