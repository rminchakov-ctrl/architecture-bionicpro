package handlers

import (
	"bionicpro-auth/models"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
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

// NewReportHandler ...
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

// GetEMGReport возвращает отчет по данным EMG-сенсоров
func (h *ReportHandler) GetEMGReport(c *gin.Context) {
	userID := c.Param("user_id")
	reportType := "emg"
	dateRange := c.DefaultQuery("date_range", "7days") // по умолчанию за 7 дней

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
            user_id, prosthesis_type, muscle_group,
            signal_frequency, signal_duration, signal_amplitude,
            signal_time
        FROM emg_sensor_data 
        WHERE user_id = ?
        ORDER BY signal_time DESC
        LIMIT 1000
    `

	rows, err := h.clickhouseDB.QueryContext(ctx, query, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to fetch EMG data",
			"details": err.Error(),
		})
		return
	}
	defer rows.Close()

	var emgData []models.EMGData
	for rows.Next() {
		var data models.EMGData
		err := rows.Scan(
			&data.UserID,
			&data.ProsthesisType,
			&data.MuscleGroup,
			&data.SignalFrequency,
			&data.SignalDuration,
			&data.SignalAmplitude,
			&data.SignalTime,
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to scan EMG data",
				"details": err.Error(),
			})
			return
		}
		emgData = append(emgData, data)
	}

	if err = rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Error iterating EMG data",
			"details": err.Error(),
		})
		return
	}

	// Формируем ответ
	response := gin.H{
		"user_id":      userID,
		"report_type":  reportType,
		"data":         emgData,
		"count":        len(emgData),
		"generated_at": time.Now(),
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
			log.Printf("Failed to cache EMG report: %v\n", err)
		}
	}()

	c.Data(http.StatusOK, "application/json", responseData)
}

// GetCustomerReport возвращает отчет по данным клиентов
func (h *ReportHandler) GetCustomerReport(c *gin.Context) {
	userID := c.Param("user_id")
	reportType := "customer"
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

	// Запрос к витрине customer_reporting_mart
	query := `
        SELECT 
            customer_id, customer_name, customer_email,
            age_group, gender, country,
            data_completeness, created_date
        FROM customer_reporting_mart 
        WHERE customer_id = ?
        LIMIT 1
    `

	var customerData models.CustomerData
	err := h.clickhouseDB.QueryRowContext(ctx, query, userID).Scan(
		&customerData.CustomerID,
		&customerData.CustomerName,
		&customerData.CustomerEmail,
		&customerData.AgeGroup,
		&customerData.Gender,
		&customerData.Country,
		&customerData.DataCompleteness,
		&customerData.CreatedDate,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{
				"error": "Customer data not found",
			})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to fetch customer data",
				"details": err.Error(),
			})
		}
		return
	}

	// Формируем ответ
	response := gin.H{
		"user_id":      userID,
		"report_type":  reportType,
		"customer":     customerData,
		"generated_at": time.Now(),
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
			log.Printf("Failed to cache customer report: %v\n", err)
		}
	}()

	c.Data(http.StatusOK, "application/json", responseData)
}

// GetSummaryReport возвращает сводный отчет
func (h *ReportHandler) GetSummaryReport(c *gin.Context) {
	userID := c.Param("user_id")
	reportType := "summary"
	dateRange := c.DefaultQuery("date_range", "current")

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

	// Сводные данные по EMG
	emgSummaryQuery := `
        SELECT 
            COUNT() as total_records,
            AVG(signal_amplitude) as avg_amplitude,
            MAX(signal_amplitude) as max_amplitude,
            MIN(signal_amplitude) as min_amplitude,
            MAX(signal_time) as last_record
        FROM emg_sensor_data 
        WHERE user_id = ?
    `

	var emgSummary struct {
		TotalRecords uint64    `json:"total_records"`
		AvgAmplitude float64   `json:"avg_amplitude"`
		MaxAmplitude float64   `json:"max_amplitude"`
		MinAmplitude float64   `json:"min_amplitude"`
		LastRecord   time.Time `json:"last_record"`
	}

	err := h.clickhouseDB.QueryRowContext(ctx, emgSummaryQuery, userID).Scan(
		&emgSummary.TotalRecords,
		&emgSummary.AvgAmplitude,
		&emgSummary.MaxAmplitude,
		&emgSummary.MinAmplitude,
		&emgSummary.LastRecord,
	)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to fetch EMG summary",
			"details": err.Error(),
		})
		return
	}

	// Данные клиента
	customerQuery := `
        SELECT 
            customer_name, customer_email, age_group,
            gender, country, data_completeness
        FROM customer_reporting_mart 
        WHERE customer_id = ?
        LIMIT 1
    `

	var customer struct {
		Name             string  `json:"name"`
		Email            string  `json:"email"`
		AgeGroup         string  `json:"age_group"`
		Gender           string  `json:"gender"`
		Country          string  `json:"country"`
		DataCompleteness float32 `json:"data_completeness"`
	}

	err = h.clickhouseDB.QueryRowContext(ctx, customerQuery, userID).Scan(
		&customer.Name,
		&customer.Email,
		&customer.AgeGroup,
		&customer.Gender,
		&customer.Country,
		&customer.DataCompleteness,
	)

	if err != nil && err != sql.ErrNoRows {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to fetch customer data",
			"details": err.Error(),
		})
		return
	}

	// Формируем ответ
	response := gin.H{
		"user_id":      userID,
		"report_type":  reportType,
		"emg_summary":  emgSummary,
		"customer":     customer,
		"generated_at": time.Now(),
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
			log.Printf("Failed to cache summary report: %v\n", err)
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
