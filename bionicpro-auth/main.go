package main

import (
	"context"
	"database/sql"
	"log"

	"bionicpro-auth/auth"
	"bionicpro-auth/config"
	"bionicpro-auth/handlers"

	"github.com/gin-gonic/gin"
	"golang.org/x/oauth2"
)

var (
	authHandler    *auth.AuthHandler
	sessionStorage *auth.SessionStorage
	oauth2Config   *oauth2.Config
)

func main() {
	cfg := config.LoadConfig()
	ctx := context.Background()

	// Инициализация хранилища сессий
	sessionStorage, err := auth.NewSessionStorage()
	if err != nil {
		log.Fatal("Failed to initialize session storage:", err)
	}
	defer sessionStorage.DB.Close()

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

	// Инициализация подключения к ClickHouse
	clickhouseDB, err := sql.Open("clickhouse",
		"tcp://"+cfg.ClickConfig.ClickhouseHost+"?database="+cfg.ClickConfig.ClickhouseDatabase+
			"&username="+cfg.ClickConfig.ClickhouseUser+"&password="+cfg.ClickConfig.ClickhousePassword)
	if err != nil {
		log.Fatal("Failed to connect to ClickHouse:", err)
	}
	defer clickhouseDB.Close()

	// Проверка подключения к ClickHouse
	if err := clickhouseDB.Ping(); err != nil {
		log.Fatal("ClickHouse ping failed:", err)
	}
	log.Println("Successfully connected to ClickHouse")

	// Инициализация обработчика аутентификации
	authHandler := auth.NewAuthHandler(cfg, sessionStorage, oauth2Config)

	// Запуск очистки просроченных сессий
	go sessionStorage.CleanupExpiredSessions()

	// Настройка Gin
	engine := gin.Default()

	// Middleware для CORS
	engine.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "http://localhost:3000")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(200)
			return
		}

		c.Next()
	})

	// Публичные routes
	engine.GET("/login", authHandler.Login)
	engine.GET("/auth/callback", authHandler.Callback)
	engine.GET("/logout", authHandler.Logout)

	reportHandler, err := handlers.NewReportHandler(ctx, clickhouseDB)
	if err != nil {
		log.Fatal(err)
	}

	// Защищенные routes
	protected := engine.Group("/")
	protected.Use(authHandler.AuthMiddleware())
	{
		protected.GET("/api/reports/emg/:user_id", reportHandler.GetEMGReport)
		protected.GET("/api/reports/customer/:user_id", reportHandler.GetCustomerReport)
		protected.GET("/api/reports/summary/:user_id", reportHandler.GetSummaryReport)
	}

	// Health check
	engine.GET("/health", func(c *gin.Context) {
		engine.GET("/health", reportHandler.GetHealthCheck)
	})

	log.Printf("Server starting on %s", cfg.ServerAddress)
	log.Fatal(engine.Run(cfg.ServerAddress))
}
