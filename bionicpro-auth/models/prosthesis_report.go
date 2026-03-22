package models

import "time"

type ProsthesisReport struct {
	UserID              string    `json:"user_id" db:"user_id"`
	ProsthesisID        string    `json:"prosthesis_id" db:"prosthesis_id"`
	ReportDate          time.Time `json:"report_date" db:"report_date"`
	UserName            string    `json:"user_name" db:"user_name"`
	UserEmail           string    `json:"user_email" db:"user_email"`
	ProsthesisModel     string    `json:"prosthesis_model" db:"prosthesis_model"`
	TotalUsageTime      int       `json:"total_usage_time" db:"total_usage_time"`
	AvgDailyUsage       int       `json:"avg_daily_usage" db:"avg_daily_usage"`
	MaxForceApplication float32   `json:"max_force_application" db:"max_force_application"`
	AvgBatteryHealth    float32   `json:"avg_battery_health" db:"avg_battery_health"`
	TotalStepsCount     int       `json:"total_steps_count" db:"total_steps_count"`
	EmergencyShutdowns  int       `json:"emergency_shutdowns" db:"emergency_shutdowns"`
	UsageIntensity      string    `json:"usage_intensity" db:"usage_intensity"`
	MaintenanceNeeded   bool      `json:"maintenance_needed" db:"maintenance_needed"`
}
