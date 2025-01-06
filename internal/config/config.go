package config

import (
    "fmt"
    "os"
)

type Config struct {
    MinioEndpoint    string
    MinioRootUser    string
    MinioRootPassword string
    VideosBucket     string
    RawVideosBucket  string
    FailedBucket  string
    ServerPort       string
    DatabaseDSN      string
}

func Load() (*Config, error) {
    cfg := &Config{
        ServerPort:       os.Getenv("SERVER_PORT"),
        MinioEndpoint:    os.Getenv("MINIO_ENDPOINT"),
        MinioRootUser:    os.Getenv("MINIO_ROOT_USER"),
        MinioRootPassword: os.Getenv("MINIO_ROOT_PASSWORD"),
        VideosBucket:     os.Getenv("VIDEOS_BUCKET"),
        RawVideosBucket:  os.Getenv("RAW_VIDEOS_BUCKET"),
        FailedBucket:     os.Getenv("FAILED_BUCKET"),
        DatabaseDSN:      os.Getenv("DATABASE_DSN"),
    }

    return cfg, cfg.validate()
}

func (c *Config) validate() error {
    if c.ServerPort == "" {
        return fmt.Errorf("SERVER_PORT is not set")
    }

    if c.MinioEndpoint == "" || c.MinioRootUser == "" ||
       c.MinioRootPassword == "" || c.VideosBucket == "" ||
       c.RawVideosBucket == "" {
        return fmt.Errorf("missing required Minio configuration")
    }

    return nil
}
