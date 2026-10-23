package harvester

import (
	"errors"
	"fmt"
	"time"
)

// GroundTruthManifest represents a JSON label manifest adhering to tools/eval/label_schema.json (Draft-07).
type GroundTruthManifest struct {
	FrameID     string        `json:"frame_id"`
	CameraID    string        `json:"camera_id,omitempty"`
	CameraName  string        `json:"camera_name,omitempty"`
	ImagePath   string        `json:"image_path"`
	Annotator   string        `json:"annotator"`
	AnnotatedAt string        `json:"annotated_at"`
	Notes       string        `json:"notes,omitempty"`
	Labels      LabelsPayload `json:"labels"`
}

// LabelsPayload represents the structured annotations payload inside GroundTruthManifest.
type LabelsPayload struct {
	Traffic               string   `json:"traffic"`
	CarriagewayDirection  string   `json:"carriageway_direction,omitempty"`
	TrafficDirA           string   `json:"traffic_dir_a,omitempty"`
	TrafficDirB           string   `json:"traffic_dir_b,omitempty"`
	IsWaitingRedLight     bool     `json:"is_waiting_red_light,omitempty"`
	QueueReason           string   `json:"queue_reason,omitempty"`
	Flood                 string   `json:"flood"`
	WaterlineDepth        string   `json:"waterline_depth,omitempty"`
	FloodSubmersionAnchor string   `json:"flood_submersion_anchor,omitempty"`
	TimeOfDay             string   `json:"time_of_day,omitempty"`
	Weather               string   `json:"weather,omitempty"`
	QualityTags           []string `json:"quality_tags,omitempty"`
}

// Allowed enum sets conforming to tools/eval/label_schema.json.
var (
	validTraffic = map[string]struct{}{
		"CLEAR":     {},
		"MODERATE":  {},
		"HEAVY":     {},
		"CONGESTED": {},
	}

	validTrafficDirB = map[string]struct{}{
		"CLEAR":          {},
		"MODERATE":       {},
		"HEAVY":          {},
		"CONGESTED":      {},
		"NOT_APPLICABLE": {},
	}

	validFlood = map[string]struct{}{
		"NONE":    {},
		"SHALLOW": {},
		"DEEP":    {},
		"UNKNOWN": {},
	}

	validCarriageway = map[string]struct{}{
		"inbound":       {},
		"outbound":      {},
		"bidirectional": {},
	}

	validQueueReason = map[string]struct{}{
		"free_flow":     {},
		"traffic_light": {},
		"bottleneck":    {},
		"flood_barrier": {},
		"accident":      {},
	}

	validWaterlineDepth = map[string]struct{}{
		"DRY":                    {},
		"WET_SURFACE":            {},
		"TIRE_QUARTER_SUBMERGED": {},
		"TIRE_HALF_SUBMERGED":    {},
		"IMPASSABLE":             {},
		"UNKNOWN":                {},
	}

	validFloodSubmersionAnchor = map[string]struct{}{
		"dry":                     {},
		"wet_asphalt_no_standing": {},
		"tire_base_under_10cm":    {},
		"exhaust_hub_over_15cm":   {},
		"unknown":                 {},
	}

	validTimeOfDay = map[string]struct{}{
		"day":       {},
		"night":     {},
		"dawn_dusk": {},
	}

	validWeather = map[string]struct{}{
		"dry":        {},
		"rain":       {},
		"heavy_rain": {},
	}

	validQualityTags = map[string]struct{}{
		"rain_on_lens": {},
		"night_glare":  {},
		"occluded":     {},
		"signal_noise": {},
	}
)

// ValidateDraft07 validates that m conforms strictly to tools/eval/label_schema.json (Draft-07).
func ValidateDraft07(m *GroundTruthManifest) error {
	if m == nil {
		return errors.New("manifest is nil")
	}

	// Required root fields
	if m.FrameID == "" {
		return errors.New("missing required field 'frame_id'")
	}
	if m.ImagePath == "" {
		return errors.New("missing required field 'image_path'")
	}
	if m.Annotator == "" {
		return errors.New("missing required field 'annotator'")
	}
	if m.Annotator != "auto_harvester" {
		return fmt.Errorf("invalid annotator %q: auto harvester must strictly tag 'auto_harvester'", m.Annotator)
	}
	if m.AnnotatedAt == "" {
		return errors.New("missing required field 'annotated_at'")
	}
	if _, err := time.Parse(time.RFC3339, m.AnnotatedAt); err != nil {
		return fmt.Errorf("invalid 'annotated_at' timestamp %q (must be RFC3339): %w", m.AnnotatedAt, err)
	}

	// Required labels fields
	if m.Labels.Traffic == "" {
		return errors.New("missing required field 'labels.traffic'")
	}
	if _, ok := validTraffic[m.Labels.Traffic]; !ok {
		return fmt.Errorf("invalid 'labels.traffic' enum value %q", m.Labels.Traffic)
	}

	if m.Labels.Flood == "" {
		return errors.New("missing required field 'labels.flood'")
	}
	if _, ok := validFlood[m.Labels.Flood]; !ok {
		return fmt.Errorf("invalid 'labels.flood' enum value %q", m.Labels.Flood)
	}

	// Optional labels enum fields
	if m.Labels.CarriagewayDirection != "" {
		if _, ok := validCarriageway[m.Labels.CarriagewayDirection]; !ok {
			return fmt.Errorf("invalid 'labels.carriageway_direction' enum value %q", m.Labels.CarriagewayDirection)
		}
	}

	if m.Labels.TrafficDirA != "" {
		if _, ok := validTraffic[m.Labels.TrafficDirA]; !ok {
			return fmt.Errorf("invalid 'labels.traffic_dir_a' enum value %q", m.Labels.TrafficDirA)
		}
	}

	if m.Labels.TrafficDirB != "" {
		if _, ok := validTrafficDirB[m.Labels.TrafficDirB]; !ok {
			return fmt.Errorf("invalid 'labels.traffic_dir_b' enum value %q", m.Labels.TrafficDirB)
		}
	}

	if m.Labels.QueueReason != "" {
		if _, ok := validQueueReason[m.Labels.QueueReason]; !ok {
			return fmt.Errorf("invalid 'labels.queue_reason' enum value %q", m.Labels.QueueReason)
		}
	}

	if m.Labels.WaterlineDepth != "" {
		if _, ok := validWaterlineDepth[m.Labels.WaterlineDepth]; !ok {
			return fmt.Errorf("invalid 'labels.waterline_depth' enum value %q", m.Labels.WaterlineDepth)
		}
	}

	if m.Labels.FloodSubmersionAnchor != "" {
		if _, ok := validFloodSubmersionAnchor[m.Labels.FloodSubmersionAnchor]; !ok {
			return fmt.Errorf("invalid 'labels.flood_submersion_anchor' enum value %q", m.Labels.FloodSubmersionAnchor)
		}
	}

	if m.Labels.TimeOfDay != "" {
		if _, ok := validTimeOfDay[m.Labels.TimeOfDay]; !ok {
			return fmt.Errorf("invalid 'labels.time_of_day' enum value %q", m.Labels.TimeOfDay)
		}
	}

	if m.Labels.Weather != "" {
		if _, ok := validWeather[m.Labels.Weather]; !ok {
			return fmt.Errorf("invalid 'labels.weather' enum value %q", m.Labels.Weather)
		}
	}

	for _, tag := range m.Labels.QualityTags {
		if _, ok := validQualityTags[tag]; !ok {
			return fmt.Errorf("invalid 'labels.quality_tags' item %q", tag)
		}
	}

	return nil
}
