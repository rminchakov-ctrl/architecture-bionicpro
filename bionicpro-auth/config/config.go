package config

import "os"

type ClickConfig struct {
	ClickhouseHost     string
	ClickhouseDatabase string
	ClickhouseUser     string
	ClickhousePassword string
}

type Config struct {
	ServerAddress string
	KeycloakURL   string
	Realm         string
	ClientID      string
	ClientSecret  string
	RedirectURL   string
	SessionSecret string
	ClickConfig   ClickConfig
}

func LoadConfig() *Config {
	return &Config{
		ServerAddress: getEnv("SERVER_ADDRESS", ":8081"),
		KeycloakURL:   getEnv("KEYCLOAK_URL", "http://localhost:8082"),
		Realm:         getEnv("REALM", "reports-realm"),
		ClientID:      getEnv("CLIENT_ID", "bionicpro-auth-bff"),
		ClientSecret:  getEnv("CLIENT_SECRET", "d6f2a1b5-c3d4-4e5f-8a9b-0c1d2e3f4a5b"),
		RedirectURL:   getEnv("REDIRECT_URL", "http://localhost:8081/auth/callback"),
		SessionSecret: getEnv("SESSION_SECRET", "default-session-secret-change-in-production"),
		ClickConfig: ClickConfig{
			ClickhouseHost:     getEnv("CLICKHOUSE_HOST", "localhost:9000"),
			ClickhouseDatabase: getEnv("CLICKHOUSE_DB", "bionicpro"),
			ClickhouseUser:     getEnv("CLICKHOUSE_USER", "default"),
			ClickhousePassword: getEnv("CLICKHOUSE_PASSWORD", ""),
		},
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
