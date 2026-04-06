package handlers

import (
	"bionicpro-auth/models"
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"
)

type reportWrapper struct {
	ReportID    string            `json:"report_id"`
	ReportType  models.ReportType `json:"report_type"`
	UserID      string            `json:"user_id"`
	GeneratedAt time.Time         `json:"generated_at"`
	Params      any               `json:"params,omitempty"`
	Data        any               `json:"data"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// userSummaryReport - данные из итоговой таблицы user_reports_summary
type userSummaryReport struct {
	UserID         string    `json:"user_id"`
	CustomerName   string    `json:"customer_name"`
	CustomerEmail  string    `json:"customer_email"`
	AgeGroup       string    `json:"age_group"`
	Gender         string    `json:"gender"`
	Country        string    `json:"country"`
	TotalSignals   uint64    `json:"total_signals"`
	AvgAmplitude   float64   `json:"avg_amplitude"`
	MaxAmplitude   float64   `json:"max_amplitude"`
	LastSignalTime time.Time `json:"last_signal_time"`
	Updated        time.Time `json:"updated"`
}

type emgRow struct {
	UserID         string    `json:"user_id"`
	ProsthesisType string    `json:"prosthesis_type"`
	MuscleGroup    string    `json:"muscle_group"`
	AvgAmplitude   float64   `json:"avg_amplitude"`
	MaxAmplitude   float64   `json:"max_amplitude"`
	MinAmplitude   float64   `json:"min_amplitude"`
	SignalCount    uint64    `json:"signal_count"`
	LastSignalTime time.Time `json:"last_signal_time"`
}

type emgTotal struct {
	TotalSignals   uint64  `json:"total_signals"`
	AvgAmplitude   float64 `json:"avg_amplitude"`
	TimePeriodDays int32   `json:"time_period_days"`
}

type emgReport struct {
	Rows  []emgRow `json:"rows"`
	Total emgTotal `json:"total"`
}

type summaryReport struct {
	UserSummary userSummaryReport `json:"user_summary"`
	EmgDetails  emgReport         `json:"emg_details"`
}

// GenerateCustomerReport генерация отчета по данным пользователя из итоговой таблицы
func (rm *ReportManager) GenerateCustomerReport(ctx context.Context, userID string, params map[string]string) (*reportWrapper, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	userSummary, err := rm.getUserSummary(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user summary: %v", err)
	}

	return &reportWrapper{
		ReportID:    rm.generateReportID(userID, models.ReportTypeCustomer, params),
		ReportType:  models.ReportTypeCustomer,
		UserID:      userID,
		GeneratedAt: time.Now(),
		Params:      params,
		Data:        *userSummary,
	}, nil
}

// GenerateEMGReport генерация отчета по EMG-данным
func (rm *ReportManager) GenerateEMGReport(ctx context.Context, userID string, params map[string]string) (*reportWrapper, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	dateFrom := params["date_from"]
	dateTo := params["date_to"]
	if dateFrom == "" {
		dateFrom = time.Now().AddDate(0, 0, -7).Format("2006-01-02")
	}
	if dateTo == "" {
		dateTo = time.Now().Format("2006-01-02")
	}

	report, err := rm.getEMGSummary(ctx, userID, dateFrom, dateTo)
	if err != nil {
		return nil, fmt.Errorf("failed to get EMG data: %w", err)
	}

	return &reportWrapper{
		ReportID:    rm.generateReportID(userID, models.ReportTypeEMG, params),
		ReportType:  models.ReportTypeEMG,
		UserID:      userID,
		GeneratedAt: time.Now(),
		Params:      params,
		Data:        report,
	}, nil
}

// GenerateSummaryReport генерация сводного отчета (использует user_reports_summary)
func (rm *ReportManager) GenerateSummaryReport(ctx context.Context, userID string, params map[string]string) (*reportWrapper, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	// Получаем данные из итоговой таблицы
	userSummary, err := rm.getUserSummary(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user summary: %v", err)
	}

	// Получаем детализацию EMG по датам
	dateFrom := params["date_from"]
	dateTo := params["date_to"]
	if dateFrom == "" {
		dateFrom = time.Now().AddDate(0, 0, -7).Format("2006-01-02")
	}
	if dateTo == "" {
		dateTo = time.Now().Format("2006-01-02")
	}

	emgDetails, err := rm.getEMGSummary(ctx, userID, dateFrom, dateTo)
	if err != nil {
		return nil, fmt.Errorf("failed to get EMG details: %v", err)
	}

	return &reportWrapper{
		ReportID:    rm.generateReportID(userID, models.ReportTypeSummary, params),
		ReportType:  models.ReportTypeSummary,
		UserID:      userID,
		GeneratedAt: time.Now(),
		Params:      params,
		Data: summaryReport{
			UserSummary: *userSummary,
			EmgDetails:  *emgDetails,
		},
	}, nil
}

func (rm *ReportManager) getUserSummary(ctx context.Context, userID string) (*userSummaryReport, error) {
	uid, _ := strconv.ParseUint(userID, 10, 64)

	query := `
		SELECT	user_id,
				any(customer_name),
				any(customer_email),
				any(age_group),
				any(gender),
				any(country),
				countMerge(total_signals) as total_signals,
				avgMerge(avg_amplitude) as avg_amplitude,
				maxMerge(max_amplitude) as max_amplitude,
				maxMerge(last_signal_time) last_signal_time,
				max(_updated)
		FROM	user_reports_summary
		WHERE	user_id = ?
		GROUP	BY user_id
	`

	var summary userSummaryReport
	err := rm.clickhouseDB.QueryRowContext(ctx, query, uid).Scan(
		&summary.UserID,
		&summary.CustomerName,
		&summary.CustomerEmail,
		&summary.AgeGroup,
		&summary.Gender,
		&summary.Country,
		&summary.TotalSignals,
		&summary.AvgAmplitude,
		&summary.MaxAmplitude,
		&summary.LastSignalTime,
		&summary.Updated,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			// Если данных нет в summary, возвращаем пустую структуру
			return &userSummaryReport{
				UserID: userID,
			}, nil
		}
		return nil, fmt.Errorf("clickhouse error: %w", err)
	}

	return &summary, nil
}

func (rm *ReportManager) getEMGSummary(ctx context.Context, userID, dateFrom, dateTo string) (*emgReport, error) {
	query := `
		SELECT	user_id,
				prosthesis_type,
				muscle_group,
				AVG(signal_amplitude) as avg_amplitude,
				MAX(signal_amplitude) as max_amplitude,
				MIN(signal_amplitude) as min_amplitude,
				COUNT() as signal_count,
				MAX(signal_time) as last_signal_time
		FROM	emg_sensor_data
		WHERE	user_id = ? AND
				signal_time >= ? AND
				signal_time <= ?
		GROUP	BY user_id, prosthesis_type, muscle_group
		ORDER	BY avg_amplitude DESC
	`

	rows, err := rm.clickhouseDB.QueryContext(ctx, query, userID, dateFrom, dateTo)
	if err != nil {
		return nil, fmt.Errorf("failed to query EMG data: %v", err)
	}
	defer rows.Close()

	var emgList []emgRow
	for rows.Next() {
		var emg emgRow

		err := rows.Scan(
			&emg.UserID, &emg.ProsthesisType, &emg.MuscleGroup,
			&emg.AvgAmplitude, &emg.MaxAmplitude, &emg.MinAmplitude,
			&emg.SignalCount, &emg.LastSignalTime,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan EMG data: %v", err)
		}

		emgList = append(emgList, emg)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating EMG data: %v", err)
	}

	return &emgReport{
		Rows: emgList,
		Total: emgTotal{
			TotalSignals:   sumSignals(emgList),
			AvgAmplitude:   calculateTotalAvg(emgList),
			TimePeriodDays: daysBetween(dateFrom, dateTo),
		}}, nil
}

func sumSignals(report []emgRow) uint64 {
	var total uint64
	for _, item := range report {
		total += item.SignalCount
	}
	return total
}

func calculateTotalAvg(report []emgRow) float64 {
	var total, count float64
	for _, item := range report {
		total += item.AvgAmplitude
		count++
	}
	if count == 0 {
		return 0
	}
	return total / count
}

func daysBetween(from, to string) int32 {
	fromTime, _ := time.Parse("2006-01-02", from)
	toTime, _ := time.Parse("2006-01-02", to)
	return int32(toTime.Sub(fromTime).Hours() / 24)
}
