package config

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	MinioEndpoint  string
	MinioAccessKey string
	MinioSecretKey string
	BucketName     string
	ServerPort     string
}

func Load() (*Config, error) {
	if err := godotenv.Load(); err != nil {
		return nil, fmt.Errorf("error loading envirnment: %v", err)
	}

	requiredVars := []struct {
		name, value string
	}{
		{"MINIO_ENDPOINT", os.Getenv("MINIO_ENDPOINT")},
		{"MINIO_ACCESS_KEY", os.Getenv("MINIO_ACCESS_KEY")},
		{"MINIO_SECRET_KEY", os.Getenv("MINIO_SECRET_KEY")},
		{"BUCKET_NAME", os.Getenv("BUCKET_NAME")},
		{"SERVER_PORT", os.Getenv("SERVER_PORT")},
	}

	for _, v := range requiredVars {
		if v.value == "" {
			return nil, fmt.Errorf("%s is not set", v.name)
		}
	}

	return &Config{
		MinioEndpoint:  requiredVars[0].value,
		MinioAccessKey: requiredVars[1].value,
		MinioSecretKey: requiredVars[2].value,
		BucketName:     requiredVars[3].value,
		ServerPort:     requiredVars[4].value,
	}, nil
}
