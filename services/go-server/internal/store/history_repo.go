package store

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"time"
)

// VerdictHistory is one append-only Hot Set observation for a camera. It maps
// 1-to-1 with a row in hotset_verdict_history. The store layer deliberately
// carries plain values instead of importing hotset, preserving the modulith
// dependency direction (internal/store is a leaf package).
type VerdictHistory struct {
	CameraID        string
	Direction       string // "a", "b", or "" for frame-wide (ADR-012 D1)
	RegionID        string // runtime geometry polygon ID
	PCUTotal        float64
	SaturationIndex float64
	Severity        string
	Confidence      float64
	Source          string
	ObservedAt      time.Time
}

// VerdictHistoryRepo appends Hot Set verdicts to hotset_verdict_history using
// hand-written SQL (no ORM, per docs/GO_BACKEND_ARCHITECTURE.md).
type VerdictHistoryRepo struct {
	db *sql.DB
}

// NewVerdictHistoryRepo constructs a VerdictHistoryRepo backed by db.
func NewVerdictHistoryRepo(db *sql.DB) *VerdictHistoryRepo {
	return &VerdictHistoryRepo{db: db}
}

// TimestampFixedLayout is a fixed-width RFC3339 layout with 9-digit fractional seconds.
// Fixed width guarantees that SQLite's ASCII lexical sorting (`ORDER BY observed_at`)
// is strictly equivalent to chronological sorting, preventing variable-width RFC3339Nano anomalies.
const TimestampFixedLayout = "2006-01-02T15:04:05.000000000Z"

// Record inserts one observation. Duplicate (camera_id, observed_at) pairs are
// ignored so a restart, a retried fan-out, or a pre-seed cannot inflate the
// time series. Timestamps are bound as explicit RFC3339Nano UTC strings: the
// modernc.org/sqlite driver otherwise persists time.Time through its Go
// String() representation, which includes a monotonic-clock suffix and is not
// portable to the Python feature extractor or to SQLite date functions.
func (r *VerdictHistoryRepo) Record(ctx context.Context, h VerdictHistory) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("verdict history repo db is nil")
	}
	if h.CameraID == "" {
		return fmt.Errorf("verdict history camera id is empty")
	}
	if h.Severity == "" {
		return fmt.Errorf("verdict history severity is empty for camera %s", h.CameraID)
	}
	observedAt := h.ObservedAt
	if observedAt.IsZero() {
		return fmt.Errorf("verdict history observed_at is zero for camera %s", h.CameraID)
	}

	query := fmt.Sprintf(`INSERT INTO %s
		(camera_id, direction, region_id, pcu_total, saturation_index, severity, confidence, source, observed_at, recorded_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(camera_id, direction, observed_at) DO NOTHING`, TableHotsetVerdictHistory)

	_, err := r.db.ExecContext(ctx, query,
		h.CameraID,
		h.Direction,
		h.RegionID,
		h.PCUTotal,
		h.SaturationIndex,
		h.Severity,
		h.Confidence,
		h.Source,
		observedAt.UTC().Format(TimestampFixedLayout),
		time.Now().UTC().Format(TimestampFixedLayout),
	)
	if err != nil {
		return fmt.Errorf("record verdict history %s: %w", h.CameraID, err)
	}
	return nil
}

// GetRecentByCamera retrieves recent frame-wide observations for cameraID within window prior to now,
// ordered descending by observed_at (newest first). If window <= 0, no lower time cutoff is applied.
// If limit <= 0, limit defaults to 2000. If limit > 10000, limit is capped at 10000.
func (r *VerdictHistoryRepo) GetRecentByCamera(ctx context.Context, cameraID string, window time.Duration, limit int) ([]VerdictHistory, error) {
	return r.GetRecentByCameraBefore(ctx, cameraID, time.Now().UTC(), window, limit)
}

// GetRecentByCameraBefore retrieves recent frame-wide observations (direction = ”) for cameraID
// observed at or before before. Preserves 100% backward compatibility for downstream time-series consumers.
func (r *VerdictHistoryRepo) GetRecentByCameraBefore(ctx context.Context, cameraID string, before time.Time, window time.Duration, limit int) ([]VerdictHistory, error) {
	return r.GetRecentByCameraAndDirection(ctx, cameraID, "", before, window, limit)
}

// GetRecentByCameraAndDirection retrieves observations for cameraID and direction observed at or before
// before, within window prior to before. If direction is empty, frame-wide observations are returned.
func (r *VerdictHistoryRepo) GetRecentByCameraAndDirection(ctx context.Context, cameraID string, direction string, before time.Time, window time.Duration, limit int) ([]VerdictHistory, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("verdict history repo db is nil")
	}
	if cameraID == "" {
		return nil, fmt.Errorf("verdict history camera id is empty")
	}
	if before.IsZero() {
		before = time.Now().UTC()
	}
	if limit <= 0 {
		limit = 2000
	} else if limit > 10000 {
		limit = 10000
	}

	var query string
	var args []any

	if window > 0 {
		since := before.Add(-window)
		query = fmt.Sprintf(`SELECT camera_id, direction, region_id, pcu_total, saturation_index, severity, confidence, source, observed_at
			FROM %s
			WHERE camera_id = ? AND direction = ? AND observed_at <= ? AND observed_at >= ? AND saturation_index IS NOT NULL
			ORDER BY observed_at DESC
			LIMIT ?`, TableHotsetVerdictHistory)
		args = []any{
			cameraID,
			direction,
			before.UTC().Format(TimestampFixedLayout),
			since.UTC().Format(TimestampFixedLayout),
			limit,
		}
	} else {
		query = fmt.Sprintf(`SELECT camera_id, direction, region_id, pcu_total, saturation_index, severity, confidence, source, observed_at
			FROM %s
			WHERE camera_id = ? AND direction = ? AND observed_at <= ? AND saturation_index IS NOT NULL
			ORDER BY observed_at DESC
			LIMIT ?`, TableHotsetVerdictHistory)
		args = []any{
			cameraID,
			direction,
			before.UTC().Format(TimestampFixedLayout),
			limit,
		}
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query verdict history for %s (dir %q): %w", cameraID, direction, err)
	}
	defer rows.Close()

	var results []VerdictHistory
	for rows.Next() {
		var (
			camID  string
			dir    string
			regID  string
			pcu    sql.NullFloat64
			si     sql.NullFloat64
			sev    string
			conf   float64
			src    string
			obsStr string
		)
		if err := rows.Scan(&camID, &dir, &regID, &pcu, &si, &sev, &conf, &src, &obsStr); err != nil {
			return nil, fmt.Errorf("scan verdict history row: %w", err)
		}

		// A missing or non-finite saturation index must never be interpreted as 0.0 (CLEAR)
		if !si.Valid || math.IsNaN(si.Float64) || math.IsInf(si.Float64, 0) {
			continue
		}

		obsTime, err := time.Parse(time.RFC3339Nano, obsStr)
		if err != nil {
			obsTime, err = time.Parse(time.RFC3339, obsStr)
			if err != nil {
				return nil, fmt.Errorf("parse observed_at timestamp %q: %w", obsStr, err)
			}
		}

		results = append(results, VerdictHistory{
			CameraID:        camID,
			Direction:       dir,
			RegionID:        regID,
			PCUTotal:        pcu.Float64,
			SaturationIndex: si.Float64,
			Severity:        sev,
			Confidence:      conf,
			Source:          src,
			ObservedAt:      obsTime.UTC(),
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate verdict history rows: %w", err)
	}

	return results, nil
}

// ObservationBounds holds the boundary timestamps for an observation window.
type ObservationBounds struct {
	Earliest *time.Time `json:"earliest,omitempty"`
	Latest   *time.Time `json:"latest,omitempty"`
}

// CoverageReport represents the continuous data accumulation and bin coverage statistics (ADR-012 D2).
type CoverageReport struct {
	CameraID             string            `json:"camera_id,omitempty"`
	Window               string            `json:"window"`
	BinIntervalMinutes   int               `json:"bin_interval_minutes"`
	ExpectedBins         int               `json:"expected_bins"`
	ObservedBins         int               `json:"observed_bins"`
	CoverageRatio        float64           `json:"coverage_ratio"`
	TotalObservations    int               `json:"total_observations"`
	ObservationBounds    ObservationBounds `json:"observation_bounds"`
	LongestGapSeconds    int64             `json:"longest_gap_seconds"`
	HorizonEligibility   map[string]bool   `json:"horizon_eligibility"`
	HistoryCoverageHours float64           `json:"history_coverage_hours"`
	Status               string            `json:"status"`
	CamerasCount         int               `json:"cameras_count,omitempty"`
}

// GetCoverage evaluates continuous time-series history for cameraID (or all cameras if empty)
// over window ending at now (defaults to time.Now().UTC()). Bin interval is fixed to 10 minutes.
func (r *VerdictHistoryRepo) GetCoverage(ctx context.Context, cameraID string, window time.Duration, now time.Time) (*CoverageReport, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("verdict history repo db is nil")
	}
	if window <= 0 {
		window = 72 * time.Hour
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}

	binIntervalMinutes := 10
	expectedBins := int(math.Ceil(window.Minutes() / float64(binIntervalMinutes)))
	if expectedBins < 1 {
		expectedBins = 1
	}
	since := now.Add(-window)

	sinceStr := since.UTC().Format(TimestampFixedLayout)
	nowStr := now.UTC().Format(TimestampFixedLayout)

	if cameraID != "" {
		query := fmt.Sprintf(`SELECT observed_at FROM %s
			WHERE camera_id = ? AND direction = '' AND observed_at >= ? AND observed_at <= ? AND saturation_index IS NOT NULL
			ORDER BY observed_at ASC`, TableHotsetVerdictHistory)

		rows, err := r.db.QueryContext(ctx, query, cameraID, sinceStr, nowStr)
		if err != nil {
			return nil, fmt.Errorf("query coverage timestamps for %s: %w", cameraID, err)
		}
		defer rows.Close()

		var timestamps []time.Time
		for rows.Next() {
			var obsStr string
			if err := rows.Scan(&obsStr); err != nil {
				return nil, fmt.Errorf("scan coverage timestamp: %w", err)
			}
			t, err := time.Parse(time.RFC3339Nano, obsStr)
			if err != nil {
				t, err = time.Parse(time.RFC3339, obsStr)
			}
			if err == nil {
				timestamps = append(timestamps, t.UTC())
			}
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("iterate coverage timestamps: %w", err)
		}

		return calculateCameraCoverage(cameraID, window, binIntervalMinutes, expectedBins, since, now, timestamps), nil
	}

	// Aggregate across all cameras
	query := fmt.Sprintf(`SELECT camera_id, observed_at FROM %s
		WHERE direction = '' AND observed_at >= ? AND observed_at <= ? AND saturation_index IS NOT NULL
		ORDER BY observed_at ASC`, TableHotsetVerdictHistory)

	rows, err := r.db.QueryContext(ctx, query, sinceStr, nowStr)
	if err != nil {
		return nil, fmt.Errorf("query aggregate coverage timestamps: %w", err)
	}
	defer rows.Close()

	byCam := make(map[string][]time.Time)
	for rows.Next() {
		var camID, obsStr string
		if err := rows.Scan(&camID, &obsStr); err != nil {
			return nil, fmt.Errorf("scan aggregate coverage timestamp: %w", err)
		}
		t, err := time.Parse(time.RFC3339Nano, obsStr)
		if err != nil {
			t, err = time.Parse(time.RFC3339, obsStr)
		}
		if err == nil {
			byCam[camID] = append(byCam[camID], t.UTC())
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate aggregate coverage timestamps: %w", err)
	}

	if len(byCam) == 0 {
		return &CoverageReport{
			Window:             window.String(),
			BinIntervalMinutes: binIntervalMinutes,
			ExpectedBins:       expectedBins,
			ObservedBins:       0,
			CoverageRatio:      0.0,
			TotalObservations:  0,
			LongestGapSeconds:  int64(window.Seconds()),
			HorizonEligibility: map[string]bool{
				"10m":              false,
				"20m":              false,
				"30m":              false,
				"serving_eligible": false,
				"refit_eligible":   false,
			},
			HistoryCoverageHours: 0.0,
			Status:               "insufficient_history",
			CamerasCount:         0,
		}, nil
	}

	totalObs := 0
	sumRatio := 0.0
	sumBins := 0
	var globalEarliest, globalLatest *time.Time
	maxGap := int64(0)
	servingCount := 0

	for camID, tsList := range byCam {
		rep := calculateCameraCoverage(camID, window, binIntervalMinutes, expectedBins, since, now, tsList)
		totalObs += rep.TotalObservations
		sumRatio += rep.CoverageRatio
		sumBins += rep.ObservedBins
		if rep.LongestGapSeconds > maxGap {
			maxGap = rep.LongestGapSeconds
		}
		if rep.HorizonEligibility["serving_eligible"] {
			servingCount++
		}
		if rep.ObservationBounds.Earliest != nil {
			if globalEarliest == nil || rep.ObservationBounds.Earliest.Before(*globalEarliest) {
				globalEarliest = rep.ObservationBounds.Earliest
			}
		}
		if rep.ObservationBounds.Latest != nil {
			if globalLatest == nil || rep.ObservationBounds.Latest.After(*globalLatest) {
				globalLatest = rep.ObservationBounds.Latest
			}
		}
	}

	camCount := len(byCam)
	avgRatio := sumRatio / float64(camCount)
	avgBins := int(math.Round(float64(sumBins) / float64(camCount)))

	historyHours := 0.0
	if globalEarliest != nil && globalLatest != nil {
		span := globalLatest.Sub(*globalEarliest).Hours()
		if span > window.Hours() {
			span = window.Hours()
		}
		historyHours = span
	}

	servingEligible := avgRatio >= 0.80 && servingCount >= int(float64(camCount)*0.75)
	refitEligible := window >= 72*time.Hour && avgRatio >= 0.70 && totalObs >= 30*camCount

	status := "ok"
	if !servingEligible {
		status = "insufficient_history"
	}

	return &CoverageReport{
		Window:             window.String(),
		BinIntervalMinutes: binIntervalMinutes,
		ExpectedBins:       expectedBins,
		ObservedBins:       avgBins,
		CoverageRatio:      math.Round(avgRatio*10000) / 10000,
		TotalObservations:  totalObs,
		ObservationBounds:  ObservationBounds{Earliest: globalEarliest, Latest: globalLatest},
		LongestGapSeconds:  maxGap,
		HorizonEligibility: map[string]bool{
			"10m":              servingEligible,
			"20m":              servingEligible && historyHours >= 0.5,
			"30m":              servingEligible && historyHours >= 0.75,
			"serving_eligible": servingEligible,
			"refit_eligible":   refitEligible,
		},
		HistoryCoverageHours: math.Round(historyHours*100) / 100,
		Status:               status,
		CamerasCount:         camCount,
	}, nil
}

// DefaultCoverageBinInterval is the fixed bin width the coverage endpoint
// reports (ADR-012 D2). Ten minutes resolves a forecast horizon while keeping a
// 24h window at 144 comparable bins.
const DefaultCoverageBinInterval = 10 * time.Minute

// BinCoverage summarises how many equal-width bins of a window contain at least
// one observation.
type BinCoverage struct {
	// BinInterval is the width of one bin.
	BinInterval time.Duration
	// ExpectedBins is the number of bins that fit in the window.
	ExpectedBins int
	// ObservedBins is the number of bins that contain at least one timestamp.
	ObservedBins int
}

// Ratio returns ObservedBins/ExpectedBins clamped to [0, 1].
func (c BinCoverage) Ratio() float64 {
	if c.ExpectedBins <= 0 {
		return 0
	}
	r := float64(c.ObservedBins) / float64(c.ExpectedBins)
	if r > 1 {
		return 1
	}
	if r < 0 {
		return 0
	}
	return r
}

// MeasureBinCoverage counts the bins of [since, since+window) that contain at
// least one observation.
//
// It is a pure function so the production coverage endpoint and offline
// verification harnesses (such as the 24h Hot Set coverage simulation for
// Issue #441) share one definition of "continuous coverage". A simulation that
// reimplemented the bin math could claim 95% while the endpoint reported
// something else.
func MeasureBinCoverage(timestamps []time.Time, since time.Time, window, binInterval time.Duration) BinCoverage {
	if binInterval <= 0 {
		binInterval = DefaultCoverageBinInterval
	}
	expectedBins := 1
	if window > 0 {
		expectedBins = int(math.Ceil(window.Minutes() / binInterval.Minutes()))
		if expectedBins < 1 {
			expectedBins = 1
		}
	}

	seen := make(map[int]struct{}, expectedBins)
	for _, t := range timestamps {
		if t.Before(since) {
			continue
		}
		b := int(t.Sub(since) / binInterval)
		if b >= 0 && b < expectedBins {
			seen[b] = struct{}{}
		}
	}

	return BinCoverage{
		BinInterval:  binInterval,
		ExpectedBins: expectedBins,
		ObservedBins: len(seen),
	}
}

func calculateCameraCoverage(cameraID string, window time.Duration, binIntervalMinutes int, expectedBins int, since time.Time, now time.Time, timestamps []time.Time) *CoverageReport {
	if len(timestamps) == 0 {
		return &CoverageReport{
			CameraID:           cameraID,
			Window:             window.String(),
			BinIntervalMinutes: binIntervalMinutes,
			ExpectedBins:       expectedBins,
			ObservedBins:       0,
			CoverageRatio:      0.0,
			TotalObservations:  0,
			LongestGapSeconds:  int64(window.Seconds()),
			HorizonEligibility: map[string]bool{
				"10m":              false,
				"20m":              false,
				"30m":              false,
				"serving_eligible": false,
				"refit_eligible":   false,
			},
			HistoryCoverageHours: 0.0,
			Status:               "insufficient_history",
		}
	}

	binCov := MeasureBinCoverage(timestamps, since, window, time.Duration(binIntervalMinutes)*time.Minute)
	observedBins := binCov.ObservedBins
	coverageRatio := binCov.Ratio()

	longestGap := int64(0)
	for i := 1; i < len(timestamps); i++ {
		gap := int64(timestamps[i].Sub(timestamps[i-1]).Seconds())
		if gap > longestGap {
			longestGap = gap
		}
	}

	earliest := timestamps[0]
	latest := timestamps[len(timestamps)-1]
	spanHours := latest.Sub(earliest).Hours()
	if spanHours > window.Hours() {
		spanHours = window.Hours()
	}

	// Trailing 60-minute coverage evaluation (ADR-012 D2: >= 80% coverage over trailing 60m and >= 15m valid history)
	trailing60mSince := now.Add(-60 * time.Minute)
	trailingBins := make(map[int]bool)
	for _, t := range timestamps {
		if !t.Before(trailing60mSince) && !t.After(now) {
			tb := int(t.Sub(trailing60mSince).Minutes() / float64(binIntervalMinutes))
			if tb >= 0 && tb < 6 {
				trailingBins[tb] = true
			}
		}
	}
	trailingRatio := float64(len(trailingBins)) / 6.0
	validSpanMinutes := latest.Sub(earliest).Minutes()

	servingEligible := trailingRatio >= 0.80 && validSpanMinutes >= 15.0
	refitEligible := window >= 72*time.Hour && coverageRatio >= 0.70 && len(timestamps) >= 30

	status := "ok"
	if !servingEligible {
		status = "insufficient_history"
	}

	return &CoverageReport{
		CameraID:           cameraID,
		Window:             window.String(),
		BinIntervalMinutes: binIntervalMinutes,
		ExpectedBins:       expectedBins,
		ObservedBins:       observedBins,
		CoverageRatio:      math.Round(coverageRatio*10000) / 10000,
		TotalObservations:  len(timestamps),
		ObservationBounds:  ObservationBounds{Earliest: &earliest, Latest: &latest},
		LongestGapSeconds:  longestGap,
		HorizonEligibility: map[string]bool{
			"10m":              servingEligible,
			"20m":              servingEligible && validSpanMinutes >= 30.0,
			"30m":              servingEligible && validSpanMinutes >= 45.0,
			"serving_eligible": servingEligible,
			"refit_eligible":   refitEligible,
		},
		HistoryCoverageHours: math.Round(spanHours*100) / 100,
		Status:               status,
	}
}

// PurgeOlderThan deletes observations older than cutoff in chunks of batchSize to avoid long table locks (ADR-012 D2).
// Returns the total number of deleted rows.
func (r *VerdictHistoryRepo) PurgeOlderThan(ctx context.Context, cutoff time.Time, batchSize int) (int64, error) {
	if r == nil || r.db == nil {
		return 0, fmt.Errorf("verdict history repo db is nil")
	}
	if cutoff.IsZero() {
		return 0, fmt.Errorf("cutoff time is zero")
	}
	if batchSize <= 0 {
		batchSize = 5000
	}

	cutoffStr := cutoff.UTC().Format(TimestampFixedLayout)
	var totalDeleted int64

	for {
		// Use rowid limit batching to avoid long SQLite write transactions
		query := fmt.Sprintf(`DELETE FROM %s WHERE rowid IN (
			SELECT rowid FROM %s WHERE observed_at < ? LIMIT ?
		)`, TableHotsetVerdictHistory, TableHotsetVerdictHistory)

		res, err := r.db.ExecContext(ctx, query, cutoffStr, batchSize)
		if err != nil {
			return totalDeleted, fmt.Errorf("purge batch: %w", err)
		}

		rowsAffected, err := res.RowsAffected()
		if err != nil {
			return totalDeleted, fmt.Errorf("rows affected: %w", err)
		}

		totalDeleted += rowsAffected
		if rowsAffected < int64(batchSize) {
			break
		}
	}

	return totalDeleted, nil
}
