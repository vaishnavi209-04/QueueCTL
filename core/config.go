package core

import (
	"os"
	"path/filepath"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	MaxRetries        int           `yaml:"max_retries"`
	BackoffBase       int           `yaml:"backoff_base"`
	JobTimeout        time.Duration `yaml:"job_timeout"`
	LeaseSeconds      int           `yaml:"lease_seconds"`
	SweepInterval     time.Duration `yaml:"sweep_interval"`
	HeartbeatInterval time.Duration `yaml:"heartbeat_interval"`
}

func DefaultConfig() Config {
	return Config{
		MaxRetries:        3,
		BackoffBase:       2,
		JobTimeout:        300 * time.Second,
		LeaseSeconds:      30,
		SweepInterval:     15 * time.Second,
		HeartbeatInterval: 10 * time.Second,
	}
}

func LoadConfig(configPath string, dbConfig map[string]string) (Config, error) {
	cfg := DefaultConfig()

	// Load from YAML
	if configPath != "" {
		if _, err := os.Stat(configPath); err == nil {
			data, err := os.ReadFile(configPath)
			if err != nil {
				return cfg, err
			}
			if err := yaml.Unmarshal(data, &cfg); err != nil {
				return cfg, err
			}
		}
	}

	// Merge from DB if available
	if v, ok := dbConfig["max_retries"]; ok {
		if val, err := strconv.Atoi(v); err == nil {
			cfg.MaxRetries = val
		}
	}
	if v, ok := dbConfig["backoff_base"]; ok {
		if val, err := strconv.Atoi(v); err == nil {
			cfg.BackoffBase = val
		}
	}
	if v, ok := dbConfig["job_timeout"]; ok {
		if val, err := time.ParseDuration(v); err == nil {
			cfg.JobTimeout = val
		}
	}
	if v, ok := dbConfig["lease_seconds"]; ok {
		if val, err := strconv.Atoi(v); err == nil {
			cfg.LeaseSeconds = val
		}
	}

	// Default DB config dir
	if configPath == "" {
		home, _ := os.UserHomeDir()
		os.MkdirAll(filepath.Join(home, ".queuectl"), 0755)
	}

	return cfg, nil
}
