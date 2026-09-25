package camera

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// Reader defines query capabilities on the camera catalog.
// (Interface Segregation Principle: callers only depend on what they read)
type Reader interface {
	All() []Camera
	ByID(id string) (*Camera, bool)
	Zones() []string
	Filter(zone, corridor string) []Camera
}

// Camera represents a single traffic camera entity.
type Camera struct {
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	DisplayName string  `json:"displayName"`
	Status      string  `json:"status"`
	IsOnline    bool    `json:"isOnline"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
	SnapshotURL string  `json:"snapshotUrl"`
	Zone        string  `json:"zone,omitempty"`
	DuplicateOf string  `json:"duplicateOf,omitempty"`
	IsGhost     bool    `json:"isGhost,omitempty"`
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

// KnownGhostCameras is a set of camera IDs confirmed to return "IMAGE NOT AVAILABLE" placeholders.
var KnownGhostCameras = map[string]struct{}{
	"595ddbbf3dcfc400106f28b8": {}, // QL1 - Cầu vượt Tân Thới Hiệp 3
	"5deb576d1dc17d7c5515ad02": {}, // Điện Biên Phủ - Pasteur
	"595f86c43dcfc400106f28ea": {}, // QL1 - Nguyễn Văn Quá 1
	"595f874d3dcfc400106f28ec": {}, // QL1 - Nguyễn Văn Quá 2
	"585b2b1ec3f96200127dc505": {}, // Nguyễn Văn Linh - Huỳnh Tấn Phát 1
	"585b2baec3f96200127dc508": {}, // Nguyễn Văn Linh - Huỳnh Tấn Phát 2
	"585b2980c3f96200127dc502": {}, // Huỳnh Tấn Phát - Lưu Trọng Lư 2
	"583f969161cfea0012cf68f7": {}, // Nguyễn Thị Định - Đường D
	"67e217c43215f90017863bc8": {}, // Nguyễn Thị Định - Đường C (20-1)
	"67e217ea3215f90017863bf1": {}, // Nguyễn Thị Định - Đường C (20-2)
	"58b571c817139d0010f35d5d": {}, // Nguyễn Văn Trỗi - Trần Huy Liệu
	"5a605f828576340017d06608": {}, // Nguyễn Thái Sơn - Nguyễn Văn Nghi 1
	"5a607e298576340017d0666b": {}, // IDICO-QL1 04
	"595dc70d3dcfc400106f289a": {}, // QL1 - Ngã 4 Ga
	"595dd6093dcfc400106f28ad": {}, // QL1 - Cầu Bến Cát 2
	"68188fe36dfb4b0018f90520": {}, // Trần Văn Giàu - đường Kênh 1
}

// Catalog holds an in-memory repository of traffic cameras with fast index lookups.
type Catalog struct {
	list  []Camera
	dict  map[string]*Camera
	zones []string
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
	for i := range c.list {
		cam := &c.list[i]
		if cam.Zone == "" {
			cam.Zone = classifier.Classify(*cam)
		}
		if primaryID, isDup := KnownDuplicateStreams[cam.ID]; isDup {
			cam.DuplicateOf = primaryID
		}
		if _, isGhost := KnownGhostCameras[cam.ID]; isGhost {
			cam.IsGhost = true
			cam.IsOnline = false
			cam.Status = "DOWN"
		}
		c.dict[cam.ID] = cam
		if cam.Zone != "" {
			zoneSet[cam.Zone] = struct{}{}
		}
	}

	c.zones = make([]string, 0, len(zoneSet))
	for z := range zoneSet {
		c.zones = append(c.zones, z)
	}
	sort.Strings(c.zones)

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
		return nil, fmt.Errorf("read file: %w", err)
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
