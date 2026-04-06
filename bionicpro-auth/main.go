package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	"bionicpro-auth/auth"
	"bionicpro-auth/config"
	"bionicpro-auth/handlers"
	"bionicpro-auth/internal/session"

	_ "github.com/ClickHouse/clickhouse-go" // Импорт драйвера ClickHouse
	"github.com/gin-gonic/gin"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"golang.org/x/oauth2"
)

const (
	maxCHWaitSeconds    = 5
	maxMinIOWaitSeconds = 5
	ttlRedisHours       = 24
	ttlMemStoreHours    = 24
)

var (
	authHandler    *auth.AuthHandler
	sessionStorage session.Store
	tokenStorage   *session.InMemoryTokenStore
	oauth2Config   *oauth2.Config
)

func main() {
	cfg := config.LoadConfig()
	// Настройка Gin
	engine := gin.Default()
	ctx := context.Background()

	// Инициализация подключения к ClickHouse
	clickhouseDB, err := initClickHouse(cfg)
	if err != nil {
		log.Fatal("ClickHouse init failed:", err)
	}
	defer clickhouseDB.Close()

	// Инициализация клиента MinIO
	minioClient, err := initMinIOClient(cfg)
	if err != nil {
		log.Fatalf("Failed to initialize MinIO client: %v", err)
	}

	reportManager := handlers.NewReportManager(ctx, clickhouseDB, minioClient, cfg)

	// Запуск CDC handler
	cdcHandler := handlers.NewCDCHandler(reportManager, cfg)
	go cdcHandler.Start(ctx)

	// Инициализация хранилища сессий
	var sessionErr error
	switch cfg.SessionBackend {
	case "redis":
		log.Printf("Using Redis session backend: %s", cfg.SessionRedisAddr)
		sessionStorage, sessionErr = session.NewRedisStore(
			cfg.SessionRedisAddr,
			cfg.SessionRedisPass,
			0,
			cfg.RefreshTokenEncKey,
			ttlRedisHours*time.Hour,
		)
	case "memory":
		log.Println("Using in-memory session backend with encryption")
		sessionStorage, sessionErr = session.NewMemoryStore(
			ttlMemStoreHours*time.Hour,
			cfg.RefreshTokenEncKey,
		)
	default:
		log.Fatal("Invalid SESSION_BACKEND: ", cfg.SessionBackend)
	}
	if sessionErr != nil {
		log.Fatal("Failed to initialize session storage:", sessionErr)
	}

	oauth2Config := &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURL,
		Endpoint: oauth2.Endpoint{
			AuthURL:  cfg.KeycloakURL + "/realms/" + cfg.Realm + "/protocol/openid-connect/auth",
			TokenURL: cfg.KeycloakURL + "/realms/" + cfg.Realm + "/protocol/openid-connect/token",
		},
		Scopes: []string{"openid", "profile", "email"},
	}

	// Инициализация in-memory хранилища для access_token
	tokenStorage = session.NewInMemoryTokenStore(1 * time.Minute)

	// Инициализация обработчика аутентификации
	authHandler := auth.NewAuthHandler(cfg, sessionStorage, tokenStorage, oauth2Config)

	// Middleware для CORS
	engine.Use(corsMiddleware())

	// Публичные routes
	engine.GET("/login", authHandler.Login)
	engine.GET("/auth/callback", authHandler.Callback)
	engine.GET("/logout", authHandler.Logout)
	engine.GET("/auth/check", authHandler.CheckAuth)
	engine.GET("/health", reportManager.GetHealthCheck)

	// Защищенные routes
	protected := engine.Group("/")
	protected.Use(authHandler.AuthMiddleware())
	{
		protected.POST("/reports", reportManager.RequestReport)
		protected.GET("/reports/:report_id/status", reportManager.CheckReportStatus)
	}

	log.Printf("Server starting on %s", cfg.ServerAddress)
	log.Fatal(engine.Run(cfg.ServerAddress))
}

func initClickHouse(cfg *config.Config) (*sql.DB, error) {
	dsn := fmt.Sprintf("tcp://%s?username=%s&password=%s&database=%s",
		cfg.ClickHouse.Host, cfg.ClickHouse.User, cfg.ClickHouse.Password, cfg.ClickHouse.Database)

	conn, err := sql.Open("clickhouse", dsn)
	if err != nil {
		return nil, err
	}

	// Проверка подключения
	ctx, cancel := context.WithTimeout(context.Background(), maxCHWaitSeconds*time.Second)
	defer cancel()
	if err := conn.PingContext(ctx); err != nil {
		return nil, err
	}
	log.Println("Successfully connected to ClickHouse")
	return conn, nil
}

func initMinIOClient(cfg *config.Config) (*minio.Client, error) {
	minioClient, err := minio.New(cfg.MinIO.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.MinIO.AccessKey, cfg.MinIO.SecretKey, ""),
		Secure: cfg.MinIO.UseSSL,
	})
	if err != nil {
		return nil, err
	}

	// Проверка подключения
	ctx, cancel := context.WithTimeout(context.Background(), maxMinIOWaitSeconds*time.Second)
	defer cancel()

	// Проверяем существование бакета
	exists, err := minioClient.BucketExists(ctx, cfg.MinIO.BucketName)
	if err != nil {
		return nil, fmt.Errorf("failed to check bucket existence: %v", err)
	}

	// Создаем бакет если не существует
	if !exists {
		err = minioClient.MakeBucket(ctx, cfg.MinIO.BucketName, minio.MakeBucketOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to create bucket: %v", err)
		}
		log.Printf("Bucket %s created successfully", cfg.MinIO.BucketName)
	}
	log.Println("Successfully connected to MinIO")
	return minioClient, nil
}

func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")
		// Разрешаем только с localhost:3000
		if origin == "http://localhost:3000" {
			c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
			c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
			c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		}

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}
