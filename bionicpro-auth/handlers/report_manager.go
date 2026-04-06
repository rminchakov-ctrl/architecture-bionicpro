package handlers

import (
	"bionicpro-auth/config"
	"bionicpro-auth/models"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/minio/minio-go/v7"
)

const (
	maxWaitReportSeconds = 30
	maxCHWaitSeconds     = 5
	maxMinIOWaitSeconds  = 5

	numWorkers     = 5
	workerPoolSize = 10
)

// ReportManager менеджер генерации и кеширования отчетов
type ReportManager struct {
	clickhouseDB       *sql.DB
	minioClient        *minio.Client
	bucketName         string
	cdnBaseURL         string
	queueMutex         sync.Mutex
	activeReportsMutex sync.Mutex
	reportQueue        []*models.ReportRequest
	activeReports      map[string]chan struct{}
	workerPool         chan struct{}
}

// NewReportManager создает новый менеджер отчетов
func NewReportManager(ctx context.Context, db *sql.DB, mc *minio.Client, cfg *config.Config) *ReportManager {
	rm := &ReportManager{
		clickhouseDB:  db,
		minioClient:   mc,
		bucketName:    cfg.MinIO.BucketName,
		cdnBaseURL:    cfg.CDN.BaseURL,
		activeReports: make(map[string]chan struct{}),
		workerPool:    make(chan struct{}, numWorkers),
	}

	// Запускаем воркеры сразу при создании
	rm.startWorkers(ctx, numWorkers)

	return rm
}

// RequestReport обработчик запроса отчета
func (rm *ReportManager) RequestReport(c *gin.Context) {
	var req struct {
		ReportType string            `json:"report_type"`
		Params     map[string]string `json:"params"`
	}

	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	// Получаем UserID из сессии - это единственный доверенный источник
	session, exists := c.Get("session")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Session not found"})
		return
	}
	authSession := session.(*models.Session)

	// UserID из сессии (заполнен при логине из Keycloak)
	userID := authSession.UserID
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not identified"})
		return
	}

	reportType := models.ReportType(req.ReportType)
	reportID, status := rm.requestReport(userID, reportType, req.Params)
	if len(reportID) > 0 {
		c.JSON(http.StatusOK, models.ReportResponse{
			Status:      models.ReportStatusReady,
			ReportURL:   rm.GetCDNURL(reportID),
			ReportID:    reportID,
			GeneratedAt: time.Now(),
		})
		return
	}

	reportID = rm.generateReportID(userID, reportType, req.Params)
	c.JSON(http.StatusAccepted, models.ReportResponse{
		Status:   status,
		ReportID: reportID,
	})
}

// CheckReportStatus проверка статуса отчета
func (rm *ReportManager) CheckReportStatus(c *gin.Context) {
	reportID := c.Param("report_id")

	exists, err := rm.checkReportExists(c.Request.Context(), reportID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check report status"})
		return
	}

	if exists {
		c.JSON(http.StatusOK, models.ReportResponse{
			Status:      models.ReportStatusReady,
			ReportURL:   rm.GetCDNURL(reportID),
			ReportID:    reportID,
			GeneratedAt: time.Now(),
		})
		return
	}

	if rm.isReportGenerating(reportID) {
		c.JSON(http.StatusOK, models.ReportResponse{
			Status:        models.ReportStatusGenerating,
			ReportID:      reportID,
			QueuePosition: rm.getQueuePosition(reportID),
			EstimatedWait: rm.getEstimatedWait(reportID),
		})
		return
	}

	c.JSON(http.StatusNotFound, gin.H{"error": "Report not found"})
}

// GetHealthCheck проверка здоровья сервиса
func (rm *ReportManager) GetHealthCheck(c *gin.Context) {
	// Проверяем подключение к ClickHouse
	ctx, cancel := context.WithTimeout(context.Background(), maxCHWaitSeconds*time.Second)
	defer cancel()

	if err := rm.clickhouseDB.PingContext(ctx); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status":  "unhealthy",
			"service": "clickhouse",
			"error":   err.Error(),
		})
		return
	}

	// Проверяем подключение к MinIO
	ctx, cancel = context.WithTimeout(context.Background(), maxMinIOWaitSeconds*time.Second)
	defer cancel()

	_, err := rm.minioClient.ListBuckets(ctx)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status":  "unhealthy",
			"service": "minio",
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":    "healthy",
		"timestamp": time.Now(),
		"services": map[string]string{
			"clickhouse": "connected",
			"minio":      "connected",
			"api":        "running",
		},
	})
}

func (rm *ReportManager) InvalidateUserCache(userID string, reportType string) error {
	ctx := context.Background()

	prefix := fmt.Sprintf("reports/%s/", userID)
	// Если указан конкретный тип отчета, добавляем его в путь
	if reportType != "" {
		prefix += reportType + "/"
	}

	log.Printf("Invalidating cache with prefix: %s", prefix)

	// Остальная логика остается прежней...
	objectsCh := rm.minioClient.ListObjects(ctx, rm.bucketName, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	})

	deletedCount := 0
	for object := range objectsCh {
		if object.Err != nil {
			continue
		}

		err := rm.minioClient.RemoveObject(ctx, rm.bucketName, object.Key, minio.RemoveObjectOptions{})
		if err != nil {
			log.Printf("Failed to delete object %s: %v", object.Key, err)
			continue
		}
		deletedCount++
	}

	log.Printf("Cache invalidated for user %s (type: %s): %d objects deleted",
		userID, reportType, deletedCount)
	return nil
}

func (rm *ReportManager) requestReport(userID string, reportType models.ReportType, params map[string]string) (string, models.ReportStatus) {
	reportID := rm.generateReportID(userID, reportType, params)

	exists, _ := rm.checkReportExists(context.Background(), reportID)
	if exists {
		return reportID, models.ReportStatusReady
	}

	if rm.isReportGenerating(reportID) {
		return "", models.ReportStatusProcessing
	}

	rm.addToQueue(&models.ReportRequest{
		UserID:     userID,
		ReportType: reportType,
		Params:     params,
	})

	return "", models.ReportStatusGenerating
}

// hashParams создает детерминированный хеш из параметров отчета
func (rm *ReportManager) hashParams(params map[string]string) string {
	if len(params) == 0 {
		return "default"
	}

	// Сортируем ключи для детерминированности
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Создаем детерминированное представление
	orderedMap := make(map[string]string)
	for _, k := range keys {
		orderedMap[k] = params[k]
	}

	jsonData, err := json.Marshal(orderedMap)
	if err != nil {
		// Fallback: конкатенация отсортированных значений
		var concat string
		for _, k := range keys {
			concat += k + "=" + params[k] + "&"
		}
		return rm.hashString(concat)
	}

	return rm.hashString(string(jsonData))
}

// hashString вспомогательная функция для хеширования
func (rm *ReportManager) hashString(input string) string {
	hash := sha256.Sum256([]byte(input))
	return hex.EncodeToString(hash[:])[:16]
}

// generateReportID генерирует детерминированный ID отчета (user + type + params hash)
func (rm *ReportManager) generateReportID(userID string, reportType models.ReportType, params map[string]string) string {
	if userID == "" {
		userID = "_"
	}

	paramsHash := rm.hashParams(params)

	// Детерминированный ID без timestamp - только user, type и hash params
	return fmt.Sprintf("%s/%s/%s",
		userID,
		reportType,
		paramsHash)
}

// generateReportKey генерирует ключ для хранения в S3
func (rm *ReportManager) generateReportKey(reportID string) string {
	return fmt.Sprintf("reports/%s.json", reportID)
}

// GetCDNURL возвращает полную ссылку на CDN
func (rm *ReportManager) GetCDNURL(reportID string) string {
	return fmt.Sprintf("%s/%s", rm.cdnBaseURL, rm.generateReportKey(reportID))
}

// checkReportExists проверяет существование отчета в S3
func (rm *ReportManager) checkReportExists(ctx context.Context, reportID string) (bool, error) {
	key := rm.generateReportKey(reportID)

	_, err := rm.minioClient.StatObject(ctx, rm.bucketName, key, minio.StatObjectOptions{})
	if err != nil {
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			return false, nil
		}
		return false, err
	}

	return true, nil
}

// addToQueue добавление отчета в очередь
func (rm *ReportManager) addToQueue(req *models.ReportRequest) int {
	rm.queueMutex.Lock()
	defer rm.queueMutex.Unlock()

	rm.reportQueue = append(rm.reportQueue, req)
	return len(rm.reportQueue) - 1
}

// isReportGenerating проверка, генерируется ли отчет
func (rm *ReportManager) isReportGenerating(reportID string) bool {
	rm.activeReportsMutex.Lock()
	defer rm.activeReportsMutex.Unlock()

	_, exists := rm.activeReports[reportID]
	return exists
}

// getQueuePosition получение позиции в очереди
func (rm *ReportManager) getQueuePosition(reportID string) int {
	rm.queueMutex.Lock()
	defer rm.queueMutex.Unlock()

	for i, req := range rm.reportQueue {
		currentID := rm.generateReportID(req.UserID, req.ReportType, req.Params)
		if currentID == reportID {
			return i
		}
	}
	return -1
}

// getEstimatedWait оценка времени ожидания
func (rm *ReportManager) getEstimatedWait(reportID string) int {
	position := rm.getQueuePosition(reportID)
	if position == -1 {
		return 0
	}
	return (position + 1) * maxWaitReportSeconds
}

// logError логирует ошибки генерации и может отправлять метрики
func (rm *ReportManager) logError(reportID string, err error) {
	log.Printf("[REPORT_ERROR] ID: %s | Error: %v", reportID, err)
	// можно добавить запись в БД об ошибке,
	// чтобы фронтенд мог показать пользователю "Ошибка генерации"
	// Можно отправить метрику в Prometheus/Sentry
	// sentry.CaptureException(err)
}
