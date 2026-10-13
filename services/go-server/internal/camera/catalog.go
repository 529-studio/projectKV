package camera

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Phuong represents a structured administrative ward/commune/town.
type Phuong struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

// Reader defines query capabilities on the camera catalog.
// (Interface Segregation Principle: callers only depend on what they read)
type Reader interface {
	All() []Camera
	ByID(id string) (*Camera, bool)
	Zones() []string
	Phuongs() []Phuong
	Filter(zone, corridor string) []Camera
	FilterByPhuong(phuongName string) []Camera
}

// Camera represents a single traffic camera entity.
type Camera struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	DisplayName  string   `json:"displayName"`
	Status       string   `json:"status"`
	IsOnline     bool     `json:"isOnline"`
	Latitude     float64  `json:"latitude"`
	Longitude    float64  `json:"longitude"`
	SnapshotURL  string   `json:"snapshotUrl"`
	Zone         string   `json:"zone,omitempty"`
	DuplicateOf  string   `json:"duplicateOf,omitempty"`
	IsGhost      bool     `json:"isGhost,omitempty"`
	PhuongID     *string  `json:"phuong_id,omitempty"`
	PhuongType   string   `json:"phuong_type,omitempty"`
	PhuongName   string   `json:"phuong_name,omitempty"`
	RoadType     string   `json:"road_type,omitempty"`
	RoadName     string   `json:"road_name,omitempty"`
	H3Res7       string   `json:"h3_res7,omitempty"`
	H3Res9       string   `json:"h3_res9,omitempty"`
	RoadKey      string   `json:"road_key,omitempty"`
	RoadKeys     []string `json:"road_keys,omitempty"`
	BaseCapacity float64  `json:"base_capacity,omitempty"`
}

// KnownDuplicateStreams maps secondary camera IDs to their primary canonical camera ID.
var KnownDuplicateStreams = map[string]string{
	// QL1 - BQB Trước UBND Q12 (hướng đi An Sương) -> QL1 - Cầu vượt Tân Thới Hiệp 1
	"595dd9ac3dcfc400106f28b4": "595ddc123dcfc400106f28ba",
	// Huỳnh Tấn Phát - Cầu Tân Thuận 1 (TTH 309) -> Huỳnh Tấn Phát - cầu Tân Thuận 1 (TTH 323.2)
	"665861d864f11e00173c73a9": "662a8e121afb9c00172d2a3f",
	// Thanh Niên - gần chùa Liên Hoa (TTH 454) -> Thanh Niên - Phạm Văn Hai (TTH 453)
	"68188e466dfb4b0018f902f2": "68188d586dfb4b0018f9018d",
}

// Static camera liveness (isOnline/status) is sourced exclusively from the
// JSON catalog loaded via LoadCatalog, which is periodically re-synchronized
// from live upstream probes (tools/crawler/sync_catalog_from_audit.js).
//
// Runtime placeholder detection (proxycache GHOST_CAMERA 503) is handled
// dynamically by the proxy layer and surfaced to clients in real time via the
// web client's markCameraGhost; it is intentionally NOT hardcoded here to avoid
// a second, drifting source of truth.

// Catalog holds an in-memory repository of traffic cameras with fast index lookups.
type Catalog struct {
	list    []Camera
	dict    map[string]*Camera
	zones   []string
	phuongs []Phuong
}

// Ensure Catalog implements Reader.
var _ Reader = (*Catalog)(nil)

// NewCatalog constructs a Catalog using the default zone classifier.
func NewCatalog(cameras []Camera) *Catalog {
	return NewCatalogWithClassifier(cameras, NewDefaultZoneClassifier())
}

// NewCatalogWithClassifier constructs a Catalog with an injected ZoneClassifier.
// (Dependency Inversion Principle: Catalog depends on ZoneClassifier abstraction)
func NewCatalogWithClassifier(cameras []Camera, classifier ZoneClassifier) *Catalog {
	if classifier == nil {
		classifier = NewDefaultZoneClassifier()
	}

	c := &Catalog{
		list: make([]Camera, len(cameras)),
		dict: make(map[string]*Camera, len(cameras)),
	}

	copy(c.list, cameras)

	zoneSet := make(map[string]struct{})
	phuongMap := make(map[string]string)
	for i := range c.list {
		cam := &c.list[i]
		if cam.Zone == "" {
			cam.Zone = classifier.Classify(*cam)
		}
		if primaryID, isDup := KnownDuplicateStreams[cam.ID]; isDup {
			cam.DuplicateOf = primaryID
		}
		c.dict[cam.ID] = cam
		if cam.Zone != "" {
			zoneSet[cam.Zone] = struct{}{}
		}
		if cam.PhuongName != "" {
			pType := cam.PhuongType
			if pType == "" {
				pType = "Phường"
			}
			phuongMap[cam.PhuongName] = pType
		}
	}

	c.zones = make([]string, 0, len(zoneSet))
	for z := range zoneSet {
		c.zones = append(c.zones, z)
	}
	sort.Strings(c.zones)

	c.phuongs = make([]Phuong, 0, len(phuongMap))
	for name, pType := range phuongMap {
		c.phuongs = append(c.phuongs, Phuong{Type: pType, Name: name})
	}
	sort.Slice(c.phuongs, func(i, j int) bool {
		return c.phuongs[i].Name < c.phuongs[j].Name
	})

	return c
}

// LoadCatalog reads camera JSON data from disk using the default zone classifier.
func LoadCatalog(path string) (*Catalog, error) {
	return LoadCatalogWithClassifier(path, NewDefaultZoneClassifier())
}

// LoadCatalogWithClassifier reads camera JSON data from disk and uses the provided classifier.
func LoadCatalogWithClassifier(path string, classifier ZoneClassifier) (*Catalog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		// Graceful fallback: if enriched data file is missing, try falling back to cameras_all.json
		if fallback := findFallbackCatalogPath(path); fallback != "" {
			slog.Warn("camera: catalog file missing, falling back to base cameras_all.json",
				"requested", path, "fallback", fallback)
			data, err = os.ReadFile(fallback)
		}
		if err != nil {
			return nil, fmt.Errorf("read file: %w", err)
		}
	}

	var list []Camera
	if err := json.Unmarshal(data, &list); err == nil {
		return NewCatalogWithClassifier(list, classifier), nil
	}

	// Try object envelope {"cameras": [...]}
	var envelope struct {
		Cameras []Camera `json:"cameras"`
	}
	if err := json.Unmarshal(data, &envelope); err == nil && len(envelope.Cameras) > 0 {
		return NewCatalogWithClassifier(envelope.Cameras, classifier), nil
	}

	// Try map[string]Camera
	var mapData map[string]Camera
	if err := json.Unmarshal(data, &mapData); err == nil && len(mapData) > 0 {
		list = make([]Camera, 0, len(mapData))
		for _, v := range mapData {
			list = append(list, v)
		}
		return NewCatalogWithClassifier(list, classifier), nil
	}

	return nil, fmt.Errorf("unmarshal json: %s", ErrInvalidCatalogJSON)
}

// All returns a slice of all cameras in the catalog.
func (c *Catalog) All() []Camera {
	res := make([]Camera, len(c.list))
	copy(res, c.list)
	return res
}

// ByID returns a camera pointer and a boolean flag indicating if found.
func (c *Catalog) ByID(id string) (*Camera, bool) {
	cam, ok := c.dict[id]
	return cam, ok
}

// Zones returns a sorted slice of unique zone identifiers.
func (c *Catalog) Zones() []string {
	res := make([]string, len(c.zones))
	copy(res, c.zones)
	return res
}

// Filter returns cameras matching optional zone and corridor parameters.
// Zone matching is case-insensitive.
// Corridor matching checks if DisplayName or Title contains the corridor keyword (case-insensitive).
func (c *Catalog) Filter(zone, corridor string) []Camera {
	res := make([]Camera, 0, len(c.list))
	zone = strings.TrimSpace(zone)
	corridor = strings.ToLower(strings.TrimSpace(corridor))

	for _, cam := range c.list {
		if zone != "" && !strings.EqualFold(cam.Zone, zone) {
			continue
		}
		if corridor != "" {
			name := strings.ToLower(cam.DisplayName)
			title := strings.ToLower(cam.Title)
			if !strings.Contains(name, corridor) && !strings.Contains(title, corridor) {
				continue
			}
		}
		res = append(res, cam)
	}
	return res
}

// Phuongs returns a sorted slice of distinct Phường structured objects.
func (c *Catalog) Phuongs() []Phuong {
	res := make([]Phuong, len(c.phuongs))
	copy(res, c.phuongs)
	return res
}

// FilterByPhuong returns cameras belonging to the specified Phường name (case-insensitive).
// Handles both prefix-less name (e.g. "Bến Nghé") and prefixed name (e.g. "Phường Bến Nghé").
func (c *Catalog) FilterByPhuong(phuongName string) []Camera {
	res := make([]Camera, 0, len(c.list))
	phuongName = strings.TrimSpace(phuongName)
	if phuongName == "" {
		return res
	}

	target := phuongName
	lowerTarget := strings.ToLower(target)
	for _, prefix := range []string{"phường ", "xã ", "thị trấn "} {
		if strings.HasPrefix(lowerTarget, prefix) {
			target = strings.TrimSpace(target[len(prefix):])
			break
		}
	}

	for _, cam := range c.list {
		if cam.PhuongName != "" {
			if strings.EqualFold(cam.PhuongName, phuongName) || strings.EqualFold(cam.PhuongName, target) {
				res = append(res, cam)
			}
		}
	}
	return res
}

// findFallbackCatalogPath searches for the base cameras_all.json dataset if the requested catalog file is missing.
func findFallbackCatalogPath(requestedPath string) string {
	baseName := filepath.Base(requestedPath)
	if baseName == "cameras_all.json" {
		return ""
	}

	candidates := []string{
		filepath.Join(filepath.Dir(requestedPath), "cameras_all.json"),
		"cameras_all.json",
		filepath.Join("..", "cameras_all.json"),
		filepath.Join("..", "..", "cameras_all.json"),
	}

	for _, candidate := range candidates {
		if fi, err := os.Stat(candidate); err == nil && !fi.IsDir() {
			return candidate
		}
	}
	return ""
}
