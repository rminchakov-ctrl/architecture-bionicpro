package handlers

import (
	"database/sql"
	"net/http"
	"time"

	"bionicpro-auth/models"

	"github.com/gin-gonic/gin"
)

type ReportHandler struct {
	clickhouseDB *sql.DB
}

func NewReportHandler(db *sql.DB) *ReportHandler {
	return &ReportHandler{
		clickhouseDB: db,
	}
}

// GetUserReport возвращает отчеты по конкретному пользователю
func (h *ReportHandler) GetUserReport(c *gin.Context) {
	userID := c.Param("user_id")

	// Проверка авторизации - пользователь может запрашивать только свои отчеты
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

	// Запрос к ClickHouse
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

	c.JSON(http.StatusOK, gin.H{
		"user_id":      userID,
		"reports":      reports,
		"generated_at": time.Now(),
	})
}

// GetProsthesisReport возвращает отчет по конкретному протезу
func (h *ReportHandler) GetProsthesisReport(c *gin.Context) {
	prosthesisID := c.Param("prosthesis_id")

	session, exists := c.Get("session")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Session not found"})
		return
	}

	authSession := session.(*models.Session)

	// Проверка что пользователь имеет доступ к этому протезу
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

	// Детальный отчет по протезу
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

	if err = rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Error iterating reports",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"prosthesis_id": prosthesisID,
		"reports":       reports,
		"generated_at":  time.Now(),
	})
}

// GetReportSummary возвращает сводку по всем протезам пользователя
func (h *ReportHandler) GetReportSummary(c *gin.Context) {
	session, exists := c.Get("session")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Session not found"})
		return
	}

	authSession := session.(*models.Session)

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

	if err = rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Error iterating summary",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"user_id":      authSession.UserID,
		"summary":      summary,
		"generated_at": time.Now(),
	})
}

// GetHealthCheck возвращает статус подключения к ClickHouse
func (h *ReportHandler) GetHealthCheck(c *gin.Context) {
	err := h.clickhouseDB.Ping()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status":  "unhealthy",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":    "healthy",
		"timestamp": time.Now(),
	})
}
