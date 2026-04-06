package handlers

import (
	"bionicpro-auth/config"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/segmentio/kafka-go"
)

// CDCMessage структура сообщения от Debezium
type CDCMessage struct {
	Op     string          `json:"op"` // 'c', 'u', 'd'
	Before json.RawMessage `json:"before,omitempty"`
	After  json.RawMessage `json:"after,omitempty"`
	Source struct {
		TSMS int64 `json:"ts_ms"`
	} `json:"source"`
}

// CDCHandler обработчик сообщений CDC
type CDCHandler struct {
	reportManager *ReportManager
	kafkaReader   *kafka.Reader
}

// NewCDCHandler создает новый обработчик CDC
func NewCDCHandler(rm *ReportManager, cfg *config.Config) *CDCHandler {
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:  []string{cfg.Kafka.BootstrapServers},
		Topic:    "crm-db-server.public.customers",
		GroupID:  "cdc-handler",
		MinBytes: 10e3,
		MaxBytes: 10e6,
	})

	return &CDCHandler{
		reportManager: rm,
		kafkaReader:   reader,
	}
}

// Start запускает обработчик CDC
func (h *CDCHandler) Start(ctx context.Context) {
	defer h.kafkaReader.Close()
	log.Println("CDC handler started")

	workerCount := 5
	msgChan := make(chan kafka.Message, 100)

	for range workerCount {
		go func() {
			for msg := range msgChan {
				h.processMessage(msg)
			}
		}()
	}

	for {
		select {
		case <-ctx.Done():
			close(msgChan)
			return
		default:
			msg, err := h.kafkaReader.ReadMessage(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				log.Printf("Kafka read error: %v", err)
				continue
			}
			select {
			case msgChan <- msg:
			case <-time.After(100 * time.Millisecond):
				// Channel full, continue
			}
		}
	}
}

func (h *CDCHandler) processMessage(msg kafka.Message) {
	var cdcMsg CDCMessage

	if err := json.Unmarshal(msg.Value, &cdcMsg); err != nil {
		log.Printf("Failed to unmarshal CDC message: %v", err)
		return
	}

	// Извлекаем тип entity из топика
	topicParts := splitTopic(msg.Topic)
	if len(topicParts) < 3 {
		log.Printf("Invalid topic format: %s", msg.Topic)
		return
	}
	entityType := topicParts[2]

	userID := h.extractUserID(cdcMsg, entityType)
	if userID == "" {
		return
	}

	reportType := h.mapEntityToReportType(entityType)
	log.Printf("Invalidating %s cache for user %s due to %s operation",
		reportType, userID, cdcMsg.Op)

	if err := h.reportManager.InvalidateUserCache(userID, reportType); err != nil {
		log.Printf("Failed to invalidate %s cache for user %s: %v", reportType, userID, err)
	}
}

func splitTopic(topic string) []string {
	parts := make([]string, 0)
	current := ""
	for _, c := range topic {
		if c == '.' {
			if current != "" {
				parts = append(parts, current)
				current = ""
			}
		} else {
			current += string(c)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}

func (h *CDCHandler) mapEntityToReportType(entityType string) string {
	switch entityType {
	case "customers":
		return "customer"
	case "emg_sensor_data":
		return "emg"
	default:
		return ""
	}
}

func (h *CDCHandler) extractUserID(msg CDCMessage, entityType string) string {
	var sourceData json.RawMessage
	if msg.Op == "d" {
		sourceData = msg.Before
	} else {
		sourceData = msg.After
	}

	if len(sourceData) == 0 {
		return ""
	}

	var data map[string]any
	if err := json.Unmarshal(sourceData, &data); err != nil {
		log.Printf("Failed to unmarshal %s data: %v", entityType, err)
		return ""
	}

	return getUserID(data)
}

func getUserID(data map[string]any) string {
	keys := []string{"user_id", "customer_id", "id"}
	for _, k := range keys {
		if val, ok := data[k]; ok && val != nil {
			return fmt.Sprintf("%v", val)
		}
	}
	return ""
}