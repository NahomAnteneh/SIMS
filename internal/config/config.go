package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// BaseConfig provides shared runtime configuration.
type BaseConfig struct {
	Port     string
	LogLevel string
}

// CollectorConfig holds runtime settings for the collector daemon.
type CollectorConfig struct {
	BaseConfig
	DatabaseURL            string
	VCenterURL             string
	VCenterUsername        string
	VCenterPassword        string
	InsecureTLS            bool
	SyncInterval           time.Duration
	KeepaliveInterval      time.Duration
	KeepaliveTimeout       time.Duration
	KeepaliveMaxFailures   int
	MaxStreamBlockDuration time.Duration
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
		DatabaseURL:            getEnv("DATABASE_URL", "postgres://collector_rw:collector_rw_pass@localhost:5432/sims?sslmode=disable"),
		VCenterURL:             getEnv("VCENTER_URL", "https://localhost:8989/sdk"),
		VCenterUsername:        getEnv("VCENTER_USER", "user"),
		VCenterPassword:        getEnv("VCENTER_PASSWORD", "pass"),
		InsecureTLS:            getEnvBool("VCENTER_INSECURE", true),
		SyncInterval:           getEnvDuration("SYNC_INTERVAL", 30*time.Second),
		KeepaliveInterval:      getEnvDuration("SIMS_COLLECTOR_KEEPALIVE_INTERVAL", 60*time.Second),
		KeepaliveTimeout:       getEnvDuration("SIMS_COLLECTOR_KEEPALIVE_TIMEOUT", 10*time.Second),
		KeepaliveMaxFailures:   getEnvInt("SIMS_COLLECTOR_KEEPALIVE_MAX_FAILURES", 2),
		MaxStreamBlockDuration: getEnvDuration("SIMS_COLLECTOR_MAX_STREAM_BLOCK_DURATION", 5*time.Minute),
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

func getEnvInt(key string, defaultVal int) int {
	if val, ok := os.LookupEnv(key); ok {
		i, err := strconv.Atoi(strings.TrimSpace(val))
		if err == nil {
			return i
		}
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

func getEnvDuration(key string, defaultVal time.Duration) time.Duration {
	if val, ok := os.LookupEnv(key); ok {
		d, err := time.ParseDuration(strings.TrimSpace(val))
		if err == nil {
			return d
		}
	}
	return defaultVal
}
