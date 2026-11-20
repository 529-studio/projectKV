package tide

import (
	"context"
	"math"
	"strconv"
	"strings"
	"time"
)

// Station identifiers in Ho Chi Minh City hydrological network.
const (
	StationPhuAn = "phu_an"
	StationNhaBe = "nha_be"

	// Alert levels in meters above Chart Datum / Hòn Dấu
	AlertLevel1 = 1.40
	AlertLevel2 = 1.50
	AlertLevel3 = 1.60
)

// LowLyingRoad defines an arterial or collector road vulnerable to tidal overtopping.
type LowLyingRoad struct {
	RoadKey           string  `json:"road_key"`
	DisplayName       string  `json:"display_name"`
	WardDistrict      string  `json:"ward_district"`
	ElevationM        float64 `json:"elevation_m"`        // Road surface height in meters
	CriticalTideM     float64 `json:"critical_tide_m"`    // Tide level causing impassability
	AssociatedStation string  `json:"associated_station"` // "phu_an" or "nha_be"
}

// Canonical vulnerable roads in HCMC susceptible to high tides.
var LowLyingRoads = []LowLyingRoad{
	{
		RoadKey:           "tran_xuan_soan",
		DisplayName:       "Đường Trần Xuân Soạn",
		WardDistrict:      "Tân Hưng / Tân Kiểng, Quận 7",
		ElevationM:        1.42,
		CriticalTideM:     1.62,
		AssociatedStation: StationPhuAn,
	},
	{
		RoadKey:           "me_coc",
		DisplayName:       "Đường Mễ Cốc",
		WardDistrict:      "Phường 15, Quận 8",
		ElevationM:        1.40,
		CriticalTideM:     1.60,
		AssociatedStation: StationPhuAn,
	},
	{
		RoadKey:           "quoc_huong",
		DisplayName:       "Đường Quốc Hương",
		WardDistrict:      "Thảo Điền, TP. Thủ Đức",
		ElevationM:        1.48,
		CriticalTideM:     1.68,
		AssociatedStation: StationPhuAn,
	},
	{
		RoadKey:           "thanh_da",
		DisplayName:       "Bán đảo Thanh Đa (Bình Quới)",
		WardDistrict:      "Phường 28, Quận Bình Thạnh",
		ElevationM:        1.45,
		CriticalTideM:     1.65,
		AssociatedStation: StationPhuAn,
	},
	{
		RoadKey:           "huynh_tan_phat",
		DisplayName:       "Đường Huỳnh Tấn Phát",
		WardDistrict:      "Phú Xuân / Nhà Bè, Quận 7 - Nhà Bè",
		ElevationM:        1.50,
		CriticalTideM:     1.70,
		AssociatedStation: StationNhaBe,
	},
	{
		RoadKey:           "le_duc_tho",
		DisplayName:       "Đường Lê Đức Thọ",
		WardDistrict:      "Phường 13 / 15, Quận Gò Vấp",
		ElevationM:        1.52,
		CriticalTideM:     1.70,
		AssociatedStation: StationPhuAn,
	},
	{
		RoadKey:           "nguyen_huu_canh",
		DisplayName:       "Đường Nguyễn Hữu Cảnh (Đoạn trũng)",
		WardDistrict:      "Phường 22, Quận Bình Thạnh",
		ElevationM:        1.56,
		CriticalTideM:     1.72,
		AssociatedStation: StationPhuAn,
	},
}

// AffectedRoad represents a specific road affected at a forecast hour.
type AffectedRoad struct {
	RoadKey          string            `json:"road_key"`
	DisplayName      string            `json:"display_name"`
	WardDistrict     string            `json:"ward_district"`
	EstimatedDepthCm float64           `json:"estimated_depth_cm"`
	HazardLevel      string            `json:"hazard_level"` // "CAUTION" | "IMPASSABLE"
	HazardLabel      map[string]string `json:"hazard_label"`
}

// HourlyForecastItem holds the combined hydrological and meteorological risk for an hour.
type HourlyForecastItem struct {
	HourOffset        int               `json:"hour_offset"`
	Time              time.Time         `json:"time"`
	TimeFormatted     string            `json:"time_formatted"` // e.g. "17:00"
	RainProbability   int               `json:"rain_probability_percent"`
	RainMm            float64           `json:"rain_mm"`
	WeatherDesc       map[string]string `json:"weather_desc"`
	TideLevelPhuAnM   float64           `json:"tide_level_phu_an_m"`
	TideLevelNhaBeM   float64           `json:"tide_level_nha_be_m"`
	TideAlertLevel    string            `json:"tide_alert_level"` // "NORMAL" | "LEVEL_1" | "LEVEL_2" | "LEVEL_3"
	TideAlertLabel    map[string]string `json:"tide_alert_label"`
	AffectedRoads     []AffectedRoad    `json:"affected_roads"`
	CommuterRisk      string            `json:"commuter_risk"` // "CLEAR" | "CAUTION" | "DANGER"
	CommuterRiskLabel map[string]string `json:"commuter_risk_label"`
}

// CommuterAdvisory highlights peak tidal and convective events for commuter decision-making.
type CommuterAdvisory struct {
	PeakTideTimeWindow string            `json:"peak_tide_time_window"` // e.g. "17:30 - 18:30"
	PeakTideLevelM     float64           `json:"peak_tide_level_m"`
	PeakTideStation    string            `json:"peak_tide_station"`
	PeakRainTime       string            `json:"peak_rain_time"`
	PeakRainMm         float64           `json:"peak_rain_mm"`
	Headline           map[string]string `json:"headline"`
	Guidance           map[string]string `json:"guidance"`
}

// HourlyForecastResponse is the JSON payload for /api/forecast/hourly.
type HourlyForecastResponse struct {
	GeneratedAt time.Time            `json:"generated_at"`
	Advisory    CommuterAdvisory     `json:"advisory"`
	Timeline    []HourlyForecastItem `json:"timeline"`
}

// Service performs harmonic tidal predictions and fuses with rain forecasts.
type Service struct {
	weatherReader WeatherReader
	nowFunc       func() time.Time
}

// NewService constructs a Service with optional WeatherReader.
func NewService(wr WeatherReader) *Service {
	return &Service{
		weatherReader: wr,
		nowFunc:       time.Now,
	}
}

// SetNowFunc overrides current time provider (for deterministic unit tests).
func (s *Service) SetNowFunc(f func() time.Time) {
	if f != nil {
		s.nowFunc = f
	}
}

// PredictTide calculates harmonic astronomical tide level in meters at a given UTC time.
// Uses principal constituents M2 (12.42h), S2 (12.0h), K1 (23.93h), O1 (25.82h)
// combined with synodic lunar phase envelope for the East Sea / Saigon River basin.
func PredictTide(station string, t time.Time) float64 {
	// Epoch: 2026-01-01 00:00:00 UTC
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	hours := t.Sub(epoch).Hours()

	// Base mean water level
	z0 := 0.85
	if station == StationNhaBe {
		z0 = 0.88 // Nhà Bè station is closer to Soài Rạp estuary, slightly higher tidal amplitude
	}

	// Angular speeds (degrees per solar hour)
	// M2: 28.9841042 deg/hr (12.4206h period)
	// S2: 30.0000000 deg/hr (12.0000h period)
	// K1: 15.0410686 deg/hr (23.9345h period)
	// O1: 13.9430356 deg/hr (25.8193h period)
	const degToRad = math.Pi / 180.0

	// Synodic lunar month modulation (Spring/Neap tide envelope): 29.530588 days
	// New Moon / Full Moon (days 1 and 15) peak around constructive interference
	lunarDay := math.Mod(hours/24.0, 29.530588)
	// Envelope factor oscillates between 0.70 (Neap / triều kém) and 1.30 (Spring / triều cường)
	springNeapFactor := 1.0 + 0.30*math.Cos(2.0*math.Pi*lunarDay/14.765)

	// Constituent amplitudes (in meters)
	aM2 := 0.52 * springNeapFactor
	aS2 := 0.22 * springNeapFactor
	aK1 := 0.24
	aO1 := 0.16

	// Station phase offsets (in degrees)
	phaseM2 := 145.0
	phaseS2 := 190.0
	phaseK1 := 65.0
	phaseO1 := 80.0

	if station == StationNhaBe {
		// Nhà Bè leads Phú An by ~25-35 minutes due to downstream position
		phaseM2 += 12.0
		phaseS2 += 14.0
		aM2 *= 1.05
	}

	m2 := aM2 * math.Cos((28.9841042*hours-phaseM2)*degToRad)
	s2 := aS2 * math.Cos((30.0000000*hours-phaseS2)*degToRad)
	k1 := aK1 * math.Cos((15.0410686*hours-phaseK1)*degToRad)
	o1 := aO1 * math.Cos((13.9430356*hours-phaseO1)*degToRad)

	height := z0 + m2 + s2 + k1 + o1
	// Round to 2 decimal places
	return math.Round(height*100) / 100
}

// GetAlertLevel maps tidal gauge height to official Vietnamese hydrological warning level.
func GetAlertLevel(height float64) (string, map[string]string) {
	if height >= AlertLevel3 {
		return "LEVEL_3", map[string]string{
			"vi": "Báo động 3 (Ngập sâu diện rộng)",
			"en": "Alert Level 3 (Critical Flood)",
		}
	}
	if height >= AlertLevel2 {
		return "LEVEL_2", map[string]string{
			"vi": "Báo động 2 (Nước bắt đầu tràn bờ)",
			"en": "Alert Level 2 (High Tide Overflow)",
		}
	}
	if height >= AlertLevel1 {
		return "LEVEL_1", map[string]string{
			"vi": "Báo động 1 (Mực nước cao)",
			"en": "Alert Level 1 (Elevated Water)",
		}
	}
	return "NORMAL", map[string]string{
		"vi": "Bình thường",
		"en": "Normal",
	}
}

// GetHourlyForecast generates the next 6-hour forecast combining rain and tidal surges.
func (s *Service) GetHourlyForecast(ctx context.Context) (*HourlyForecastResponse, error) {
	now := s.nowFunc().UTC()

	var rainForecasts []RainForecast
	if s.weatherReader != nil {
		rf, err := s.weatherReader.GetHourlyRainForecast(ctx)
		if err == nil {
			rainForecasts = rf
		}
	}

	hcmcLoc, err := time.LoadLocation("Asia/Ho_Chi_Minh")
	if err != nil {
		hcmcLoc = time.FixedZone("ICT", 7*3600)
	}

	timeline := make([]HourlyForecastItem, 0, 7)
	var maxTideLevel float64
	var maxTideTime time.Time
	var maxRainMm float64
	var maxRainTime string

	for hour := 0; hour <= 6; hour++ {
		t := now.Add(time.Duration(hour) * time.Hour)
		localT := t.In(hcmcLoc)

		tidePhuAn := PredictTide(StationPhuAn, t)
		tideNhaBe := PredictTide(StationNhaBe, t)

		if tidePhuAn > maxTideLevel {
			maxTideLevel = tidePhuAn
			maxTideTime = localT
		}

		alertLevel, alertLabel := GetAlertLevel(tidePhuAn)

		// Rain metrics matching this hour
		rainProb := 0
		rainMm := 0.0
		descVi := "Không mưa"
		descEn := "No rain"

		if len(rainForecasts) > 0 {
			targetTimeStr := localT.Format("2006-01-02T15:00")
			for _, rf := range rainForecasts {
				if strings.HasPrefix(rf.Time, targetTimeStr) {
					rainProb = rf.PrecipitationProbability
					rainMm = rf.RainMm
					if rf.WeatherDescVi != "" {
						descVi = rf.WeatherDescVi
					}
					if rf.WeatherDescEn != "" {
						descEn = rf.WeatherDescEn
					}
					break
				}
			}
		}

		if rainMm > maxRainMm {
			maxRainMm = rainMm
			maxRainTime = localT.Format("15:04")
		}

		// Assess affected low-lying roads
		affected := make([]AffectedRoad, 0)
		for _, road := range LowLyingRoads {
			gauge := tidePhuAn
			if road.AssociatedStation == StationNhaBe {
				gauge = tideNhaBe
			}

			if gauge >= road.ElevationM {
				depthCm := math.Round((gauge - road.ElevationM) * 100)
				hazardLevel := "CAUTION"
				hazardLabel := map[string]string{
					"vi": "Cảnh báo ngập mép đường",
					"en": "Caution - Curb overflow",
				}
				if gauge >= road.CriticalTideM || depthCm >= 20.0 {
					hazardLevel = "IMPASSABLE"
					hazardLabel = map[string]string{
						"vi": "Nguy cơ chết máy / Ngập sâu",
						"en": "Impassable - Deep flood risk",
					}
				}

				affected = append(affected, AffectedRoad{
					RoadKey:          road.RoadKey,
					DisplayName:      road.DisplayName,
					WardDistrict:     road.WardDistrict,
					EstimatedDepthCm: depthCm,
					HazardLevel:      hazardLevel,
					HazardLabel:      hazardLabel,
				})
			}
		}

		// Overall commuter risk rating for this hour
		commuterRisk := "CLEAR"
		riskLabel := map[string]string{
			"vi": "Thông thoáng",
			"en": "Clear",
		}

		if len(affected) > 0 || alertLevel == "LEVEL_2" || (rainProb >= 60 && rainMm >= 5.0) {
			commuterRisk = "CAUTION"
			riskLabel = map[string]string{
				"vi": "Cần chú ý ngập triều / mưa",
				"en": "Caution - Tidal or rain flood",
			}
		}

		if alertLevel == "LEVEL_3" || (rainMm >= 25.0) || hasImpassableRoad(affected) {
			commuterRisk = "DANGER"
			riskLabel = map[string]string{
				"vi": "Nguy hiểm - Tránh di chuyển",
				"en": "Danger - Avoid travel",
			}
		}

		timeline = append(timeline, HourlyForecastItem{
			HourOffset:      hour,
			Time:            t,
			TimeFormatted:   localT.Format("15:04"),
			RainProbability: rainProb,
			RainMm:          rainMm,
			WeatherDesc: map[string]string{
				"vi": descVi,
				"en": descEn,
			},
			TideLevelPhuAnM:   tidePhuAn,
			TideLevelNhaBeM:   tideNhaBe,
			TideAlertLevel:    alertLevel,
			TideAlertLabel:    alertLabel,
			AffectedRoads:     affected,
			CommuterRisk:      commuterRisk,
			CommuterRiskLabel: riskLabel,
		})
	}

	// Commuter peak advisory construction
	peakWindowStart := maxTideTime.Add(-30 * time.Minute).Format("15:04")
	peakWindowEnd := maxTideTime.Add(30 * time.Minute).Format("15:04")
	peakWindow := peakWindowStart + " - " + peakWindowEnd

	headlineVi := "Thủy triều ổn định, mặt đường khô ráo."
	headlineEn := "Stable tides, dry pavements."
	guidanceVi := "Thời tiết và thủy triều thuận lợi cho lộ trình di chuyển của bạn."
	guidanceEn := "Weather and river conditions favorable for commute."

	if maxTideLevel >= AlertLevel2 {
		headlineVi = "Đỉnh triều cường: " + peakWindow + " (+" + formatFloat(maxTideLevel) + "m)"
		headlineEn = "Peak tide: " + peakWindow + " (+" + formatFloat(maxTideLevel) + "m)"
		if maxRainMm > 5.0 {
			headlineVi += " | Mưa dự báo: " + formatFloat(maxRainMm) + "mm lúc " + maxRainTime
			headlineEn += " | Forecast rain: " + formatFloat(maxRainMm) + "mm at " + maxRainTime
		}
		guidanceVi = "Các tuyến ven sông (Trần Xuân Soạn, Mễ Cốc, Quốc Hương) có nguy cơ ngập sâu; khuyến cáo chọn lộ trình thay thế hoặc lùi giờ xuất phát."
		guidanceEn = "Riverside roads (Tran Xuan Soan, Me Coc, Quoc Huong) at high flood risk; consider alternative routes or delaying departure."
	} else if maxRainMm > 5.0 {
		headlineVi = "Vũ lượng mưa dự báo: " + formatFloat(maxRainMm) + "mm lúc " + maxRainTime
		headlineEn = "Forecast rain: " + formatFloat(maxRainMm) + "mm at " + maxRainTime
		guidanceVi = "Mưa dông có thể gây ứ đọng nước cục bộ tại các đoạn trũng thấp."
		guidanceEn = "Convective rain may cause localized ponding in low-lying sections."
	}

	advisory := CommuterAdvisory{
		PeakTideTimeWindow: peakWindow,
		PeakTideLevelM:     maxTideLevel,
		PeakTideStation:    "Phú An (Sông Sài Gòn)",
		PeakRainTime:       maxRainTime,
		PeakRainMm:         maxRainMm,
		Headline: map[string]string{
			"vi": headlineVi,
			"en": headlineEn,
		},
		Guidance: map[string]string{
			"vi": guidanceVi,
			"en": guidanceEn,
		},
	}

	return &HourlyForecastResponse{
		GeneratedAt: now,
		Advisory:    advisory,
		Timeline:    timeline,
	}, nil
}

func hasImpassableRoad(roads []AffectedRoad) bool {
	for _, r := range roads {
		if r.HazardLevel == "IMPASSABLE" {
			return true
		}
	}
	return false
}

func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', 2, 64)
}
