package handlers

import (
	"bionicpro-auth/models"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/minio/minio-go/v7"
)

func (rm *ReportManager) startWorkers(ctx context.Context, numWorkers int) {
	for i := range numWorkers {
		go rm.worker(ctx, i)
	}
}

func (rm *ReportManager) worker(ctx context.Context, workerID int) {
	log.Printf("Report worker %d started", workerID)

	for {
		select {
		case <-ctx.Done():
			log.Printf("Worker %d stopping", workerID)
			return
		case rm.workerPool <- struct{}{}:
			reportReq := rm.getNextFromQueue()
			if reportReq == nil {
				<-rm.workerPool
				time.Sleep(5 * time.Second)
				continue
			}

			rm.processReport(reportReq)
			<-rm.workerPool
		}
	}
}

func (rm *ReportManager) processReport(req *models.ReportRequest) {
	reportID := rm.generateReportID(req.UserID, req.ReportType, req.Params)

	rm.setActive(reportID, true)
	defer rm.setActive(reportID, false)

	log.Printf("Starting generation for report: %s", reportID)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	var reportData *reportWrapper
	var err error

	switch req.ReportType {
	case models.ReportTypeCustomer:
		reportData, err = rm.GenerateCustomerReport(ctx, req.UserID, req.Params)
	case models.ReportTypeEMG:
		reportData, err = rm.GenerateEMGReport(ctx, req.UserID, req.Params)
	case models.ReportTypeSummary:
		reportData, err = rm.GenerateSummaryReport(ctx, req.UserID, req.Params)
	default:
		err = fmt.Errorf("unknown report type: %s", req.ReportType)
	}

	if err != nil {
		rm.logError(reportID, fmt.Errorf("generation failed: %w", err))
		return
	}

	if err := rm.saveReportToS3(ctx, reportID, reportData); err != nil {
		rm.logError(reportID, fmt.Errorf("S3 upload failed: %w", err))
		return
	}

	log.Printf("Report %s successfully generated and saved to S3", reportID)
}

func (rm *ReportManager) getNextFromQueue() *models.ReportRequest {
	rm.queueMutex.Lock()
	defer rm.queueMutex.Unlock()

	if len(rm.reportQueue) == 0 {
		return nil
	}

	req := rm.reportQueue[0]
	rm.reportQueue = rm.reportQueue[1:]
	return req
}

func (rm *ReportManager) setActive(reportID string, active bool) {
	rm.activeReportsMutex.Lock()
	defer rm.activeReportsMutex.Unlock()

	if active {
		rm.activeReports[reportID] = make(chan struct{})
	} else {
		if ch, exists := rm.activeReports[reportID]; exists {
			close(ch)
		}
		delete(rm.activeReports, reportID)
	}
}

func (rm *ReportManager) saveReportToS3(ctx context.Context, reportID string, data *reportWrapper) error {
	if data == nil {
		return fmt.Errorf("empty report data")
	}

	jsonData, err := json.Marshal(*data)
	if err != nil {
		return fmt.Errorf("marshal error: %v", err)
	}

	key := rm.generateReportKey(reportID)

	_, err = rm.minioClient.PutObject(ctx, rm.bucketName, key,
		bytes.NewReader(jsonData), int64(len(jsonData)), minio.PutObjectOptions{
			ContentType: "application/json",
			UserMetadata: map[string]string{
				"generated_at": time.Now().Format(time.RFC3339),
			},
		})

	return err
}
