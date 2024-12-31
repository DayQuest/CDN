package config

import (
    "fmt"
    "os"
)

type StorageType string

const (
    StorageTypeMinio StorageType = "minio"
    StorageTypeLocal StorageType = "local"
)

type Config struct {
    StorageType      StorageType
    MinioEndpoint    string
    MinioRootUser    string
    MinioRootPassword string
    BucketName       string
    LocalStoragePath string
    ServerPort       string
}
func Load() (*Config, error) {
    storageType := StorageType(os.Getenv("STORAGE_TYPE"))
    if storageType != StorageTypeMinio && storageType != StorageTypeLocal {
        return nil, fmt.Errorf("invalid storage type: %s", storageType)
    }

    cfg := &Config{
        StorageType: storageType,
        ServerPort:  os.Getenv("SERVER_PORT"),
    }

    if storageType == StorageTypeMinio {
        cfg.MinioEndpoint = os.Getenv("MINIO_ENDPOINT")
        cfg.MinioRootUser = os.Getenv("MINIO_ROOT_USER")
        cfg.MinioRootPassword = os.Getenv("MINIO_ROOT_PASSWORD")
        cfg.BucketName = os.Getenv("BUCKET_NAME")
    } else if storageType == StorageTypeLocal {
        cfg.LocalStoragePath = "/data"
    }

    return cfg, cfg.validate()
}


func (c *Config) validate() error {
    if c.ServerPort == "" {
        return fmt.Errorf("SERVER_PORT is not set")
    }

    if c.StorageType == StorageTypeMinio {
        if c.MinioEndpoint == "" || c.MinioRootUser == "" ||
           c.MinioRootPassword == "" || c.BucketName == "" {
            return fmt.Errorf("missing required Minio configuration")
        }
    } else if c.StorageType == StorageTypeLocal {
        if c.LocalStoragePath == "" {
            return fmt.Errorf("LOCAL_STORAGE_PATH not set")
        }
    }

    return nil
}
