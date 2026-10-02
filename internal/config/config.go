package config

import (
	"os"
	"strconv"
	"strings"
)

// BaseConfig provides shared runtime configuration.
type BaseConfig struct {
	Port     string
	LogLevel string
}

// CollectorConfig holds runtime settings for the collector daemon.
type CollectorConfig struct {
	BaseConfig
	DatabaseURL     string
	VCenterURL      string
	VCenterUsername string
	VCenterPassword string
	InsecureTLS     bool
}

// QueryAPIConfig holds runtime settings for the query API daemon.
type QueryAPIConfig struct {
	BaseConfig
	DatabaseURL        string
	CORSAllowedOrigins string
}

// LoadCollectorConfig populates CollectorConfig from environment variables.
func LoadCollectorConfig() CollectorConfig {
	return CollectorConfig{
		BaseConfig: BaseConfig{
			Port:     getEnv("PORT", "8081"),
			LogLevel: getEnv("LOG_LEVEL", "info"),
		},
		DatabaseURL:     getEnv("DATABASE_URL", "postgres://collector_rw:collector_rw_pass@localhost:5432/sims?sslmode=disable"),
		VCenterURL:      getEnv("VCENTER_URL", "https://localhost:8989/sdk"),
		VCenterUsername: getEnv("VCENTER_USER", "user"),
		VCenterPassword: getEnv("VCENTER_PASSWORD", "pass"),
		InsecureTLS:     getEnvBool("VCENTER_INSECURE", true),
	}
}

// LoadQueryAPIConfig populates QueryAPIConfig from environment variables.
func LoadQueryAPIConfig() QueryAPIConfig {
	return QueryAPIConfig{
		BaseConfig: BaseConfig{
			Port:     getEnv("PORT", "8080"),
			LogLevel: getEnv("LOG_LEVEL", "info"),
		},
		DatabaseURL:        getEnv("DATABASE_URL", "postgres://query_ro:query_ro_pass@localhost:5432/sims?sslmode=disable"),
		CORSAllowedOrigins: getEnv("CORS_ALLOWED_ORIGINS", ""),
	}
}

func getEnv(key, defaultVal string) string {
	if val, ok := os.LookupEnv(key); ok && strings.TrimSpace(val) != "" {
		return strings.TrimSpace(val)
	}
	return defaultVal
}

func getEnvBool(key string, defaultVal bool) bool {
	if val, ok := os.LookupEnv(key); ok {
		b, err := strconv.ParseBool(strings.TrimSpace(val))
		if err == nil {
			return b
		}
	}
	return defaultVal
}
