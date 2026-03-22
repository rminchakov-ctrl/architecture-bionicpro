package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"

	"bionicpro-auth/auth"
	"bionicpro-auth/config"
	"bionicpro-auth/handlers"
	"bionicpro-auth/models"

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

	// Инициализация хранилища сессий
	var err error
	sessionStorage, err = auth.NewSessionStorage()
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

	reportHandler := handlers.NewReportHandler(clickhouseDB)

	// Защищенные routes
	protected := engine.Group("/")
	protected.Use(authHandler.AuthMiddleware())
	{
		protected.GET("/api/user", getUserInfo)
		protected.GET("/api/reports", getReports)
		protected.GET("/api/prosthesis-data", getProsthesisData)

		protected.GET("/api/reports/user/:user_id", reportHandler.GetUserReport)
		protected.GET("/api/reports/prosthesis/:prosthesis_id", reportHandler.GetProsthesisReport)
	}

	// Health check
	engine.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "OK"})
	})

	log.Printf("Server starting on %s", cfg.ServerAddress)
	log.Fatal(engine.Run(cfg.ServerAddress))
}

// Обработчик для получения информации о пользователе
func getUserInfo(c *gin.Context) {
	session := c.MustGet("session").(*models.Session)

	c.JSON(200, gin.H{
		"user_id": session.UserID,
		"message": "Authenticated successfully",
	})
}

// Обработчик для получения отчетов (прокси к report-service)
func getReports(c *gin.Context) {
	session := c.MustGet("session").(*models.Session)

	client := &http.Client{}
	req, _ := http.NewRequest("GET", "http://report-service:8082/api/reports", nil)
	req.Header.Add("Authorization", "Bearer "+session.AccessToken)

	resp, err := client.Do(req)
	if err != nil {
		c.JSON(502, gin.H{"error": "Failed to connect to report service"})
		return
	}
	defer resp.Body.Close()

	// Проксируем ответ от сервиса отчетов
	var reportData any
	json.NewDecoder(resp.Body).Decode(&reportData)

	c.JSON(resp.StatusCode, reportData)
}

// Обработчик для данных протезов (прокси к prosthesis-service)
func getProsthesisData(c *gin.Context) {
	session := c.MustGet("session").(*models.Session)
	prosthesisID := c.Query("prosthesis_id")

	client := &http.Client{}
	req, _ := http.NewRequest("GET", "http://prosthesis-service:8083/api/data?prosthesis_id="+prosthesisID, nil)
	req.Header.Add("Authorization", "Bearer "+session.AccessToken)

	resp, err := client.Do(req)
	if err != nil {
		c.JSON(502, gin.H{"error": "Failed to connect to prosthesis service"})
		return
	}
	defer resp.Body.Close()

	var prosthesisData any
	json.NewDecoder(resp.Body).Decode(&prosthesisData)

	c.JSON(resp.StatusCode, prosthesisData)
}
