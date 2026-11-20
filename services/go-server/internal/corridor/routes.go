// Package corridor provides arterial corridor geometry definitions and route analytics.
package corridor

import "strings"

// CorridorDefinition holds the static geographic geometry and operational attributes of an arterial corridor.
type CorridorDefinition struct {
	Key              string       `json:"key"`
	DisplayName      string       `json:"display_name"`
	Description      string       `json:"description"`
	LengthKm         float64      `json:"length_km"`
	FreeFlowSpeedKmh float64      `json:"free_flow_speed_kmh"`
	Aliases          []string     `json:"aliases,omitempty"`
	Coordinates      [][2]float64 `json:"coordinates"` // [longitude, latitude] in WGS84
}

// ArterialCorridors contains pre-computed canonical arterial corridors in Ho Chi Minh City.
var ArterialCorridors = map[string]CorridorDefinition{
	"vo_van_kiet": {
		Key:              "vo_van_kiet",
		DisplayName:      "Đại lộ Võ Văn Kiệt",
		Description:      "Bình Tân - Quận 1 - Hầm Thủ Thiêm",
		LengthKm:         13.5,
		FreeFlowSpeedKmh: 40.0,
		Aliases:          []string{"vo-van-kiet"},
		Coordinates: [][2]float64{
			{106.600943, 10.717053},
			{106.610904, 10.72224},
			{106.611468, 10.721839},
			{106.619933, 10.726362},
			{106.620351, 10.726567},
			{106.645521, 10.741362},
			{106.650842, 10.74425},
			{106.659994, 10.749083},
			{106.660348, 10.74894},
			{106.663234, 10.749979},
			{106.668738, 10.750337},
			{106.670079, 10.750369},
			{106.673293, 10.749336},
			{106.677198, 10.75196},
			{106.683673, 10.751944},
			{106.684102, 10.751433},
			{106.686441, 10.753642},
			{106.686913, 10.754321},
			{106.692545, 10.75878},
			{106.697652, 10.763792},
			{106.698285, 10.763902},
			{106.700147, 10.765478},
			{106.703092, 10.768719},
		},
	},
	"mai_chi_tho": {
		Key:              "mai_chi_tho",
		DisplayName:      "Đường Mai Chí Thọ",
		Description:      "Hầm Thủ Thiêm - Nút giao An Phú",
		LengthKm:         6.5,
		FreeFlowSpeedKmh: 45.0,
		Aliases:          []string{"mai-chi-tho"},
		Coordinates: [][2]float64{
			{106.716717, 10.771317},
			{106.722586, 10.773214},
			{106.722919, 10.773383},
			{106.727377, 10.775723},
			{106.727618, 10.775797},
			{106.739709, 10.782089},
			{106.74001, 10.781794},
			{106.749607, 10.78788},
			{106.749945, 10.787685},
			{106.751103, 10.791532},
			{106.751447, 10.792628},
			{106.752707, 10.804985},
			{106.754065, 10.806255},
			{106.756039, 10.809195},
		},
	},
	"dien_bien_phu": {
		Key:              "dien_bien_phu",
		DisplayName:      "Đường Điện Biên Phủ",
		Description:      "Ngã 7 Lý Thái Tổ - Hàng Xanh - Cầu Sài Gòn",
		LengthKm:         6.8,
		FreeFlowSpeedKmh: 35.0,
		Aliases:          []string{"dien-bien-phu"},
		Coordinates: [][2]float64{
			{106.679091, 10.77273},
			{106.683694, 10.776571},
			{106.687294, 10.780012},
			{106.6907, 10.783338},
			{106.690915, 10.783274},
			{106.691553, 10.784344},
			{106.69372, 10.786631},
			{106.697491, 10.790705},
			{106.699739, 10.792085},
			{106.715162, 10.80108},
			{106.718965, 10.798688},
			{106.721127, 10.797818},
		},
	},
	"nguyen_huu_canh": {
		Key:              "nguyen_huu_canh",
		DisplayName:      "Đường Nguyễn Hữu Cảnh",
		Description:      "Tôn Đức Thắng - Hầm chui - Cầu Sài Gòn",
		LengthKm:         3.2,
		FreeFlowSpeedKmh: 35.0,
		Aliases:          []string{"nguyen-huu-canh"},
		Coordinates: [][2]float64{
			{106.706235, 10.782632},
			{106.707914, 10.784623},
			{106.711800, 10.787600},
			{106.715097, 10.789203},
			{106.716428, 10.789145},
			{106.716954, 10.788797},
			{106.718182, 10.791126},
			{106.719800, 10.792500},
			{106.721400, 10.794200},
			{106.722800, 10.796500},
			{106.723707, 10.799131},
		},
	},
	"cong_hoa": {
		Key:              "cong_hoa",
		DisplayName:      "Đường Cộng Hòa",
		Description:      "Lăng Cha Cả - Mũi Tàu Trường Chinh",
		LengthKm:         4.5,
		FreeFlowSpeedKmh: 30.0,
		Aliases:          []string{"cong-hoa"},
		Coordinates: [][2]float64{
			{106.634953, 10.807066},
			{106.637651, 10.804136},
			{106.641573, 10.802398},
			{106.645108, 10.801865},
			{106.647366, 10.801312},
			{106.648386, 10.801828},
			{106.649898, 10.801713},
			{106.651111, 10.801607},
			{106.657757, 10.800105},
			{106.657854, 10.801038},
			{106.660756, 10.800948},
			{106.660783, 10.800674},
		},
	},
	"pham_van_dong": {
		Key:              "pham_van_dong",
		DisplayName:      "Đường Phạm Văn Đồng",
		Description:      "Sân bay TSN - Cầu Bình Lợi - Linh Đông",
		LengthKm:         12.0,
		FreeFlowSpeedKmh: 45.0,
		Aliases:          []string{"pham-van-dong"},
		Coordinates: [][2]float64{
			{106.689193, 10.819275},
			{106.689869, 10.820055},
			{106.693715, 10.82123},
			{106.694809, 10.820766},
			{106.702968, 10.823047},
			{106.703446, 10.822831},
			{106.714014, 10.826272},
			{106.714357, 10.825471},
			{106.722366, 10.829138},
			{106.722747, 10.828928},
			{106.732848, 10.837405},
			{106.743545, 10.840819},
			{106.743824, 10.841193},
			{106.751463, 10.853395},
			{106.759043, 10.865065},
			{106.761661, 10.866498},
			{106.762283, 10.866392},
		},
	},
	"nam_ky_khoi_nghia": {
		Key:              "nam_ky_khoi_nghia",
		DisplayName:      "Nam Kỳ Khởi Nghĩa - Nguyễn Văn Trỗi",
		Description:      "Quận 1 - Sân bay Tân Sơn Nhất",
		LengthKm:         4.8,
		FreeFlowSpeedKmh: 30.0,
		Aliases:          []string{"nam-ky-khoi-nghia", "nguyen-van-troi", "nam_ky_khoi_nghia_nguyen_van_troi"},
		Coordinates: [][2]float64{
			{106.684788, 10.788602},
			{106.687224, 10.786315},
			{106.688544, 10.784534},
			{106.6907, 10.783338},
			{106.690915, 10.783274},
			{106.692835, 10.781472},
			{106.693908, 10.780539},
			{106.695083, 10.779754},
			{106.695174, 10.77947},
			{106.697996, 10.776787},
			{106.699042, 10.775615},
			{106.699482, 10.774706},
			{106.70004, 10.7731},
			{106.701128, 10.770969},
		},
	},
	"cach_mang_thang_tam": {
		Key:              "cach_mang_thang_tam",
		DisplayName:      "Đường Cách Mạng Tháng 8",
		Description:      "Ngã 6 Phù Đổng - Ngã 4 Bảy Hiền",
		LengthKm:         5.5,
		FreeFlowSpeedKmh: 25.0,
		Aliases:          []string{"cach-mang-thang-tam", "cmtt"},
		Coordinates: [][2]float64{
			{106.660182, 10.789292},
			{106.660681, 10.789045},
			{106.664624, 10.78691},
			{106.66649, 10.785862},
			{106.670326, 10.78388},
			{106.672096, 10.782879},
			{106.677144, 10.780223},
			{106.678587, 10.779454},
			{106.683694, 10.776571},
			{106.686639, 10.774969},
			{106.688018, 10.774342},
			{106.689579, 10.773652},
			{106.690051, 10.773035},
			{106.691065, 10.772645},
		},
	},
	"nguyen_van_linh": {
		Key:              "nguyen_van_linh",
		DisplayName:      "Đại lộ Nguyễn Văn Linh",
		Description:      "Quốc lộ 1 - Phú Mỹ Hưng - Huỳnh Tấn Phát",
		LengthKm:         17.8,
		FreeFlowSpeedKmh: 45.0,
		Aliases:          []string{"nguyen-van-linh"},
		Coordinates: [][2]float64{
			{106.595364, 10.69015},
			{106.61047, 10.693987},
			{106.633537, 10.702822},
			{106.634653, 10.7024},
			{106.655665, 10.719937},
			{106.660874, 10.723273},
			{106.677117, 10.728449},
			{106.67789, 10.727574},
			{106.700313, 10.728986},
			{106.720451, 10.731443},
			{106.721234, 10.737936},
			{106.721824, 10.737999},
			{106.725065, 10.752103},
			{106.728331, 10.752545},
			{106.728594, 10.752883},
		},
	},
	"quoc_lo_1": {
		Key:              "quoc_lo_1",
		DisplayName:      "Quốc lộ 1",
		Description:      "Bến xe Miền Tây - An Sương - Suối Tiên",
		LengthKm:         32.0,
		FreeFlowSpeedKmh: 50.0,
		Aliases:          []string{"quoc-lo-1", "ql1"},
		Coordinates: [][2]float64{
			{106.555839, 10.656222}, {106.569808, 10.664788}, {106.570795, 10.665627}, {106.580263, 10.673703},
			{106.580477, 10.674193}, {106.592531, 10.689412}, {106.595364, 10.69015}, {106.596699, 10.696554},
			{106.596463, 10.696634}, {106.597981, 10.705953}, {106.599076, 10.711403}, {106.600943, 10.717053},
			{106.600669, 10.722398}, {106.601694, 10.723505}, {106.601028, 10.724}, {106.601785, 10.728617},
			{106.602809, 10.73421}, {106.598872, 10.741457}, {106.595476, 10.747085}, {106.594237, 10.748566},
			{106.593974, 10.749236}, {106.593508, 10.749963}, {106.592435, 10.756329}, {106.591759, 10.756524},
			{106.591324, 10.756466}, {106.590536, 10.764224}, {106.590557, 10.7661}, {106.590756, 10.767201},
			{106.591437, 10.769963}, {106.591292, 10.770237}, {106.595111, 10.787785}, {106.595449, 10.789429},
			{106.595412, 10.790014}, {106.596544, 10.794767}, {106.596807, 10.795858}, {106.598539, 10.804136},
			{106.60091, 10.816129}, {106.601307, 10.816235}, {106.603265, 10.824897}, {106.606779, 10.834444},
			{106.607273, 10.834339}, {106.61842, 10.845477}, {106.62475, 10.849112}, {106.626456, 10.852231},
			{106.632432, 10.852916}, {106.633172, 10.8532}, {106.639572, 10.856667}, {106.645693, 10.860039},
			{106.649796, 10.86183}, {106.649732, 10.862088}, {106.653299, 10.861983}, {106.654415, 10.861925},
			{106.663309, 10.86164}, {106.663513, 10.86163}, {106.671018, 10.861767}, {106.678491, 10.86163},
			{106.679971, 10.861524}, {106.682246, 10.861345}, {106.686398, 10.861082}, {106.691998, 10.860734},
			{106.698827, 10.860102}, {106.705431, 10.859106}, {106.711578, 10.858938}, {106.71514, 10.860703},
			{106.7484, 10.874779}, {106.751575, 10.875622}, {106.758962, 10.875222}, {106.76504, 10.87333},
			{106.765501, 10.873557}, {106.768742, 10.872082}, {106.775951, 10.86968}, {106.787941, 10.86723},
			{106.788429, 10.867193}, {106.793847, 10.865339},
		},
	},
}

// PassabilityVerdict represents the high-level motorcycle passability verdict for a corridor.
type PassabilityVerdict string

const (
	PassabilityPassable   PassabilityVerdict = "PASSABLE"   // THÔNG THOÁNG
	PassabilityCaution    PassabilityVerdict = "CAUTION"    // CẢNH BÁO XE GẦM THẤP / ÙN TẮC
	PassabilityImpassable PassabilityVerdict = "IMPASSABLE" // NGUY CƠ CHẾT MÁY / THỦY KÍCH
	PassabilityUnknown    PassabilityVerdict = "UNKNOWN"    // CHƯA ĐỦ DỮ LIỆU
)

// VehicleType represents the commuter vehicle profile for hazard threshold evaluation (Issue #510).
type VehicleType string

const (
	VehicleScooter   VehicleType = "scooter"
	VehicleMotorbike VehicleType = "motorbike"
	VehicleSedan     VehicleType = "sedan"
	VehicleSUV       VehicleType = "suv"
)

// VehicleClearanceThresholds defines depth boundaries in cm for each vehicle category.
type VehicleClearanceThresholds struct {
	CautionDepthCm    float64
	ImpassableDepthCm float64
}

// VehicleThresholds maps each vehicle category to its empirical physical immersion limits.
var VehicleThresholds = map[VehicleType]VehicleClearanceThresholds{
	VehicleScooter: {
		CautionDepthCm:    10.0,
		ImpassableDepthCm: 20.0,
	},
	VehicleMotorbike: {
		CautionDepthCm:    15.0,
		ImpassableDepthCm: 28.0,
	},
	VehicleSedan: {
		CautionDepthCm:    15.0,
		ImpassableDepthCm: 25.0,
	},
	VehicleSUV: {
		CautionDepthCm:    25.0,
		ImpassableDepthCm: 40.0,
	},
}

// NormalizeVehicleType parses and normalizes input vehicle string, defaulting safely to VehicleScooter.
func NormalizeVehicleType(v string) VehicleType {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "motorbike", "motorcycle", "xe_so", "xe_con":
		return VehicleMotorbike
	case "sedan", "car", "o_to":
		return VehicleSedan
	case "suv", "truck", "ban_tai":
		return VehicleSUV
	default:
		return VehicleScooter
	}
}

// CorridorRouteSummary provides aggregated travel duration, cumulative hazard index, and passability verdict.
type CorridorRouteSummary struct {
	RoadKey            string             `json:"road_key"`
	DisplayName        string             `json:"display_name"`
	Description        string             `json:"description"`
	LengthKm           float64            `json:"length_km"`
	FreeFlowSpeedKmh   float64            `json:"free_flow_speed_kmh"`
	FreeFlowSeconds    int                `json:"free_flow_seconds"`
	EstimatedSeconds   int                `json:"estimated_seconds"`
	DelaySeconds       int                `json:"delay_seconds"`
	WorstSeverity      string             `json:"worst_severity"`       // "CLEAR" | "MODERATE" | "HEAVY" | "CONGESTED" | "UNKNOWN"
	WorstFloodSeverity string             `json:"worst_flood_severity"` // "DRY" | "WET" | "FLOODED_SHALLOW" | "FLOODED_DEEP" | "UNKNOWN"
	MaxDepthCm         *float64           `json:"max_depth_cm"`
	MaxHazardLevel     *int               `json:"max_hazard_level"`
	PassabilityVerdict PassabilityVerdict `json:"passability_verdict"`
	StatusColor        string             `json:"status_color"` // "#22c55e" | "#eab308" | "#ef4444" | "#71717a"
	TotalCameras       int                `json:"total_cameras"`
	VerifiedCameras    int                `json:"verified_cameras"`
	CoveragePct        float64            `json:"coverage_pct"`
	ChokepointCount    int                `json:"chokepoint_count"`
	Vehicle            VehicleType        `json:"vehicle,omitempty"`
}

// GeoJSONGeometry models the geometry of a GeoJSON feature.
type GeoJSONGeometry struct {
	Type        string      `json:"type"` // "LineString" | "Point"
	Coordinates interface{} `json:"coordinates"`
}

// GeoJSONFeature models a single GeoJSON feature with properties.
type GeoJSONFeature struct {
	Type       string                 `json:"type"` // "Feature"
	Geometry   GeoJSONGeometry        `json:"geometry"`
	Properties map[string]interface{} `json:"properties"`
}

// GeoJSONFeatureCollection models a standard GeoJSON FeatureCollection.
type GeoJSONFeatureCollection struct {
	Type     string           `json:"type"` // "FeatureCollection"
	Features []GeoJSONFeature `json:"features"`
}

// LookupCorridor finds a registered arterial corridor definition by roadKey or alias.
func LookupCorridor(key string) (*CorridorDefinition, bool) {
	norm := normalizeCorridorKey(key)
	if norm == "" {
		return nil, false
	}
	if def, ok := ArterialCorridors[norm]; ok {
		return &def, true
	}
	for _, def := range ArterialCorridors {
		for _, alias := range def.Aliases {
			if normalizeCorridorKey(alias) == norm {
				return &def, true
			}
		}
	}
	return nil, false
}

func normalizeCorridorKey(k string) string {
	k = strings.ToLower(strings.TrimSpace(k))
	k = strings.ReplaceAll(k, "-", "_")
	return k
}
