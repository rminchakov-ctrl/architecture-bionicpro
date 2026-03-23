package models

import "time"

// EMGData представляет запись данных EMG-датчика
type EMGData struct {
	UserID          uint32    `json:"user_id"`
	ProsthesisType  string    `json:"prosthesis_type"`
	MuscleGroup     string    `json:"muscle_group"`
	SignalFrequency uint32    `json:"signal_frequency"`
	SignalDuration  uint32    `json:"signal_duration"`
	SignalAmplitude float64   `json:"signal_amplitude"` // Исправляем на float64 для JSON
	SignalTime      time.Time `json:"signal_time"`
}

// CustomerData представляет данные клиента из витрины
type CustomerData struct {
	CustomerID       uint64    `json:"customer_id"`
	CustomerName     string    `json:"customer_name"`
	CustomerEmail    string    `json:"customer_email"`
	AgeGroup         string    `json:"age_group"`
	Gender           string    `json:"gender"`
	Country          string    `json:"country"`
	DataCompleteness float32   `json:"data_completeness"`
	CreatedDate      time.Time `json:"created_date"`
}
