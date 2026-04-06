package models

import (
	"time"
)

// ReportType тип отчета
type ReportType string

const (
	// ReportTypeCustomer ...
	ReportTypeCustomer ReportType = "customer"
	// ReportTypeEMG ...
	ReportTypeEMG ReportType = "emg"
	// ReportTypeSummary ...
	ReportTypeSummary ReportType = "summary"
)

// ReportStatus статус генерации отчета
type ReportStatus string

const (
	// ReportStatusGenerating ...
	ReportStatusGenerating ReportStatus = "generating"
	// ReportStatusReady ...
	ReportStatusReady ReportStatus = "ready"
	// ReportStatusProcessing ...
	ReportStatusProcessing ReportStatus = "processing"
)

// ReportRequest параметры запроса отчета
type ReportRequest struct {
	UserID     string            `json:"user_id"`
	ReportType ReportType        `json:"report_type"`
	Params     map[string]string `json:"params"`
}

// ReportResponse ответ с ссылкой на отчет
type ReportResponse struct {
	Status        ReportStatus `json:"status"`
	ReportURL     string       `json:"report_url,omitempty"`
	ReportID      string       `json:"report_id"`
	QueuePosition int          `json:"queue_position,omitempty"`
	EstimatedWait int          `json:"estimated_wait,omitempty"`
	Error         string       `json:"error,omitempty"`
	GeneratedAt   time.Time    `json:"generated_at"`
}

// ReportData данные отчета
type ReportData struct {
	ReportID  string            `json:"report_id"`
	UserID    string            `json:"user_id"`
	Data      any               `json:"data"`
	Params    map[string]string `json:"params"`
	CreatedAt time.Time         `json:"created_at"`
}
