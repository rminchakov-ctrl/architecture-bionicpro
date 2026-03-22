package handlers

import (
	"bionicpro-auth/models"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type ReportHandler struct {
	clickhouseDB *sql.DB
	minioClient  *minio.Client
	bucketName   string
	cdnBaseURL   string
}

func NewReportHandler(ctx context.Context, db *sql.DB) (*ReportHandler, error) {
	// Инициализация MinIO клиента
	minioClient, err := minio.New("minio:9000", &minio.Options{
		Creds:  credentials.NewStaticV4("minio_user", "minio_password", ""),
		Secure: false,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create MinIO client: %v", err)
	}

	handler := &ReportHandler{
		clickhouseDB: db,
		minioClient:  minioClient,
		bucketName:   "reports",
		cdnBaseURL:   "http://nginx/reports",
	}

	// Создание bucket при инициализации
	exists, err := minioClient.BucketExists(ctx, handler.bucketName)
	if err != nil {
		return nil, fmt.Errorf("failed to check bucket existence: %v", err)
	}

	if !exists {
		err = minioClient.MakeBucket(ctx, handler.bucketName, minio.MakeBucketOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to create bucket: %v", err)
		}
	}

	return handler, nil
}

// generateReportKey генерирует ключ для хранения отчета в S3
func (h *ReportHandler) generateReportKey(userID, reportType, dateRange string) string {
	return fmt.Sprintf("user_%s/%s/%s_%s.json",
		userID, reportType, dateRange, time.Now().Format("2006-01-02"))
}

// getCachedReport пытается получить отчет из S3
func (h *ReportHandler) getCachedReport(ctx context.Context, objectKey string) ([]byte, bool) {
	object, err := h.minioClient.GetObject(ctx, h.bucketName, objectKey, minio.GetObjectOptions{})
	if err != nil {
		return nil, false
	}
	defer object.Close()

	data, err := io.ReadAll(object)
	if err != nil {
		return nil, false
	}

	return data, true
}

// cacheReport сохраняет отчет в S3
func (h *ReportHandler) cacheReport(ctx context.Context, objectKey string, reportData []byte) error {
	_, err := h.minioClient.PutObject(ctx, h.bucketName, objectKey,
		bytes.NewReader(reportData), int64(len(reportData)), minio.PutObjectOptions{
			ContentType: "application/json",
		})
	return err
}

// GetUserReport возвращает отчеты по конкретному пользователю с кэшированием
func (h *ReportHandler) GetUserReport(c *gin.Context) {
	userID := c.Param("user_id")
	reportType := "user"
	dateRange := c.DefaultQuery("date_range", "all")

	// Проверка авторизации
	session, exists := c.Get("session")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Session not found"})
		return
	}
	authSession := session.(*models.Session)
	if authSession.UserID != userID {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied to other user reports"})
		return
	}

	ctx := c.Request.Context()
	// Проверяем кэш
	objectKey := h.generateReportKey(userID, reportType, dateRange)
	if cachedData, found := h.getCachedReport(ctx, objectKey); found {
		c.Data(http.StatusOK, "application/json", cachedData)
		return
	}

	// Если нет в кэше, генерируем новый отчет
	query := `
        SELECT 
            user_id, prosthesis_id, report_date,
            user_name, user_email, prosthesis_model,
            total_usage_time, avg_daily_usage, max_force_application,
            avg_battery_health, total_steps_count, emergency_shutdowns,
            usage_intensity, maintenance_needed
        FROM prosthesis_reports_mart 
        WHERE user_id = ?
        ORDER BY report_date DESC
        LIMIT 100
    `

	rows, err := h.clickhouseDB.Query(query, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to fetch user reports",
			"details": err.Error(),
		})
		return
	}
	defer rows.Close()

	var reports []models.ProsthesisReport
	for rows.Next() {
		var report models.ProsthesisReport
		err := rows.Scan(
			&report.UserID,
			&report.ProsthesisID,
			&report.ReportDate,
			&report.UserName,
			&report.UserEmail,
			&report.ProsthesisModel,
			&report.TotalUsageTime,
			&report.AvgDailyUsage,
			&report.MaxForceApplication,
			&report.AvgBatteryHealth,
			&report.TotalStepsCount,
			&report.EmergencyShutdowns,
			&report.UsageIntensity,
			&report.MaintenanceNeeded,
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to scan report",
				"details": err.Error(),
			})
			return
		}
		reports = append(reports, report)
	}

	if err = rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Error iterating reports",
			"details": err.Error(),
		})
		return
	}

	// Формируем ответ
	response := gin.H{
		"user_id":      userID,
		"reports":      reports,
		"generated_at": time.Now(),
		"cache_status": "miss",
		"report_url":   fmt.Sprintf("%s/%s/%s", h.cdnBaseURL, h.bucketName, objectKey),
	}

	// Сериализуем и кэшируем
	responseData, err := json.Marshal(response)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to serialize response",
			"details": err.Error(),
		})
		return
	}

	// Сохраняем в S3 (асинхронно)
	go func() {
		if err := h.cacheReport(context.Background(), objectKey, responseData); err != nil {
			fmt.Printf("Failed to cache report: %v\n", err)
		}
	}()

	c.Data(http.StatusOK, "application/json", responseData)
}

// GetProsthesisReport возвращает отчет по конкретному протезу с кэшированием
func (h *ReportHandler) GetProsthesisReport(c *gin.Context) {
	prosthesisID := c.Param("prosthesis_id")
	reportType := "prosthesis"
	dateRange := c.DefaultQuery("date_range", "all")

	session, exists := c.Get("session")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Session not found"})
		return
	}
	authSession := session.(*models.Session)

	// Проверка доступа
	var userID string
	err := h.clickhouseDB.QueryRow(`
        SELECT user_id FROM prosthesis_reports_mart 
        WHERE prosthesis_id = ? AND user_id = ?
        LIMIT 1
    `, prosthesisID, authSession.UserID).Scan(&userID)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusForbidden, gin.H{
				"error": "Access denied or prosthesis not found",
			})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Database error",
				"details": err.Error(),
			})
		}
		return
	}

	ctx := c.Request.Context()
	// Проверяем кэш
	objectKey := h.generateReportKey(userID, reportType, dateRange)
	if cachedData, found := h.getCachedReport(ctx, objectKey); found {
		c.Data(http.StatusOK, "application/json", cachedData)
		return
	}

	// Генерируем отчет
	query := `
        SELECT 
            user_id, prosthesis_id, report_date,
            user_name, user_email, prosthesis_model,
            total_usage_time, avg_daily_usage, max_force_application,
            avg_battery_health, total_steps_count, emergency_shutdowns,
            usage_intensity, maintenance_needed
        FROM prosthesis_reports_mart 
        WHERE prosthesis_id = ?
        ORDER BY report_date DESC
        LIMIT 30
    `

	rows, err := h.clickhouseDB.Query(query, prosthesisID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to fetch prosthesis reports",
			"details": err.Error(),
		})
		return
	}
	defer rows.Close()

	var reports []models.ProsthesisReport
	for rows.Next() {
		var report models.ProsthesisReport
		err := rows.Scan(
			&report.UserID,
			&report.ProsthesisID,
			&report.ReportDate,
			&report.UserName,
			&report.UserEmail,
			&report.ProsthesisModel,
			&report.TotalUsageTime,
			&report.AvgDailyUsage,
			&report.MaxForceApplication,
			&report.AvgBatteryHealth,
			&report.TotalStepsCount,
			&report.EmergencyShutdowns,
			&report.UsageIntensity,
			&report.MaintenanceNeeded,
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to scan report",
				"details": err.Error(),
			})
			return
		}
		reports = append(reports, report)
	}

	response := gin.H{
		"prosthesis_id": prosthesisID,
		"reports":       reports,
		"generated_at":  time.Now(),
		"cache_status":  "miss",
		"report_url":    fmt.Sprintf("%s/%s/%s", h.cdnBaseURL, h.bucketName, objectKey),
	}

	responseData, err := json.Marshal(response)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to serialize response",
			"details": err.Error(),
		})
		return
	}

	go func() {
		if err := h.cacheReport(context.Background(), objectKey, responseData); err != nil {
			fmt.Printf("Failed to cache prosthesis report: %v\n", err)
		}
	}()

	c.Data(http.StatusOK, "application/json", responseData)
}

// GetReportSummary возвращает сводку с кэшированием
func (h *ReportHandler) GetReportSummary(c *gin.Context) {
	session, exists := c.Get("session")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Session not found"})
		return
	}
	authSession := session.(*models.Session)
	reportType := "summary"
	dateRange := "current"

	ctx := c.Request.Context()
	// Проверяем кэш
	objectKey := h.generateReportKey(authSession.UserID, reportType, dateRange)
	if cachedData, found := h.getCachedReport(ctx, objectKey); found {
		c.Data(http.StatusOK, "application/json", cachedData)
		return
	}

	query := `
        SELECT 
            prosthesis_id,
            MAX(report_date) as last_report_date,
            AVG(total_usage_time) as avg_usage_time,
            MAX(emergency_shutdowns) as total_emergencies,
            MAX(maintenance_needed) as needs_maintenance
        FROM prosthesis_reports_mart 
        WHERE user_id = ?
        GROUP BY prosthesis_id
        ORDER BY last_report_date DESC
    `

	rows, err := h.clickhouseDB.Query(query, authSession.UserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to fetch report summary",
			"details": err.Error(),
		})
		return
	}
	defer rows.Close()

	type SummaryItem struct {
		ProsthesisID     string    `json:"prosthesis_id"`
		LastReportDate   time.Time `json:"last_report_date"`
		AvgUsageTime     float64   `json:"avg_usage_time"`
		TotalEmergencies int       `json:"total_emergencies"`
		NeedsMaintenance bool      `json:"needs_maintenance"`
	}

	var summary []SummaryItem
	for rows.Next() {
		var item SummaryItem
		err := rows.Scan(
			&item.ProsthesisID,
			&item.LastReportDate,
			&item.AvgUsageTime,
			&item.TotalEmergencies,
			&item.NeedsMaintenance,
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to scan summary item",
				"details": err.Error(),
			})
			return
		}
		summary = append(summary, item)
	}

	response := gin.H{
		"user_id":      authSession.UserID,
		"summary":      summary,
		"generated_at": time.Now(),
		"cache_status": "miss",
		"report_url":   fmt.Sprintf("%s/%s/%s", h.cdnBaseURL, h.bucketName, objectKey),
	}

	responseData, err := json.Marshal(response)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to serialize response",
			"details": err.Error(),
		})
		return
	}

	go func() {
		if err := h.cacheReport(context.Background(), objectKey, responseData); err != nil {
			fmt.Printf("Failed to cache summary report: %v\n", err)
		}
	}()

	c.Data(http.StatusOK, "application/json", responseData)
}

// GetHealthCheck проверяет соединения со всеми сервисами
func (h *ReportHandler) GetHealthCheck(c *gin.Context) {
	// Проверяем ClickHouse
	if err := h.clickhouseDB.Ping(); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status":  "unhealthy",
			"details": fmt.Sprintf("ClickHouse: %v", err.Error()),
		})
		return
	}
	// Проверяем MinIO
	ctx := c.Request.Context()
	_, err := h.minioClient.ListBuckets(ctx)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status":  "unhealthy",
			"details": fmt.Sprintf("MinIO: %v", err.Error()),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":    "healthy",
		"timestamp": time.Now(),
	})
}

// InvalidateCache ручка для принудительной инвалидации кэша
func (h *ReportHandler) InvalidateCache(c *gin.Context) {
	userID := c.Param("user_id")
	reportType := c.Query("report_type")
	ctx := c.Request.Context()

	// Удаляем все отчеты пользователя или конкретного типа
	objectCh := h.minioClient.ListObjects(ctx, h.bucketName, minio.ListObjectsOptions{
		Prefix:    fmt.Sprintf("user_%s/", userID),
		Recursive: true,
	})

	for object := range objectCh {
		if object.Err != nil {
			continue
		}
		if reportType == "" || strings.Contains(object.Key, reportType) {
			h.minioClient.RemoveObject(ctx, h.bucketName, object.Key, minio.RemoveObjectOptions{})
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"status":      "cache_invalidated",
		"user_id":     userID,
		"report_type": reportType,
	})
}
