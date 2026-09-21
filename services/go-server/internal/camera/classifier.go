package camera

import (
	"regexp"
	"strings"
)

// ZoneClassifier defines the contract for classifying a camera into a geographic zone.
// (Single Responsibility Principle & Dependency Inversion Principle)
type ZoneClassifier interface {
	Classify(cam Camera) string
}

// BoundingBox defines geographic coordinate boundaries.
type BoundingBox struct {
	MinLat float64
	MaxLat float64
	MinLon float64
	MaxLon float64
}

// Contains checks whether a given coordinate falls within the bounding box.
func (b BoundingBox) Contains(lat, lon float64) bool {
	return lat >= b.MinLat && lat <= b.MaxLat && lon >= b.MinLon && lon <= b.MaxLon
}

// ZoneRule defines a single matching rule for zone assignment.
// (Open-Closed Principle: New zones or rules can be added without modifying the classifier engine)
type ZoneRule interface {
	Match(cam Camera) bool
	Zone() string
}

// RuleBasedZoneClassifier evaluates cameras against an ordered list of ZoneRules.
type RuleBasedZoneClassifier struct {
	rules       []ZoneRule
	defaultZone string
}

// NewDefaultZoneClassifier constructs a classifier with the standard HCMC zone rules.
func NewDefaultZoneClassifier() *RuleBasedZoneClassifier {
	catLaiBox := BoundingBox{
		MinLat: 10.748,
		MaxLat: 10.785,
		MinLon: 106.745,
		MaxLon: 106.815,
	}

	southSaigonBox := BoundingBox{
		MinLat: 0.001,
		MaxLat: 10.755,
		MinLon: 106.68,
		MaxLon: 106.75,
	}

	return &RuleBasedZoneClassifier{
		rules: []ZoneRule{
			&baRiaVungTauRule{minLon: 107.0},
			&binhDuongRule{
				keywords: []string{
					"mỹ phước", "tân vạn", "dĩ an", "thuận an",
					"dt743", "đại lộ bình dương",
				},
				minLat: 10.92,
				minLon: 106.65,
			},
			&catLaiPortRule{
				box: catLaiBox,
				keywords: []string{
					"cát lái", "mỹ thủy", "lê phụng hiểu",
				},
				roadRegex: regexp.MustCompile(`\bđường [abcdgk]\b`),
			},
			&southSaigonRule{
				box: southSaigonBox,
				keywords: []string{
					"huỳnh tấn phát", "nguyễn văn linh", "nhà bè", "nguyễn hữu thọ",
				},
			},
			&thuDucRule{
				minLon: 106.725,
				minLat: 10.74,
			},
			&binhTanRule{
				maxLon: 106.62,
				keywords: []string{
					"bình chánh", "bình tân", "tân kiên", "an lạc",
					"võ trần chí", "kinh dương vương",
				},
			},
			&q12HocMonCuChiRule{
				minLat: 10.835,
				maxLon: 106.72,
				keywords: []string{
					"hóc môn", "củ chi", "quốc lộ 22", "ngã 4 ga", "an sương",
				},
			},
		},
		defaultZone: ZoneNoiThanh,
	}
}

// Classify evaluates the camera against the registered rules in order.
func (c *RuleBasedZoneClassifier) Classify(cam Camera) string {
	for _, rule := range c.rules {
		if rule.Match(cam) {
			return rule.Zone()
		}
	}
	return c.defaultZone
}

// Helper to extract clean lowercase name from camera.
func cameraSearchText(cam Camera) string {
	name := strings.TrimSpace(cam.DisplayName)
	if name == "" {
		name = strings.TrimSpace(cam.Title)
	}
	return strings.ToLower(name)
}

// 1. Ba Ria - Vung Tau Rule
type baRiaVungTauRule struct {
	minLon float64
}

func (r *baRiaVungTauRule) Match(cam Camera) bool {
	return cam.Longitude > r.minLon
}

func (r *baRiaVungTauRule) Zone() string {
	return ZoneBaRiaVungTau
}

// 2. Binh Duong Rule
type binhDuongRule struct {
	keywords []string
	minLat   float64
	minLon   float64
}

func (r *binhDuongRule) Match(cam Camera) bool {
	text := cameraSearchText(cam)
	for _, kw := range r.keywords {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return cam.Latitude > r.minLat && cam.Longitude > r.minLon
}

func (r *binhDuongRule) Zone() string {
	return ZoneBinhDuong
}

// 3. Cat Lai Port Cluster Rule
type catLaiPortRule struct {
	box       BoundingBox
	keywords  []string
	roadRegex *regexp.Regexp
}

func (r *catLaiPortRule) Match(cam Camera) bool {
	if !r.box.Contains(cam.Latitude, cam.Longitude) {
		return false
	}
	text := cameraSearchText(cam)
	for _, kw := range r.keywords {
		if strings.Contains(text, kw) {
			return true
		}
	}
	if strings.Contains(text, "nguyễn thị định") && !strings.Contains(text, "thân văn nhiếp") {
		return true
	}
	if strings.Contains(text, "đồng văn cống") && (strings.Contains(text, "đường c") ||
		strings.Contains(text, "nguyễn thị định") ||
		strings.Contains(text, "phan văn đáng") ||
		strings.Contains(text, "phía cảng")) {
		return true
	}
	return r.roadRegex != nil && r.roadRegex.MatchString(text)
}

func (r *catLaiPortRule) Zone() string {
	return ZoneCatLai
}

// 4. South Saigon (District 7 - Nha Be) Rule
type southSaigonRule struct {
	box      BoundingBox
	keywords []string
}

func (r *southSaigonRule) Match(cam Camera) bool {
	if r.box.Contains(cam.Latitude, cam.Longitude) {
		return true
	}
	text := cameraSearchText(cam)
	for _, kw := range r.keywords {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}

func (r *southSaigonRule) Zone() string {
	return ZoneQuan7NhaBe
}

// 5. Thu Duc City Rule
type thuDucRule struct {
	minLon float64
	minLat float64
}

func (r *thuDucRule) Match(cam Camera) bool {
	return cam.Longitude >= r.minLon && cam.Latitude >= r.minLat
}

func (r *thuDucRule) Zone() string {
	return ZoneThuDuc
}

// 6. Western Gateway (Binh Tan - Binh Chanh) Rule
type binhTanRule struct {
	maxLon   float64
	keywords []string
}

func (r *binhTanRule) Match(cam Camera) bool {
	if cam.Longitude > 0 && cam.Longitude < r.maxLon {
		return true
	}
	text := cameraSearchText(cam)
	for _, kw := range r.keywords {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}

func (r *binhTanRule) Zone() string {
	return ZoneBinhTan
}

// 7. Northwestern Gateway (District 12 - Hoc Mon - Cu Chi) Rule
type q12HocMonCuChiRule struct {
	minLat   float64
	maxLon   float64
	keywords []string
}

func (r *q12HocMonCuChiRule) Match(cam Camera) bool {
	if cam.Latitude >= r.minLat && cam.Longitude > 0 && cam.Longitude < r.maxLon {
		return true
	}
	text := cameraSearchText(cam)
	for _, kw := range r.keywords {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}

func (r *q12HocMonCuChiRule) Zone() string {
	return ZoneQ12HocMonCuChi
}
