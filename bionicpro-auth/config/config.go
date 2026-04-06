package config

import (
	"os"
	"strconv"
)

// ClickHouseConfig ...
type ClickHouseConfig struct {
	Host     string
	Database string
	User     string
	Password string
}

// MinIOConfig ...
type MinIOConfig struct {
	Endpoint   string
	AccessKey  string
	SecretKey  string
	BucketName string
	UseSSL     bool
}

// CDNConfig ...
type CDNConfig struct {
	BaseURL string
}

// KafkaConfig ...
type KafkaConfig struct {
	BootstrapServers string
}

// Config ...
type Config struct {
	ServerAddress      string
	KeycloakURL        string
	Realm              string
	ClientID           string
	ClientSecret       string
	RedirectURL        string
	SessionSecret      string
	SessionBackend     string
	SessionRedisAddr   string
	SessionRedisPass   string
	RefreshTokenEncKey string
	AccessTokenTTL     int
	ClickHouse         ClickHouseConfig
	MinIO              MinIOConfig
	CDN                CDNConfig
	Kafka              KafkaConfig
}

// LoadConfig ...
func LoadConfig() *Config {
	return &Config{
		ServerAddress:      getEnv("SERVER_ADDRESS", ":3000"),
		KeycloakURL:        getEnv("KEYCLOAK_URL", "http://localhost:8082"),
		Realm:              getEnv("REALM", "reports-realm"),
		ClientID:           getEnv("CLIENT_ID", "bionicpro-auth-bff"),
		ClientSecret:       getEnv("CLIENT_SECRET", "d6f2a1b5-c3d4-4e5f-8a9b-0c1d2e3f4a5b"),
		RedirectURL:        getEnv("REDIRECT_URL", "http://localhost:3001/auth/callback"),
		SessionSecret:      getEnv("SESSION_SECRET", "d6f2a1b5-c3d4-4e5f-8a9b-0c1d2e3f4a5b"),
		SessionBackend:     getEnv("SESSION_BACKEND", "memory"),
		SessionRedisAddr:   getEnv("SESSION_REDIS_ADDR", "localhost:6379"),
		SessionRedisPass:   getEnv("SESSION_REDIS_PASSWORD", ""),
		RefreshTokenEncKey: getEnv("REFRESH_TOKEN_ENC_KEY", "d6f2a1b5c3d44e5f8a9b0c1d2e3f4a5b"),
		AccessTokenTTL:     getEnvInt("ACCESS_TOKEN_TTL", 120), // 2 минуты по умолчанию
		ClickHouse: ClickHouseConfig{
			Host:     getEnv("CLICKHOUSE_HOST", "localhost:9000"),
			Database: getEnv("CLICKHOUSE_DB", "bionicpro"),
			User:     getEnv("CLICKHOUSE_USER", "default"),
			Password: getEnv("CLICKHOUSE_PASSWORD", ""),
		},
		MinIO: MinIOConfig{
			Endpoint:   getEnv("MINIO_ENDPOINT", "minio:9000"),
			AccessKey:  getEnv("MINIO_USER", "minio_user"),
			SecretKey:  getEnv("MINIO_SECRET", "minio_password"),
			BucketName: getEnv("MINIO_BUCKET", "reports"),
			UseSSL:     false,
		},
		CDN: CDNConfig{
			BaseURL: getEnv("CDN_URL", "http://cdn.example.com"),
		},
		Kafka: KafkaConfig{
			BootstrapServers: getEnv("KAFKA_SERVER", "kafka:9092"),
		},
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultValue
}
