package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Server   ServerConfig
	Database DatabaseConfig
	Cache    CacheConfig
	Jobs     JobsConfig
	Auth     AuthConfig
	AI       AIConfig
}

type ServerConfig struct {
	Address string
	Port    int
}

type DatabaseConfig struct {
	Path string
}

type CacheConfig struct {
	Path string
}

type JobsConfig struct {
	Workers   int
	QueueSize int
}

type AuthConfig struct {
	Enabled      bool
	Username     string
	Password     string
	PasswordHash string
	Secret       string
}

type AIConfig struct {
	Enabled bool
	APIKey  string
	Model   string
	Timeout time.Duration
}

func Load() (Config, error) {
	cfg := Config{
		Server: ServerConfig{
			Address: envString("ALEXANDRIA_SERVER_ADDRESS", "0.0.0.0"),
			Port:    8080,
		},
		Database: DatabaseConfig{
			Path: envString("ALEXANDRIA_DATABASE_PATH", "/config/alexandria.db"),
		},
		Cache: CacheConfig{
			Path: envString("ALEXANDRIA_CACHE_PATH", "/config/cache"),
		},
		Jobs: JobsConfig{
			Workers:   1,
			QueueSize: 100,
		},
		Auth: AuthConfig{
			Username:     envString("ALEXANDRIA_AUTH_USERNAME", "admin"),
			Password:     os.Getenv("ALEXANDRIA_AUTH_PASSWORD"),
			PasswordHash: os.Getenv("ALEXANDRIA_AUTH_PASSWORD_HASH"),
			Secret:       os.Getenv("ALEXANDRIA_AUTH_SECRET"),
		},
		AI: AIConfig{
			APIKey:  os.Getenv("ALEXANDRIA_GEMINI_API_KEY"),
			Model:   envString("ALEXANDRIA_AI_MODEL", "gemini-2.5-flash-lite"),
			Timeout: 20 * time.Second,
		},
	}

	var err error
	cfg.Server.Port, err = envInt("ALEXANDRIA_SERVER_PORT", cfg.Server.Port)
	if err != nil {
		return Config{}, err
	}
	cfg.Jobs.Workers, err = envInt("ALEXANDRIA_JOBS_WORKERS", cfg.Jobs.Workers)
	if err != nil {
		return Config{}, err
	}
	cfg.Jobs.QueueSize, err = envInt("ALEXANDRIA_JOBS_QUEUE_SIZE", cfg.Jobs.QueueSize)
	if err != nil {
		return Config{}, err
	}
	aiTimeoutSeconds, err := envInt("ALEXANDRIA_AI_TIMEOUT_SECONDS", 20)
	if err != nil {
		return Config{}, err
	}
	cfg.AI.Timeout = time.Duration(aiTimeoutSeconds) * time.Second
	cfg.AI.Enabled = cfg.AI.APIKey != ""
	if rawEnabled := os.Getenv("ALEXANDRIA_AI_ENABLED"); rawEnabled != "" {
		cfg.AI.Enabled, err = strconv.ParseBool(rawEnabled)
		if err != nil {
			return Config{}, fmt.Errorf("ALEXANDRIA_AI_ENABLED must be a boolean: %w", err)
		}
	}
	cfg.Auth.Enabled = cfg.Auth.Password != "" || cfg.Auth.PasswordHash != ""
	if rawEnabled := os.Getenv("ALEXANDRIA_AUTH_ENABLED"); rawEnabled != "" {
		cfg.Auth.Enabled, err = strconv.ParseBool(rawEnabled)
		if err != nil {
			return Config{}, fmt.Errorf("ALEXANDRIA_AUTH_ENABLED must be a boolean: %w", err)
		}
	}

	if cfg.Server.Port <= 0 || cfg.Server.Port > 65535 {
		return Config{}, fmt.Errorf("ALEXANDRIA_SERVER_PORT must be between 1 and 65535")
	}
	if cfg.Jobs.Workers < 1 {
		return Config{}, fmt.Errorf("ALEXANDRIA_JOBS_WORKERS must be at least 1")
	}
	if cfg.Jobs.QueueSize < 1 {
		return Config{}, fmt.Errorf("ALEXANDRIA_JOBS_QUEUE_SIZE must be at least 1")
	}
	if cfg.AI.Timeout <= 0 || cfg.AI.Timeout > 5*time.Minute {
		return Config{}, fmt.Errorf("ALEXANDRIA_AI_TIMEOUT_SECONDS must be between 1 and 300")
	}
	if cfg.AI.Enabled && cfg.AI.APIKey == "" {
		return Config{}, fmt.Errorf("ALEXANDRIA_GEMINI_API_KEY is required when AI is enabled")
	}
	if cfg.Auth.Enabled {
		if cfg.Auth.Username == "" {
			return Config{}, fmt.Errorf("ALEXANDRIA_AUTH_USERNAME is required when auth is enabled")
		}
		if cfg.Auth.Password == "" && cfg.Auth.PasswordHash == "" {
			return Config{}, fmt.Errorf("ALEXANDRIA_AUTH_PASSWORD or ALEXANDRIA_AUTH_PASSWORD_HASH is required when auth is enabled")
		}
	}

	return cfg, nil
}

func envString(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func envInt(key string, fallback int) (int, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", key, err)
	}
	return parsed, nil
}
