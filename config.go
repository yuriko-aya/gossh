package main

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/fernet/fernet-go"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Server struct {
		Address string `yaml:"address"`
		Port    int    `yaml:"port"`
	} `yaml:"server"`
	Security struct {
		FernetKey string `yaml:"fernet_key"`
		TokenTTL    string `yaml:"token_ttl"`
	} `yaml:"security"`
	RateLimit struct {
		ConnectPerMinute int `yaml:"connect_per_minute"`
		WSPerMinute      int `yaml:"ws_per_minute"`
		PresignPerMinute int `yaml:"presign_per_minute"`
	} `yaml:"rate_limit"`
	S3 struct {
		Region          string `yaml:"region"`
		Bucket          string `yaml:"bucket"`
		AccessKeyID     string `yaml:"access_key_id"`
		SecretAccessKey string `yaml:"secret_access_key"`
	} `yaml:"s3"`
}

func loadConfig(filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("error opening config file: %v", err)
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		return fmt.Errorf("error reading config file: %v", err)
	}

	if err := yaml.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("error parsing config file: %v", err)
	}

	applyEnvOverrides()
	applyConfigDefaults()
	return validateConfig()
}

func applyEnvOverrides() {
	if v := os.Getenv("FERNET_KEY"); v != "" {
		config.Security.FernetKey = v
	}
	if v := os.Getenv("S3_REGION"); v != "" {
		config.S3.Region = v
	}
	if v := os.Getenv("S3_BUCKET"); v != "" {
		config.S3.Bucket = v
	}
	if v := os.Getenv("S3_ACCESS_KEY_ID"); v != "" {
		config.S3.AccessKeyID = v
	}
	if v := os.Getenv("S3_SECRET_ACCESS_KEY"); v != "" {
		config.S3.SecretAccessKey = v
	}
}

func applyConfigDefaults() {
	if config.Server.Address == "" {
		config.Server.Address = "0.0.0.0"
	}
	if config.Server.Port == 0 {
		config.Server.Port = 8088
	}
	if config.Security.TokenTTL == "" {
		config.Security.TokenTTL = "24h"
	}
	if config.RateLimit.ConnectPerMinute == 0 {
		config.RateLimit.ConnectPerMinute = 10
	}
	if config.RateLimit.WSPerMinute == 0 {
		config.RateLimit.WSPerMinute = 20
	}
	if config.RateLimit.PresignPerMinute == 0 {
		config.RateLimit.PresignPerMinute = 30
	}
}

func validateConfig() error {
	if config.Security.FernetKey == "" {
		return fmt.Errorf("fernet key is not configured: set security.fernet_key in config.yaml or FERNET_KEY env var")
	}
	if _, err := fernet.DecodeKeys(config.Security.FernetKey); err != nil {
		return fmt.Errorf("invalid fernet key: %v", err)
	}
	if _, err := time.ParseDuration(config.Security.TokenTTL); err != nil {
		return fmt.Errorf("invalid security.token_ttl %q: %v", config.Security.TokenTTL, err)
	}
	return nil
}

func tokenTTL() time.Duration {
	d, err := time.ParseDuration(config.Security.TokenTTL)
	if err != nil {
		return 24 * time.Hour
	}
	return d
}

func getDefaultFernetKey() string {
	return config.Security.FernetKey
}
